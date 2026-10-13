// Package cluster is quo's boundary with the outside world. Everything quo
// reads from outside its own process passes through the Cluster interface, so
// that a test can substitute a fake and never needs a running cluster or an
// HTTP server.
package cluster

import (
	"context"
	"errors"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ErrNotFound reports an object the cluster does not have. A VirtualMachine
// can refer to something that has since been deleted, which is not a reason to
// abandon a capture, so the caller is given a signal it can act on rather than
// an error from the Kubernetes client.
var ErrNotFound = errors.New("object not found")

// Cluster is everything quo needs from outside its own process.
type Cluster interface {
	// GetVirtualMachine returns the named VirtualMachine.
	GetVirtualMachine(ctx context.Context, namespace, name string) (vm *unstructured.Unstructured, err error)
	// GetObject returns the object the reference names. It reports ErrNotFound
	// when the cluster does not have it.
	GetObject(ctx context.Context, ref ObjectRef) (object *unstructured.Unstructured, err error)
	// GetInfo returns what a bundle records about the cluster it came from.
	GetInfo(ctx context.Context) (info Info, err error)
	// ExportDisks creates a VirtualMachineExport for the named VirtualMachine
	// and waits for it to become ready. The caller closes the result, which
	// removes the export from the cluster.
	ExportDisks(ctx context.Context, namespace, name string) (export DiskExport, err error)
}

// ObjectRef names one object quo reads from a cluster. A cluster-scoped
// object carries no namespace.
type ObjectRef struct {
	Kind      string
	Namespace string
	Name      string
}

// Kinds quo reads while working out a VirtualMachine's closure. KubeVirt's
// own object graph is the specification for the list: a VirtualMachineInstance
// and a virt-launcher Pod appear in it but are absent here, because a stopped
// VirtualMachine has neither.
const (
	KindConfigMap                         = "ConfigMap"
	KindControllerRevision                = "ControllerRevision"
	KindDataVolume                        = "DataVolume"
	KindNetworkAttachmentDefinition       = "NetworkAttachmentDefinition"
	KindPersistentVolumeClaim             = "PersistentVolumeClaim"
	KindSecret                            = "Secret"
	KindServiceAccount                    = "ServiceAccount"
	KindVirtualMachine                    = "VirtualMachine"
	KindVirtualMachineClusterInstancetype = "VirtualMachineClusterInstancetype"
	KindVirtualMachineClusterPreference   = "VirtualMachineClusterPreference"
	KindVirtualMachineInstancetype        = "VirtualMachineInstancetype"
	KindVirtualMachinePreference          = "VirtualMachinePreference"
)

// DiskExport is a VirtualMachineExport that has become ready: the disks of one
// VirtualMachine, each addressed by the PersistentVolumeClaim behind it.
type DiskExport interface {
	// Disks returns what the export offers, in the order its status lists it.
	Disks() (disks []ExportedDisk)
	// OpenDisk opens the byte stream carrying the disk that backs the named
	// PersistentVolumeClaim. The caller closes it.
	OpenDisk(ctx context.Context, claim string) (stream io.ReadCloser, err error)
	// Close removes the export from the cluster.
	Close() (err error)
}

// ExportedDisk is one disk an export offers.
type ExportedDisk struct {
	// Claim is the PersistentVolumeClaim the disk is held in.
	Claim string
	// Format is the compressed form the bytes arrive in.
	Format DiskFormat
}

// DiskFormat names the form a disk arrives in. quo always asks for a
// compressed one, so that a sparsely used volume transfers at roughly its used
// size rather than its provisioned size.
type DiskFormat string

// Disk formats
const (
	// FormatGzip is a gzip-compressed raw disk image, which is what a volume
	// holding a block device is offered as.
	FormatGzip DiskFormat = "gzip"
	// FormatArchiveGzip is a gzip-compressed tar, which is what a volume
	// holding files is offered as.
	FormatArchiveGzip DiskFormat = "tar.gz"
)

// Info describes the cluster a bundle was captured from.
//
// A version is empty when quo was not allowed to read it. Capturing with only
// namespace-level permissions is expected, and a missing version is not a
// reason to abandon a capture.
type Info struct {
	SourceCluster     string
	KubeVirtVersion   string
	KubernetesVersion string
}
