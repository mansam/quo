package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
// test wants a cluster to have. Nothing in it touches a network, and the disk
// stream is served from memory so that no test needs an HTTP server.
type fakeCluster struct {
	vm      *unstructured.Unstructured
	info    cluster.Info
	disks   map[string][]byte
	vmErr   error
	infoErr error
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

func (r *fakeCluster) OpenDisk(ctx context.Context, namespace, claim string) (stream io.ReadCloser, err error) {
	data, found := r.disks[claim]
	if !found {
		err = fmt.Errorf("no disk for claim %q", claim)
		return
	}
	stream = io.NopCloser(bytes.NewReader(data))
	return
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
	}
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
