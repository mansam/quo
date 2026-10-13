package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/kvo/pkg/cluster"
)

// The VirtualMachine every test captures.
const (
	testNamespace = "support-cases"
	testName      = "db-01"
)

// Identity values a bundle must carry through untouched. Any of them could be
// the thing causing the problem a bundle was captured for.
const (
	testMAC     = "52:54:00:3a:7b:1c"
	testUUID    = "5d307ca9-b3ef-428c-8861-06e72d69f223"
	testSerial  = "SN-00042"
	testChassis = "CHS-00042"
)

// What the evidence every fixture cluster offers says about the source
// environment.
const (
	testKubeVirtNamespace = "kubevirt"
	testKubeVirtName      = "kubevirt"
	testFeatureGate       = "HotplugVolumes"
	// Resolved values a stopped VirtualMachine does not carry and only
	// expand-spec can supply.
	testMachineType   = "pc-q35-rhel9.4.0"
	testExpandedCores = int64(4)
)

// Annotation values that a general YAML library would be liable to reinterpret
// as something other than a string.
const (
	versionAnnotation = "example.com/schema-version"
	versionValue      = "1.30"
	sealedAnnotation  = "example.com/sealed"
	sealedValue       = "no"
)

// fakeCluster is quo's one fake Cluster: a plain struct holding the objects a
// test wants a cluster to have. Nothing in it touches a network, and disks are
// served from memory so that no test needs an HTTP server.
type fakeCluster struct {
	vm     *unstructured.Unstructured
	info   cluster.Info
	export *fakeExport
	// objects is everything the cluster holds besides the VirtualMachine.
	// Anything asked for and not held is reported as not found, which is what
	// a reference to a deleted object looks like.
	objects map[cluster.ObjectRef]*unstructured.Unstructured
	// errByRef is what the cluster reports in place of an object, so a test
	// can make one object forbidden or its kind absent while the rest of the
	// closure reads normally.
	errByRef map[cluster.ObjectRef]error
	// requested records every reference the capture asked for, in order, so a
	// test can see what the traversal looked for as well as what it found.
	requested []cluster.ObjectRef
	// kubevirt is KubeVirt's own configuration, and expanded is what the
	// cluster resolves the VirtualMachine to. Neither is addressed by
	// reference, so each is held on its own.
	kubevirt  *unstructured.Unstructured
	expanded  *unstructured.Unstructured
	vmErr     error
	infoErr   error
	exportErr error
	kvErr     error
	expandErr error
}

var _ cluster.Cluster = &fakeCluster{}

func (r *fakeCluster) GetVirtualMachine(ctx context.Context, namespace, name string) (vm *unstructured.Unstructured, err error) {
	if r.vmErr != nil {
		err = r.vmErr
		return
	}
	if r.vm == nil {
		err = fmt.Errorf("virtualmachines.kubevirt.io %q not found", name)
		return
	}
	vm = r.vm.DeepCopy()
	return
}

func (r *fakeCluster) GetObject(ctx context.Context, ref cluster.ObjectRef) (object *unstructured.Unstructured, err error) {
	r.requested = append(r.requested, ref)
	err = ctx.Err()
	if err != nil {
		// What a real client does once the capture has been cancelled or has
		// run out of time.
		err = fmt.Errorf("get %s %q: %w", ref.Kind, ref.Name, err)
		return
	}
	refused, found := r.errByRef[ref]
	if found {
		err = refused
		return
	}
	held, found := r.objects[ref]
	if !found {
		err = fmt.Errorf("%s %q: %w", ref.Kind, ref.Name, cluster.ErrNotFound)
		return
	}
	object = held.DeepCopy()
	return
}

func (r *fakeCluster) GetKubeVirt(ctx context.Context) (kv *unstructured.Unstructured, err error) {
	err = ctx.Err()
	if err != nil {
		err = fmt.Errorf("list %s: %w", cluster.KindKubeVirt, err)
		return
	}
	if r.kvErr != nil {
		err = r.kvErr
		return
	}
	if r.kubevirt == nil {
		err = fmt.Errorf("%s: %w", cluster.KindKubeVirt, cluster.ErrNotFound)
		return
	}
	kv = r.kubevirt.DeepCopy()
	return
}

