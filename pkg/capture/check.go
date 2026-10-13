package capture

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// Check writes to out what a capture of the named VirtualMachine would
// collect and what it would skip, and stops there.
//
// It reads what a capture reads — the VirtualMachine, the resources it
// refers to, and what describes the source cluster — and gives every object
// the outcome a capture would give it, under the reasons a capture record
// uses. A permissions problem is then found in seconds rather than at the end
// of a transfer that took hours.
//
// Nothing is transferred and no VirtualMachineExport is created. The disks are
// reported from the claims behind them.
//
// A VirtualMachine that is not stopped is reported rather than refused.
// Saying that a capture would refuse it is the most useful thing a check can
// say about a running VirtualMachine, and the rest of what a capture would do
// is worth knowing before it is stopped.
//
// An object that could not be read is the finding a check exists to produce,
// not a failure of the check. The error returned is for a check that could
// not report at all.
func Check(ctx context.Context, c cluster.Cluster, opt Options, out io.Writer) (err error) {
	vm, err := c.GetVirtualMachine(ctx, opt.Namespace, opt.Name)
	if err != nil {
		return
	}
	co := &collector{sink: &discard{}}
	required, err := co.collect(ctx, c, vm)
	if err != nil {
		return
	}
	checked := &check{
		opt:    opt,
		status: printableStatus(vm),
		record: co.record,
		disks:  buildCheckedDisks(vm, required),
	}
	err = checked.describe(out)
	return
}

// check is what a capture of one VirtualMachine would do.
type check struct {
	opt    Options
	status string
	record bundle.CaptureRecord
	disks  []checkedDisk
}

// checkedDisk is one disk a capture would transfer and what it would cost.
type checkedDisk struct {
	claim   string
	volumes []string
	// recorded is the provisioned size of the claim behind the disk, as the
	// claim gives it, and size is that size in bytes. known is false for a
	// claim quo could not read, whose size is therefore unknown.
	recorded string
	size     int64
	known    bool
}

// discard is the sink of a check: it keeps nothing, because a check reports
// what a capture would collect and collects none of it.
type discard struct{}

// Collect keeps nothing. NO-OP.
func (r *discard) Collect(resource bundle.Resource, data []byte) (err error) {
	return
}

// AddExpandedSpec keeps nothing. NO-OP.
func (r *discard) AddExpandedSpec(data []byte) {
}

// buildCheckedDisks returns the disks a capture would transfer, each with the
// provisioned size of the claim behind it.
//
// The sizes are the ones the requirements record, so that a check and a bundle
// say the same thing about a disk.
func buildCheckedDisks(
	vm *unstructured.Unstructured,
	required bundle.Requirements) (disks []checkedDisk) {
	//
	sizeByClaim := make(map[string]string, len(required.Storage))
	for i := range required.Storage {
		sizeByClaim[required.Storage[i].Claim] = required.Storage[i].Size
	}
	for _, backing := range claimedVolumes(vm) {
		disk := checkedDisk{
			claim:    backing.name,
			volumes:  backing.volumes,
			recorded: sizeByClaim[backing.name],
		}
		disk.size, disk.known = parseSize(disk.recorded)
		disks = append(disks, disk)
	}
	return
}

// parseSize reads a provisioned size the requirements record. A size quo could
// not read is reported as unknown rather than as zero, which would read as a
// disk that costs nothing.
func parseSize(recorded string) (size int64, known bool) {
	if recorded == "" {
		return
	}
	quantity, err := resource.ParseQuantity(recorded)
	if err != nil {
		return
	}
	size = quantity.Value()
	known = true
	return
}

// diskSpace returns the local scratch space a capture would need and the total
// provisioned size of the disks it would transfer.
//
// Scratch space is the largest single disk rather than the total, because a
// capture stages one disk at a time and deletes it before the next starts.
//
// complete is false when a claim quo could not read left a disk with no size,
// which makes both figures lower bounds. The disk still travels in a bundle,
// so leaving it out of the total would understate the transfer.
func diskSpace(disks []checkedDisk) (scratch, provisioned int64, complete bool) {
	complete = true
	for i := range disks {
		if !disks[i].known {
			complete = false
			continue
		}
		provisioned += disks[i].size
		if disks[i].size > scratch {
			scratch = disks[i].size
		}
	}
	return
}

// describe writes a check in a form meant to be read by a person.
//
// Write errors are discarded per line because tabwriter reports them again
// from Flush, which is checked.
func (r *check) describe(out io.Writer) (err error) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Check of VirtualMachine %s/%s\n", r.opt.Namespace, r.opt.Name)
	_, _ = fmt.Fprintf(tw, "  Status:\t%s\n", orUnreported(r.status))
	r.describeRefusal(tw)
	r.describeCollected(tw)
	r.describeRequired(tw)
	r.describeSkipped(tw)
	r.describeDisks(tw)
	err = tw.Flush()
	if err != nil {
		err = fmt.Errorf("write check: %w", err)
		return
	}
	return
}

