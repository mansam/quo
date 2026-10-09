// Package capture turns a stopped VirtualMachine on a cluster into a bundle.
//
// Export is the library entry point. The CLI is a thin wrapper over it, and it
// is the point tests attach to: given a fake cluster and a buffer, a test gets
// back the bundle a real capture would have produced.
package capture

// Options names the VirtualMachine to capture.
type Options struct {
	Namespace string
	Name      string
}

// Statuses
const (
	// StatusStopped is the only printable status quo will capture. KubeVirt
	// reports many others, and all of them mean the disks could change while
	// they are being read.
	StatusStopped = "Stopped"
)
