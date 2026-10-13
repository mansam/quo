package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

func TestExportRecordsWhatBecameOfEveryObjectItAttempted(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := referentialCluster(t)

	record, captured, err := captureRecording(t, c, stagingIn(t, c, testOptions()))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	_, content := readBundle(t, captured)
	manifest := capturedManifest(t, content)
	// The VirtualMachine the capture had already read, then every object it
	// went to the cluster for in the order it asked, then what describes the
	// source cluster and is not addressed by reference at all, and last what
	// the bundle needs a target to provide.
	g.Expect(attempted(manifest)).To(gomega.Equal(append(
		append([]cluster.ObjectRef{ref(cluster.KindVirtualMachine, testName)}, c.requested...),
		refIn(cluster.KindKubeVirt, testKubeVirtNamespace, testKubeVirtName),
		ref(bundle.KindExpandedSpec, testName),
		ref(cluster.KindNetworkAttachmentDefinition, testNetwork))))
	// Everything the capture read was collected, and the network the
	// VirtualMachine expects is a requirement as well: the bundle holds the
	// attachment to be read, and a target still has to offer a network of its
	// own.
	countOf := map[bundle.Outcome]int{}
	for i := range manifest.CaptureRecord {
		countOf[manifest.CaptureRecord[i].Outcome]++
	}
	g.Expect(countOf).To(gomega.Equal(map[bundle.Outcome]int{
		bundle.OutcomeCollected: 1 + len(c.requested) + 2,
		bundle.OutcomeRequired:  1,
	}))
	// The record the capture reports is the record the bundle carries, so a
	// reader of either is looking at the same thing.
	g.Expect(record).To(gomega.Equal(manifest.CaptureRecord))
	g.Expect(record.Skipped()).To(gomega.BeEmpty())
}

func TestExportContinuesWhenAnObjectCannotBeRead(t *testing.T) {
	tests := []struct {
		name string
		// failed is what the cluster reports in place of the ConfigMap.
		failed error
		want   bundle.SkipReason
	}{
		{
			name:   "an object the capture is not allowed to read is recorded as forbidden",
			failed: refusal(cluster.KindConfigMap, testConfigMap, cluster.ErrForbidden),
			want:   bundle.SkipForbidden,
		},
		{
			name:   "an object the cluster does not have is recorded as not found",
			failed: refusal(cluster.KindConfigMap, testConfigMap, cluster.ErrNotFound),
			want:   bundle.SkipNotFound,
		},
		{
			name:   "a kind the cluster serves nothing for is recorded as not installed",
			failed: fmt.Errorf("%s: %w", cluster.KindConfigMap, cluster.ErrNotInstalled),
			want:   bundle.SkipNotInstalled,
		},
		{
			name:   "any other failure to read is recorded as unreadable",
			failed: errors.New("read tcp 10.0.0.1:6443: connection reset by peer"),
			want:   bundle.SkipUnreadable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := partlyReadableCluster(t, tt.failed)

			record, captured, err := captureRecording(t, c, stagingIn(t, c, testOptions()))

			// One object quo could not read is not worth failing a capture
			// that may have taken hours over.
			g.Expect(err).ToNot(gomega.HaveOccurred())
			// The bundle that came out is whole: every member matches the
			// digest the manifest records for it, and nothing is indexed
			// that the bundle does not hold.
			manifest, readErr := bundle.Read(bytes.NewReader(captured))
			g.Expect(readErr).ToNot(gomega.HaveOccurred())
			g.Expect(manifest.CaptureRecord.Skipped()).To(gomega.Equal([]bundle.Attempt{
				{
					Kind:      cluster.KindConfigMap,
					Namespace: testNamespace,
					Name:      testConfigMap,
					Outcome:   bundle.OutcomeSkipped,
					Reason:    tt.want,
					Message:   tt.failed.Error(),
				},
			}))
			g.Expect(record.Skipped()).To(gomega.Equal(manifest.CaptureRecord.Skipped()))
			// Everything else was collected, disks included.
			g.Expect(collected(manifest)).To(gomega.Equal(append(
				[]cluster.ObjectRef{
					ref(cluster.KindVirtualMachine, testName),
					ref(cluster.KindPersistentVolumeClaim, rootClaim),
					ref(cluster.KindSecret, testSecret),
				},
				sourceCluster()...)))
			g.Expect(manifest.Disks).To(gomega.HaveLen(1))
			_, content := readBundle(t, captured)
			g.Expect(content).ToNot(
				gomega.HaveKey(bundle.ResourcePath(cluster.KindConfigMap, testConfigMap)))
		})
	}
}

