package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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
	vm        *unstructured.Unstructured
	info      cluster.Info
	export    *fakeExport
	vmErr     error
	infoErr   error
	exportErr error
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
func clusterWithStatus(t *testing.T, status string) (c *fakeCluster) {
	t.Helper()
	c = &fakeCluster{
		vm: virtualMachine(t, status),
		info: cluster.Info{
			SourceCluster:     "https://api.source.example.com:6443",
			KubeVirtVersion:   "v1.3.1",
			KubernetesVersion: "v1.31.4",
		},
		export: &fakeExport{content: map[string][]byte{}},
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
