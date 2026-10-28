package deploy

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// dryRunOptions deploys the bundle at path the way a dry run does: every
// refusal still runs and the record says what a real deploy would do, and
// nothing is created.
func dryRunOptions(path string) (opt Options) {
	opt = testOptions(path)
	opt.DryRun = true
	return
}

func TestDeployDryRunRunsEveryRefusalTheRealDeployRuns(t *testing.T) {
	// The bundle refusals, in the shapes the real deploy's own tests give
	// them: a later format version, and a bundle whose capture could not
	// read the expansion.
	laterFormat := writeBytes(t, "later-format.quo", bytes.ReplaceAll(
		readFile(t, writeFixtureBundle(t, true)),
		[]byte("formatVersion: "+bundle.FormatVersion),
		[]byte("formatVersion: v9")))
	// The disks bundle, for the refusals about placement; the closure
	// bundle, for the collisions; the networks bundle, for the mapping.
	disks := writeDisksFixtureBundle(t)
	closureBundle := writeClosureFixtureBundle(t)
	networks := writeNetworksFixtureBundle(t, testAttachment)
	// Targets the refusals need: one with nothing on it at all; one with
	// CDI and no storage class; one whose own account of the class rules
	// out the root disk's volume mode.
	bare := target()
	cdiOnly := target()
	cdiOnly.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	filesystemOnly := target()
	filesystemOnly.hold(cluster.ObjectRef{Kind: cluster.KindCDIConfig, Name: cluster.CDIConfigName}, cdiConfig())
	filesystemOnly.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: testStorageClass},
		storageClassNamed(testStorageClass))
	filesystemOnly.hold(
		cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: testStorageClass},
		storageProfile(testStorageClass, "Filesystem"))
	// A name taken of every kind a deploy of the closure bundle would
	// create: a DataVolume, a closure object and the VirtualMachine.
	taken := []cluster.ObjectRef{
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testRootClaim,
		},
		{
			Kind:      cluster.KindConfigMap,
			Namespace: testDeployNamespace,
			Name:      testSysprepConfigMap,
		},
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
		},
	}

	tests := []struct {
		name      string
		bundle    string
		namespace string
		target    *fakeTarget
		hold      []cluster.ObjectRef
		// want is the refusal's type. A nil want means the refusal has
		// no single type worth naming and only its presence is asserted.
		want error
		// names is what the refusal names, which is how a reader tells
		// one refusal from another.
		names []string
	}{
		{
			name:   "a bundle that is not there",
			bundle: filepath.Join(t.TempDir(), "never-written.quo"),
			target: bare,
			names:  []string{"never-written.quo"},
		},
		{
			name:   "a bundle whose format version is unknown",
			bundle: laterFormat,
			target: bare,
			want:   &bundle.UnsupportedVersionError{},
			names:  []string{"v9"},
		},
		{
			name:   "a bundle that holds no expanded spec",
			bundle: writeFixtureBundle(t, false),
			target: bare,
			want:   &NoExpandedSpecError{},
			names:  []string{"no expanded spec"},
		},
		{
			name:      "a namespace the target does not have",
			bundle:    closureBundle,
			namespace: "not-there",
			target:    bare,
			want:      &MissingNamespaceError{},
			names:     []string{"not-there"},
		},
		{
			name:   "a DataVolume, a closure object and the VirtualMachine already on the target",
			bundle: closureBundle,
			target: storageTarget(),
			hold:   taken,
			want:   &AlreadyExistsError{},
			names: []string{
				"DataVolume " + testDeployNamespace + "/" + testRootClaim,
				"ConfigMap " + testDeployNamespace + "/" + testSysprepConfigMap,
				"VirtualMachine " + testDeployNamespace + "/" + testVirtualMachineName,
			},
		},
		{
			name:   "a target with no CDI, from a bundle holding disks to upload",
			bundle: disks,
			target: bare,
			want:   &NoCDIError{},
			names:  []string{"install CDI on the target cluster first"},
		},
		{
			name:   "a storage class that resolves to nothing",
			bundle: disks,
			target: cdiOnly,
			want:   &UnresolvedRequirementError{},
			names:  []string{"place it with --storage-class"},
		},
		{
			name:   "a disk whose volume mode the matched class cannot provide",
			bundle: disks,
			target: filesystemOnly,
			want:   &UnsupportedVolumeModeError{},
			names:  []string{"Block"},
		},
		{
			name:   "a network that resolves to nothing",
			bundle: networks,
			target: bare,
			want:   &UnresolvedRequirementError{},
			names:  []string{"attach it with --network"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			for i := range tt.hold {
				tt.target.hold(tt.hold[i], occupied(tt.hold[i]))
			}
			opt := dryRunOptions(tt.bundle)
			if tt.namespace != "" {
				opt.Namespace = tt.namespace
			}

			record, err := Deploy(context.TODO(), tt.target, opt)

			if tt.want == nil {
				g.Expect(err).To(gomega.HaveOccurred())
			} else {
				g.Expect(err).To(gomega.MatchError(tt.want))
			}
			// The refusal names what it found, exactly as the real
			// deploy's does.
			for _, named := range tt.names {
				g.Expect(err.Error()).To(gomega.ContainSubstring(named))
			}
			// A deploy that would be refused refuses before anything
			// would have been created, so nothing was attempted and
			// nothing touched the target.
			g.Expect(record.Steps).To(gomega.BeEmpty())
			g.Expect(record.Requirements).To(gomega.BeEmpty())
			g.Expect(tt.target.created).To(gomega.BeEmpty())
			g.Expect(tt.target.uploaded).To(gomega.BeEmpty())
			g.Expect(tt.target.events).To(gomega.BeEmpty())
		})
	}
}