func (r *fakeCluster) ExpandSpec(
	ctx context.Context,
	namespace, name string) (expanded *unstructured.Unstructured, err error) {
	//
	err = ctx.Err()
	if err != nil {
		err = fmt.Errorf("get %s %q: %w", cluster.KindVirtualMachine, name, err)
		return
	}
	if r.expandErr != nil {
		err = r.expandErr
		return
	}
	if r.expanded == nil {
		err = fmt.Errorf("%s %q: %w", cluster.KindVirtualMachine, name, cluster.ErrNotFound)
		return
	}
	expanded = r.expanded.DeepCopy()
	return
}

func (r *fakeCluster) GetInfo(ctx context.Context) (info cluster.Info, err error) {
	if r.infoErr != nil {
		err = r.infoErr
		return
	}
	info = r.info
	return
}

func (r *fakeCluster) ExportDisks(ctx context.Context, namespace, name string) (export cluster.DiskExport, err error) {
	if r.exportErr != nil {
		err = r.exportErr
		return
	}
	r.export.created++
	export = r.export
	return
}

// fakeExport serves canned disk bytes in place of a VirtualMachineExport, and
// records what the capture did with it.
type fakeExport struct {
	offered []cluster.ExportedDisk
	content map[string][]byte
	openErr error
	// scratch is where the capture stages disks. The export counts what is in
	// it as each transfer starts, which is how a test sees that disks are
	// staged one at a time.
	scratch string
	staged  int
	created int
	closed  int
	opened  []string
}

var _ cluster.DiskExport = &fakeExport{}

func (r *fakeExport) Disks() (disks []cluster.ExportedDisk) {
	disks = r.offered
	return
}

func (r *fakeExport) OpenDisk(ctx context.Context, claim string) (stream io.ReadCloser, err error) {
	r.opened = append(r.opened, claim)
	r.observe()
	if r.openErr != nil {
		err = r.openErr
		return
	}
	content, found := r.content[claim]
	if !found {
		err = fmt.Errorf("no disk for claim %q", claim)
		return
	}
	stream = io.NopCloser(bytes.NewReader(content))
	return
}

func (r *fakeExport) Close() (err error) {
	r.closed++
	return
}

// observe records the most disks ever staged at once.
func (r *fakeExport) observe() {
	if r.scratch == "" {
		return
	}
	entries, err := os.ReadDir(r.scratch)
	if err != nil {
		return
	}
	if len(entries) > r.staged {
		r.staged = len(entries)
	}
}

// apiVersionByKind is the apiVersion the fake stamps on each kind it serves,
// so that a carried object reads as the Kubernetes object it stands for.
var apiVersionByKind = map[string]string{
	cluster.KindConfigMap:                         "v1",
	cluster.KindControllerRevision:                "apps/v1",
	cluster.KindDataVolume:                        "cdi.kubevirt.io/v1beta1",
	cluster.KindKubeVirt:                          "kubevirt.io/v1",
	cluster.KindNetworkAttachmentDefinition:       "k8s.cni.cncf.io/v1",
	cluster.KindPersistentVolume:                  "v1",
	cluster.KindPersistentVolumeClaim:             "v1",
	cluster.KindSecret:                            "v1",
	cluster.KindServiceAccount:                    "v1",
	cluster.KindStorageClass:                      "storage.k8s.io/v1",
	cluster.KindStorageProfile:                    "cdi.kubevirt.io/v1beta1",
	cluster.KindVirtualMachineClusterInstancetype: "instancetype.kubevirt.io/v1beta1",
	cluster.KindVirtualMachineClusterPreference:   "instancetype.kubevirt.io/v1beta1",
	cluster.KindVirtualMachineInstancetype:        "instancetype.kubevirt.io/v1beta1",
	cluster.KindVirtualMachinePreference:          "instancetype.kubevirt.io/v1beta1",
}

