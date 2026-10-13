package capture

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// testVMMember is where the fixture VirtualMachine lands in a bundle.
const testVMMember = "resources/virtualmachine/db-01.yaml"

// Where what every fixture cluster says about itself lands in a bundle.
const (
	testKubeVirtMember = "resources/kubevirt/kubevirt.yaml"
	testExpandedMember = "expanded-spec.yaml"
)

// The claims behind the fixture VirtualMachine's disks, and where each lands
// in a bundle.
const (
	rootClaim  = "db-01-root"
	dataClaim  = "db-01-data"
	rootMember = "disks/db-01-root.gz"
	dataMember = "disks/db-01-data.gz"
)

// Canned disk content. quo never looks inside a disk, so these only have to
// be distinguishable from one another.
var (
	rootContent = []byte("\x1f\x8b\x08 the root disk, as the export server sent it")
	dataContent = []byte("\x1f\x8b\x08 the data disk")
)

func TestExportWritesTheManifestAsTheFirstMember(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	order, _ := captureBundle(t)

	g.Expect(order).ToNot(gomega.BeEmpty())
	g.Expect(order[0]).To(gomega.Equal(bundle.ManifestPath))
}

func TestExportRecordsTheFormatVersionAndProvenance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureBundle(t)

	manifest := capturedManifest(t, content)
	g.Expect(manifest.FormatVersion).To(gomega.Equal(bundle.FormatVersion))
	g.Expect(manifest.Provenance.SourceCluster).To(gomega.Equal("https://api.source.example.com:6443"))
	g.Expect(manifest.Provenance.Namespace).To(gomega.Equal(testNamespace))
	g.Expect(manifest.Provenance.VirtualMachine).To(gomega.Equal(testName))
	g.Expect(manifest.Provenance.KubeVirtVersion).To(gomega.Equal("v1.3.1"))
	g.Expect(manifest.Provenance.KubernetesVersion).To(gomega.Equal("v1.31.4"))
	g.Expect(manifest.Provenance.CapturedAt).To(
		gomega.BeTemporally("~", time.Now().UTC(), time.Minute))
}

func TestExportCarriesTheVirtualMachineUnderTheObjectsGrouping(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	order, content := captureBundle(t)

	g.Expect(order).To(gomega.Equal(withSourceCluster([]string{
		bundle.ManifestPath,
		testVMMember,
	})))
	carried := decodeMember(t, content, testVMMember)
	g.Expect(carried["kind"]).To(gomega.Equal("VirtualMachine"))
	g.Expect(carried["apiVersion"]).To(gomega.Equal("kubevirt.io/v1"))
}

func TestExportLeavesVirtualMachineIdentityUntouched(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureBundle(t)

	carried := decodeMember(t, content, testVMMember)
	domain := nested(t, carried, "spec", "template", "spec", "domain")
	firmware := domain["firmware"].(map[string]any)
	chassis := domain["chassis"].(map[string]any)
	devices := domain["devices"].(map[string]any)
	interfaces := devices["interfaces"].([]any)
	first := interfaces[0].(map[string]any)

	g.Expect(firmware["uuid"]).To(gomega.Equal(testUUID))
	g.Expect(firmware["serial"]).To(gomega.Equal(testSerial))
	g.Expect(chassis["serial"]).To(gomega.Equal(testChassis))
	g.Expect(first["macAddress"]).To(gomega.Equal(testMAC))
}

func TestExportKeepsAwkwardAnnotationValuesAsStrings(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		want       string
	}{
		{
			name:       "a version-like annotation is not reinterpreted as a number",
			annotation: versionAnnotation,
			want:       versionValue,
		},
		{
			name:       "a bare no annotation is not reinterpreted as boolean false",
			annotation: sealedAnnotation,
			want:       sealedValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			_, content := captureBundle(t)

			carried := decodeMember(t, content, testVMMember)
			annotations := nested(t, carried, "metadata", "annotations")
			g.Expect(annotations[tt.annotation]).To(gomega.BeAssignableToTypeOf(""))
			g.Expect(annotations[tt.annotation]).To(gomega.Equal(tt.want))
		})
	}
}

func TestExportIndexesEveryMemberWithASha256Digest(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	order, content := captureBundle(t)

	manifest := capturedManifest(t, content)

	var indexed []string
	for _, member := range manifest.Members {
		indexed = append(indexed, member.Path)
		g.Expect(member.Digest).To(gomega.HavePrefix("sha256:"))
		g.Expect(member.Digest).To(gomega.HaveLen(len("sha256:") + 64))
		g.Expect(member.Size).To(gomega.BeEquivalentTo(len(content[member.Path])))
	}
	// The manifest indexes every member of the bundle except itself.
	g.Expect(indexed).To(gomega.Equal(order[1:]))
}

