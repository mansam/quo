package deploy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/cluster"
)

func TestUndeployDeletesExactlyTheOneBundlesDeployment(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// Two bundles deployed side by side in the namespace: one holding
	// only a VirtualMachine, one holding the disk fixture's machine.
	// Their manifests differ, so their identities differ. That is what
	// makes an undeploy delete exactly one deployment. The disk
	// fixture's machine is deployed under a name of its own, because a
	// namespace cannot hold two VirtualMachines of the same name.
	machine := writeFixtureBundle(t, true)
	disks := writeDisksFixtureBundle(t)
	machineIdentity := bundleLabelOf(t, machine)
	disksIdentity := bundleLabelOf(t, disks)
	fake := target()
	fake.holdLabeled(cluster.KindVirtualMachine, testVirtualMachineName, machineIdentity)
	fake.holdLabeled(cluster.KindVirtualMachine, "db-01-copy", disksIdentity)
	for _, claim := range []string{testRootClaim, testDataClaim, testGapClaim, testThinClaim} {
		fake.holdLabeled(cluster.KindDataVolume, claim, disksIdentity)
	}

	deleted, err := Undeploy(context.TODO(), fake, testUndeployOptions(machine))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// What it deleted is the one bundle's deployment, whole.
	g.Expect(deleted).To(gomega.Equal([]Deletion{
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
		},
	}))
	g.Expect(fake.events).To(gomega.Equal([]string{
		"delete VirtualMachine " + testDeployNamespace + "/" + testVirtualMachineName,
	}))
	// The other bundle's deployment is untouched, because it carries a
	// different identity.
	g.Expect(fake.holds(cluster.KindVirtualMachine, "db-01-copy")).To(gomega.BeTrue())
	for _, claim := range []string{testRootClaim, testDataClaim, testGapClaim, testThinClaim} {
		g.Expect(fake.holds(cluster.KindDataVolume, claim)).To(gomega.BeTrue())
	}
}

// testUndeployOptions undeploys the bundle at path from the deploy
// namespace.
func testUndeployOptions(path string) (opt UndeployOptions) {
	opt = UndeployOptions{Bundle: path, Namespace: testDeployNamespace}
	return
}

func TestUndeployTakesADataVolumesClaimWithIt(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	disks := writeDisksFixtureBundle(t)
	identity := bundleLabelOf(t, disks)
	fake := target()
	fake.holdLabeled(cluster.KindVirtualMachine, testVirtualMachineName, identity)
	for _, claim := range []string{testRootClaim, testDataClaim} {
		fake.holdLabeled(cluster.KindDataVolume, claim, identity)
		// The claim CDI made for the DataVolume, named after it. It
		// carries the label too, and is still never selected on its
		// own: it goes with the DataVolume that owns it.
		fake.holdLabeled(cluster.KindPersistentVolumeClaim, claim, identity)
	}

	deleted, err := Undeploy(context.TODO(), fake, testUndeployOptions(disks))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The claims are not in what was deleted: each went with the
	// DataVolume that owns it. That is the cluster's doing, and not
	// something quo asked for. CDI deletes the claim through the owner
	// reference it gave it.
	g.Expect(deleted).To(gomega.Equal([]Deletion{
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testDataClaim,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testRootClaim,
		},
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
		},
	}))
	for _, claim := range []string{testRootClaim, testDataClaim} {
		g.Expect(fake.holds(cluster.KindDataVolume, claim)).To(gomega.BeFalse())
		g.Expect(fake.holds(cluster.KindPersistentVolumeClaim, claim)).To(gomega.BeFalse())
	}
	g.Expect(fake.holds(cluster.KindVirtualMachine, testVirtualMachineName)).To(gomega.BeFalse())
}

