package capture

import "fmt"

// NotStoppedError reports a VirtualMachine that quo will not capture because
// it is not stopped. Capturing a running VirtualMachine would read its disks
// while the guest is writing to them, producing a bundle that cannot be
// trusted to reproduce anything.
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