func TestExportRefusesAVirtualMachineThatIsNotStopped(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		captured bool
	}{
		{
			name:     "stopped VirtualMachine is captured",
			status:   StatusStopped,
			captured: true,
		},
		{
			name:   "running VirtualMachine is refused",
			status: "Running",
		},
		{
			name:   "starting VirtualMachine is refused",
			status: "Starting",
		},
		{
			name:   "paused VirtualMachine is refused",
			status: "Paused",
		},
		{
			name:   "migrating VirtualMachine is refused",
			status: "Migrating",
		},
		{
			name:   "VirtualMachine reporting no status at all is refused",
			status: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			captured, err := captureInto(t, clusterWithStatus(t, tt.status), testOptions())

			if tt.captured {
				g.Expect(err).ToNot(gomega.HaveOccurred())
				g.Expect(captured).ToNot(gomega.BeEmpty())
				return
			}
			g.Expect(err).To(gomega.MatchError(&NotStoppedError{}))
			// The refusal names the VirtualMachine and says what to do.
			g.Expect(err.Error()).To(gomega.ContainSubstring(testNamespace + "/" + testName))
			g.Expect(err.Error()).To(gomega.ContainSubstring("Stop it"))
			// Nothing is written when quo refuses.
			g.Expect(captured).To(gomega.BeEmpty())
		})
	}
}

func TestExportReportsWhatItCouldNotReadFromTheCluster(t *testing.T) {
	forbidden := errors.New("virtualmachines.kubevirt.io is forbidden")

	tests := []struct {
		name string
		c    *fakeCluster
	}{
		{
			name: "a VirtualMachine that cannot be read fails the capture",
			c:    &fakeCluster{vmErr: forbidden},
		},
		{
			name: "cluster information that cannot be read fails the capture",
			c: &fakeCluster{
				vm:      virtualMachine(t, StatusStopped),
				infoErr: forbidden,
			},
		},
		{
			name: "a VirtualMachine that does not exist fails the capture",
			c:    &fakeCluster{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			captured, err := captureInto(t, tt.c, testOptions())

			g.Expect(err).To(gomega.HaveOccurred())
			g.Expect(captured).To(gomega.BeEmpty())
		})
	}
}

func TestExportKeysDiskMembersByClaimRatherThanByVolumeName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	order, content := captureWithDisks(t, clusterWithTwoDisks(t), testOptions())

	g.Expect(order).To(gomega.Equal(withSourceCluster(
		[]string{bundle.ManifestPath, testVMMember},
		rootMember,
		dataMember)))
	manifest := capturedManifest(t, content)
	g.Expect(manifest.Disks).To(gomega.Equal([]bundle.Disk{
		{
			Claim:   rootClaim,
			Path:    rootMember,
			Format:  bundle.DiskGzip,
			Volumes: []string{"rootdisk", "rootdisk-again"},
		},
		{
			Claim:   dataClaim,
			Path:    dataMember,
			Format:  bundle.DiskGzip,
			Volumes: []string{"datadisk"},
		},
	}))
}

func TestExportDownloadsAClaimTwoVolumesShareOnlyOnce(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := clusterWithTwoDisks(t)

	order, _ := captureWithDisks(t, c, testOptions())

	g.Expect(c.export.opened).To(gomega.Equal([]string{rootClaim, dataClaim}))
	// One member per claim rather than one per volume.
	g.Expect(order).To(gomega.Equal(withSourceCluster(
		[]string{bundle.ManifestPath, testVMMember},
		rootMember,
		dataMember)))
}

func TestExportStoresTheDiskBytesItReceivedVerbatim(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureWithDisks(t, clusterWithTwoDisks(t), testOptions())

	g.Expect(content[rootMember]).To(gomega.Equal(rootContent))
	g.Expect(content[dataMember]).To(gomega.Equal(dataContent))
}

