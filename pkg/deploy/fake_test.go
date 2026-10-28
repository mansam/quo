package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// Where the fixture bundle came from, and where it is deployed to.
const (
	testSourceCluster      = "https://api.source.example.com:6443"
	testSourceNamespace    = "support-cases"
	testVirtualMachineName = "db-01"
	testDeployNamespace    = "diagnostics"
	testBundleFilename     = "db-01.quo"
)

// Identity values the expanded spec carries, any of which could be the
// thing causing the problem a bundle was captured for, and the values
// only the source cluster's resolution knows.
const (
	testMAC         = "52:54:00:3a:7b:1c"
	testUUID        = "5d307ca9-b3ef-428c-8861-06e72d69f223"
	testMachineType = "pc-q35-rhel9.4.0"
)

// testCapturedAt is when the fixture bundle was captured, fixed so a test
// can assert the annotation exactly.
var testCapturedAt = time.Date(2026, 3, 14, 15, 9, 26, 0, time.UTC)

// versionAnnotation is an annotation whose value a general YAML library
// would be liable to reinterpret as a number, so a test can see that a
// deploy carries what the source wrote through unchanged.
const (
	versionAnnotation = "example.com/schema-version"
	versionValue      = "1.30"
)

// testOptions deploys the bundle at path into the deploy namespace.
func testOptions(path string) (opt Options) {
	opt = Options{Bundle: path, Namespace: testDeployNamespace}
	return
}

// fakeTarget is quo's one fake Target: a plain struct holding what a test
// wants a cluster to have and recording everything created on it. Nothing
// in it touches a network.
type fakeTarget struct {
	// namespaces is the namespaces the cluster has. A deploy into any
	// other namespace is told the namespace is not there.
	namespaces map[string]bool
	// objects is everything the cluster holds besides namespaces, so a
	// test can take a name a deploy would want.
	objects map[cluster.ObjectRef]*unstructured.Unstructured
	// created records every object the deploy created, in order.
	created []*unstructured.Unstructured
	// createErr is what Create reports in place of creating, so a test
	// can make a create fail while the reads behave normally.
	createErr error
}

var _ cluster.Target = &fakeTarget{}

// target returns a fake target with the deploy namespace in place and
// nothing held.
func target() (t *fakeTarget) {
	t = &fakeTarget{
		namespaces: map[string]bool{testDeployNamespace: true},
		objects:    map[cluster.ObjectRef]*unstructured.Unstructured{},
	}
	return
}

// GetNamespace returns the named namespace, reporting a cancelled context
// the way a real client does once the deploy has been interrupted.
func (r *fakeTarget) GetNamespace(ctx context.Context, name string) (namespace *unstructured.Unstructured, err error) {
	err = ctx.Err()
	if err != nil {
		err = fmt.Errorf("get namespace %q: %w", name, err)
		return
	}
	if !r.namespaces[name] {
		err = fmt.Errorf("namespace %q: %w", name, cluster.ErrNotFound)
		return
	}
	namespace = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": name},
	}}
	return
}

// GetObject returns the object the reference names, reporting a cancelled
// context the way a real client does once the deploy has been interrupted.
func (r *fakeTarget) GetObject(ctx context.Context, ref cluster.ObjectRef) (object *unstructured.Unstructured, err error) {
	err = ctx.Err()
	if err != nil {
		err = fmt.Errorf("get %s %q: %w", ref.Kind, ref.Name, err)
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

// Create records the object, reporting a cancelled context the way a real
// client does once the deploy has been interrupted.
func (r *fakeTarget) Create(ctx context.Context, object *unstructured.Unstructured) (err error) {
	err = ctx.Err()
	if err != nil {
		err = fmt.Errorf("create %s %q: %w", object.GetKind(), object.GetName(), err)
		return
	}
	if r.createErr != nil {
		err = r.createErr
		return
	}
	r.created = append(r.created, object.DeepCopy())
	return
}

// hold puts an object on the fake target, the way a cluster holds work
// that was there before the deploy.
func (r *fakeTarget) hold(ref cluster.ObjectRef, object *unstructured.Unstructured) {
	r.objects[ref] = object
}

// writeFixtureBundle writes a bundle holding what a capture of the fixture
// VirtualMachine would hold: the declaration, the instancetype and
// preference it names, the revisions that pinned them, and the source
// cluster's expansion of the machine. It goes through the same writer a
// capture does, so the fixture is built by the same code that builds real
// bundles.
//
// expand tells it to leave the expansion out, which is what a capture
// whose expand-spec read failed produces.
func writeFixtureBundle(t *testing.T, expand bool) (path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), testBundleFilename)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()
	writer := bundle.NewWriter(file)
	collect(t, writer, declaration())
	collect(t, writer, instancetype())
	collect(t, writer, preference())
	collect(t, writer, controllerRevision())
	if expand {
		expanded, marshalErr := yaml.Marshal(expandedSpec().Object)
		if marshalErr != nil {
			t.Fatalf("marshal the expanded spec: %v", marshalErr)
		}
		writer.AddExpandedSpec(expanded)
	}
	err = writer.Open(fixtureProvenance())
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatalf("close bundle: %v", err)
	}
	return
}

