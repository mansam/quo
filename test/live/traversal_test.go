//go:build live

package live

import (
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
)

// TestQuoFindsEveryMemberOfTheClosureKubeVirtsObjectGraphFinds is why the
// suite exists.
//
// quo traverses a VirtualMachine's closure itself rather than calling the
// objectgraph subresource. That subresource sits behind a feature gate
// that is off by default. So quo cannot rely on it. KubeVirt's
// server-side implementation is the specification quo's traversal follows.
// Nothing keeps the copy in step with the original.
//
// On a cluster where the gate is on, the two can be asked the same
// question. An object KubeVirt follows and quo does not appears here and
// nowhere else.
func TestQuoFindsEveryMemberOfTheClosureKubeVirtsObjectGraphFinds(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)
	namespace := s.fixture.namespace
	name := s.fixture.named(partVM)

	root := graphOf(t, s)
	graph := root.refs()
	// A graph that decoded to nothing would make every comparison pass.
	// So the suite would report agreement where it asked nothing. The
	// likeliest cause is a rename in KubeVirt's own type.
	g.Expect(graph).ToNot(gomega.BeEmpty())

	record, _ := s.captured(t)
	d := compare(traversed(record), graph)
	if len(d.extra) > 0 {
		t.Log(d.report(namespace, name))
	}
	g.Expect(d.diverged()).To(gomega.BeFalse(), d.report(namespace, name))
}

func TestTheObjectGraphNamesTheVirtualMachineItWasAskedAbout(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)

	root := graphOf(t, s)

	// The root node of the graph is the VirtualMachine itself. Reading it
	// back is what says the response decoded into the field names this suite
	// mirrors rather than into an empty node.
	g.Expect(root.ObjectReference.Kind).To(gomega.Equal("VirtualMachine"))
	g.Expect(root.ObjectReference.Name).To(gomega.Equal(s.fixture.named(partVM)))
	g.Expect(root.ObjectReference.Namespace).ToNot(gomega.BeNil())
	g.Expect(*root.ObjectReference.Namespace).To(gomega.Equal(s.fixture.namespace))
	g.Expect(root.Children).ToNot(gomega.BeEmpty())
}

// graphOf reads KubeVirt's object graph for the fixture VirtualMachine.
//
// A cluster that serves the subresource with the feature gate off answers
// every request with a refusal. That is not a finding about quo. So the
// test skips and names the gate.
func graphOf(t *testing.T, s *suite) (root graphNode) {
	t.Helper()
	root, err := s.objectGraph(
		context.Background(),
		s.fixture.namespace,
		s.fixture.named(partVM))
	if errors.Is(err, errGateOff) {
		t.Skip(unmet(err.Error()))
	}
	if err != nil {
		t.Fatalf("read the object graph: %v", err)
	}
	return
}