func TestExportIndexesEachDiskWithItsDigestAndSize(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureWithDisks(t, clusterWithTwoDisks(t), testOptions())

	manifest := capturedManifest(t, content)
	indexed := map[string]bundle.Member{}
	for _, member := range manifest.Members {
		indexed[member.Path] = member
	}
	g.Expect(indexed[rootMember].Size).To(gomega.BeEquivalentTo(len(rootContent)))
	g.Expect(indexed[rootMember].Digest).To(gomega.Equal(sha256Of(rootContent)))
	g.Expect(indexed[dataMember].Size).To(gomega.BeEquivalentTo(len(dataContent)))
	g.Expect(indexed[dataMember].Digest).To(gomega.Equal(sha256Of(dataContent)))
}

func TestExportStagesOneDiskAtATime(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := clusterWithTwoDisks(t)
	opt := stagingIn(t, c, testOptions())

	_, err := captureInto(t, c, opt)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Peak local disk is the largest single compressed disk, not the total.
	g.Expect(c.export.staged).To(gomega.Equal(1))
	g.Expect(entryNames(t, opt.Scratch)).To(gomega.BeEmpty())
}

func TestExportCapturesEveryVolumeThatAPersistentVolumeClaimBacks(t *testing.T) {
	tests := []struct {
		name    string
		backing map[string]any
		claim   string
	}{
		{
			name:    "a volume naming a claim directly",
			backing: map[string]any{"persistentVolumeClaim": map[string]any{"claimName": rootClaim}},
			claim:   rootClaim,
		},
		{
			// A DataVolume provisions a claim of its own name.
			name:    "a volume provisioned by a DataVolume",
			backing: map[string]any{"dataVolume": map[string]any{"name": rootClaim}},
			claim:   rootClaim,
		},
		{
			name:    "a volume holding a memory dump",
			backing: map[string]any{"memoryDump": map[string]any{"claimName": rootClaim}},
			claim:   rootClaim,
		},
		{
			// Nothing a cluster could hand back as a disk.
			name:    "a volume served from a container image",
			backing: map[string]any{"containerDisk": map[string]any{"image": "quay.io/example/disk:latest"}},
		},
		{
			name:    "a volume holding cloud-init configuration",
			backing: map[string]any{"cloudInitNoCloud": map[string]any{"userData": "#cloud-config"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := clusterWithDisks(t, nil, []disk{{claim: rootClaim, content: rootContent}})
			backVolume(t, c.vm, "rootdisk", tt.backing)

			_, content := captureWithDisks(t, c, testOptions())

			manifest := capturedManifest(t, content)
			if tt.claim == "" {
				g.Expect(manifest.Disks).To(gomega.BeEmpty())
				g.Expect(c.export.created).To(gomega.Equal(0))
				return
			}
			g.Expect(manifest.Disks).To(gomega.HaveLen(1))
			g.Expect(manifest.Disks[0].Claim).To(gomega.Equal(tt.claim))
			g.Expect(manifest.Disks[0].Volumes).To(gomega.Equal([]string{"rootdisk"}))
		})
	}
}

func TestExportRecordsAFilesystemVolumeAsAnArchive(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := clusterWithDisks(t,
		[]volume{{name: "rootdisk", claim: rootClaim}},
		[]disk{{claim: rootClaim, format: cluster.FormatArchiveGzip, content: rootContent}})

	order, content := captureWithDisks(t, c, testOptions())

	g.Expect(order).To(gomega.ContainElement("disks/db-01-root.tar.gz"))
	manifest := capturedManifest(t, content)
	g.Expect(manifest.Disks[0].Format).To(gomega.Equal(bundle.DiskArchiveGzip))
}

func TestExportCleansUpTheExportWhateverHappens(t *testing.T) {
	tests := []struct {
		name   string
		broken bool
	}{
		{
			name: "a capture that finishes removes the export",
		},
		{
			name:   "a capture that fails part-way removes the export",
			broken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := clusterWithTwoDisks(t)
			if tt.broken {
				c.export.openErr = errors.New("the export server went away")
			}

			_, err := captureInto(t, c, stagingIn(t, c, testOptions()))

			g.Expect(err != nil).To(gomega.Equal(tt.broken))
			g.Expect(c.export.created).To(gomega.Equal(1))
			g.Expect(c.export.closed).To(gomega.Equal(1))
		})
	}
}

func TestExportCreatesNoExportForAVirtualMachineWithNoDisks(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)

	_, err := captureInto(t, c, testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(c.export.created).To(gomega.Equal(0))
}

