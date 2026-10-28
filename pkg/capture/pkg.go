// Package capture turns a stopped VirtualMachine on a cluster into a bundle.
//
// Export is the library entry point. The CLI only parses a command line and
// calls it. Tests call it directly. Given a fake cluster and somewhere to
// write, a test receives the bundle a real capture would have produced.
package capture

// Options names the VirtualMachine to capture and how to capture it.
type Options struct {
	Namespace string
	Name      string
	// Scratch is where a disk is staged while it is captured. Empty means the
	// system temporary directory. Only one disk is ever staged at a time, so
	// it needs room for the largest single compressed disk.
	Scratch string
	// Progress, when set, is called while a disk transfers. The caller can
	// then tell a capture that takes hours from one that has hung.
	Progress func(p Progress)
}

// Progress describes how far a disk transfer has got.
type Progress struct {
	// Claim is the PersistentVolumeClaim the capture is transferring.
	Claim string
	// Volumes names the volumes the disk serves.
	Volumes []string
	// Disk is the position of this disk in the capture, counting from one,
	// and Disks is how many there are.
	Disk  int
	Disks int
	// Transferred is how many bytes have arrived so far.
	Transferred int64
	// Done reports the disk as fully transferred.
	Done bool
}

// Statuses
const (
	// StatusStopped is the only printable status quo will capture. KubeVirt
	// reports many others, and all of them mean the disks could change while
	// the capture reads them.
	StatusStopped = "Stopped"
)