// ref names an object in the VirtualMachine's own namespace.
func ref(kind, name string) (r cluster.ObjectRef) {
	r = cluster.ObjectRef{Kind: kind, Namespace: testNamespace, Name: name}
	return
}

// refIn names an object in some other namespace.
func refIn(kind, namespace, name string) (r cluster.ObjectRef) {
	r = cluster.ObjectRef{Kind: kind, Namespace: namespace, Name: name}
	return
}

// clusterRef names a cluster-scoped object, which has no namespace.
func clusterRef(kind, name string) (r cluster.ObjectRef) {
	r = cluster.ObjectRef{Kind: kind, Name: name}
	return
}

// hold puts an object in the fake cluster, carrying whatever fields beyond
// its kind and name the test cares about.
func (r *fakeCluster) hold(object cluster.ObjectRef, fields map[string]any) {
	metadata := map[string]any{"name": object.Name}
	if object.Namespace != "" {
		metadata["namespace"] = object.Namespace
	}
	held := map[string]any{
		"apiVersion": apiVersionByKind[object.Kind],
		"kind":       object.Kind,
		"metadata":   metadata,
	}
	for key, value := range fields {
		held[key] = value
	}
	if r.objects == nil {
		r.objects = map[cluster.ObjectRef]*unstructured.Unstructured{}
	}
	r.objects[object] = &unstructured.Unstructured{Object: held}
}

// refuse makes the cluster report err in place of an object, the way a real
// cluster reports a read the caller is not allowed to make.
func (r *fakeCluster) refuse(object cluster.ObjectRef, err error) {
	if r.errByRef == nil {
		r.errByRef = map[cluster.ObjectRef]error{}
	}
	r.errByRef[object] = err
}

// volume is a volume of the fixture VirtualMachine and the claim behind it.
type volume struct {
	name  string
	claim string
}

// disk is a disk the fake export offers.
type disk struct {
	claim   string
	format  cluster.DiskFormat
	content []byte
}

// testOptions names the VirtualMachine the fixtures build.
func testOptions() (opt Options) {
	opt = Options{Namespace: testNamespace, Name: testName}
	return
}

// stoppedCluster builds a cluster holding one stopped VirtualMachine.
func stoppedCluster(t *testing.T) (c *fakeCluster) {
	t.Helper()
	c = clusterWithStatus(t, StatusStopped)
	return
}

// clusterWithStatus builds a cluster holding one VirtualMachine reporting the
// given printable status. An empty status means it reports none at all.
//
// The cluster has KubeVirt installed and serves expand-spec, because every
// cluster quo captures from does, and a fixture missing either would make
// every bundle look as though a capture had gone wrong.
func clusterWithStatus(t *testing.T, status string) (c *fakeCluster) {
	t.Helper()
	c = &fakeCluster{
		vm: virtualMachine(t, status),
		info: cluster.Info{
			SourceCluster:     "https://api.source.example.com:6443",
			KubeVirtVersion:   "v1.3.1",
			KubernetesVersion: "v1.31.4",
		},
		kubevirt: kubeVirtConfiguration(t),
		expanded: expandedVirtualMachine(t, status),
		export:   &fakeExport{content: map[string][]byte{}},
	}
	return
}

// clusterWithDisks builds a cluster holding one stopped VirtualMachine with
// the given volumes, whose export offers the given disks.
func clusterWithDisks(t *testing.T, volumes []volume, disks []disk) (c *fakeCluster) {
	t.Helper()
	c = stoppedCluster(t)
	attach(t, c.vm, volumes)
	for i := range disks {
		format := disks[i].format
		if format == "" {
			format = cluster.FormatGzip
		}
		c.export.offered = append(c.export.offered, cluster.ExportedDisk{
			Claim:  disks[i].claim,
			Format: format,
		})
		c.export.content[disks[i].claim] = disks[i].content
	}
	return
}