func TestExportRefusesWhenTheExportOffersNoDiskForAClaim(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := clusterWithDisks(t,
		[]volume{{name: "rootdisk", claim: rootClaim}},
		[]disk{{claim: "some-other-claim", content: rootContent}})

	captured, err := captureInto(t, c, stagingIn(t, c, testOptions()))

	g.Expect(err).To(gomega.MatchError(&DiskNotExportedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring(rootClaim))
	g.Expect(err.Error()).To(gomega.ContainSubstring("rootdisk"))
	g.Expect(captured).To(gomega.BeEmpty())
}

func TestExportReportsTransferProgress(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := clusterWithTwoDisks(t)
	opt := stagingIn(t, c, testOptions())
	var reported []Progress
	opt.Progress = func(p Progress) {
		reported = append(reported, p)
	}

	_, err := captureInto(t, c, opt)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Each disk is reported as it starts and again once it has all arrived,
	// so a capture that is moving can be told from one that has hung.
	g.Expect(reported).To(gomega.Equal([]Progress{
		{Claim: rootClaim, Volumes: []string{"rootdisk", "rootdisk-again"}, Disk: 1, Disks: 2},
		{
			Claim:       rootClaim,
			Volumes:     []string{"rootdisk", "rootdisk-again"},
			Disk:        1,
			Disks:       2,
			Transferred: int64(len(rootContent)),
			Done:        true,
		},
		{Claim: dataClaim, Volumes: []string{"datadisk"}, Disk: 2, Disks: 2},
		{
			Claim:       dataClaim,
			Volumes:     []string{"datadisk"},
			Disk:        2,
			Disks:       2,
			Transferred: int64(len(dataContent)),
			Done:        true,
		},
	}))
}

func TestExportToFileWritesABundleThatReadsBack(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "db-01.bundle")

	_, err := ExportToFile(context.TODO(), stoppedCluster(t), testOptions(), dest)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	file, err := os.Open(dest)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	manifest, err := bundle.Read(file)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(manifest.Provenance.VirtualMachine).To(gomega.Equal(testName))
	// The bundle is the only thing left in the directory.
	g.Expect(entryNames(t, dir)).To(gomega.Equal([]string{"db-01.bundle"}))
}

func TestExportToFileStagesDisksBesideTheBundleAndClearsUpAfterItself(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "db-01.bundle")
	c := clusterWithTwoDisks(t)
	c.export.scratch = dir

	_, err := ExportToFile(context.TODO(), c, testOptions(), dest)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	file, err := os.Open(dest)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	// Read verifies every member against the digest the manifest records.
	manifest, err := bundle.Read(file)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(manifest.Disks).To(gomega.HaveLen(2))
	// The bundle is the only thing left: no staged disk, no partial file.
	g.Expect(entryNames(t, dir)).To(gomega.Equal([]string{"db-01.bundle"}))
}

func TestExportToFileLeavesNoBundleBehindWhenTheCaptureFails(t *testing.T) {
	tests := []struct {
		name string
		c    *fakeCluster
	}{
		{
			name: "a refused VirtualMachine leaves no bundle behind",
			c:    clusterWithStatus(t, "Running"),
		},
		{
			name: "a capture that fails after it has started leaves no bundle behind",
			c: &fakeCluster{
				vm:      virtualMachine(t, StatusStopped),
				infoErr: errors.New("forbidden"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			dir := t.TempDir()
			dest := filepath.Join(dir, "db-01.bundle")

			_, err := ExportToFile(context.TODO(), tt.c, testOptions(), dest)

			g.Expect(err).To(gomega.HaveOccurred())
			_, statErr := os.Stat(dest)
			g.Expect(os.IsNotExist(statErr)).To(gomega.BeTrue())
			// Not even a partial file is left to be mistaken for a bundle.
			g.Expect(entryNames(t, dir)).To(gomega.BeEmpty())
		})
	}
}

func TestExportToFileFailsWhenTheOutputPathCannotBeWritten(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// A capture that cannot put the bundle anywhere is not worth running, so
	// this is reported rather than collected and discarded.
	dest := filepath.Join(t.TempDir(), "no-such-directory", "db-01.bundle")

	_, err := ExportToFile(context.TODO(), stoppedCluster(t), testOptions(), dest)

	g.Expect(err).To(gomega.HaveOccurred())
	_, statErr := os.Stat(dest)
	g.Expect(os.IsNotExist(statErr)).To(gomega.BeTrue())
}

// captureBundle captures the fixture VirtualMachine and returns the members
// of the bundle that came out, in the order they appear.
func captureBundle(t *testing.T) (order []string, content map[string][]byte) {
	t.Helper()
	captured, err := captureInto(t, stoppedCluster(t), testOptions())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	order, content = readBundle(t, captured)
	return
}

// captureDisks captures a VirtualMachine that has disks, staging them in a
// directory of their own so that a test can see what was in it.
func captureWithDisks(t *testing.T, c *fakeCluster, opt Options) (order []string, content map[string][]byte) {
	t.Helper()
	captured, err := captureInto(t, c, stagingIn(t, c, opt))
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	order, content = readBundle(t, captured)
	return
}

// stagingIn points a capture and the export that feeds it at the same staging
// directory.
func stagingIn(t *testing.T, c *fakeCluster, opt Options) (staging Options) {
	t.Helper()
	staging = opt
	staging.Scratch = t.TempDir()
	c.export.scratch = staging.Scratch
	return
}

// captureInto captures into a file and returns whatever was written to it.
func captureInto(t *testing.T, c cluster.Cluster, opt Options) (captured []byte, err error) {
	t.Helper()
	_, captured, err = captureRecording(t, c, opt)
	return
}

// captureRecording captures into a file and returns the capture record along
// with whatever was written.
//
// The destination is a file rather than a buffer because a bundle's manifest
// is written into room reserved at the front, which a stream cannot offer.
func captureRecording(
	t *testing.T,
	c cluster.Cluster,
	opt Options) (record bundle.CaptureRecord, captured []byte, err error) {
	//
	t.Helper()
	dest := filepath.Join(t.TempDir(), "db-01.bundle")
	file, createErr := os.Create(dest)
	if createErr != nil {
		t.Fatalf("create bundle: %v", createErr)
	}
	defer func() {
		_ = file.Close()
	}()
	record, err = Export(context.TODO(), c, opt, file)
	captured, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read bundle: %v", readErr)
	}
	return
}

