package capture

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// Export captures the VirtualMachine named in opt and writes a bundle to w.
//
// It refuses a VirtualMachine that is not stopped, before writing anything.
// The VirtualMachine is carried exactly as the cluster returned it: quo
// normalises nothing, so MAC addresses, firmware UUID, serial and chassis
// values travel as they are.
func Export(ctx context.Context, c cluster.Cluster, opt Options, w io.Writer) (err error) {
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
	encoded, err := yaml.Marshal(vm.Object)
	if err != nil {
		err = fmt.Errorf("marshal VirtualMachine %s/%s: %w", opt.Namespace, opt.Name, err)
		return
	}
	writer := &bundle.Writer{}
	writer.Add(bundle.VirtualMachinePath(opt.Name), encoded)
	provenance := bundle.Provenance{
		SourceCluster:     info.SourceCluster,
		Namespace:         opt.Namespace,
		VirtualMachine:    opt.Name,
		CapturedAt:        time.Now().UTC(),
		KubeVirtVersion:   info.KubeVirtVersion,
		KubernetesVersion: info.KubernetesVersion,
	}
	err = writer.Write(w, provenance)
	return
}

// ExportToFile captures into the file at dest.
//
// The bundle is written beside dest under a temporary name and moved into
// place only once the capture has succeeded, so a capture that fails part-way
// leaves no bundle behind to be mistaken for a whole one.
func ExportToFile(ctx context.Context, c cluster.Cluster, opt Options, dest string) (err error) {
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
