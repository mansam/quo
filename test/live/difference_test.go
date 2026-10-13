//go:build live

package live

import (
	"fmt"
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// Outcomes that mean quo's traversal found the object.
//
// A skipped object was found and then could not be read. Leaving it out
// would report quo as having missed something it looked for and was refused,
// which is a different problem with a different fix.
var traversal = map[bundle.Outcome]bool{
	bundle.OutcomeCollected: true,
	bundle.OutcomeSkipped:   true,
}

// Kinds quo reads for reasons of its own rather than because the
// VirtualMachine refers to them. KubeVirt's graph has no opinion about any of
// them, so comparing them would report a disagreement that is not one.
//
// This is a list of what to leave out rather than a list of what to compare,
// so that a kind neither side knows about today still shows up tomorrow.
// Comparing against a list of known kinds would filter the new kind off both
// sides and report agreement, which is the one answer this suite must never
// give wrongly.
//
// Since the capture record stopped marking these apart from the objects the
// traversal reached, this list is the only thing separating them, and a kind
// added to the environment reads has to be added here too.
var untraversedKinds = map[string]bool{
	cluster.KindKubeVirt:         true,
	cluster.KindPersistentVolume: true,
	cluster.KindStorageClass:     true,
	cluster.KindStorageProfile:   true,
	bundle.KindExpandedSpec:      true,
}

// difference is what one traversal found and the other did not.
type difference struct {
	// missed is what KubeVirt's graph found and quo's traversal did not.
	// Each one is a member of the closure a bundle would leave behind.
	missed []cluster.ObjectRef
	// extra is what quo's traversal found and KubeVirt's graph did not.
	extra []cluster.ObjectRef
}

// traversed returns the objects quo's closure traversal found, from the
// record of what a capture attempted.
//
// Requirements take no part: a network or a container disk image is
// something a target has to provide rather than an object quo read. Nor do
// the kinds quo reads for reasons of its own.
func traversed(record bundle.CaptureRecord) (refs []cluster.ObjectRef) {
	seen := map[cluster.ObjectRef]bool{}
	for i := range record {
		if !traversal[record[i].Outcome] {
			continue
		}
		if untraversedKinds[record[i].Kind] {
			continue
		}
		ref := cluster.ObjectRef{
			Kind:      record[i].Kind,
			Namespace: record[i].Namespace,
			Name:      record[i].Name,
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return
}

// compare reports where quo's traversal and KubeVirt's object graph disagree
// about one VirtualMachine.
func compare(quo, graph []cluster.ObjectRef) (d difference) {
	d.missed = subtract(graph, quo)
	d.extra = subtract(quo, graph)
	return
}

// diverged reports a difference that means quo is wrong: KubeVirt's graph
// names a member of the closure quo's traversal did not find, so a bundle would not
// carry it.
//
// Objects quo found and the graph did not are not divergence. quo follows
// references KubeVirt's graph does not — a sysprep volume, an ephemeral
// volume's claim, a memory dump, the Secret a container disk is pulled with
// — and finding more than the graph does is the traversal working.
func (r difference) diverged() (wrong bool) {
	wrong = len(r.missed) > 0
	return
}

// report renders the difference so that a failure names the objects on each
// side rather than only saying that the two disagree.
func (r difference) report(namespace, name string) (report string) {
	out := &strings.Builder{}
	_, _ = fmt.Fprintf(
		out,
		"quo's traversal and KubeVirt's objectgraph subresource disagree "+
			"about VirtualMachine %s/%s.\n",
		namespace,
		name)
	describeSide(
		out,
		r.missed,
		"Found by the subresource and missed by quo (%d), which a bundle would not carry")
	describeSide(
		out,
		r.extra,
		"Found by quo and not by the subresource (%d), which is expected where quo "+
			"deliberately follows a reference KubeVirt's graph does not")
	return out.String()
}

// describeSide writes one side of the difference under a heading, and writes
// nothing at all for a side with nothing on it.
func describeSide(out *strings.Builder, refs []cluster.ObjectRef, heading string) {
	if len(refs) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\n"+heading+":\n", len(refs))
	for _, ref := range refs {
		_, _ = fmt.Fprintf(out, "  %s\n", describe(ref))
	}
}

// subtract returns the references in one set and not in the other, in the
// order the first gives them.
func subtract(these, those []cluster.ObjectRef) (only []cluster.ObjectRef) {
	held := make(map[cluster.ObjectRef]bool, len(those))
	for _, ref := range those {
		held[ref] = true
	}
	for _, ref := range these {
		if held[ref] {
			continue
		}
		only = append(only, ref)
	}
	return
}

// describe names one object the way a reader would go looking for it. A
// cluster-scoped object is named without a namespace, because it has none.
func describe(ref cluster.ObjectRef) (described string) {
	described = ref.Kind + " " + ref.Name
	if ref.Namespace == "" {
		return
	}
	described = ref.Kind + " " + ref.Namespace + "/" + ref.Name
	return
}

// The objects the comparison tests reason about.
var (
	collectedClaim = cluster.ObjectRef{
		Kind:      cluster.KindPersistentVolumeClaim,
		Namespace: "support-cases",
		Name:      "db-01-root",
	}
	collectedSecret = cluster.ObjectRef{
		Kind:      cluster.KindSecret,
		Namespace: "support-cases",
		Name:      "db-01-cloudinit",
	}
	clusterInstancetype = cluster.ObjectRef{
		Kind: cluster.KindVirtualMachineClusterInstancetype,
		Name: "large-memory",
	}
)

func TestComparisonReportsWhatEachTraversalFoundAndTheOtherDidNot(t *testing.T) {
	tests := []struct {
		name   string
		quo    []cluster.ObjectRef
		graph  []cluster.ObjectRef
		missed []cluster.ObjectRef
		extra  []cluster.ObjectRef
	}{
		{
			name:  "two traversals that agree",
			quo:   []cluster.ObjectRef{collectedClaim, collectedSecret},
			graph: []cluster.ObjectRef{collectedSecret, collectedClaim},
		},
		{
			name:   "a member of the closure KubeVirt's graph names and quo did not find",
			quo:    []cluster.ObjectRef{collectedClaim},
			graph:  []cluster.ObjectRef{collectedClaim, collectedSecret},
			missed: []cluster.ObjectRef{collectedSecret},
		},
		{
			name:  "a reference quo follows and KubeVirt's graph does not",
			quo:   []cluster.ObjectRef{collectedClaim, collectedSecret},
			graph: []cluster.ObjectRef{collectedClaim},
			extra: []cluster.ObjectRef{collectedSecret},
		},
		{
			// One name can mean a namespaced object or a cluster-scoped one,
			// and the two are different objects.
			name:   "one name under two scopes",
			quo:    []cluster.ObjectRef{clusterInstancetype},
			graph:  []cluster.ObjectRef{{Kind: clusterInstancetype.Kind, Namespace: "support-cases", Name: clusterInstancetype.Name}},
			missed: []cluster.ObjectRef{{Kind: clusterInstancetype.Kind, Namespace: "support-cases", Name: clusterInstancetype.Name}},
			extra:  []cluster.ObjectRef{clusterInstancetype},
		},
		{
			name:   "a graph quo has nothing to compare against",
			graph:  []cluster.ObjectRef{collectedClaim},
			missed: []cluster.ObjectRef{collectedClaim},
		},
		{
			name: "two empty traversals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			d := compare(tt.quo, tt.graph)

			g.Expect(d.missed).To(gomega.Equal(tt.missed))
			g.Expect(d.extra).To(gomega.Equal(tt.extra))
			g.Expect(d.diverged()).To(gomega.Equal(len(tt.missed) > 0))
		})
	}
}

func TestTheDifferenceReportNamesTheObjectsOnEachSide(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	d := compare(
		[]cluster.ObjectRef{collectedClaim, clusterInstancetype},
		[]cluster.ObjectRef{collectedClaim, collectedSecret})

	report := d.report("support-cases", "db-01")

	g.Expect(report).To(gomega.ContainSubstring("VirtualMachine support-cases/db-01"))
	g.Expect(report).To(gomega.ContainSubstring("missed by quo (1)"))
	g.Expect(report).To(gomega.ContainSubstring("Secret support-cases/db-01-cloudinit"))
	g.Expect(report).To(gomega.ContainSubstring("not by the subresource (1)"))
	// A cluster-scoped object is named without a namespace it does not have.
	g.Expect(report).To(gomega.ContainSubstring("VirtualMachineClusterInstancetype large-memory"))
	g.Expect(report).ToNot(gomega.ContainSubstring("PersistentVolumeClaim"))
}

func TestTheDifferenceReportLeavesOutASideWithNothingOnIt(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	d := compare(
		[]cluster.ObjectRef{collectedClaim},
		[]cluster.ObjectRef{collectedClaim, collectedSecret})

	report := d.report("support-cases", "db-01")

	g.Expect(report).To(gomega.ContainSubstring("missed by quo (1)"))
	g.Expect(report).ToNot(gomega.ContainSubstring("not by the subresource"))
}

func TestTheTraversalIsReadFromWhatACaptureCollectedAndWhatItCouldNotRead(t *testing.T) {
	tests := []struct {
		name   string
		record bundle.CaptureRecord
		want   []cluster.ObjectRef
	}{
		{
			name: "an object the capture collected",
			record: bundle.CaptureRecord{
				attempt(collectedClaim, bundle.OutcomeCollected),
			},
			want: []cluster.ObjectRef{collectedClaim},
		},
		{
			// The traversal found it. That the capture was then refused the
			// read is a different problem with a different fix.
			name: "an object the capture found and was not allowed to read",
			record: bundle.CaptureRecord{
				attempt(collectedSecret, bundle.OutcomeSkipped),
			},
			want: []cluster.ObjectRef{collectedSecret},
		},
		{
			name: "what quo reads about the source cluster, which is its own idea",
			record: bundle.CaptureRecord{
				attempt(collectedClaim, bundle.OutcomeCollected),
				attempt(
					cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: "fast"},
					bundle.OutcomeCollected),
				attempt(
					cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: "fast"},
					bundle.OutcomeSkipped),
				attempt(
					cluster.ObjectRef{Kind: cluster.KindPersistentVolume, Name: "pv-01"},
					bundle.OutcomeSkipped),
				attempt(cluster.ObjectRef{Kind: cluster.KindKubeVirt}, bundle.OutcomeSkipped),
			},
			want: []cluster.ObjectRef{collectedClaim},
		},
		{
			name: "something the bundle declares it needs and does not carry",
			record: bundle.CaptureRecord{
				attempt(collectedClaim, bundle.OutcomeCollected),
				attempt(
					cluster.ObjectRef{Kind: bundle.KindContainerDisk, Name: "quay.io/example/disk:latest"},
					bundle.OutcomeRequired),
			},
			want: []cluster.ObjectRef{collectedClaim},
		},
		{
			// The expansion is recorded under a kind of its own. KubeVirt's
			// graph names nothing of that kind, so counting it would report a
			// disagreement that is not one.
			name: "the VirtualMachine and the source cluster's expansion of it",
			record: bundle.CaptureRecord{
				attempt(virtualMachineRef, bundle.OutcomeCollected),
				attempt(expandedSpecRef, bundle.OutcomeCollected),
			},
			want: []cluster.ObjectRef{virtualMachineRef},
		},
		{
			name: "an object reached from more than one place",
			record: bundle.CaptureRecord{
				attempt(collectedSecret, bundle.OutcomeCollected),
				attempt(collectedSecret, bundle.OutcomeCollected),
			},
			want: []cluster.ObjectRef{collectedSecret},
		},
		{
			name:   "a capture that attempted nothing",
			record: bundle.CaptureRecord{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			g.Expect(traversed(tt.record)).To(gomega.Equal(tt.want))
		})
	}
}

// virtualMachineRef is the VirtualMachine a capture collects, and
// expandedSpecRef is the source cluster's expansion of it, which the capture
// record names under a kind of its own.
var (
	virtualMachineRef = cluster.ObjectRef{
		Kind:      cluster.KindVirtualMachine,
		Namespace: "support-cases",
		Name:      "db-01",
	}
	expandedSpecRef = cluster.ObjectRef{
		Kind:      bundle.KindExpandedSpec,
		Namespace: "support-cases",
		Name:      "db-01",
	}
)

// attempt is one entry of a capture record.
func attempt(ref cluster.ObjectRef, outcome bundle.Outcome) (a bundle.Attempt) {
	a = bundle.Attempt{
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
		Outcome:   outcome,
	}
	return
}
