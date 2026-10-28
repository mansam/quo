package deploy

import (
	"fmt"
)

// NoExpandedSpecError reports a bundle whose capture could not read the
// source cluster's expansion of the VirtualMachine. The expansion is what
// a deploy applies — the captured VirtualMachine object stays the
// declaration of record — so a bundle holding none cannot be deployed at
// all, and quo refuses rather than deploy half of one.
type NoExpandedSpecError struct {
	Bundle string
}

// Error names the bundle and says what it lacks.
func (r *NoExpandedSpecError) Error() string {
	return fmt.Sprintf("bundle %q holds no expanded spec, so it cannot be deployed", r.Bundle)
}

// Is reports whether err is a NoExpandedSpecError.
func (r *NoExpandedSpecError) Is(err error) (matched bool) {
	_, matched = err.(*NoExpandedSpecError)
	return
}

// MissingNamespaceError reports a namespace the target cluster does not
// have. A deploy refuses before creating anything, rather than leave
// objects half-way into a namespace that was never there.
type MissingNamespaceError struct {
	Namespace string
}

// Error names the namespace and says how to make it.
func (r *MissingNamespaceError) Error() string {
	return fmt.Sprintf(
		"namespace %q does not exist on the target cluster; create it first: kubectl create namespace %q",
		r.Namespace,
		r.Namespace)
}

// Is reports whether err is a MissingNamespaceError.
func (r *MissingNamespaceError) Is(err error) (matched bool) {
	_, matched = err.(*MissingNamespaceError)
	return
}

// AlreadyExistsError reports an object the target cluster already holds
// where a deploy would create one of its own. Someone else's work is
// neither overwritten nor built around.
type AlreadyExistsError struct {
	Kind      string
	Namespace string
	Name      string
}

// Error names the object that is already there.
func (r *AlreadyExistsError) Error() string {
	return fmt.Sprintf("%s %s/%s already exists on the target cluster", r.Kind, r.Namespace, r.Name)
}

// Is reports whether err is an AlreadyExistsError.
func (r *AlreadyExistsError) Is(err error) (matched bool) {
	_, matched = err.(*AlreadyExistsError)
	return
}

// NotCreatedError reports a deploy that ran but did not create everything
// it tried to. The deploy record names each object and what became of it;
// this error is what makes the exit code non-zero, so that a script can
// tell a complete deploy from a partial one.
type NotCreatedError struct {
	NotCreated int
	Attempted  int
}

// Error says how much of the deploy arrived.
func (r *NotCreatedError) Error() string {
	return fmt.Sprintf(
		"the deploy created %d of the %d objects it tried to",
		r.Attempted-r.NotCreated,
		r.Attempted)
}

// Is reports whether err is a NotCreatedError.
func (r *NotCreatedError) Is(err error) (matched bool) {
	_, matched = err.(*NotCreatedError)
	return
}
