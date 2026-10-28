package deploy

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

func TestDeployPutsEveryDiskBackAsAnUploadDataVolume(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The record says what the run did: one DataVolume per disk the bundle
	// holds, in the order the machine's volumes reference their claims,
	// and the VirtualMachine last.
	g.Expect(record).To(gomega.Equal(Record{
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testRootClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testDataClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testGapClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testThinClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
			Outcome:   OutcomeCreated,
		},
	}))
	// Four DataVolumes and the VirtualMachine, and only those: the bundle
	// holds no other object a deploy creates.
	g.Expect(fake.created).To(gomega.HaveLen(5))
	g.Expect(fake.created[0].Object).To(gomega.Equal(
		expectedDataVolume(t, path, testRootClaim, "32Gi", "Block", "kubevirt").Object))
	g.Expect(fake.created[1].Object).To(gomega.Equal(
		expectedDataVolume(t, path, testDataClaim, "100Gi", "Filesystem", "archive").Object))
	g.Expect(fake.created[2].GetKind()).To(gomega.Equal(cluster.KindDataVolume))
	g.Expect(fake.created[2].GetName()).To(gomega.Equal(testGapClaim))
	g.Expect(fake.created[3].GetKind()).To(gomega.Equal(cluster.KindDataVolume))
	g.Expect(fake.created[3].GetName()).To(gomega.Equal(testThinClaim))
	// The bytes that arrived are the bundle's, streamed unchanged: what
	// the target received is the member's content, at its recorded size.
	// The tar.gz form passes through as it is, with the DataVolume's
	// content type saying what it holds.
	g.Expect(fake.uploaded).To(gomega.HaveLen(4))
	for i, claim := range []string{testRootClaim, testDataClaim, testGapClaim, testThinClaim} {
		format := bundle.DiskGzip
		if claim == testDataClaim {
			format = bundle.DiskArchiveGzip
		}
		member := memberOf(t, path, bundle.DiskPath(claim, format))
		g.Expect(fake.uploaded[i].namespace).To(gomega.Equal(testDeployNamespace))
		g.Expect(fake.uploaded[i].name).To(gomega.Equal(claim))
		g.Expect(fake.uploaded[i].content).To(gomega.Equal(member))
		g.Expect(fake.uploaded[i].size).To(gomega.BeEquivalentTo(len(member)))
	}
	// The VirtualMachine that landed is the expanded spec carrying the
	// forced rewrites, with the dataVolumeTemplates removed — they became
	// standalone DataVolumes — and the volume stanzas unchanged, so a
	// volume referencing a claim by name finds the claim the DataVolume
	// creates and a volume of type dataVolume names the DataVolume
	// directly.
	created := fake.created[4]
	g.Expect(created.GetKind()).To(gomega.Equal(cluster.KindVirtualMachine))
	_, found, _ := unstructured.NestedFieldNoCopy(created.Object, "spec", "dataVolumeTemplates")
	g.Expect(found).To(gomega.BeFalse())
	volumes, _, _ := unstructured.NestedSlice(created.Object, "spec", "template", "spec", "volumes")
	declared, _, _ := unstructured.NestedSlice(
		disksExpandedSpec().Object, "spec", "template", "spec", "volumes")
	g.Expect(volumes).To(gomega.Equal(declared))
}

func TestDeployCreatesTheDataVolumesThenUploadsThenTheVirtualMachineLast(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t)
	fake := storageTarget()

	_, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Nothing in the namespace looks like a VirtualMachine until its
	// disks are in place: every DataVolume is created, then every disk is
	// uploaded, then the machine. Disks upload one at a time.
	g.Expect(fake.events).To(gomega.Equal([]string{
		"create DataVolume " + testDeployNamespace + "/" + testRootClaim,
		"create DataVolume " + testDeployNamespace + "/" + testDataClaim,
		"create DataVolume " + testDeployNamespace + "/" + testGapClaim,
		"create DataVolume " + testDeployNamespace + "/" + testThinClaim,
		"upload " + testDeployNamespace + "/" + testRootClaim,
		"upload " + testDeployNamespace + "/" + testDataClaim,
		"upload " + testDeployNamespace + "/" + testGapClaim,
		"upload " + testDeployNamespace + "/" + testThinClaim,
		"create VirtualMachine " + testDeployNamespace + "/" + testVirtualMachineName,
	}))
}

