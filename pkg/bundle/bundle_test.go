package bundle

import (
	"archive/tar"
	"bytes"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

// The carried object the fixtures put in a bundle.
const objectPath = "objects/virtualmachine/db-01.yaml"

var objectBody = []byte("apiVersion: kubevirt.io/v1\nkind: VirtualMachine\n")

func TestReadReturnsTheManifestOfABundleItWrote(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	manifest, err := Read(bytes.NewReader(validBundle(t)))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(manifest.FormatVersion).To(gomega.Equal(FormatVersion))
	g.Expect(manifest.Provenance).To(gomega.Equal(testProvenance()))
	g.Expect(manifest.Members).To(gomega.HaveLen(1))
	g.Expect(manifest.Members[0].Path).To(gomega.Equal(objectPath))
	g.Expect(manifest.Members[0].Size).To(gomega.BeEquivalentTo(len(objectBody)))
	g.Expect(manifest.Members[0].Digest).To(gomega.Equal(digest(objectBody)))
}

func TestReadRefusesSomethingThatIsNotABundle(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
	}{
		{
			name:    "an empty bundle is refused",
			archive: tarball(t),
		},
		{
			name: "a bundle whose first member is not the manifest is refused",
			archive: tarball(t,
				rawMember{path: objectPath, data: objectBody},
				rawMember{path: ManifestPath, data: encodeManifest(t, indexing(t))},
			),
		},
		{
			name: "a manifest that is not valid YAML is refused",
			archive: tarball(t,
				rawMember{path: ManifestPath, data: []byte("{{{ not yaml")},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			manifest, err := Read(bytes.NewReader(tt.archive))

			g.Expect(err).To(gomega.MatchError(&FormatError{}))
			g.Expect(manifest).To(gomega.BeNil())
		})
	}
}

func TestReadRefusesAFormatVersionItDoesNotUnderstand(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{
			name:    "a later format version is refused",
			version: "v2",
		},
		{
			name:    "a manifest declaring no format version is refused",
			version: "",
		},
		{
			name:    "a format version that is merely similar is refused",
			version: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			manifest := indexing(t)
			manifest.FormatVersion = tt.version
			archive := tarball(t,
				rawMember{path: ManifestPath, data: encodeManifest(t, manifest)},
				rawMember{path: objectPath, data: objectBody},
			)

			read, err := Read(bytes.NewReader(archive))

			g.Expect(err).To(gomega.MatchError(&UnsupportedVersionError{}))
			g.Expect(err.Error()).To(gomega.ContainSubstring(FormatVersion))
			g.Expect(read).To(gomega.BeNil())
		})
	}
}

func TestReadReportsAMemberThatDoesNotMatchItsDigest(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	manifest := indexing(t)
	archive := tarball(t,
		rawMember{path: ManifestPath, data: encodeManifest(t, manifest)},
		rawMember{path: objectPath, data: []byte("corrupted in transit")},
	)

	read, err := Read(bytes.NewReader(archive))

	g.Expect(err).To(gomega.MatchError(&DigestMismatchError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring(objectPath))
	g.Expect(read).To(gomega.BeNil())
}

func TestReadReportsAMemberTheManifestDisagreesAbout(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
	}{
		{
			name: "a member the manifest indexes but the bundle does not carry",
			archive: tarball(t,
				rawMember{path: ManifestPath, data: encodeManifest(t, indexing(t))},
			),
		},
		{
			name: "a member the bundle carries but the manifest does not index",
			archive: tarball(t,
				rawMember{path: ManifestPath, data: encodeManifest(t, indexing(t))},
				rawMember{path: objectPath, data: objectBody},
				rawMember{path: "objects/configmap/stowaway.yaml", data: []byte("kind: ConfigMap\n")},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			read, err := Read(bytes.NewReader(tt.archive))

			g.Expect(err).To(gomega.MatchError(&FormatError{}))
			g.Expect(read).To(gomega.BeNil())
		})
	}
}

func TestInspectDescribesABundleWithoutContactingACluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(validBundle(t)), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	g.Expect(report).To(gomega.ContainSubstring(FormatVersion))
	g.Expect(report).To(gomega.ContainSubstring("https://api.source.example.com:6443"))
	g.Expect(report).To(gomega.ContainSubstring("support-cases"))
	g.Expect(report).To(gomega.ContainSubstring("db-01"))
	g.Expect(report).To(gomega.ContainSubstring("2026-10-09T12:00:00Z"))
	g.Expect(report).To(gomega.ContainSubstring("v1.3.1"))
	g.Expect(report).To(gomega.ContainSubstring("v1.31.4"))
	g.Expect(report).To(gomega.ContainSubstring(objectPath))
	g.Expect(report).To(gomega.ContainSubstring(digest(objectBody)))
}

func TestInspectReportsAVersionItCouldNotRead(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	provenance := testProvenance()
	provenance.KubeVirtVersion = ""
	writer := &Writer{}
	writer.Add(objectPath, objectBody)
	captured := &bytes.Buffer{}
	err := writer.Write(captured, provenance)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	described := &bytes.Buffer{}

	err = Inspect(bytes.NewReader(captured.Bytes()), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(described.String()).To(gomega.ContainSubstring("(unknown)"))
}

// rawMember is a tar member written exactly as given.
type rawMember struct {
	path string
	data []byte
}

// tarball writes members into a tar exactly as given, including shapes a valid
// bundle would never have.
func tarball(t *testing.T, members ...rawMember) (archive []byte) {
	t.Helper()
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	for _, member := range members {
		header := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     member.path,
			Mode:     0o644,
			Size:     int64(len(member.data)),
			Format:   tar.FormatPAX,
		}
		err := tw.WriteHeader(header)
		if err != nil {
			t.Fatalf("write header for %q: %v", member.path, err)
		}
		_, err = tw.Write(member.data)
		if err != nil {
			t.Fatalf("write member %q: %v", member.path, err)
		}
	}
	err := tw.Close()
	if err != nil {
		t.Fatalf("close the tar: %v", err)
	}
	archive = buf.Bytes()
	return
}

// validBundle builds a bundle carrying one object.
func validBundle(t *testing.T) (archive []byte) {
	t.Helper()
	writer := &Writer{}
	writer.Add(objectPath, objectBody)
	buf := &bytes.Buffer{}
	err := writer.Write(buf, testProvenance())
	if err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	archive = buf.Bytes()
	return
}

// indexing builds a manifest that correctly indexes the carried object, for
// tests that then corrupt one part of the bundle.
func indexing(t *testing.T) (manifest *Manifest) {
	t.Helper()
	manifest = &Manifest{
		FormatVersion: FormatVersion,
		Provenance:    testProvenance(),
		Members: []Member{
			{
				Path:   objectPath,
				Size:   int64(len(objectBody)),
				Digest: digest(objectBody),
			},
		},
	}
	return
}

// encodeManifest renders a manifest as its bundle member.
func encodeManifest(t *testing.T, manifest *Manifest) (data []byte) {
	t.Helper()
	data, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return
}

// testProvenance is the provenance the fixtures record.
func testProvenance() (provenance Provenance) {
	provenance = Provenance{
		SourceCluster:     "https://api.source.example.com:6443",
		Namespace:         "support-cases",
		VirtualMachine:    "db-01",
		CapturedAt:        time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		KubeVirtVersion:   "v1.3.1",
		KubernetesVersion: "v1.31.4",
	}
	return
}
