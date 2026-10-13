package capture

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// Export captures the VirtualMachine named in opt and writes a bundle to out.
//
// It refuses a VirtualMachine that is not stopped, before writing anything.
// The VirtualMachine is carried exactly as the cluster returned it: quo
// normalises nothing, so MAC addresses, firmware UUID, serial and chassis
// values travel as they are.
//
// The destination is seekable because the manifest is the first member of a
// bundle and indexes every other one, including disks whose size and digest
// are only known once they have arrived. The writer reserves room for the
// manifest at the front and fills it in at the end.
func Export(ctx context.Context, c cluster.Cluster, opt Options, out io.WriteSeeker) (err error) {
	vm, err := c.GetVirtualMachine(ctx, opt.Namespace, opt.Name)
	if err != nil {
		return
	}
	status := printableStatus(vm)
	if status != StatusStopped {
		err = &NotStoppedError{
			Namespace: opt.Namespace,
			Name:      opt.Name,
			Status:    status,
		}
		return
	}
	info, err := c.GetInfo(ctx)
	if err != nil {
		return
	}
	writer := bundle.NewWriter(out)
	err = carryVirtualMachine(writer, vm)
	if err != nil {
		return
	}
	err = carryClosure(ctx, c, writer, vm)
	if err != nil {
		return
	}
	provenance := bundle.Provenance{
		SourceCluster:     info.SourceCluster,
		Namespace:         opt.Namespace,
		VirtualMachine:    opt.Name,
		CapturedAt:        time.Now().UTC(),
		KubeVirtVersion:   info.KubeVirtVersion,
		KubernetesVersion: info.KubernetesVersion,
	}
	claims := claimedVolumes(vm)
	if len(claims) == 0 {
		err = writeBundle(ctx, writer, provenance, nil)
		return
	}
	// The export is created before anything is written and removed whatever
	// happens afterwards.
	export, err := c.ExportDisks(ctx, opt.Namespace, opt.Name)
	if err != nil {
		return
	}
	defer func() {
		// A capture that has just spent hours transferring disks is not worth
		// failing over an export that outlives it, and the export expires on
		// its own. The capture record will record this once it exists.
		_ = export.Close()
	}()
	transfers, err := planTransfers(claims, export, opt)
	if err != nil {
		return
	}
	err = writeBundle(ctx, writer, provenance, transfers)
	return
}

// ExportToFile captures into the file at dest.
//
// The bundle is written beside dest under a temporary name and moved into
// place only once the capture has succeeded, so a capture that fails part-way
// leaves no bundle behind to be mistaken for a whole one. Disks are staged
// beside it too, because the directory the user chose for the bundle is the
// one they chose for having room.
func ExportToFile(ctx context.Context, c cluster.Cluster, opt Options, dest string) (err error) {
	if opt.Scratch == "" {
		opt.Scratch = filepath.Dir(dest)
	}
	partial, err := os.CreateTemp(filepath.Dir(dest), ".quo-*.partial")
	if err != nil {
		err = fmt.Errorf("create temporary bundle: %w", err)
		return
	}
	committed := false
	defer func() {
		_ = partial.Close()
		if !committed {
			_ = os.Remove(partial.Name())
		}
	}()
	err = Export(ctx, c, opt, partial)
	if err != nil {
		return
	}
	err = partial.Close()
	if err != nil {
		err = fmt.Errorf("close temporary bundle: %w", err)
		return
	}
	err = os.Rename(partial.Name(), dest)
	if err != nil {
		err = fmt.Errorf("move bundle to %q: %w", dest, err)
		return
	}
	committed = true
	return
}

// printableStatus returns the VirtualMachine's printable status, empty when it
// reports none.
func printableStatus(vm *unstructured.Unstructured) (status string) {
	status, _, _ = unstructured.NestedString(vm.Object, "status", "printableStatus")
	return
}
