package deploy

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/cluster"
)

func TestDeployMapsANetworkOntoTheAttachmentOfItsName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeNetworksFixtureBundle(t, testAttachment)
	fake := target()
	fake.hold(cluster.ObjectRef{
		Kind:      cluster.KindNetworkAttachmentDefinition,
		Namespace: testDeployNamespace,
		Name:      testAttachment,
	}, heldAttachment(testDeployNamespace, testAttachment))

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The mapping is automatic when the deploy namespace holds a
	// NetworkAttachmentDefinition of the attachment's name, and it is
	// reported: nothing maps silently.
	g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
		{
			Kind: "network",
			From: testMultusNetwork,
			To:   testAttachment,
			How:  "by name",
		},
	}))
	// The applied VirtualMachine's networkName carries the mapping: the
	// source cluster's own qualified form is replaced by the
	// attachment's name, bare because its definition lives in the deploy
	// namespace, where an unqualified name means the namespace of the
	// VirtualMachine itself. The pod network stanza is carried through
	// untouched, because the pod network is never mapped.
	created := fake.created[0]
	networks, _, _ := unstructured.NestedSlice(
		created.Object, "spec", "template", "spec", "networks")
	g.Expect(networks).To(gomega.Equal([]any{
		map[string]any{"name": "default", "pod": map[string]any{}},
		map[string]any{
			"name":   testMultusNetwork,
			"multus": map[string]any{"networkName": testAttachment},
		},
	}))
	// Only the VirtualMachine was created: a mapped network creates no
	// object of its own, and the definition is never written.
	g.Expect(fake.created).To(gomega.HaveLen(1))
}

func TestDeployAttachesANetworkToTheAttachmentAMappingNames(t *testing.T) {
	tests := []struct {
		name   string
		named  string
		holds  string
		wantTo string
	}{
		{
			name:   "an attachment in the deploy namespace",
			named:  testAttachment,
			holds:  testDeployNamespace,
			wantTo: testAttachment,
		},
		{
			name:   "an attachment named with its namespace inside the deploy namespace",
			named:  testDeployNamespace + "/" + testAttachment,
			holds:  testDeployNamespace,
			wantTo: testAttachment,
		},
		{
			name:   "an attachment outside the deploy namespace, namespace-qualified",
			named:  testOutsideNamespace + "/" + testAttachmentElsewhere,
			holds:  testOutsideNamespace,
			wantTo: testOutsideNamespace + "/" + testAttachmentElsewhere,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			path := writeNetworksFixtureBundle(t, testAttachment)
			fake := target()
			// Hold the definition the flag names, wherever it lives.
			attachment := testAttachment
			if tt.wantTo != testAttachment {
				attachment = testAttachmentElsewhere
			}
			fake.hold(cluster.ObjectRef{
				Kind:      cluster.KindNetworkAttachmentDefinition,
				Namespace: tt.holds,
				Name:      attachment,
			}, heldAttachment(tt.holds, attachment))
			opt := testOptions(path)
			opt.Networks = map[string]string{testMultusNetwork: tt.named}

			record, err := Deploy(context.TODO(), fake, opt)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			// The mapping is reported, saying the flag that named it.
			// What it maps onto is named the way the applied
			// VirtualMachine names it: qualified only when the
			// definition lives outside the deploy namespace.
			g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
				{
					Kind: "network",
					From: testMultusNetwork,
					To:   tt.wantTo,
					How:  "by --network",
				},
			}))
			created := fake.created[0]
			g.Expect(networkNameOf(t, created, testMultusNetwork)).To(gomega.Equal(tt.wantTo))
			// The mapping creates no object of its own.
			g.Expect(fake.created).To(gomega.HaveLen(1))
		})
	}
}

// networkNameOf returns what the created VirtualMachine's networkName
// names for one of its networks, which is where a mapping lands.
func networkNameOf(t *testing.T, vm *unstructured.Unstructured, network string) (attached string) {
	t.Helper()
	declared, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "networks")
	for i := range declared {
		named, cast := declared[i].(map[string]any)
		if !cast {
			continue
		}
		name, _, _ := unstructured.NestedString(named, "name")
		if name != network {
			continue
		}
		attached, _, _ = unstructured.NestedString(named, "multus", "networkName")
		return
	}
	return
}

