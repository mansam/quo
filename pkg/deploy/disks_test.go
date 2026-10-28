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
	g.Expect(record.Steps).To(gomega.Equal([]Step{
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

func TestDeployReportsAStorageClassMatchedByName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// A mapping is automatic when the target has a storage class of the
	// requirement's own name, and it is reported: nothing maps silently.
	// Every disk of the fixture asks for the one class.
	g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
		{
			Kind: "storage class",
			From: testStorageClass,
			To:   testStorageClass,
			How:  "by name",
		},
	}))
	// The disks landed on the class of the same name.
	for i := range fake.created {
		if fake.created[i].GetKind() != cluster.KindDataVolume {
			continue
		}
		class, _, _ := unstructured.NestedString(
			fake.created[i].Object, "spec", "storage", "storageClassName")
		g.Expect(class).To(gomega.Equal(testStorageClass))
	}
}

func TestDeployPlacesTheDisksOfOneClassWhereAMappingSays(t *testing.T) {
	tests := []struct {
		name  string
		holds bool
	}{
		{
			name:  "a target without the class the bundle records",
			holds: false,
		},
		{
			name:  "a target holding the class the bundle records, which the mapping overrides",
			holds: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			path := writeDisksFixtureBundle(t)
			fake := target()
			fake.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
			if tt.holds {
				fake.hold(
					cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testStorageClass},
					storageClassNamed(testStorageClass))
			}
			fake.hold(
				cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testMappedStorageClass},
				storageClassNamed(testMappedStorageClass))
			opt := testOptions(path)
			opt.StorageClasses = map[string]string{
				testStorageClass: testMappedStorageClass,
			}

			record, err := Deploy(context.TODO(), fake, opt)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			// The mapping is reported like any other, saying the flag
			// that named it.
			g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
				{
					Kind: "storage class",
					From: testStorageClass,
					To:   testMappedStorageClass,
					How:  "by --storage-class",
				},
			}))
			// Every disk landed on the class the mapping names, whatever
			// the target holds under the class the bundle records.
			g.Expect(fake.created).To(gomega.HaveLen(5))
			for i := range fake.created {
				if fake.created[i].GetKind() != cluster.KindDataVolume {
					continue
				}
				class, _, _ := unstructured.NestedString(
					fake.created[i].Object, "spec", "storage", "storageClassName")
				g.Expect(class).To(gomega.Equal(testMappedStorageClass))
			}
		})
	}
}

func TestDeployCatchesTheStorageClassesThatDoNotMatchByName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// One disk of a class the target holds and one of a class it does
	// not, which is what a machine whose disks were provisioned from
	// more than one class looks like.
	path := writeStorageFixtureBundle(t,
		bundle.StorageRequirement{
			Claim:        testRootClaim,
			StorageClass: testStorageClass,
			Size:         "32Gi",
			VolumeMode:   "Block",
		},
		bundle.StorageRequirement{
			Claim:        testThinClaim,
			StorageClass: testSlowStorageClass,
			Size:         "5Gi",
			VolumeMode:   "Filesystem",
		},
	)
	fake := target()
	fake.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	fake.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testStorageClass},
		storageClassNamed(testStorageClass))
	fake.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testMappedStorageClass},
		storageClassNamed(testMappedStorageClass))
	opt := testOptions(path)
	// The catch-all names where the classes that do not match by name
	// go; it does not take the ones that do.
	opt.StorageClasses = map[string]string{"*": testMappedStorageClass}

	record, err := Deploy(context.TODO(), fake, opt)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
		{
			Kind: "storage class",
			From: testStorageClass,
			To:   testStorageClass,
			How:  "by name",
		},
		{
			Kind: "storage class",
			From: testSlowStorageClass,
			To:   testMappedStorageClass,
			How:  "by --storage-class *",
		},
	}))
	// The disk of the held class stayed on it; the disk of the other
	// one landed on the catch-all's class.
	classOf := func(at int) string {
		class, _, _ := unstructured.NestedString(
			fake.created[at].Object, "spec", "storage", "storageClassName")
		return class
	}
	g.Expect(classOf(0)).To(gomega.Equal(testStorageClass))
	g.Expect(classOf(1)).To(gomega.Equal(testMappedStorageClass))
}