// collect puts one object in the fixture bundle as a collected resource.
func collect(t *testing.T, writer *bundle.Writer, object *unstructured.Unstructured) {
	t.Helper()
	encoded, err := yaml.Marshal(object.Object)
	if err != nil {
		t.Fatalf("marshal %s %s: %v", object.GetKind(), object.GetName(), err)
	}
	err = writer.Collect(bundle.Resource{
		Kind:      object.GetKind(),
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}, encoded)
	if err != nil {
		t.Fatalf("collect %s %s: %v", object.GetKind(), object.GetName(), err)
	}
}

// fixtureProvenance is what the fixture bundle records about where it came
// from.
func fixtureProvenance() (provenance bundle.Provenance) {
	provenance = bundle.Provenance{
		SourceCluster:     testSourceCluster,
		Namespace:         testSourceNamespace,
		VirtualMachine:    testVirtualMachineName,
		CapturedAt:        testCapturedAt,
		KubeVirtVersion:   "v1.3.1",
		KubernetesVersion: "v1.31.4",
	}
	return
}

// declaration is the captured VirtualMachine object the bundle holds: the
// declaration of record, naming an instancetype and preference the source
// cluster resolved into the expanded spec. A deploy never applies it.
func declaration() (captured *unstructured.Unstructured) {
	captured = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]any{
			"name":      testVirtualMachineName,
			"namespace": testSourceNamespace,
		},
		"spec": map[string]any{
			"running": false,
			"instancetype": map[string]any{
				"kind": "VirtualMachineInstancetype",
				"name": "standard-large",
			},
			"preference": map[string]any{
				"kind": "VirtualMachinePreference",
				"name": "windows-defaults",
			},
			"template": map[string]any{
				"spec": map[string]any{
					"domain": map[string]any{
						"firmware": map[string]any{"uuid": testUUID},
						"devices": map[string]any{
							"interfaces": []any{
								map[string]any{"name": "default", "macAddress": testMAC},
							},
						},
					},
				},
			},
		},
	}}
	return
}

// expandedSpec builds the form a source cluster resolves the fixture
// VirtualMachine to: the identity values, annotations and metadata the
// cluster returned — including the resourceVersion, uid, creation
// timestamp, generation and managed fields it assigned itself, which a
// deploy clears because no target can recreate them — plus the machine
// type and CPU topology that only the resolution knows.
func expandedSpec() (expanded *unstructured.Unstructured) {
	expanded = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]any{
			"name":              testVirtualMachineName,
			"namespace":         testSourceNamespace,
			"uid":               "2b1f38d2-0e33-4f6a-9d21-8f4e0a4c9b77",
			"resourceVersion":   "158213",
			"generation":        int64(3),
			"creationTimestamp": "2026-01-06T09:15:00Z",
			"managedFields": []any{
				map[string]any{
					"manager":    "kubevirt.io/virtualmachine-controller",
					"operation":  "Update",
					"apiVersion": "kubevirt.io/v1",
					"time":       "2026-02-11T12:52:05Z",
					"fieldsType": "FieldsV1",
					"fieldsV1":   map[string]any{"f:status": map[string]any{}},
				},
			},
			"annotations": map[string]any{
				versionAnnotation: versionValue,
			},
		},
		"spec": map[string]any{
			"running": false,
			"template": map[string]any{
				"spec": map[string]any{
					"domain": map[string]any{
						"cpu":      map[string]any{"cores": int64(4)},
						"machine":  map[string]any{"type": testMachineType},
						"firmware": map[string]any{"uuid": testUUID},
						"devices": map[string]any{
							"interfaces": []any{
								map[string]any{"name": "default", "macAddress": testMAC},
							},
						},
					},
				},
			},
		},
		"status": map[string]any{"printableStatus": "Stopped"},
	}}
	return
}

// instancetype is a resource the bundle collected that a deploy must never
// create: the target runs the source cluster's resolution, so it needs no
// instancetype of its own.
func instancetype() (held *unstructured.Unstructured) {
	held = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "instancetype.kubevirt.io/v1beta1",
		"kind":       "VirtualMachineInstancetype",
		"metadata":   map[string]any{"name": "standard-large", "namespace": testSourceNamespace},
		"spec":       map[string]any{"cpu": map[string]any{"guest": int64(4)}},
	}}
	return
}

// preference is a resource the bundle collected that a deploy must never
// create, for the same reason as the instancetype.
func preference() (held *unstructured.Unstructured) {
	held = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "instancetype.kubevirt.io/v1beta1",
		"kind":       "VirtualMachinePreference",
		"metadata":   map[string]any{"name": "windows-defaults", "namespace": testSourceNamespace},
		"spec":       map[string]any{"devices": map[string]any{"preferredDiskBus": "virtio"}},
	}}
	return
}

// controllerRevision is a resource the bundle collected that a deploy must
// never create: the revisions a bundle holds are read-only diagnostics.
func controllerRevision() (held *unstructured.Unstructured) {
	held = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "ControllerRevision",
		"metadata":   map[string]any{"name": "db-01-4f5e6d7c", "namespace": testSourceNamespace},
		"data":       map[string]any{},
	}}
	return
}