func TestDeployRecordsADiskTheBundleHoldsNoBytesFor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t, testGapClaim)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// A disk the bundle holds no bytes for creates nothing, the gap is
	// recorded, and the run continues past it: the machine and the disks
	// that were held all arrive, and the error is what says the deploy was
	// a partial one.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("4 of the 5 objects"))
	g.Expect(record).To(gomega.HaveLen(5))
	g.Expect(record[2].Name).To(gomega.Equal(testGapClaim))
	g.Expect(record[2].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record[2].Reason).To(gomega.Equal("the bundle holds no bytes for it"))
	g.Expect(fake.created).To(gomega.HaveLen(4))
	g.Expect(fake.uploaded).To(gomega.HaveLen(3))
	g.Expect(fake.uploaded[2].name).To(gomega.Equal(testThinClaim))
}

func TestDeployRecordsADiskTheBundleCannotSize(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeUnsizedDiskBundle(t)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The bytes are held but nothing says how big a target has to make
	// them, and quo will not guess: the disk creates nothing, the gap is
	// recorded, and the run continues to the machine.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("1 of the 2 objects"))
	g.Expect(record).To(gomega.HaveLen(2))
	g.Expect(record[0].Name).To(gomega.Equal(testThinClaim))
	g.Expect(record[0].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record[0].Reason).To(gomega.Equal("the bundle records no size for it"))
	g.Expect(fake.created).To(gomega.HaveLen(1))
	g.Expect(fake.uploaded).To(gomega.BeEmpty())
}

func TestDeployRefusesATargetThatCannotTakeTheDisks(t *testing.T) {
	path := writeDisksFixtureBundle(t)
	// A target with nothing held: no CDI, no storage class, no account of
	// what the class can provide.
	bare := target()
	// A target with CDI but not the class the bundle records.
	noClass := target()
	noClass.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	// A target whose account of the class rules out the volume mode the
	// root disk needs.
	filesystemOnly := target()
	filesystemOnly.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	filesystemOnly.hold(cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testStorageClass}, storageClass())
	filesystemOnly.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: testStorageClass},
		storageProfile("Filesystem"))

	tests := []struct {
		name   string
		target *fakeTarget
		want   error
		names  string
	}{
		{
			name:   "a target with no CDI, from a bundle holding disks to upload",
			target: bare,
			want:   &NoCDIError{},
			names:  "install CDI on the target cluster first",
		},
		{
			name:   "a disk whose storage class the target does not have",
			target: noClass,
			want:   &MissingStorageClassError{},
			names:  testStorageClass,
		},
		{
			name:   "a disk whose volume mode the matched class cannot provide",
			target: filesystemOnly,
			want:   &UnsupportedVolumeModeError{},
			names:  "Block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			record, err := Deploy(context.TODO(), tt.target, testOptions(path))

			g.Expect(err).To(gomega.MatchError(tt.want))
			// The refusal names what it found.
			g.Expect(err.Error()).To(gomega.ContainSubstring(tt.names))
			// It refused before creating anything, so nothing was
			// attempted and nothing was created: disks are not uploaded
			// that cannot be placed.
			g.Expect(record).To(gomega.BeEmpty())
			g.Expect(tt.target.created).To(gomega.BeEmpty())
			g.Expect(tt.target.uploaded).To(gomega.BeEmpty())
		})
	}
}

func TestDeployRecordsADiskThatFailsItsDigestAndGoesOn(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// The root disk's bytes changed in transit. The claim's name is in
	// every line of the member's stand-in content, so replacing it keeps
	// the tar the manifest indexes aligned while changing what the digest
	// is computed over.
	corrupted := bytes.ReplaceAll(
		readFile(t, writeDisksFixtureBundle(t)),
		[]byte("disk of "+testRootClaim),
		[]byte("disk 0f "+testRootClaim))
	path := writeBytes(t, "corrupted-disks.quo", corrupted)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The run ends with what it managed and the error says how much of it
	// arrived.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("4 of the 5 objects"))
	g.Expect(record).To(gomega.HaveLen(5))
	// The corrupted disk is skipped and recorded: what was deployed for
	// it is not what the bundle holds. The VirtualMachine is created too,
	// because a member that fails its digest is a recorded gap rather
	// than a refusal — the expansion itself arrived whole.
	g.Expect(record[0].Name).To(gomega.Equal(testRootClaim))
	g.Expect(record[0].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record[0].Reason).To(gomega.ContainSubstring("does not match its digest"))
	g.Expect(record[4].Outcome).To(gomega.Equal(OutcomeCreated))
	// The disks behind it proceeded: their DataVolumes were created and
	// their bytes uploaded.
	g.Expect(fake.uploaded).To(gomega.HaveLen(3))
	g.Expect(fake.uploaded[0].name).To(gomega.Equal(testDataClaim))
}

