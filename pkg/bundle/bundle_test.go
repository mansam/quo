package bundle

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

// Where the resource the fixtures put in a bundle lands.
const resourcePath = "resources/virtualmachine/db-01.yaml"

// The claim behind the disk the fixtures stage.
const diskClaim = "db-01-root"

var objectBody = []byte("apiVersion: kubevirt.io/v1\nkind: VirtualMachine\n")

// The same VirtualMachine as the source cluster expands it, which is a member
// of its own rather than a resource.
var expandedBody = []byte("apiVersion: kubevirt.io/v1\nkind: VirtualMachine\nspec: {}\n")

// Stand-in for the compressed bytes an export server sends.
var diskBody = []byte("\x1f\x8b\x08not really gzip, but opaque to quo")

func TestReadReturnsTheManifestOfABundleItWrote(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	manifest, err := Read(bytes.NewReader(validBundle(t)))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(manifest.FormatVersion).To(gomega.Equal(FormatVersion))
	g.Expect(manifest.Provenance).To(gomega.Equal(testProvenance()))
	g.Expect(manifest.Members).To(gomega.HaveLen(1))
	g.Expect(manifest.Members[0].Path).To(gomega.Equal(resourcePath))
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
				rawMember{path: resourcePath, data: objectBody},
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
				rawMember{path: resourcePath, data: objectBody},
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
		rawMember{path: resourcePath, data: []byte("corrupted in transit")},
	)

	read, err := Read(bytes.NewReader(archive))

	g.Expect(err).To(gomega.MatchError(&DigestMismatchError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring(resourcePath))
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
				rawMember{path: resourcePath, data: objectBody},
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
	g.Expect(report).To(gomega.ContainSubstring(resourcePath))
	g.Expect(report).To(gomega.ContainSubstring(digest(objectBody)))
}

func TestInspectListsTheResourcesByKindAndName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	manifest := indexing(t)
	manifest.Resources = []Resource{
		testResource(),
		{
			Kind:      "NetworkAttachmentDefinition",
			Namespace: "shared-networks",
			Name:      "storage-net",
			Path:      ResourcePath("NetworkAttachmentDefinition", "storage-net"),
		},
	}
	archive := tarball(t,
		rawMember{path: ManifestPath, data: encodeManifest(t, manifest)},
		rawMember{path: resourcePath, data: objectBody})
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(archive), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	g.Expect(report).To(gomega.ContainSubstring("Resources (2)"))
	g.Expect(report).To(gomega.MatchRegexp(`VirtualMachine\s+db-01\n`))
	// An object borrowed from another namespace is named with it; one from
	// the VirtualMachine's own namespace is not.
	g.Expect(report).To(gomega.MatchRegexp(`NetworkAttachmentDefinition\s+shared-networks/storage-net\n`))
}

func TestInspectNamesTheMemberHoldingTheExpandedSpec(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	manifest := indexing(t)
	manifest.Resources = []Resource{testResource()}
	manifest.ExpandedSpec = ExpandedSpecPath
	manifest.Members = append(manifest.Members, Member{
		Path:   ExpandedSpecPath,
		Size:   int64(len(expandedBody)),
		Digest: digest(expandedBody),
	})
	archive := tarball(t,
		rawMember{path: ManifestPath, data: encodeManifest(t, manifest)},
		rawMember{path: resourcePath, data: objectBody},
		rawMember{path: ExpandedSpecPath, data: expandedBody})
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(archive), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	// The expansion is not a resource, so it is named on its own and the
	// VirtualMachine is listed once rather than twice under one name.
	g.Expect(report).To(gomega.ContainSubstring("Resources (1)"))
	g.Expect(report).To(gomega.ContainSubstring("Expanded spec"))
	g.Expect(report).To(gomega.ContainSubstring(ExpandedSpecPath))
	listed := regexp.MustCompile(`VirtualMachine\s+db-01\n`).FindAllString(report, -1)
	g.Expect(listed).To(gomega.HaveLen(1))
}

func TestInspectSaysNothingAboutTheExpandedSpecWhenABundleHoldsNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(validBundle(t)), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(described.String()).ToNot(gomega.ContainSubstring("Expanded spec"))
}

