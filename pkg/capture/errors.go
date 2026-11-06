package capture

import (
	"fmt"
	"strings"
)

// DiskNotExportedError reports a claim the VirtualMachine references that the
// export does not offer. Capturing without it would produce a bundle missing
// one of the machine's disks, which is worse than no bundle at all.
type DiskNotExportedError struct {
	Claim   string
	Volumes []string
}

// Error names the claim and the volumes that would have been served by it.
func (r *DiskNotExportedError) Error() string {
	return fmt.Sprintf(
		"the export offers no disk for PersistentVolumeClaim %q, which backs volume %s",
		r.Claim,
		strings.Join(r.Volumes, ", "))
}

// Is reports whether err is a DiskNotExportedError.
func (r *DiskNotExportedError) Is(err error) (matched bool) {
	_, matched = err.(*DiskNotExportedError)
	return
}

// NotStoppedError reports a VirtualMachine that quo will not capture because
// it is not stopped. Capturing a running VirtualMachine would read its disks
// while the guest is writing to them. That would produce a bundle that
// cannot be trusted to reproduce anything.
type NotStoppedError struct {
	Namespace string
	Name      string
	Status    string
}

// Error names the VirtualMachine, its status, and how to stop it.
func (r *NotStoppedError) Error() string {
	return fmt.Sprintf(
		"VirtualMachine %s/%s is %s, not Stopped. Stop it and capture it again: virtctl stop -n %s %s",
		r.Namespace,
		r.Name,
		r.observedStatus(),
		r.Namespace,
		r.Name)
}

// Is reports whether err is a NotStoppedError.
func (r *NotStoppedError) Is(err error) (matched bool) {
	_, matched = err.(*NotStoppedError)
	return
}

// observedStatus renders the status for a reader. A VirtualMachine that
// reports none is still refused: quo cannot confirm it is stopped.
func (r *NotStoppedError) observedStatus() (status string) {
	status = r.Status
	if status == "" {
		status = "in an unreported state"
	}
	return
}