func TestUndeployContinuesWhenADeletionFails(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	disks := writeDisksFixtureBundle(t)
	identity := bundleLabelOf(t, disks)
	fake := target()
	fake.holdLabeled(cluster.KindVirtualMachine, testVirtualMachineName, identity)
	fake.holdLabeled(cluster.KindDataVolume, testRootClaim, identity)
	fake.holdLabeled(cluster.KindDataVolume, testDataClaim, identity)
	refused := errors.New("the API server went away")
	fake.deleteErrFor[testDataClaim] = refused

	deleted, err := Undeploy(context.TODO(), fake, testUndeployOptions(disks))

	// The run continued past the refusal: the rest of the deployment is
	// gone.
	g.Expect(fake.holds(cluster.KindVirtualMachine, testVirtualMachineName)).To(gomega.BeFalse())
	g.Expect(fake.holds(cluster.KindDataVolume, testRootClaim)).To(gomega.BeFalse())
	// The refusal is recorded on the one object, not swallowed: the
	// entry says why it is still on the cluster.
	g.Expect(deleted).To(gomega.HaveLen(3))
	g.Expect(deleted[0].Err).To(gomega.MatchError(refused))
	g.Expect(deleted[1].Err).To(gomega.BeNil())
	g.Expect(deleted[2].Err).To(gomega.BeNil())
	// And the error is what makes the exit code say the undeploy was a
	// partial one.
	g.Expect(err).To(gomega.MatchError(&NotDeletedError{NotDeleted: 1, Attempted: 3}))
	g.Expect(fake.holds(cluster.KindDataVolume, testDataClaim)).To(gomega.BeTrue())
}

func TestUndeployDryRunDeletesNothing(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	disks := writeDisksFixtureBundle(t)
	identity := bundleLabelOf(t, disks)
	fake := target()
	fake.holdLabeled(cluster.KindVirtualMachine, testVirtualMachineName, identity)
	for _, claim := range []string{testRootClaim, testDataClaim} {
		fake.holdLabeled(cluster.KindDataVolume, claim, identity)
		fake.holdLabeled(cluster.KindPersistentVolumeClaim, claim, identity)
	}
	opt := testUndeployOptions(disks)
	opt.DryRun = true

	deleted, err := Undeploy(context.TODO(), fake, opt)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// It lists what it would delete, the whole deployment...
	g.Expect(deleted).To(gomega.Equal([]Deletion{
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testDataClaim,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testRootClaim,
		},
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
		},
	}))
	// ...and deletes nothing: the deployment is all still there,
	// claims with it, and nothing was attempted.
	g.Expect(fake.holds(cluster.KindVirtualMachine, testVirtualMachineName)).To(gomega.BeTrue())
	for _, claim := range []string{testRootClaim, testDataClaim} {
		g.Expect(fake.holds(cluster.KindDataVolume, claim)).To(gomega.BeTrue())
		g.Expect(fake.holds(cluster.KindPersistentVolumeClaim, claim)).To(gomega.BeTrue())
	}
	g.Expect(fake.events).To(gomega.BeEmpty())
}

func TestUndeployRefusesABundleItCannotRead(t *testing.T) {
	// A file that is not a bundle at all, and an empty one.
	garbage := writeBytes(t, "garbage.quo", []byte("not a bundle, just bytes"))
	empty := writeBytes(t, "empty.quo", nil)

	tests := []struct {
		name   string
		bundle string
		// names is what the refusal names, which is how a reader tells
		// one refusal from another. Empty means the refusal names
		// nothing a test can pin down.
		names string
	}{
		{
			name:   "a bundle that is not there",
			bundle: filepath.Join(t.TempDir(), "never-written.quo"),
			names:  "never-written.quo",
		},
		{
			name:   "a file that is not a bundle",
			bundle: garbage,
		},
		{
			name:   "an empty file",
			bundle: empty,
			names:  "the bundle is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			fake := target()

			deleted, err := Undeploy(context.TODO(), fake, testUndeployOptions(tt.bundle))

			g.Expect(err).To(gomega.HaveOccurred())
			// The refusal names what it found.
			if tt.names != "" {
				g.Expect(err.Error()).To(gomega.ContainSubstring(tt.names))
			}
			// Nothing was selected, so nothing was deleted.
			g.Expect(deleted).To(gomega.BeEmpty())
			g.Expect(fake.events).To(gomega.BeEmpty())
		})
	}
}