func TestInspectPrintsWhatABundleNeedsFromItsLeadingManifestAlone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	manifest := indexing(t)
	manifest.Requirements = testRequirements()
	// Nothing but the manifest. This is what a support engineer has once the
	// head of a transfer has arrived and before anything the manifest indexes
	// has, which is the moment they want to know whether their cluster can
	// satisfy the bundle at all.
	archive := tarball(t, rawMember{path: ManifestPath, data: encodeManifest(t, manifest)})
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(archive), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	// Nothing was extracted and no disk was waited for: what a target would
	// have to provide is in the first member.
	g.Expect(report).To(gomega.ContainSubstring("Members (1), 1 not yet arrived"))
	g.Expect(report).To(gomega.MatchRegexp(resourcePath + `\s+\d+ bytes\s+\(not yet arrived\)\n`))
	g.Expect(report).To(gomega.ContainSubstring("Networks required (2)"))
	// Every cluster has a pod network; a secondary one names the attachment
	// the source cluster backed it with.
	g.Expect(report).To(gomega.MatchRegexp(`default\s+pod\n`))
	g.Expect(report).To(gomega.MatchRegexp(`secondary\s+multus\s+shared-networks/storage-net\n`))
	g.Expect(report).To(gomega.ContainSubstring("Storage required (2)"))
	g.Expect(report).To(gomega.MatchRegexp(
		`db-01-root\s+32Gi\s+Block\s+class: ocs-storagecluster-ceph-rbd\n`))
	// A disk whose claim quo could not read says so rather than reading as a
	// disk that needs nothing.
	g.Expect(report).To(gomega.MatchRegexp(`db-01-data\s+\(unknown\)\s+\(unknown\)\s+class: \(unknown\)\n`))
	g.Expect(report).To(gomega.ContainSubstring("Container disk images required (1)"))
	g.Expect(report).To(gomega.MatchRegexp(`containerdisk\s+quay\.io/example/fedora:41\n`))
}

func TestInspectDescribesATransferThatStoppedPartWayThroughADisk(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	disk := testDisk()
	archive := writeBundle(t, testProvenance(), staged(t, disk))
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(arrived(t, archive)), described)

	// Where a transfer in flight actually is: the manifest and the objects
	// have arrived and the disk is still coming. Refusing to say anything
	// about it would withhold the part the reader is waiting on the rest for.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	g.Expect(report).To(gomega.ContainSubstring("Members (2), 1 not yet arrived"))
	g.Expect(report).To(gomega.MatchRegexp(resourcePath + `\s+\d+ bytes\s+sha256:`))
	g.Expect(report).To(gomega.MatchRegexp(disk.Path + `\s+\d+ bytes\s+\(not yet arrived\)\n`))
}