func TestDeployCatchesEveryClassWhenNoneMatchByName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeDisksFixtureBundle(t)
	// A target holding only the class the catch-all names: no disk of
	// the bundle matches by name, so every one of them is caught.
	fake := target()
	fake.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	fake.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testMappedStorageClass},
		storageClassNamed(testMappedStorageClass))
	opt := testOptions(path)
	opt.StorageClasses = map[string]string{"*": testMappedStorageClass}

	record, err := Deploy(context.TODO(), fake, opt)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
		{
			Kind: "storage class",
			From: testStorageClass,
			To:   testMappedStorageClass,
			How:  "by --storage-class *",
		},
	}))
	for i := range fake.created {
		if fake.created[i].GetKind() != cluster.KindDataVolume {
			continue
		}
		class, _, _ := unstructured.NestedString(
			fake.created[i].Object, "spec", "storage", "storageClassName")
		g.Expect(class).To(gomega.Equal(testMappedStorageClass))
	}
}

func TestDeployPlacesAnUnnamedClassOnlyThroughTheCatchAll(t *testing.T) {
	// One disk whose requirement records a size but no class, which is
	// what a claim bound to a volume made by hand leaves behind: the
	// bytes are held and their size is known, but nothing says what
	// provisioned them.
	path := writeStorageFixtureBundle(t, bundle.StorageRequirement{
		Claim:      testThinClaim,
		Size:       "5Gi",
		VolumeMode: "Filesystem",
	})

	tests := []struct {
		name    string
		catchUp string
	}{
		{
			name:    "the catch-all places it, as the class that does not match by name",
			catchUp: testMappedStorageClass,
		},
		{
			name:    "with no catch-all it stays a gap, because quo will not guess at placement",
			catchUp: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			fake := target()
			fake.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
			fake.hold(
				cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testMappedStorageClass},
				storageClassNamed(testMappedStorageClass))
			opt := testOptions(path)
			if tt.catchUp != "" {
				opt.StorageClasses = map[string]string{"*": tt.catchUp}
			}

			record, err := Deploy(context.TODO(), fake, opt)

			if tt.catchUp == "" {
				// A disk with no class recorded and no catch-all given
				// creates nothing: the gap is recorded and the run goes
				// on to the machine.
				g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
				g.Expect(record.Steps).To(gomega.HaveLen(2))
				g.Expect(record.Steps[0].Name).To(gomega.Equal(testThinClaim))
				g.Expect(record.Steps[0].Outcome).To(gomega.Equal(OutcomeSkipped))
				g.Expect(record.Steps[0].Reason).To(gomega.Equal("the bundle records no storage class for it"))
				g.Expect(record.Requirements).To(gomega.BeEmpty())
				g.Expect(fake.created).To(gomega.HaveLen(1))
				return
			}
			g.Expect(err).ToNot(gomega.HaveOccurred())
			// The unnamed class is one of the classes that do not match
			// by name, so the catch-all places it and the mapping is
			// reported.
			g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
				{
					Kind: "storage class",
					To:   testMappedStorageClass,
					How:  "by --storage-class *",
				},
			}))
			class, _, _ := unstructured.NestedString(
				fake.created[0].Object, "spec", "storage", "storageClassName")
			g.Expect(class).To(gomega.Equal(testMappedStorageClass))
			g.Expect(fake.uploaded).To(gomega.HaveLen(1))
		})
	}
}

func TestDeployRefusesAStorageClassThatResolvesToNothing(t *testing.T) {
	path := writeDisksFixtureBundle(t)
	// A target with CDI but nothing else: no class the bundle records
	// and no class a mapping could name.
	bare := target()
	bare.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	// A target holding the class the bundle records, so only the mapping
	// stands between the disks and a class that is not there.
	holdsClass := target()
	holdsClass.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	holdsClass.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testStorageClass},
		storageClassNamed(testStorageClass))

	tests := []struct {
		name   string
		target *fakeTarget
		mapped map[string]string
		names  string
	}{
		{
			name:   "no storage class of that name and no mapping names another",
			target: bare,
			// The refusal names the requirement, what was tried, and the
			// flag that would place it.
			names: "place it with --storage-class " + testStorageClass + "=<target>",
		},
		{
			name:   "a mapping that names a class the target does not have",
			target: holdsClass,
			mapped: map[string]string{testStorageClass: testMappedStorageClass},
			names:  "--storage-class " + testStorageClass + "=" + testMappedStorageClass,
		},
		{
			name:   "a catch-all that names a class the target does not have",
			target: bare,
			mapped: map[string]string{"*": testMappedStorageClass},
			names:  "--storage-class *=" + testMappedStorageClass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			opt := testOptions(path)
			opt.StorageClasses = tt.mapped

			record, err := Deploy(context.TODO(), tt.target, opt)

			g.Expect(err).To(gomega.MatchError(&UnresolvedRequirementError{}))
			// The refusal names the requirement and what was tried.
			g.Expect(err.Error()).To(gomega.ContainSubstring(testStorageClass))
			g.Expect(err.Error()).To(gomega.ContainSubstring(tt.names))
			// It refused before creating anything, so nothing was
			// attempted and nothing was created: disks are not uploaded
			// that cannot be placed.
			g.Expect(record.Steps).To(gomega.BeEmpty())
			g.Expect(record.Requirements).To(gomega.BeEmpty())
			g.Expect(tt.target.created).To(gomega.BeEmpty())
			g.Expect(tt.target.uploaded).To(gomega.BeEmpty())
		})
	}
}

