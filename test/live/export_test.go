//go:build live

package live

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/capture"
	"github.com/mansam/kvo/pkg/cluster"
)

// sharedCapture is the one capture of the fixture VirtualMachine the tests share.
//
// A capture creates a real VirtualMachineExport and waits for the export
// server to come up, which is by far the slowest thing the suite does, so the
// tests take one between them rather than one each.
type sharedCapture struct {
	once     sync.Once
	record   bundle.CaptureRecord
	manifest *bundle.Manifest
	scratch  string
	path     string
	err      error
}

// captured returns the one capture of the fixture VirtualMachine, taking it
// the first time it is asked for.
func (r *suite) captured(t *testing.T) (record bundle.CaptureRecord, manifest *bundle.Manifest) {
	t.Helper()
	r.capture.once.Do(func() {
		r.capture.take(context.Background(), r)
	})
	if r.capture.err != nil {
		t.Fatalf("capture the fixture VirtualMachine: %v", r.capture.err)
	}
	record = r.capture.record
	manifest = r.capture.manifest
	return
}

// take captures the fixture VirtualMachine into a bundle and reads the bundle
// back.
//
// It goes through the library entry point with the real cluster adapter,
// which is the point of doing it here: the everyday suite substitutes a fake
// for the adapter, so this is the only place the adapter's field names and
// subresource paths are sent to an API that rejects the wrong ones.
//
// The bundle is read through bundle.Read, which checks every member against
// the digest the manifest records for it and refuses a bundle whose indexed
// members have not all arrived.
func (r *sharedCapture) take(ctx context.Context, s *suite) {
	r.scratch, r.err = os.MkdirTemp("", "quo-live-*")
	if r.err != nil {
		return
	}
	r.path = filepath.Join(r.scratch, "fixture.bundle")
	r.record, r.err = capture.ExportToFile(
		ctx,
		s.quo,
		capture.Options{
			Namespace: s.fixture.namespace,
			Name:      s.fixture.named(partVM),
		},
		r.path)
	if r.err != nil {
		return
	}
	file, err := os.Open(r.path)
	if err != nil {
		r.err = err
		return
	}
	defer func() {
		_ = file.Close()
	}()
	r.manifest, r.err = bundle.Read(file)
}

func TestCapturingARealVirtualMachineProducesABundleThatReadsBack(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	_, manifest := s.captured(t)

	g.Expect(manifest.FormatVersion).To(gomega.Equal(bundle.FormatVersion))
	g.Expect(manifest.Members).ToNot(gomega.BeEmpty())
}

func TestCapturingARealVirtualMachineRecordsWhereItCameFrom(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	_, manifest := s.captured(t)

	g.Expect(manifest.Provenance.Namespace).To(gomega.Equal(s.fixture.namespace))
	g.Expect(manifest.Provenance.VirtualMachine).To(gomega.Equal(s.fixture.named(partVM)))
	g.Expect(manifest.Provenance.CapturedAt).To(
		gomega.BeTemporally("~", time.Now().UTC(), time.Hour))
	g.Expect(manifest.Provenance.SourceCluster).ToNot(gomega.BeEmpty())
	g.Expect(manifest.Provenance.KubernetesVersion).ToNot(gomega.BeEmpty())
	g.Expect(manifest.Provenance.KubeVirtVersion).ToNot(gomega.BeEmpty())
}

func TestCapturingARealVirtualMachineCarriesItsClosure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	_, manifest := s.captured(t)

	carried := describeObjects(manifest.Objects)
	g.Expect(carried).To(gomega.ContainElements(
		describe(s.fixture.ref(cluster.KindVirtualMachine, partVM)),
		describe(s.fixture.ref(cluster.KindConfigMap, partConfig)),
		describe(s.fixture.ref(cluster.KindSecret, partCloudInit)),
		describe(s.fixture.ref(cluster.KindSecret, partCredentials)),
		describe(s.fixture.ref(cluster.KindServiceAccount, partAccount)),
		describe(s.fixture.ref(cluster.KindDataVolume, partDisk)),
		describe(s.fixture.ref(cluster.KindPersistentVolumeClaim, partDisk)),
		describe(s.fixture.ref(cluster.KindVirtualMachineInstancetype, partInstancetype)),
		describe(s.fixture.ref(cluster.KindVirtualMachinePreference, partPreference)),
	))
	// KubeVirt pins the instancetype to a ControllerRevision of its own
	// naming, which the fixture waits for before anything reads the
	// VirtualMachine.
	g.Expect(carried).To(gomega.ContainElement(
		gomega.HavePrefix(cluster.KindControllerRevision + " " + s.fixture.namespace + "/")))
}