// describeRefusal says whether a capture would go ahead. Fixing permissions
// will not clear a VirtualMachine that is not stopped, so it is written at
// the top rather than left to the bottom with the rest of the findings.
//
// The rest of the check is written either way: it says what a capture would
// collect once the VirtualMachine has been stopped. What it cannot show is a
// volume hotplugged into the running VirtualMachine, which a stopped one does
// not have.
func (r *check) describeRefusal(out io.Writer) {
	if r.status == StatusStopped {
		_, _ = fmt.Fprintf(out, "  Capture:\twould run\n")
		return
	}
	refused := &NotStoppedError{
		Namespace: r.opt.Namespace,
		Name:      r.opt.Name,
		Status:    r.status,
	}
	_, _ = fmt.Fprintf(out, "  Capture:\trefused: %s\n", refused)
}

// describeCollected lists every object a capture would collect.
func (r *check) describeCollected(out io.Writer) {
	collected := r.record.Collected()
	if len(collected) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\nWould be collected (%d)\n", len(collected))
	for i := range collected {
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\n",
			collected[i].Kind,
			r.named(collected[i]))
	}
}

// describeRequired lists what a bundle would declare it needs from a target.
//
// It is kept apart from what would be collected, because a requirement is
// exactly the thing a bundle does not carry. What a target would have to
// provide is still worth settling before a transfer rather than after it.
func (r *check) describeRequired(out io.Writer) {
	required := r.record.Required()
	if len(required) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\nWould be required from a target (%d)\n", len(required))
	for i := range required {
		_, _ = fmt.Fprintf(out, "  %s\t%s\n", required[i].Kind, r.named(required[i]))
	}
}

// describeSkipped lists everything a capture would not collect, with the
// reason a capture record would give for it.
//
// The heading is written even when nothing would be skipped, because the
// engineer running a check is asking exactly that question and no section at
// all would leave them wondering whether quo had looked.
func (r *check) describeSkipped(out io.Writer) {
	skipped := r.record.Skipped()
	_, _ = fmt.Fprintf(out, "\nWould be skipped (%d)\n", len(skipped))
	for i := range skipped {
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\t%s\t%s\n",
			skipped[i].Kind,
			r.named(skipped[i]),
			skipped[i].Reason,
			skipped[i].Message)
	}
}

// describeDisks writes the disks a capture would transfer and what they would
// cost: the local scratch space, and the total a reader needs in order to
// judge the scale of what they are about to transfer.
//
// Both are provisioned sizes. A disk is transferred compressed, so a sparsely
// used volume costs a fraction of this, and the figures are an upper bound
// rather than a prediction.
func (r *check) describeDisks(out io.Writer) {
	_, _ = fmt.Fprintf(out, "\nDisks (%d)\n", len(r.disks))
	if len(r.disks) == 0 {
		return
	}
	for i := range r.disks {
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\tvolumes: %s\n",
			r.disks[i].claim,
			renderDiskSize(r.disks[i]),
			strings.Join(r.disks[i].volumes, ", "))
	}
	scratch, provisioned, complete := diskSpace(r.disks)
	_, _ = fmt.Fprintf(
		out,
		"\n  Scratch space needed:\t%s\n",
		renderSpace(scratch, complete))
	_, _ = fmt.Fprintf(
		out,
		"  Total disk size:\t%s\n",
		renderSpace(provisioned, complete))
}

// named renders the name of the object one entry of the record is about.
//
// A namespace is shown only when it is not the one being captured from, which
// is how a reader spots an object from elsewhere in the cluster. An entry
// carrying no name is one quo never learned the name of: KubeVirt's
// configuration is a singleton whose name only reading it supplies, so a read
// that failed leaves the name empty.
func (r *check) named(attempt bundle.Attempt) (named string) {
	named = attempt.Name
	if named == "" {
		named = "(unknown)"
		return
	}
	if attempt.Namespace == "" || attempt.Namespace == r.opt.Namespace {
		return
	}
	named = attempt.Namespace + "/" + attempt.Name
	return
}

// renderDiskSize renders one disk's provisioned size, in the form the claim
// behind it gives the size rather than one of quo's own. It is the figure a
// reader will see again from kubectl.
func renderDiskSize(disk checkedDisk) (rendered string) {
	rendered = "(unknown)"
	if !disk.known {
		return
	}
	rendered = disk.recorded
	return
}

// renderSpace renders an amount of disk space a capture would need. A figure
// quo could only partly account for is given as a lower bound, and one it
// could not account for at all is not given as a number.
//
// These are quo's own arithmetic over sizes the claims gave in whatever units
// each was written in, so they are rendered the way quo renders a transfer
// rather than as a Kubernetes quantity.
func renderSpace(size int64, complete bool) (rendered string) {
	rendered = FormatSize(size)
	if complete {
		return
	}
	if size == 0 {
		rendered = "(unknown)"
		return
	}
	rendered = "at least " + rendered
	return
}

// orUnreported renders a printable status, saying so when the VirtualMachine
// reports none.
func orUnreported(status string) (rendered string) {
	rendered = status
	if rendered == "" {
		rendered = "(unreported)"
	}
	return
}