func TestDeployDryRunReportsWhatItWouldCreate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeClosureFixtureBundle(t)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, dryRunOptions(path))

	// A dry run of a deploy that would be clean is a clean run: the
	// error is what says a real deploy would have created everything it
	// tried to.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The record is the one a real deploy would print: every object it
	// would create, in the order it would create them — the DataVolumes,
	// then the closure objects, then the VirtualMachine last — each said
	// as created, because that is what a real deploy would do with each.
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
			Kind:      cluster.KindConfigMap,
			Namespace: testDeployNamespace,
			Name:      testSysprepConfigMap,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindSecret,
			Namespace: testDeployNamespace,
			Name:      testCloudInitSecret,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindSecret,
			Namespace: testDeployNamespace,
			Name:      testRegistrySecret,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindServiceAccount,
			Namespace: testDeployNamespace,
			Name:      testServiceAccount,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
			Outcome:   OutcomeCreated,
		},
	}))
	// The mappings are reported as a real deploy reports them, because
	// they are what a dry run is for checking before anything is created.
	g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
		{
			Kind: "storage class",
			From: testStorageClass,
			To:   testStorageClass,
			How:  "by name",
		},
	}))
	// And nothing touched the target: nothing was created, nothing was
	// uploaded, and no labels landed anywhere, because the target holds
	// none of what the record says a deploy would put there.
	g.Expect(fake.created).To(gomega.BeEmpty())
	g.Expect(fake.uploaded).To(gomega.BeEmpty())
	g.Expect(fake.events).To(gomega.BeEmpty())
	g.Expect(fake.holds(cluster.KindVirtualMachine, testVirtualMachineName)).To(gomega.BeFalse())
	g.Expect(fake.holds(cluster.KindDataVolume, testRootClaim)).To(gomega.BeFalse())
	g.Expect(fake.holds(cluster.KindConfigMap, testSysprepConfigMap)).To(gomega.BeFalse())
}

