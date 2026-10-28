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

// NoCDIError reports a target cluster with no CDI, from a bundle that
// holds disks to upload. A disk goes back onto a cluster only through
// CDI's upload, so there is no way past this but installing it: failing
// disk by disk for an hour is what refusing up front avoids.
type NoCDIError struct {
	// Disks is how many disks the bundle holds to upload.
	Disks int
}

// Error says what the target lacks and what needed it.
func (r *NoCDIError) Error() string {
	disks := fmt.Sprintf("%d disks", r.Disks)
	if r.Disks == 1 {
		disks = "one disk"
	}
	return fmt.Sprintf(
		"the target cluster has no CDI, and the bundle holds %s to upload; install CDI on the target cluster first",
		disks)
}

// Is reports whether err is a NoCDIError.
func (r *NoCDIError) Is(err error) (matched bool) {
	_, matched = err.(*NoCDIError)
	return
}

// UnresolvedRequirementError reports a requirement of the bundle's that
// resolved to nothing on the target cluster. The disks are not uploaded
// and the machine is not created for a bundle whose needs cannot be
// placed; the error says what was tried, so the mapping that is missing
// can be named on the command line.
type UnresolvedRequirementError struct {
	// Kind is the sort of thing the requirement asks the target for: a
	// storage class or a network.
	Kind string
	// Requirement is the bundle's own name for what it needs: the class
	// a disk was provisioned from, or the VirtualMachine's name for the
	// network.
	Requirement string
	// Tried says every way the deploy tried to place it, and what would
	// place it instead.
	Tried string
}

// Error names the requirement and what was tried.
func (r *UnresolvedRequirementError) Error() string {
	return fmt.Sprintf(
		"%s %q resolves to nothing on the target cluster: %s",
		r.Kind,
		r.Requirement,
		r.Tried)
}

// Is reports whether err is an UnresolvedRequirementError.
func (r *UnresolvedRequirementError) Is(err error) (matched bool) {
	_, matched = err.(*UnresolvedRequirementError)
	return
}

// UnsupportedVolumeModeError reports a disk whose volume mode the target
// cluster's own account of the matched storage class rules out. No mapping
// fixes that: the class cannot hold the disk as it was captured, so the
// machine would not be the one the customer has.
type UnsupportedVolumeModeError struct {
	Claim        string
	StorageClass string
	VolumeMode   string
}

// Error names the disk, the mode it needs and the class that cannot
// provide it.
func (r *UnsupportedVolumeModeError) Error() string {
	return fmt.Sprintf(
		"PersistentVolumeClaim %q needs volumeMode %s, which storage class %q on the target cluster cannot provide",
		r.Claim,
		r.VolumeMode,
		r.StorageClass)
}

// Is reports whether err is an UnsupportedVolumeModeError.
func (r *UnsupportedVolumeModeError) Is(err error) (matched bool) {
	_, matched = err.(*UnsupportedVolumeModeError)
	return
}
