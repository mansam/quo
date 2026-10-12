package capture

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// progressInterval is how often a transfer in flight is reported.
const progressInterval = 2 * time.Second

// claim is a PersistentVolumeClaim the VirtualMachine references and the
// volumes that reference it.
type claim struct {
	name    string
	volumes []string
}

// transfer is one disk on its way from the export into the bundle.
type transfer struct {
	export  cluster.DiskExport
	disk    bundle.Disk
	scratch string
	report  func(transferred int64, done bool)
}

// writeBundle opens the bundle, stages every disk into it, and closes it.
func writeBundle(
	ctx context.Context,
	writer *bundle.Writer,
	provenance bundle.Provenance,
	transfers []transfer) (err error) {
	//
	for i := range transfers {
		writer.Reserve(transfers[i].disk)
	}
	err = writer.Open(provenance)
	if err != nil {
		return
	}
	for i := range transfers {
		err = transfers[i].stage(ctx, writer)
		if err != nil {
			return
		}
	}
	err = writer.Close()
	return
}

// planTransfers pairs each claim the VirtualMachine references with the
// compressed format the export offers it in.
func planTransfers(claims []claim, export cluster.DiskExport, opt Options) (transfers []transfer, err error) {
	offered := export.Disks()
	formatByClaim := make(map[string]cluster.DiskFormat, len(offered))
	for i := range offered {
		formatByClaim[offered[i].Claim] = offered[i].Format
	}
	for i := range claims {
		format, found := formatByClaim[claims[i].name]
		if !found {
			transfers = nil
			err = &DiskNotExportedError{Claim: claims[i].name, Volumes: claims[i].volumes}
			return
		}
		var recorded bundle.DiskFormat
		recorded, err = recordedFormat(claims[i].name, format)
		if err != nil {
			transfers = nil
			return
		}
		transfers = append(transfers, transfer{
			export: export,
			disk: bundle.Disk{
				Claim:   claims[i].name,
				Path:    bundle.DiskPath(claims[i].name, recorded),
				Format:  recorded,
				Volumes: claims[i].volumes,
			},
			scratch: opt.Scratch,
		})
	}
	for i := range transfers {
		transfers[i].report = newReporter(opt, transfers, i)
	}
	return
}

// recordedFormat maps the format an export offers onto the one a bundle
// records. It is the single point at which KubeVirt's vocabulary becomes
// quo's, so an unknown format is reported rather than stored.
func recordedFormat(claim string, format cluster.DiskFormat) (recorded bundle.DiskFormat, err error) {
	switch format {
	case cluster.FormatGzip:
		recorded = bundle.DiskGzip
	case cluster.FormatArchiveGzip:
		recorded = bundle.DiskArchiveGzip
	default:
		err = fmt.Errorf("disk %q is offered as %q, which quo does not store", claim, format)
	}
	return
}

// stage downloads one disk and writes it into the bundle.
//
// The disk is staged in a file, which establishes its compressed size and
// digest, written into the bundle, and deleted before the next one starts.
// Peak local disk is therefore the largest single compressed disk rather than
// the total.
func (r *transfer) stage(ctx context.Context, writer *bundle.Writer) (err error) {
	staged, err := os.CreateTemp(r.scratch, ".quo-disk-*")
	if err != nil {
		err = fmt.Errorf("stage disk %q: %w", r.disk.Claim, err)
		return
	}
	defer func() {
		_ = staged.Close()
		_ = os.Remove(staged.Name())
	}()
	size, err := r.download(ctx, staged)
	if err != nil {
		return
	}
	_, err = staged.Seek(0, io.SeekStart)
	if err != nil {
		err = fmt.Errorf("rewind staged disk %q: %w", r.disk.Claim, err)
		return
	}
	err = writer.Stage(r.disk.Path, staged, size)
	return
}

// download transfers one disk into the staged file, reporting progress as it
// goes. The bytes are stored exactly as they arrive: the export server has
// already compressed them and quo compresses nothing of its own.
func (r *transfer) download(ctx context.Context, staged io.Writer) (size int64, err error) {
	r.report(0, false)
	stream, err := r.export.OpenDisk(ctx, r.disk.Claim)
	if err != nil {
		return
	}
	defer func() {
		_ = stream.Close()
	}()
	transferred := &progressReader{in: stream, report: r.report, reported: time.Now()}
	size, err = io.Copy(staged, transferred)
	if err != nil {
		size = 0
		err = fmt.Errorf("transfer disk %q: %w", r.disk.Claim, err)
		return
	}
	r.report(size, true)
	return
}

// newReporter returns the function that reports one disk's progress, which
// does nothing when the caller asked for no progress.
func newReporter(opt Options, transfers []transfer, at int) (report func(transferred int64, done bool)) {
	if opt.Progress == nil {
		report = func(transferred int64, done bool) {}
		return
	}
	p := Progress{
		Claim:   transfers[at].disk.Claim,
		Volumes: transfers[at].disk.Volumes,
		Disk:    at + 1,
		Disks:   len(transfers),
	}
	report = func(transferred int64, done bool) {
		p.Transferred = transferred
		p.Done = done
		opt.Progress(p)
	}
	return
}

// progressReader reports how much of a disk has arrived while it is being
// read. Reports are spaced out in time, because a disk is read in small
// pieces and a report per piece would be too many. The caller sets reported
// to the time the transfer started, so a disk that arrives quickly is
// reported only as started and as finished.
type progressReader struct {
	in       io.Reader
	report   func(transferred int64, done bool)
	received int64
	reported time.Time
}

// Read passes the read through and reports what has arrived so far.
func (r *progressReader) Read(b []byte) (n int, err error) {
	n, err = r.in.Read(b)
	r.received += int64(n)
	if time.Since(r.reported) < progressInterval {
		return
	}
	r.reported = time.Now()
	r.report(r.received, false)
	return
}

// claimedVolumes returns the claims the VirtualMachine's volumes reference, in
// the order they first appear.
//
// Two volumes referencing one claim produce one entry naming both. The bytes
// behind them are the same bytes, so downloading and storing them twice would
// double a capture for no gain.
func claimedVolumes(vm *unstructured.Unstructured) (claims []claim) {
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	indexOf := map[string]int{}
	for i := range volumes {
		volume, cast := volumes[i].(map[string]any)
		if !cast {
			continue
		}
		name, _, _ := unstructured.NestedString(volume, "name")
		backing := claimName(volume)
		if name == "" || backing == "" {
			continue
		}
		at, found := indexOf[backing]
		if !found {
			indexOf[backing] = len(claims)
			claims = append(claims, claim{name: backing, volumes: []string{name}})
			continue
		}
		claims[at].volumes = append(claims[at].volumes, name)
	}
	return
}

// claimName returns the PersistentVolumeClaim a volume is backed by, empty
// when it is backed by something that is not a claim. A DataVolume provisions
// a claim of its own name, so all three forms resolve to a claim.
func claimName(volume map[string]any) (name string) {
	name, found, _ := unstructured.NestedString(volume, "persistentVolumeClaim", "claimName")
	if found {
		return
	}
	name, found, _ = unstructured.NestedString(volume, "dataVolume", "name")
	if found {
		return
	}
	name, _, _ = unstructured.NestedString(volume, "memoryDump", "claimName")
	return
}