// clusterWithTwoDisks builds a cluster whose VirtualMachine has three volumes
// over two claims, one of which two volumes share.
func clusterWithTwoDisks(t *testing.T) (c *fakeCluster) {
	t.Helper()
	c = clusterWithDisks(t,
		[]volume{
			{name: "rootdisk", claim: rootClaim},
			{name: "datadisk", claim: dataClaim},
			{name: "rootdisk-again", claim: rootClaim},
		},
		[]disk{
			{claim: rootClaim, content: rootContent},
			{claim: dataClaim, content: dataContent},
		})
	return
}

// sha256Of renders content's digest the way a manifest records it.
func sha256Of(content []byte) (sum string) {
	hashed := sha256.Sum256(content)
	sum = "sha256:" + hex.EncodeToString(hashed[:])
	return
}

// readBundle untars a captured bundle the way an ordinary tool would, and
// returns its members in the order they appear along with their content.
func readBundle(t *testing.T, captured []byte) (order []string, content map[string][]byte) {
	t.Helper()
	content = map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(captured))
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read bundle: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read member %q: %v", header.Name, err)
		}
		order = append(order, header.Name)
		content[header.Name] = body
	}
	return
}

// capturedManifest decodes the manifest out of a captured bundle.
func capturedManifest(t *testing.T, content map[string][]byte) (manifest *bundle.Manifest) {
	t.Helper()
	manifest = &bundle.Manifest{}
	err := yaml.Unmarshal(content[bundle.ManifestPath], manifest)
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return
}

// decodeMember decodes one carried object out of a captured bundle.
func decodeMember(t *testing.T, content map[string][]byte, path string) (object map[string]any) {
	t.Helper()
	body, found := content[path]
	if !found {
		t.Fatalf("bundle has no member %q", path)
	}
	object = map[string]any{}
	err := yaml.Unmarshal(body, &object)
	if err != nil {
		t.Fatalf("decode member %q: %v", path, err)
	}
	return
}

// nested walks into a decoded object, failing the test if the path is absent.
func nested(t *testing.T, object map[string]any, keys ...string) (found map[string]any) {
	t.Helper()
	found = object
	for _, key := range keys {
		next, ok := found[key].(map[string]any)
		if !ok {
			t.Fatalf("object has no map at %q", key)
		}
		found = next
	}
	return
}

// withSourceCluster returns the members a bundle holds when the given
// resources were collected and the given disks staged. What describes the
// source cluster sits between the two, which is where a capture writes it:
// after the referenced resources have been read and before any disk arrives.
func withSourceCluster(resources []string, disks ...string) (order []string) {
	order = append(order, resources...)
	order = append(order, testKubeVirtMember, testExpandedMember)
	order = append(order, disks...)
	return
}

// entryNames lists what a directory holds.
func entryNames(t *testing.T, dir string) (names []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return
}