func TestReadStillRefusesABundleThatStoppedPartWayThroughADisk(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	archive := writeBundle(t, testProvenance(), staged(t, testDisk()))

	read, err := Read(bytes.NewReader(arrived(t, archive)))

	// Describing the part of a bundle that arrived is one thing. Handing back
	// a manifest as though the bundle behind it were whole is another: every
	// other caller reads one to work with the members it indexes.
	g.Expect(err).To(gomega.MatchError(&FormatError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring(DiskPath(diskClaim, DiskGzip)))
	g.Expect(read).To(gomega.BeNil())
}

func TestInspectSaysNothingAboutRequirementsWhenABundleDeclaresNone(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(validBundle(t)), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(described.String()).ToNot(gomega.ContainSubstring("required"))
}

func TestWriterRecordsTheRequirementsInTheManifest(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	file, err := os.Create(filepath.Join(t.TempDir(), "bundle"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	writer := NewWriter(file)

	writer.Require(testRequirements())

	err = writer.Open(testProvenance())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	err = writer.Close()
	g.Expect(err).ToNot(gomega.HaveOccurred())
	archive, err := os.ReadFile(file.Name())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	manifest, err := Read(bytes.NewReader(archive))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The room reserved for the manifest is sized from what it holds, so
	// requirements declared before the bundle was opened come back whole.
	g.Expect(manifest.Requirements).To(gomega.Equal(testRequirements()))
}

func TestWriterKeepsTheExpandedSpecOutOfTheResources(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	file, err := os.Create(filepath.Join(t.TempDir(), "bundle"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	writer := NewWriter(file)
	err = writer.Collect(testResource(), objectBody)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	// The expansion carries the VirtualMachine's own kind and name, so it
	// would land on top of it among the resources. It is a member of its own
	// instead.
	writer.AddExpandedSpec(expandedBody)

	// A resource collected twice still collides.
	g.Expect(writer.Collect(testResource(), objectBody)).To(gomega.HaveOccurred())
	err = writer.Open(testProvenance())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	err = writer.Close()
	g.Expect(err).ToNot(gomega.HaveOccurred())
	archive, err := os.ReadFile(file.Name())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	manifest, err := Read(bytes.NewReader(archive))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(manifest.Resources).To(gomega.HaveLen(1))
	g.Expect(manifest.Resources[0].Path).To(gomega.Equal(resourcePath))
	g.Expect(manifest.ExpandedSpec).To(gomega.Equal(ExpandedSpecPath))
}

func TestInspectPrintsTheCaptureRecordWithTheSkippedObjectsInASectionOfTheirOwn(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	manifest := indexing(t)
	manifest.CaptureRecord = []Attempt{
		{
			Kind:      "VirtualMachine",
			Namespace: "support-cases",
			Name:      "db-01",
			Outcome:   OutcomeCollected,
		},
		{
			Kind:      "Secret",
			Namespace: "support-cases",
			Name:      "db-01-cloudinit",
			Outcome:   OutcomeSkipped,
			Reason:    SkipForbidden,
			Message:   `secrets "db-01-cloudinit" is forbidden`,
		},
		{
			Kind:      "NetworkAttachmentDefinition",
			Namespace: "shared-networks",
			Name:      "storage-net",
			Outcome:   OutcomeSkipped,
			Reason:    SkipNotInstalled,
		},
	}
	archive := tarball(t,
		rawMember{path: ManifestPath, data: encodeManifest(t, manifest)},
		rawMember{path: resourcePath, data: objectBody})
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(archive), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	g.Expect(report).To(gomega.ContainSubstring("Capture record (3)"))
	g.Expect(report).To(gomega.MatchRegexp(`collected\s+1\n`))
	g.Expect(report).To(gomega.MatchRegexp(`skipped\s+2\n`))
	// What a reader has to act on is listed under a heading of its own
	// rather than counted among everything that was collected.
	g.Expect(report).To(gomega.ContainSubstring("Skipped (2)"))
	g.Expect(report).To(gomega.MatchRegexp(
		`Secret\s+db-01-cloudinit\s+forbidden\s+secrets "db-01-cloudinit" is forbidden\n`))
	g.Expect(report).To(gomega.MatchRegexp(
		`NetworkAttachmentDefinition\s+shared-networks/storage-net\s+not installed\s*\n`))
	// An outcome nothing got is not printed.
	g.Expect(report).ToNot(gomega.ContainSubstring(string(OutcomeRequired)))
}

func TestInspectReportsAVersionItCouldNotRead(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	provenance := testProvenance()
	provenance.KubeVirtVersion = ""
	captured := writeBundle(t, provenance)
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(captured), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(described.String()).To(gomega.ContainSubstring("(unknown)"))
}

func TestWriterStagesADiskWithoutHoldingItInMemory(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	disk := testDisk()

	archive := writeBundle(t, testProvenance(), staged(t, disk))

	manifest, err := Read(bytes.NewReader(archive))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(manifest.Disks).To(gomega.Equal([]Disk{disk}))
	// The disk is indexed with the size and digest it turned out to have, not
	// the widest values the reservation was sized from.
	g.Expect(manifest.Members).To(gomega.ContainElement(Member{
		Path:   disk.Path,
		Size:   int64(len(diskBody)),
		Digest: digest(diskBody),
	}))
}

func TestWriterFillsTheManifestInWhateverPrecisionTheCaptureTimeHas(t *testing.T) {
	tests := []struct {
		name       string
		capturedAt time.Time
	}{
		{
			name:       "a capture time on the second writes a plain tar header",
			capturedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			// Which puts a record of its own in front of the manifest.
			name:       "a capture time with nanoseconds writes an extended tar header",
			capturedAt: time.Date(2026, 10, 9, 12, 0, 0, 123456789, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			provenance := testProvenance()
			provenance.CapturedAt = tt.capturedAt

			archive := writeBundle(t, provenance, staged(t, testDisk()))

			manifest, err := Read(bytes.NewReader(archive))
			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(manifest.Provenance.CapturedAt).To(gomega.BeTemporally("==", tt.capturedAt))
			g.Expect(manifest.Disks).To(gomega.HaveLen(1))
		})
	}
}

func TestWriterReportsADiskThatWasDeclaredButNeverStaged(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	file, err := os.Create(filepath.Join(t.TempDir(), "bundle"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	writer := NewWriter(file)
	writer.Reserve(testDisk())
	err = writer.Open(testProvenance())
	g.Expect(err).ToNot(gomega.HaveOccurred())

	err = writer.Close()

	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("never staged"))
}

func TestInspectListsTheDisksAndTheVolumesEachServes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	disk := testDisk()
	archive := writeBundle(t, testProvenance(), staged(t, disk))
	described := &bytes.Buffer{}

	err := Inspect(bytes.NewReader(archive), described)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	report := described.String()
	g.Expect(report).To(gomega.ContainSubstring("Disks (1)"))
	g.Expect(report).To(gomega.ContainSubstring("db-01-root"))
	g.Expect(report).To(gomega.ContainSubstring(string(DiskGzip)))
	g.Expect(report).To(gomega.ContainSubstring("rootdisk, clone-of-rootdisk"))
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

// arrived returns the leading bytes of a bundle: everything up to the point
// where the disk's content begins, which is where a transfer that is still
// running has got to.
func arrived(t *testing.T, archive []byte) (head []byte) {
	t.Helper()
	at := bytes.Index(archive, diskBody)
	if at < 0 {
		t.Fatal("the bundle carries no disk to stop part-way through")
	}
	head = archive[:at]
	return
}

// validBundle builds a bundle carrying one object.
func validBundle(t *testing.T) (archive []byte) {
	t.Helper()
	archive = writeBundle(t, testProvenance())
	return
}

// stagedDisk is a disk a test stages into a bundle out of a file, the way a
// capture stages one it has just downloaded.
type stagedDisk struct {
	disk   Disk
	source string
}

// writeBundle writes a bundle carrying the fixture object and the given
// disks, and returns the bytes that landed on disk.
func writeBundle(t *testing.T, provenance Provenance, disks ...stagedDisk) (archive []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bundle")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()
	writer := NewWriter(file)
	err = writer.Collect(testResource(), objectBody)
	if err != nil {
		t.Fatalf("carry object: %v", err)
	}
	for i := range disks {
		writer.Reserve(disks[i].disk)
	}
	err = writer.Open(provenance)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	for i := range disks {
		stage(t, writer, disks[i])
	}
	err = writer.Close()
	if err != nil {
		t.Fatalf("close bundle: %v", err)
	}
	archive, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	return
}

// testResource is the resource the fixtures put in a bundle.
func testResource() (resource Resource) {
	resource = Resource{
		Kind:      "VirtualMachine",
		Namespace: "support-cases",
		Name:      "db-01",
		Path:      resourcePath,
	}
	return
}

// testRequirements is what the fixtures declare a target would have to
// provide: one of each kind of requirement, and one disk whose claim quo
// could not read.
func testRequirements() (required Requirements) {
	required = Requirements{
		Networks: []NetworkRequirement{
			{Name: "default", Type: NetworkPod},
			{
				Name:       "secondary",
				Type:       NetworkMultus,
				Namespace:  "shared-networks",
				Attachment: "storage-net",
			},
		},
		Storage: []StorageRequirement{
			{
				Claim:        diskClaim,
				StorageClass: "ocs-storagecluster-ceph-rbd",
				Size:         "32Gi",
				VolumeMode:   "Block",
			},
			{Claim: "db-01-data"},
		},
		ContainerDisks: []ContainerDiskRequirement{
			{Volume: "containerdisk", Image: "quay.io/example/fedora:41"},
		},
	}
	return
}

// testDisk is the disk the fixtures stage: one claim, two volumes.
func testDisk() (disk Disk) {
	disk = Disk{
		Claim:   diskClaim,
		Path:    DiskPath(diskClaim, DiskGzip),
		Format:  DiskGzip,
		Volumes: []string{"rootdisk", "clone-of-rootdisk"},
	}
	return
}

// staged writes the fixture disk content to a file for the writer to pick up.
func staged(t *testing.T, disk Disk) (ready stagedDisk) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "disk")
	err := os.WriteFile(source, diskBody, 0o600)
	if err != nil {
		t.Fatalf("stage disk: %v", err)
	}
	ready = stagedDisk{disk: disk, source: source}
	return
}

// stage writes one staged disk into the bundle.
func stage(t *testing.T, writer *Writer, ready stagedDisk) {
	t.Helper()
	file, err := os.Open(ready.source)
	if err != nil {
		t.Fatalf("open staged disk: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("measure staged disk: %v", err)
	}
	err = writer.Stage(ready.disk.Path, file, info.Size())
	if err != nil {
		t.Fatalf("stage disk %q: %v", ready.disk.Claim, err)
	}
}

// indexing builds a manifest that correctly indexes the resource, for
// tests that then corrupt one part of the bundle.
func indexing(t *testing.T) (manifest *Manifest) {
	t.Helper()
	manifest = &Manifest{
		FormatVersion: FormatVersion,
		Provenance:    testProvenance(),
		Members: []Member{
			{
				Path:   resourcePath,
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
