//go:build live

package live

import (
	"fmt"
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// Dispositions that mean quo's traversal found the object.
//
// A skipped object was found and then could not be read. Leaving it out
// would report quo as having missed something it looked for and was refused,
// which is a different problem with a different fix.
var traversal = map[bundle.Disposition]bool{
	bundle.DispositionCarried: true,
	bundle.DispositionSkipped: true,
}

// Kinds quo reads only as evidence about the source environment, never as
// part of the closure. They are quo's own idea and KubeVirt's graph has no
// opinion about them.
//
// This is a list of what to leave out rather than a list of what to compare,
// so that a kind neither side knows about today still shows up tomorrow.
// Comparing against a list of known kinds would filter the new kind off both
// sides and report agreement, which is the one answer this suite must never
// give wrongly.
var evidenceKinds = map[string]bool{
	cluster.KindKubeVirt:         true,
	cluster.KindPersistentVolume: true,
	cluster.KindStorageClass:     true,
	cluster.KindStorageProfile:   true,
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
// something a target has to provide rather than an object quo read. Neither
// does evidence, except that the form the source cluster expands the
// VirtualMachine to is recorded under the VirtualMachine's own kind and
// name, so it arrives as a duplicate of the carried one rather than as
// something new.
func traversed(record bundle.CaptureRecord) (refs []cluster.ObjectRef) {
	seen := map[cluster.ObjectRef]bool{}
	for i := range record {
		if !traversal[record[i].Disposition] {
			continue
		}
		if evidenceKinds[record[i].Kind] {
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
	carriedClaim = cluster.ObjectRef{
		Kind:      cluster.KindPersistentVolumeClaim,
		Namespace: "support-cases",
		Name:      "db-01-root",
	}
	carriedSecret = cluster.ObjectRef{
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
			quo:   []cluster.ObjectRef{carriedClaim, carriedSecret},
			graph: []cluster.ObjectRef{carriedSecret, carriedClaim},
		},
		{
			name:   "a member of the closure KubeVirt's graph names and quo did not find",
			quo:    []cluster.ObjectRef{carriedClaim},
			graph:  []cluster.ObjectRef{carriedClaim, carriedSecret},
			missed: []cluster.ObjectRef{carriedSecret},
		},
		{
			name:  "a reference quo follows and KubeVirt's graph does not",
			quo:   []cluster.ObjectRef{carriedClaim, carriedSecret},
			graph: []cluster.ObjectRef{carriedClaim},
			extra: []cluster.ObjectRef{carriedSecret},
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
			graph:  []cluster.ObjectRef{carriedClaim},
			missed: []cluster.ObjectRef{carriedClaim},
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
		[]cluster.ObjectRef{carriedClaim, clusterInstancetype},
		[]cluster.ObjectRef{carriedClaim, carriedSecret})

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
		[]cluster.ObjectRef{carriedClaim},
		[]cluster.ObjectRef{carriedClaim, carriedSecret})

	report := d.report("support-cases", "db-01")

	g.Expect(report).To(gomega.ContainSubstring("missed by quo (1)"))
	g.Expect(report).ToNot(gomega.ContainSubstring("not by the subresource"))
}

func TestTheTraversalIsReadFromWhatACaptureCarriedAndWhatItCouldNotRead(t *testing.T) {
	tests := []struct {
		name   string
		record bundle.CaptureRecord
		want   []cluster.ObjectRef
	}{
		{
			name: "an object the capture carried",
			record: bundle.CaptureRecord{
				attempt(carriedClaim, bundle.DispositionCarried),
			},
			want: []cluster.ObjectRef{carriedClaim},
		},
		{
			// The traversal found it. That the capture was then refused the
			// read is a different problem with a different fix.
			name: "an object the capture found and was not allowed to read",
			record: bundle.CaptureRecord{
				attempt(carriedSecret, bundle.DispositionSkipped),
			},
			want: []cluster.ObjectRef{carriedSecret},
		},
		{
			name: "evidence about the source environment, which is quo's own idea",
			record: bundle.CaptureRecord{
				attempt(carriedClaim, bundle.DispositionCarried),
				attempt(
					cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: "fast"},
					bundle.DispositionEvidence),
				attempt(
					cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: "fast"},
					bundle.DispositionSkipped),
				attempt(
					cluster.ObjectRef{Kind: cluster.KindPersistentVolume, Name: "pv-01"},
					bundle.DispositionSkipped),
				attempt(cluster.ObjectRef{Kind: cluster.KindKubeVirt}, bundle.DispositionSkipped),
			},
			want: []cluster.ObjectRef{carriedClaim},
		},
		{
			name: "something the bundle declares it needs and does not carry",
			record: bundle.CaptureRecord{
				attempt(carriedClaim, bundle.DispositionCarried),
				attempt(
					cluster.ObjectRef{Kind: bundle.KindContainerDisk, Name: "quay.io/example/disk:latest"},
					bundle.DispositionRequirement),
			},
			want: []cluster.ObjectRef{carriedClaim},
		},
		{
			// The expanded form is held as evidence under the
			// VirtualMachine's own kind and name, so it would otherwise
			// arrive twice.
			name: "the VirtualMachine, carried and expanded",
			record: bundle.CaptureRecord{
				attempt(virtualMachineRef, bundle.DispositionCarried),
				attempt(virtualMachineRef, bundle.DispositionEvidence),
			},
			want: []cluster.ObjectRef{virtualMachineRef},
		},
		{
			name: "an object reached from more than one place",
			record: bundle.CaptureRecord{
				attempt(carriedSecret, bundle.DispositionCarried),
				attempt(carriedSecret, bundle.DispositionCarried),
			},
			want: []cluster.ObjectRef{carriedSecret},
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

// virtualMachineRef is the VirtualMachine a capture carries, which it also
// holds as evidence in the form the source cluster expands it to.
var virtualMachineRef = cluster.ObjectRef{
	Kind:      cluster.KindVirtualMachine,
	Namespace: "support-cases",
	Name:      "db-01",
}

// attempt is one entry of a capture record.
func attempt(ref cluster.ObjectRef, disposition bundle.Disposition) (a bundle.Attempt) {
	a = bundle.Attempt{
		Kind:        ref.Kind,
		Namespace:   ref.Namespace,
		Name:        ref.Name,
		Disposition: disposition,
	}
	return
}