func TestDeployRefusesANetworkThatMapsToNothing(t *testing.T) {
	tests := []struct {
		name       string
		attachment string
		networks   map[string]string
		names      string
	}{
		{
			name:       "no definition of the attachment's name in the deploy namespace, and no mapping",
			attachment: testAttachment,
			// The refusal names the network, what was tried, and the
			// flag that would attach it.
			names: "--network " + testMultusNetwork + "=<namespace>/<attachment>",
		},
		{
			name:       "a mapping that names an attachment the target does not have",
			attachment: testAttachment,
			networks:   map[string]string{testMultusNetwork: testOutsideNamespace + "/" + testAttachmentElsewhere},
			names:      testOutsideNamespace + "/" + testAttachmentElsewhere,
		},
		{
			name:       "a network whose capture recorded no attachment, and no mapping",
			attachment: "",
			names:      "--network " + testMultusNetwork + "=<namespace>/<attachment>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			// Nothing is held: no NetworkAttachmentDefinition anywhere.
			path := writeNetworksFixtureBundle(t, tt.attachment)
			fake := target()
			opt := testOptions(path)
			opt.Networks = tt.networks

			record, err := Deploy(context.TODO(), fake, opt)

			g.Expect(err).To(gomega.MatchError(&UnmappedRequirementError{}))
			// The refusal names the network and what was tried.
			g.Expect(err.Error()).To(gomega.ContainSubstring(testMultusNetwork))
			g.Expect(err.Error()).To(gomega.ContainSubstring(tt.names))
			// It refused before creating anything, so nothing was
			// attempted and nothing was created: the machine is not
			// created whose networks cannot be attached.
			g.Expect(record.Steps).To(gomega.BeEmpty())
			g.Expect(record.Requirements).To(gomega.BeEmpty())
			g.Expect(fake.created).To(gomega.BeEmpty())
		})
	}
}

func TestDeployReportsANetworkMappingThatMatchedNoNetworkOfTheMachine(t *testing.T) {
	tests := []struct {
		name    string
		network string
	}{
		{
			name:    "a network the machine has not",
			network: "office",
		},
		{
			name:    "the pod network, which is never mapped",
			network: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			path := writeNetworksFixtureBundle(t, testAttachment)
			fake := target()
			fake.hold(cluster.ObjectRef{
				Kind:      cluster.KindNetworkAttachmentDefinition,
				Namespace: testDeployNamespace,
				Name:      testAttachment,
			}, heldAttachment(testDeployNamespace, testAttachment))
			opt := testOptions(path)
			opt.Networks = map[string]string{tt.network: testAttachment}

			record, err := Deploy(context.TODO(), fake, opt)

			// The deploy goes ahead, because a mapping that matched
			// nothing is not a refusal, and the record says the flag
			// took no effect: nothing maps silently, and nothing that
			// tried to map is silent either.
			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
				{
					Kind: "network",
					From: testMultusNetwork,
					To:   testAttachment,
					How:  "by name",
				},
				{
					Kind: "network",
					From: tt.network,
					To:   testAttachment,
					How:  "the bundle declares no such network",
				},
			}))
			// The machine the bundle holds is created as ever.
			g.Expect(fake.created).To(gomega.HaveLen(1))
		})
	}
}

func TestDeployReportsAContainerDiskImageAsARequirementItNeverMaps(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeContainerDiskFixtureBundle(t)
	// A target that holds nothing at all: the image is never mapped onto
	// anything and never pull-checked, so nothing needs to be there for
	// the deploy to succeed.
	fake := target()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The image is reported as a requirement with nothing mapped onto it:
	// quo holds no registry credentials, so the entry says what the
	// target has to be able to do for itself.
	g.Expect(record.Requirements).To(gomega.Equal([]Requirement{
		{
			Kind: "container disk",
			From: testContainerDiskImage,
			How:  "the target has to be able to pull it",
		},
	}))
	// Nothing was created for it and nothing was uploaded: only the
	// VirtualMachine, whose volume stanza is carried through unchanged.
	g.Expect(fake.created).To(gomega.HaveLen(1))
	g.Expect(fake.uploaded).To(gomega.BeEmpty())
	volumes, _, _ := unstructured.NestedSlice(
		fake.created[0].Object, "spec", "template", "spec", "volumes")
	g.Expect(volumes).To(gomega.Equal([]any{
		map[string]any{
			"name":          "tools",
			"containerDisk": map[string]any{"image": testContainerDiskImage},
		},
	}))
}