// attach puts volumes on the fixture VirtualMachine.
func attach(t *testing.T, vm *unstructured.Unstructured, volumes []volume) {
	t.Helper()
	var attached []any
	for _, v := range volumes {
		attached = append(attached, map[string]any{
			"name": v.name,
			"persistentVolumeClaim": map[string]any{
				"claimName": v.claim,
			},
		})
	}
	err := unstructured.SetNestedSlice(vm.Object, attached, "spec", "template", "spec", "volumes")
	if err != nil {
		t.Fatalf("attach volumes: %v", err)
	}
}

// offeredContent is what the fake export serves for a disk a test gave no
// content of its own. quo never looks inside a disk.
var offeredContent = []byte("\x1f\x8b\x08 a disk")

// offer adds a disk to what the fake export serves, so that a VirtualMachine
// referencing a claim can be captured without the test caring about disks.
func (r *fakeCluster) offer(claim string) {
	r.export.offered = append(r.export.offered, cluster.ExportedDisk{
		Claim:  claim,
		Format: cluster.FormatGzip,
	})
	r.export.content[claim] = offeredContent
}

// setField sets a field on the fixture VirtualMachine.
func setField(t *testing.T, vm *unstructured.Unstructured, value any, keys ...string) {
	t.Helper()
	err := unstructured.SetNestedField(vm.Object, value, keys...)
	if err != nil {
		t.Fatalf("set %s: %v", strings.Join(keys, "."), err)
	}
}

// backVolume puts one volume on the fixture VirtualMachine, backed by
// whatever the caller says backs it.
func backVolume(t *testing.T, vm *unstructured.Unstructured, name string, backing map[string]any) {
	t.Helper()
	volume := map[string]any{"name": name}
	for key, value := range backing {
		volume[key] = value
	}
	err := unstructured.SetNestedSlice(
		vm.Object,
		[]any{volume},
		"spec", "template", "spec", "volumes")
	if err != nil {
		t.Fatalf("attach volume %q: %v", name, err)
	}
}

// kubeVirtConfiguration builds KubeVirt's own configuration. Its feature
// gates are what a support engineer reads it for, and its namespace and name
// are an installation's rather than something quo knows in advance.
func kubeVirtConfiguration(t *testing.T) (kv *unstructured.Unstructured) {
	t.Helper()
	kv = &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersionByKind[cluster.KindKubeVirt],
			"kind":       cluster.KindKubeVirt,
			"metadata": map[string]any{
				"name":      testKubeVirtName,
				"namespace": testKubeVirtNamespace,
			},
			"spec": map[string]any{
				"configuration": map[string]any{
					"developerConfiguration": map[string]any{
						"featureGates": []any{testFeatureGate},
					},
				},
			},
		},
	}
	return
}

// expandedVirtualMachine builds the form the source cluster resolves the
// fixture VirtualMachine to. It carries the machine type and CPU topology a
// stopped VirtualMachine does not have, which is the whole reason expand-spec
// is worth calling.
func expandedVirtualMachine(t *testing.T, status string) (expanded *unstructured.Unstructured) {
	t.Helper()
	expanded = virtualMachine(t, status)
	setField(t, expanded, testMachineType, "spec", "template", "spec", "domain", "machine", "type")
	setField(t, expanded, testExpandedCores, "spec", "template", "spec", "domain", "cpu", "cores")
	return
}

// virtualMachine builds a VirtualMachine carrying the identity values and the
// awkward annotation values a capture must not disturb.
func virtualMachine(t *testing.T, status string) (vm *unstructured.Unstructured) {
	t.Helper()
	vm = &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]any{
				"name":      testName,
				"namespace": testNamespace,
				"annotations": map[string]any{
					versionAnnotation: versionValue,
					sealedAnnotation:  sealedValue,
				},
			},
			"spec": map[string]any{
				"running": false,
				"template": map[string]any{
					"spec": map[string]any{
						"domain": map[string]any{
							"firmware": map[string]any{
								"uuid":   testUUID,
								"serial": testSerial,
							},
							"chassis": map[string]any{
								"serial": testChassis,
							},
							"devices": map[string]any{
								"interfaces": []any{
									map[string]any{
										"name":       "default",
										"macAddress": testMAC,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if status != "" {
		vm.Object["status"] = map[string]any{"printableStatus": status}
	}
	return
}
