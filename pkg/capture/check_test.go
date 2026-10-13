package capture

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/cluster"
)

// The provisioned sizes of the two disks the fixture VirtualMachine has, as
// their claims give them, and what a capture of them would cost: the largest
// single disk in local scratch space, because disks are staged one at a time,
// and the two together in transfer.
const (
	testRootSize    = "32Gi"
	testDataSize    = "100Gi"
	testScratch     = "100.0 GiB"
	testProvisioned = "132.0 GiB"
	testRootSpace   = "32.0 GiB"
)

func TestCheckNamesEveryObjectACaptureWouldCollect(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	checked, err := runCheck(t, checkableCluster(t), testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(checked).To(gomega.MatchRegexp(`VirtualMachine\s+db-01\n`))
	g.Expect(checked).To(gomega.MatchRegexp(`PersistentVolumeClaim\s+db-01-root\n`))
	g.Expect(checked).To(gomega.MatchRegexp(`PersistentVolumeClaim\s+db-01-data\n`))
	g.Expect(checked).To(gomega.MatchRegexp(`StorageClass\s+` + testStorageClass + `\n`))
	g.Expect(checked).To(gomega.MatchRegexp(`StorageProfile\s+` + testStorageClass + `\n`))
	// KubeVirt's configuration lives in a namespace of its own, which is how
	// a reader spots an object from outside the one being captured from.
	g.Expect(checked).To(gomega.MatchRegexp(`KubeVirt\s+kubevirt/kubevirt\n`))
	// The expansion is recorded under a kind of its own, so it does not read
	// as the VirtualMachine a second time.
	g.Expect(checked).To(gomega.MatchRegexp(`ExpandedSpec\s+db-01\n`))
	g.Expect(checked).To(gomega.ContainSubstring("Would be skipped (0)"))
}

func TestCheckKeepsWhatATargetWouldHaveToProvideApartFromWhatWouldBeCollected(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	checked, err := runCheck(t, checkableCluster(t), testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// A StorageClass is collected because quo read it and a requirement
	// because a target has to have one. Both are true at once, and a
	// requirement is the one thing a bundle deliberately does not hold.
	g.Expect(checked).To(gomega.MatchRegexp(
		`Would be required from a target \(1\)\n` +
			`\s+StorageClass\s+` + testStorageClass + `\n`))
}

func TestCheckTransfersNoDiskDataAndCreatesNoVirtualMachineExport(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := checkableCluster(t)

	_, err := runCheck(t, c, testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// A check that created an export would have asked the cluster to stage
	// hundreds of gigabytes to answer a question about permissions.
	g.Expect(c.export.created).To(gomega.Equal(0))
	g.Expect(c.export.opened).To(gomega.BeEmpty())
	g.Expect(c.export.closed).To(gomega.Equal(0))
}

func TestCheckReportsTheScratchSpaceACaptureNeedsAndTheTotalItWouldTransfer(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	checked, err := runCheck(t, checkableCluster(t), testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Each disk is given the size its claim gives, which is the figure a
	// reader will see again from kubectl.
	g.Expect(checked).To(gomega.MatchRegexp(`db-01-root\s+` + testRootSize + `\s+volumes: rootdisk`))
	g.Expect(checked).To(gomega.MatchRegexp(`db-01-data\s+` + testDataSize + `\s+volumes: datadisk`))
	// Scratch space is the largest single disk rather than the total,
	// because a capture stages one disk at a time.
	g.Expect(checked).To(gomega.MatchRegexp(
		`Scratch space needed:\s+` + regexp.QuoteMeta(testScratch)))
	g.Expect(checked).To(gomega.MatchRegexp(
		`Total disk size:\s+` + regexp.QuoteMeta(testProvisioned)))
}

func TestCheckReportsEveryForbiddenReadAndStillSucceeds(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := checkableCluster(t)
	// What a namespace owner without cluster read sees: the claims in their
	// own namespace are theirs, and nothing behind them is.
	forbidden := []cluster.ObjectRef{
		clusterRef(cluster.KindStorageClass, testStorageClass),
		clusterRef(cluster.KindStorageProfile, testStorageClass),
		clusterRef(cluster.KindPersistentVolume, testRootPV),
	}
	for _, object := range forbidden {
		c.refuse(object, refusal(object.Kind, object.Name, cluster.ErrForbidden))
	}

	checked, err := runCheck(t, c, testOptions())

	// A check that found something it could not read has done the job it
	// exists for, so it succeeds. The exit status says whether quo could
	// report, not whether it liked what it found.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(checked).To(gomega.ContainSubstring("Would be skipped (3)"))
	g.Expect(checked).To(gomega.MatchRegexp(`StorageClass\s+` + testStorageClass + `\s+forbidden`))
	g.Expect(checked).To(gomega.MatchRegexp(`StorageProfile\s+` + testStorageClass + `\s+forbidden`))
	g.Expect(checked).To(gomega.MatchRegexp(`PersistentVolume\s+` + testRootPV + `\s+forbidden`))
	// The message is what carries the detail a permissions problem is fixed
	// from.
	g.Expect(checked).To(gomega.ContainSubstring(cluster.ErrForbidden.Error()))
}

func TestCheckGivesASkippedObjectTheSameReasonVocabularyACaptureRecordUses(t *testing.T) {
	tests := []struct {
		name   string
		failed error
		want   string
	}{
		{
			name:   "an object the caller is not allowed to read is forbidden",
			failed: cluster.ErrForbidden,
			want:   "forbidden",
		},
		{
			name:   "an object the cluster does not have is not found",
			failed: cluster.ErrNotFound,
			want:   "not found",
		},
		{
			name:   "a kind the cluster serves no resource for is not installed",
			failed: cluster.ErrNotInstalled,
			want:   "not installed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := checkableCluster(t)
			object := clusterRef(cluster.KindStorageClass, testStorageClass)
			c.refuse(object, refusal(object.Kind, object.Name, tt.failed))

			checked, err := runCheck(t, c, testOptions())

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(checked).To(gomega.MatchRegexp(
				`StorageClass\s+` + testStorageClass + `\s+` + tt.want))
		})
	}
}

func TestCheckSaysAVirtualMachineThatIsRunningWouldBeRefused(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	checked, err := runCheck(t, clusterWithStatus(t, "Running"), testOptions())

	// Being asked about a running VirtualMachine is not a failure of the
	// check: saying that a capture would refuse it is the most useful thing
	// a check can say about one.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(checked).To(gomega.MatchRegexp(`Status:\s+Running`))
	g.Expect(checked).To(gomega.ContainSubstring("virtctl stop -n support-cases db-01"))
	g.Expect(checked).To(gomega.MatchRegexp(`Capture:\s+refused`))
	// It says what a capture would collect either way, so that stopping the
	// VirtualMachine and capturing holds no further surprises.
	g.Expect(checked).To(gomega.MatchRegexp(`VirtualMachine\s+db-01\n`))
}

func TestCheckSaysAStoppedVirtualMachineWouldBeCaptured(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	checked, err := runCheck(t, stoppedCluster(t), testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(checked).To(gomega.MatchRegexp(`Status:\s+Stopped`))
	g.Expect(checked).To(gomega.MatchRegexp(`Capture:\s+would run`))
}

func TestCheckReportsADiskWhoseClaimItCouldNotReadAsAnUnknownSize(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := checkableCluster(t)
	object := ref(cluster.KindPersistentVolumeClaim, dataClaim)
	c.refuse(object, refusal(object.Kind, object.Name, cluster.ErrForbidden))

	checked, err := runCheck(t, c, testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(checked).To(gomega.MatchRegexp(`db-01-data\s+\(unknown\)`))
	// A claim quo was not allowed to read leaves the totals a lower bound
	// rather than a number it can stand behind. The disk is still in the
	// capture, so dropping it from the total would understate the transfer.
	g.Expect(checked).To(gomega.MatchRegexp(
		`Scratch space needed:\s+at least ` + regexp.QuoteMeta(testRootSpace)))
	g.Expect(checked).To(gomega.MatchRegexp(
		`Total disk size:\s+at least ` + regexp.QuoteMeta(testRootSpace)))
}

func TestCheckSaysWhenTheVirtualMachineHasNoDisks(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	checked, err := runCheck(t, stoppedCluster(t), testOptions())

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(checked).To(gomega.ContainSubstring("Disks (0)"))
}

func TestCheckFailsOnlyWhenItCouldNotReportAtAll(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	// A VirtualMachine quo cannot read is the one failure a check has no
	// answer to give around: everything it reports comes from that object.
	c.vmErr = fmt.Errorf("virtualmachines.kubevirt.io %q: %w", testName, cluster.ErrForbidden)

	_, err := runCheck(t, c, testOptions())

	g.Expect(err).To(gomega.HaveOccurred())
}

// runCheck checks a cluster and returns what the check wrote.
func runCheck(t *testing.T, c cluster.Cluster, opt Options) (checked string, err error) {
	t.Helper()
	described := &bytes.Buffer{}
	err = Check(context.TODO(), c, opt, described)
	checked = described.String()
	return
}

// checkableCluster builds the cluster a clean check runs against: a stopped
// VirtualMachine with two disks of different sizes, everything behind them
// readable.
func checkableCluster(t *testing.T) (c *fakeCluster) {
	t.Helper()
	c = provisionedCluster(t)
	attach(t, c.vm, []volume{
		{name: "rootdisk", claim: rootClaim},
		{name: "datadisk", claim: dataClaim},
	})
	boundClaim(t, c, rootClaim, testRootPV, testRootSize)
	boundClaim(t, c, dataClaim, testDataPV, testDataSize)
	return
}

// boundClaim puts a PersistentVolumeClaim bound at the given capacity in the
// cluster, along with the PersistentVolume behind it.
func boundClaim(t *testing.T, c *fakeCluster, name, bound, capacity string) {
	t.Helper()
	c.hold(ref(cluster.KindPersistentVolumeClaim, name), map[string]any{
		"spec": map[string]any{
			"storageClassName": testStorageClass,
			"volumeName":       bound,
		},
		"status": map[string]any{
			"capacity": map[string]any{"storage": capacity},
		},
	})
	c.hold(clusterRef(cluster.KindPersistentVolume, bound), nil)
}
