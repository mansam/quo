// Package cluster is quo's boundary with the outside world. Everything quo
// reads from outside its own process passes through the Cluster interface.
// A test can therefore substitute a fake. The test never needs a running
// cluster or an HTTP server.
package cluster

import (
	"context"
	"errors"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Why an object could not be read. None of these is a reason to abandon a
// capture. The caller therefore receives one of these errors to act on and
// record, rather than an error from the Kubernetes client.
var (
	// ErrNotFound reports an object the cluster does not have. A
	// VirtualMachine can refer to something that was later deleted.
	ErrNotFound = errors.New("object not found")
	// ErrForbidden reports an object the caller is not allowed to read.
	// Capturing as a namespace owner without cluster read is expected.
	ErrForbidden = errors.New("not allowed to read object")
	// ErrNotInstalled reports a kind the cluster serves no resource for,
	// because the optional component providing it is absent. CDI and Multus
	// are both optional. The absence of either is an error.
	ErrNotInstalled = errors.New("kind is not installed on the cluster")
)

// Cluster is everything quo needs from outside its own process.
type Cluster interface {
	// GetVirtualMachine returns the named VirtualMachine.
	GetVirtualMachine(ctx context.Context, namespace, name string) (vm *unstructured.Unstructured, err error)
	// GetObject returns the object the reference names. GetObject reports
	// a failed read as ErrNotFound, ErrForbidden or ErrNotInstalled where
	// that is what happened. A capture can then record why it skipped the
	// object.
	GetObject(ctx context.Context, ref ObjectRef) (object *unstructured.Unstructured, err error)
	// GetKubeVirt returns KubeVirt's own configuration, above all its
	// enabled feature gates. KubeVirt is a singleton whose namespace and
	// name differ between installations, so quo does not address it by
	// reference.
	GetKubeVirt(ctx context.Context) (kv *unstructured.Unstructured, err error)
	// ExpandSpec returns the form the cluster resolves the named
	// VirtualMachine to, through KubeVirt's expand-spec subresource.
	ExpandSpec(ctx context.Context, namespace, name string) (expanded *unstructured.Unstructured, err error)
	// GetInfo returns what a bundle records about the cluster it came from.
	GetInfo(ctx context.Context) (info Info, err error)
	// ExportDisks creates a VirtualMachineExport for the VirtualMachine
	// the export names and waits for it to become ready. The caller
	// closes the result, which deletes the export from the cluster.
	// While the export becomes ready, ExportDisks calls export.Waiting,
	// when set, once per check it makes, saying what it waits on.
	ExportDisks(ctx context.Context, export Export) (disk DiskExport, err error)
}

// ObjectRef names one object quo reads from a cluster. A cluster-scoped
// object carries no namespace.
type ObjectRef struct {
	Kind      string
	Namespace string
	Name      string
}

// Kinds quo reads while it computes a VirtualMachine's closure. KubeVirt's
// own object graph is the specification for the list. A
// VirtualMachineInstance and a virt-launcher Pod appear in it but are
// absent here, because a stopped VirtualMachine has neither.
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

// Kinds quo reads because they describe the source cluster rather than
// because the VirtualMachine refers to them. All of them sit outside the
// namespace the capture runs in. A capture run by a namespace owner has
// no permission there.
//
// quo reads KubeVirt through GetKubeVirt rather than by reference, because
// it is a singleton whose namespace and name differ between installations.
const (
	KindKubeVirt         = "KubeVirt"
	KindPersistentVolume = "PersistentVolume"
	KindStorageClass     = "StorageClass"
	KindStorageProfile   = "StorageProfile"
)

// KindCDIConfig is CDI's own configuration. A deploy reads it to tell a
// target that has CDI from one that does not. Only CDI's upload puts a
// bundle's disks onto a cluster.
const KindCDIConfig = "CDIConfig"

// CDIConfigName is the name of the CDIConfig singleton: CDI creates one
// and calls it config.
const CDIConfigName = "config"

// DiskExport is a VirtualMachineExport that is ready. It offers the disks
// of one VirtualMachine. The caller addresses each disk by the
// PersistentVolumeClaim behind it.
type DiskExport interface {
	// Disks returns what the export offers, in the order its status lists it.
	Disks() (disks []ExportedDisk)
	// OpenDisk opens the byte stream carrying the disk that backs the named
	// PersistentVolumeClaim. The caller closes it.
	OpenDisk(ctx context.Context, claim string) (stream io.ReadCloser, err error)
	// Close deletes the export from the cluster.
	Close() (err error)
}

// ExportedDisk is one disk an export offers.
type ExportedDisk struct {
	// Claim is the PersistentVolumeClaim that holds the disk.
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
	// FormatGzip is a gzip-compressed raw disk image. The export offers a
	// volume holding a block device in that form.
	FormatGzip DiskFormat = "gzip"
	// FormatArchiveGzip is a gzip-compressed tar. The export offers a
	// volume holding files in that form.
	FormatArchiveGzip DiskFormat = "tar.gz"
)

// Info describes the cluster a bundle was captured from.
//
// A version is empty when quo was not allowed to read it. Capturing with
// only namespace-level permissions is expected. A missing version is not
// a reason to abandon a capture.
type Info struct {
	SourceCluster     string
	KubeVirtVersion   string
	KubernetesVersion string
}