func TestDeployRefusesADiskWhoseMappedClassCannotProvideItsVolumeMode(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeStorageFixtureBundle(t, bundle.StorageRequirement{
		Claim:        testRootClaim,
		StorageClass: testStorageClass,
		Size:         "32Gi",
		VolumeMode:   "Block",
	})
	// The target holds the class the mapping names, but its own account
	// of that class rules out the volume mode the disk needs. No mapping
	// fixes that: the class cannot hold the disk as it was captured.
	fake := target()
	fake.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	fake.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testMappedStorageClass},
		storageClassNamed(testMappedStorageClass))
	fake.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: testMappedStorageClass},
		storageProfile(testMappedStorageClass, "Filesystem"))
	opt := testOptions(path)
	opt.StorageClasses = map[string]string{
		testStorageClass: testMappedStorageClass,
	}

	record, err := Deploy(context.TODO(), fake, opt)

	g.Expect(err).To(gomega.MatchError(&UnsupportedVolumeModeError{}))
	// The refusal names the class the disk was mapped onto, which is the
	// one whose account of itself rules the mode out.
	g.Expect(err.Error()).To(gomega.ContainSubstring(testMappedStorageClass))
	g.Expect(record.Steps).To(gomega.BeEmpty())
	g.Expect(fake.created).To(gomega.BeEmpty())
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
	g.Expect(record.Steps).To(gomega.HaveLen(5))
	g.Expect(record.Steps[2].Name).To(gomega.Equal(testGapClaim))
	g.Expect(record.Steps[2].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record.Steps[2].Reason).To(gomega.Equal("the bundle holds no bytes for it"))
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
	g.Expect(record.Steps).To(gomega.HaveLen(2))
	g.Expect(record.Steps[0].Name).To(gomega.Equal(testThinClaim))
	g.Expect(record.Steps[0].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record.Steps[0].Reason).To(gomega.Equal("the bundle records no size for it"))
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
	filesystemOnly.hold(cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testStorageClass}, storageClassNamed(testStorageClass))
	filesystemOnly.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: testStorageClass},
		storageProfile(testStorageClass, "Filesystem"))

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
			want:   &UnresolvedRequirementError{},
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
			g.Expect(record.Steps).To(gomega.BeEmpty())
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
	g.Expect(record.Steps).To(gomega.HaveLen(5))
	// The corrupted disk is skipped and recorded: what was deployed for
	// it is not what the bundle holds. The VirtualMachine is created too,
	// because a member that fails its digest is a recorded gap rather
	// than a refusal — the expansion itself arrived whole.
	g.Expect(record.Steps[0].Name).To(gomega.Equal(testRootClaim))
	g.Expect(record.Steps[0].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record.Steps[0].Reason).To(gomega.ContainSubstring("does not match its digest"))
	g.Expect(record.Steps[4].Outcome).To(gomega.Equal(OutcomeCreated))
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
	g.Expect(record.Steps).To(gomega.HaveLen(5))
	// The failed upload is recorded with its reason, and the DataVolume
	// step says failed rather than created: the target holds the
	// DataVolume, but it does not hold the disk.
	g.Expect(record.Steps[0].Name).To(gomega.Equal(testRootClaim))
	g.Expect(record.Steps[0].Outcome).To(gomega.Equal(OutcomeFailed))
	g.Expect(record.Steps[0].Reason).To(gomega.ContainSubstring("the upload failed"))
	g.Expect(record.Steps[0].Reason).To(gomega.ContainSubstring("connection reset by peer"))
	// The disks behind it proceeded, and the VirtualMachine was created
	// last whatever became of them.
	g.Expect(fake.uploaded).To(gomega.HaveLen(3))
	g.Expect(fake.uploaded[0].name).To(gomega.Equal(testDataClaim))
	g.Expect(record.Steps[4].Outcome).To(gomega.Equal(OutcomeCreated))
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