func TestCapturingARealVirtualMachineHoldsEvidenceAboutTheSourceCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	_, manifest := s.captured(t)

	kinds := kindsOf(manifest.Evidence)
	// KubeVirt's own configuration, which is read by listing a singleton
	// whose namespace and name differ between installations.
	g.Expect(kinds).To(gomega.ContainElement(cluster.KindKubeVirt))
	// The expand-spec subresource, which is the one subresource a capture
	// reads and is served by virt-api rather than by the API server.
	g.Expect(kinds).To(gomega.ContainElement(cluster.KindVirtualMachine))
	// The storage behind the claim, which is cluster-scoped.
	g.Expect(kinds).To(gomega.ContainElement(cluster.KindStorageClass))
}

func TestCapturingARealVirtualMachineCarriesItsDisk(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	_, manifest := s.captured(t)

	g.Expect(manifest.Disks).To(gomega.HaveLen(1))
	disk := manifest.Disks[0]
	g.Expect(disk.Claim).To(gomega.Equal(s.fixture.named(partDisk)))
	g.Expect(disk.Volumes).To(gomega.Equal([]string{volumeDisk}))
	g.Expect(disk.Format).To(gomega.BeElementOf(bundle.DiskGzip, bundle.DiskArchiveGzip))
	// bundle.Read has already checked the member against its digest, so what
	// is left to say is that the export server sent something.
	g.Expect(memberSize(manifest.Members, disk.Path)).To(gomega.BeNumerically(">", 0))
}

func TestCapturingARealVirtualMachineDeclaresWhatItDoesNotCarry(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	_, manifest := s.captured(t)

	g.Expect(manifest.Requirements.ContainerDisks).To(gomega.ContainElement(
		bundle.ContainerDiskRequirement{Volume: volumeContainer, Image: fixtureImage}))
	g.Expect(manifest.Requirements.Storage).To(gomega.HaveLen(1))
	g.Expect(manifest.Requirements.Storage[0].Claim).To(gomega.Equal(s.fixture.named(partDisk)))
	g.Expect(manifest.Requirements.Storage[0].Size).ToNot(gomega.BeEmpty())
	g.Expect(manifest.Requirements.Storage[0].VolumeMode).ToNot(gomega.BeEmpty())
}

func TestCapturingARealVirtualMachineLeavesItsIdentityAlone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	s.captured(t)

	carried := s.read(t, bundle.ObjectPath(cluster.KindVirtualMachine, s.fixture.named(partVM)))
	uuid, _, err := unstructured.NestedString(
		carried.Object, "spec", "template", "spec", "domain", "firmware", "uuid")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(uuid).To(gomega.Equal(fixtureUUID))
	serial, _, err := unstructured.NestedString(
		carried.Object, "spec", "template", "spec", "domain", "firmware", "serial")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(serial).To(gomega.Equal(fixtureSerial))
}

// read returns one object the captured bundle holds, decoded from the
// Kubernetes YAML it is stored as.
func (r *suite) read(t *testing.T, path string) (object *unstructured.Unstructured) {
	t.Helper()
	object = &unstructured.Unstructured{}
	err := yaml.Unmarshal(r.member(t, path), object)
	if err != nil {
		t.Fatalf("decode bundle member %q: %v", path, err)
	}
	return
}

// member returns the content of one member of the captured bundle.
func (r *suite) member(t *testing.T, path string) (content []byte) {
	t.Helper()
	file, err := os.Open(r.capture.path)
	if err != nil {
		t.Fatalf("open the captured bundle: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()
	reader := tar.NewReader(file)
	for {
		header, next := reader.Next()
		if errors.Is(next, io.EOF) {
			break
		}
		if next != nil {
			t.Fatalf("read the captured bundle: %v", next)
		}
		if header.Name != path {
			continue
		}
		content, err = io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read bundle member %q: %v", path, err)
		}
		return
	}
	t.Fatalf("the captured bundle holds no member %q", path)
	return
}

// describeObjects names every object a bundle holds, the way the difference report names
// one, so that a failure reads the same whichever test produced it.
func describeObjects(objects []bundle.Object) (names []string) {
	for i := range objects {
		names = append(names, describe(cluster.ObjectRef{
			Kind:      objects[i].Kind,
			Namespace: objects[i].Namespace,
			Name:      objects[i].Name,
		}))
	}
	return
}

// kindsOf is the kinds of the objects a bundle holds.
func kindsOf(objects []bundle.Object) (kinds []string) {
	for i := range objects {
		kinds = append(kinds, objects[i].Kind)
	}
	return
}

// memberSize is the recorded size of one member of a bundle, zero when the
// manifest indexes no such member.
func memberSize(members []bundle.Member, path string) (size int64) {
	for i := range members {
		if members[i].Path != path {
			continue
		}
		size = members[i].Size
		return
	}
	return
}
