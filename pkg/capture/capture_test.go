package capture

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
)

// testVMMember is where the fixture VirtualMachine lands in a bundle.
const testVMMember = "objects/virtualmachine/db-01.yaml"

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

	g.Expect(order).To(gomega.Equal([]string{
		bundle.ManifestPath,
		testVMMember,
	}))
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

			captured := &bytes.Buffer{}
			err := Export(context.TODO(), clusterWithStatus(t, tt.status), testOptions(), captured)

			if tt.captured {
				g.Expect(err).ToNot(gomega.HaveOccurred())
				g.Expect(captured.Len()).To(gomega.BeNumerically(">", 0))
				return
			}
			g.Expect(err).To(gomega.MatchError(&NotStoppedError{}))
			// The refusal names the VirtualMachine and says what to do.
			g.Expect(err.Error()).To(gomega.ContainSubstring(testNamespace + "/" + testName))
			g.Expect(err.Error()).To(gomega.ContainSubstring("Stop it"))
			// Nothing is written when quo refuses.
			g.Expect(captured.Len()).To(gomega.Equal(0))
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

			captured := &bytes.Buffer{}
			err := Export(context.TODO(), tt.c, testOptions(), captured)

			g.Expect(err).To(gomega.HaveOccurred())
			g.Expect(captured.Len()).To(gomega.Equal(0))
		})
	}
}

func TestExportToFileWritesABundleThatReadsBack(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "db-01.bundle")

	err := ExportToFile(context.TODO(), stoppedCluster(t), testOptions(), dest)

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

			err := ExportToFile(context.TODO(), tt.c, testOptions(), dest)

			g.Expect(err).To(gomega.HaveOccurred())
			_, statErr := os.Stat(dest)
			g.Expect(os.IsNotExist(statErr)).To(gomega.BeTrue())
			// Not even a partial file is left to be mistaken for a bundle.
			g.Expect(entryNames(t, dir)).To(gomega.BeEmpty())
		})
	}
}

// captureBundle captures the fixture VirtualMachine and returns the members
// of the bundle that came out, in the order they appear.
func captureBundle(t *testing.T) (order []string, content map[string][]byte) {
	t.Helper()
	captured := &bytes.Buffer{}
	err := Export(context.TODO(), stoppedCluster(t), testOptions(), captured)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	order, content = readBundle(t, captured.Bytes())
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