func TestDeployRecordsAnUploadThatFailedAndGoesOn(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t)
	fake := storageTarget()
	fake.uploadErrFor[testRootClaim] = errUploadRefused

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("4 of the 5 objects"))
	g.Expect(record).To(gomega.HaveLen(5))
	// The failed upload is recorded with its reason, and the DataVolume
	// step says failed rather than created: the target holds the
	// DataVolume, but it does not hold the disk.
	g.Expect(record[0].Name).To(gomega.Equal(testRootClaim))
	g.Expect(record[0].Outcome).To(gomega.Equal(OutcomeFailed))
	g.Expect(record[0].Reason).To(gomega.ContainSubstring("the upload failed"))
	g.Expect(record[0].Reason).To(gomega.ContainSubstring("connection reset by peer"))
	// The disks behind it proceeded, and the VirtualMachine was created
	// last whatever became of them.
	g.Expect(fake.uploaded).To(gomega.HaveLen(3))
	g.Expect(fake.uploaded[0].name).To(gomega.Equal(testDataClaim))
	g.Expect(record[4].Outcome).To(gomega.Equal(OutcomeCreated))
}

func TestDeployReportsTheProgressOfEachUpload(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t)
	fake := storageTarget()
	var reported []Progress
	opt := testOptions(path)
	opt.Progress = func(p Progress) {
		reported = append(reported, p)
	}

	_, err := Deploy(context.TODO(), fake, opt)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Each uploaded disk is reported as started and as finished, with the
	// volumes it serves and its position among the disks of the deploy —
	// the same numbering the deploy record uses.
	done := 0
	for i := range reported {
		if !reported[i].Done {
			continue
		}
		done++
		switch done {
		case 1:
			g.Expect(reported[i].Claim).To(gomega.Equal(testRootClaim))
			g.Expect(reported[i].Volumes).To(gomega.Equal([]string{"root", "logs"}))
			g.Expect(reported[i].Disk).To(gomega.Equal(1))
			g.Expect(reported[i].Transferred).To(gomega.BeEquivalentTo(
				len(memberOf(t, path, bundle.DiskPath(testRootClaim, bundle.DiskGzip)))))
		case 2:
			g.Expect(reported[i].Claim).To(gomega.Equal(testDataClaim))
			g.Expect(reported[i].Volumes).To(gomega.Equal([]string{"data"}))
			g.Expect(reported[i].Disk).To(gomega.Equal(2))
		case 3:
			g.Expect(reported[i].Claim).To(gomega.Equal(testGapClaim))
			g.Expect(reported[i].Disk).To(gomega.Equal(3))
		case 4:
			g.Expect(reported[i].Claim).To(gomega.Equal(testThinClaim))
			g.Expect(reported[i].Disk).To(gomega.Equal(4))
		}
	}
	g.Expect(done).To(gomega.Equal(4))
	for i := range reported {
		g.Expect(reported[i].Disks).To(gomega.Equal(4))
	}
}

// errUploadRefused is what a transfer that died on the way to the upload
// proxy looks like.
var errUploadRefused = errors.New(
	"write tcp 127.0.0.1:44321->127.0.0.1:8443: connection reset by peer")

// expectedDataVolume states what the target should hold for one of the
// disk fixtures' claims: the DataVolume that receives the disk, built from
// the requirement the bundle records. The rewrites are stated here as
// literal values rather than by calling the code under test, so this is an
// independent statement of the contract rather than a restatement of it.
func expectedDataVolume(
	t *testing.T,
	path, claim, size, volumeMode, contentType string) (expected *unstructured.Unstructured) {
	//
	t.Helper()
	expected = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataVolume",
		"metadata": map[string]any{
			"name":      claim,
			"namespace": testDeployNamespace,
		},
		"spec": map[string]any{
			// A claim cannot be populated on its own, because CDI's
			// upload works through a DataVolume.
			"source":      map[string]any{"upload": map[string]any{}},
			"contentType": contentType,
			"storage": map[string]any{
				"resources":        map[string]any{"requests": map[string]any{"storage": size}},
				"storageClassName": testStorageClass,
				"volumeMode":       volumeMode,
			},
		},
	}}
	expected.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "quo",
		"quo.mansam.dev/bundle":        bundleLabelOf(t, path),
	})
	// The machine is deployed stopped, so nothing will ever be the first
	// consumer a WaitForFirstConsumer class waits for, and the upload
	// cannot start until the claim is bound.
	expected.SetAnnotations(map[string]string{
		"quo.mansam.dev/bundle-file":                       path,
		"cdi.kubevirt.io/storage.bind.immediate.requested": "",
	})
	return
}