func TestExportTellsAnObjectThatNeverExistedApartFromOneItWasNotAllowedToRead(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{
			"name":    "sysprep",
			"sysprep": map[string]any{"configMap": map[string]any{"name": testConfigMap}},
		},
		map[string]any{
			"name": "cloudinit",
			"cloudInitNoCloud": map[string]any{
				"secretRef": map[string]any{"name": testSecret},
			},
		},
	}, "spec", "template", "spec", "volumes")
	// The cluster holds neither, but for different reasons: the ConfigMap is
	// simply not there, and the Secret is one this caller cannot read.
	c.refuse(
		ref(cluster.KindSecret, testSecret),
		refusal(cluster.KindSecret, testSecret, cluster.ErrForbidden))

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	reasonByName := map[string]bundle.SkipReason{}
	for _, attempt := range manifest.CaptureRecord.Skipped() {
		reasonByName[attempt.Name] = attempt.Reason
	}
	g.Expect(reasonByName).To(gomega.Equal(map[string]bundle.SkipReason{
		testConfigMap: bundle.SkipNotFound,
		testSecret:    bundle.SkipForbidden,
	}))
}

func TestExportRecordsASkippedObjectWithWhatTheClusterSaidAboutIt(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	denial := errors.New(
		`configmaps "db-01-sysprep" is forbidden: ` +
			`User "sam" cannot get resource "configmaps" in API group "" in the namespace "support-cases"`)
	c := partlyReadableCluster(t, denial)

	_, content := captureWithDisks(t, c, stagingIn(t, c, testOptions()))

	manifest := capturedManifest(t, content)
	// The missing permission is the actionable part, and only the cluster
	// knows it.
	g.Expect(manifest.CaptureRecord.Skipped()).To(gomega.HaveLen(1))
	g.Expect(manifest.CaptureRecord.Skipped()[0].Message).To(gomega.Equal(denial.Error()))
}

func TestExportAbortsWhenTheCaptureIsCancelledRatherThanRecordingTheRestAsUnreadable(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := referentialCluster(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := filepath.Join(t.TempDir(), "db-01.bundle")
	file, err := os.Create(dest)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	defer func() {
		_ = file.Close()
	}()

	record, err := Export(ctx, c, testOptions(), file)

	// Best effort covers one object quo could not read, not a capture that
	// has stopped asking. A record saying every object was unreadable would
	// describe something that did not happen.
	g.Expect(err).To(gomega.MatchError(context.Canceled))
	g.Expect(record.Skipped()).To(gomega.BeEmpty())
	g.Expect(c.requested).To(gomega.HaveLen(1))
}

// partlyReadableCluster builds a cluster whose stopped VirtualMachine has a
// disk, a Secret and a ConfigMap, and which reports failed in place of the
// ConfigMap.
func partlyReadableCluster(t *testing.T, failed error) (c *fakeCluster) {
	t.Helper()
	c = stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{
			"name":                  "rootdisk",
			"persistentVolumeClaim": map[string]any{"claimName": rootClaim},
		},
		map[string]any{
			"name":    "sysprep",
			"sysprep": map[string]any{"configMap": map[string]any{"name": testConfigMap}},
		},
		map[string]any{
			"name": "cloudinit",
			"cloudInitNoCloud": map[string]any{
				"secretRef": map[string]any{"name": testSecret},
			},
		},
	}, "spec", "template", "spec", "volumes")
	c.hold(ref(cluster.KindPersistentVolumeClaim, rootClaim), nil)
	c.hold(ref(cluster.KindSecret, testSecret), nil)
	c.refuse(ref(cluster.KindConfigMap, testConfigMap), failed)
	c.offer(rootClaim)
	return
}

// refusal builds the error a cluster reports for an object it will not hand
// over, shaped the way the real adapter shapes one.
func refusal(kind, name string, reason error) (err error) {
	err = fmt.Errorf("%s %q: %w", kind, name, reason)
	return
}

// attempted returns the objects a capture record says were attempted.
func attempted(manifest *bundle.Manifest) (refs []cluster.ObjectRef) {
	for i := range manifest.CaptureRecord {
		attempt := manifest.CaptureRecord[i]
		refs = append(refs, cluster.ObjectRef{
			Kind:      attempt.Kind,
			Namespace: attempt.Namespace,
			Name:      attempt.Name,
		})
	}
	return
}