func TestDeployDryRunSaysWhatWouldNotBeCreated(t *testing.T) {
	// The root disk's bytes changed in transit. The claim's name is in
	// every line of the member's stand-in content, so replacing it keeps
	// the tar the manifest indexes aligned while changing what the digest
	// is computed over.
	corruptedDisk := writeBytes(t, "corrupted-disk.quo", bytes.ReplaceAll(
		readFile(t, writeDisksFixtureBundle(t)),
		[]byte("disk of "+testRootClaim),
		[]byte("disk 0f "+testRootClaim)))
	// The sysprep ConfigMap's answer file changed in transit, the same
	// way.
	corruptedClosure := writeBytes(t, "corrupted-closure.quo", bytes.ReplaceAll(
		readFile(t, writeClosureFixtureBundle(t)),
		[]byte(sysprepAnswer),
		[]byte(sysprepMangled)))
	// One byte of the expansion changed in transit. The machine type
	// sits in the expansion alone — the declaration names an instancetype
	// instead — so its last occurrence in the bundle is in the member.
	corruptedExpansion := bytes.Clone(readFile(t, writeClosureFixtureBundle(t)))
	at := bytes.LastIndex(corruptedExpansion, []byte(testMachineType))
	if at < 0 {
		t.Fatal("the fixture bundle does not carry the machine type")
	}
	corruptedExpansion[at] = 'x'
	corruptedExpansionPath := writeBytes(t, "corrupted-expansion.quo", corruptedExpansion)

	tests := []struct {
		name   string
		bundle string
		// at is where the skipped step sits in the record.
		at int
		// reason is what the skip says, count is how the error says the
		// run fell short, and total is how many steps the record holds.
		reason string
		count  string
		total  int
	}{
		{
			name:   "a disk member that fails its digest",
			bundle: corruptedDisk,
			at:     0,
			reason: "does not match its digest",
			count:  "4 of the 5 objects",
			total:  5,
		},
		{
			name:   "a closure member that fails its digest",
			bundle: corruptedClosure,
			at:     4,
			reason: "does not match its digest",
			count:  "8 of the 9 objects",
			total:  9,
		},
		{
			name:   "the expanded spec failing its digest, which ends the run",
			bundle: corruptedExpansionPath,
			at:     0,
			reason: "does not match its digest",
			count:  "0 of the 1 objects",
			total:  1,
		},
		{
			name:   "a disk the bundle holds no bytes for",
			bundle: writeDisksFixtureBundle(t, testGapClaim),
			at:     2,
			reason: "the bundle holds no bytes for it",
			count:  "4 of the 5 objects",
			total:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			fake := storageTarget()

			record, err := Deploy(context.TODO(), fake, dryRunOptions(tt.bundle))

			// The skip lands in the record the dry run would print, and
			// the error is the one a real deploy's would be: what the
			// dry run describes is a partial deploy, and the exit code
			// says so the same way.
			g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
			g.Expect(err.Error()).To(gomega.ContainSubstring(tt.count))
			g.Expect(record.Steps).To(gomega.HaveLen(tt.total))
			g.Expect(record.Steps[tt.at].Outcome).To(gomega.Equal(OutcomeSkipped))
			g.Expect(record.Steps[tt.at].Reason).To(gomega.ContainSubstring(tt.reason))
			// Everything else is what a real deploy would create.
			for i := range record.Steps {
				if i == tt.at {
					continue
				}
				g.Expect(record.Steps[i].Outcome).To(gomega.Equal(OutcomeCreated))
			}
			// And nothing touched the target all the same.
			g.Expect(fake.created).To(gomega.BeEmpty())
			g.Expect(fake.uploaded).To(gomega.BeEmpty())
			g.Expect(fake.events).To(gomega.BeEmpty())
		})
	}
}

func TestDeployDryRunReportsTheMappingsItWouldApply(t *testing.T) {
	tests := []struct {
		name     string
		networks map[string]string
		// attachment and where say the NetworkAttachmentDefinition the
		// mapping resolves onto, and the namespace holding it: the
		// deploy namespace for the mapping by name, outside it for the
		// flag's qualified form.
		attachment string
		where      string
		want       []Requirement
	}{
		{
			name:       "a network mapped onto the attachment of its name",
			attachment: testAttachment,
			where:      testDeployNamespace,
			want: []Requirement{{
				Kind: "network",
				From: testMultusNetwork,
				To:   testAttachment,
				How:  "by name",
			}},
		},
		{
			name:       "a network mapped by the flag onto an attachment outside the deploy namespace",
			networks:   map[string]string{testMultusNetwork: testOutsideNamespace + "/" + testAttachmentElsewhere},
			attachment: testAttachmentElsewhere,
			where:      testOutsideNamespace,
			want: []Requirement{{
				Kind: "network",
				From: testMultusNetwork,
				To:   testOutsideNamespace + "/" + testAttachmentElsewhere,
				How:  "by --network",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			path := writeNetworksFixtureBundle(t, testAttachment)
			fake := target()
			fake.hold(cluster.ObjectRef{
				Kind:      cluster.KindNetworkAttachmentDefinition,
				Namespace: tt.where,
				Name:      tt.attachment,
			}, heldAttachment(tt.where, tt.attachment))
			opt := dryRunOptions(path)
			opt.Networks = tt.networks

			record, err := Deploy(context.TODO(), fake, opt)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			// Every mapping a real deploy would apply is in the record.
			g.Expect(record.Requirements).To(gomega.Equal(tt.want))
			// The one object a real deploy would create is the machine,
			// said as created and not put anywhere.
			g.Expect(record.Steps).To(gomega.Equal([]Step{{
				Kind:      cluster.KindVirtualMachine,
				Namespace: testDeployNamespace,
				Name:      testVirtualMachineName,
				Outcome:   OutcomeCreated,
			}}))
			g.Expect(fake.created).To(gomega.BeEmpty())
			g.Expect(fake.uploaded).To(gomega.BeEmpty())
			g.Expect(fake.events).To(gomega.BeEmpty())
		})
	}
}
