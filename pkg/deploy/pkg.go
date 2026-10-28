// Package deploy puts a bundle onto a cluster as a stopped VirtualMachine
// with its disks populated through CDI's upload.
//
// Deploy is the library entry point. The CLI only parses a command line
// and calls it, and tests call it directly: given a fake target and a
// bundle, a test gets back the deploy record a real deploy would print.
//
// The VirtualMachine a deploy creates is the bundle's expanded spec — the
// source cluster's own resolution of the machine — carrying the forced
// rewrites and nothing else. The captured VirtualMachine object is never
// applied: it stays the declaration of record. Instancetypes, preferences
// and ControllerRevisions are never created either; they are in the
// bundle to be read.
//
// Every disk becomes a DataVolume named after the claim it backs, with
// source upload, and the bundle's bytes are streamed into it unchanged.
// The VirtualMachine's volume stanzas are left alone: a volume referencing
// a claim by name finds the claim the DataVolume creates, and the
// dataVolumeTemplates the declaration carried are removed, because the
// DataVolumes they describe are created on their own.
//
// The bundle's requirements — its storage classes and its networks — are
// mapped onto the target's resources, automatically by name or as the
// options name, and every mapping is reported in the deploy record,
// because nothing maps silently.
package deploy

// Options is a deploy once the command line is parsed.
type Options struct {
	// Bundle is the path of the bundle to deploy, exactly as the command
	// line named it. The exact filename lands in an annotation on every
	// created object, so that a running machine can be tied back to the
	// bundle — and the case — it came from.
	Bundle string
	// Namespace is the namespace to deploy into. It is required: quo
	// never falls back to the kubeconfig's current namespace, because a
	// diagnostic VirtualMachine landing in someone's default namespace by
	// accident is a quiet mistake.
	Namespace string
	// Name renames the deployed VirtualMachine. Empty deploys the machine
	// under the name it was captured with; nothing in a VirtualMachine's
	// spec references its own name, so a rename forces no other rewrite.
	Name string
	// StorageClasses places the disks of the bundle's storage classes.
	// The key is the class the bundle records — or *, which covers every
	// class that does not match by name, the unnamed class included —
	// and the value is the target's class those disks land on. A class
	// that resolves to nothing, by name or by mapping, refuses the
	// deploy.
	StorageClasses map[string]string
	// Networks attaches the machine's multus networks. The key is the
	// VirtualMachine's own name for the network, and the value names the
	// NetworkAttachmentDefinition of the target's it attaches to,
	// namespace-qualified when it lives outside the deploy namespace. A
	// network that resolves to nothing, by the attachment's name or by a
	// mapping, refuses the deploy.
	Networks map[string]string
	// Progress, when set, is called while a disk uploads, so that a
	// deploy that takes hours can be told apart from one that has hung.
	Progress func(p Progress)
}

// Progress describes how far a disk upload has got.
type Progress struct {
	// Claim is the PersistentVolumeClaim the disk is going back into.
	Claim string
	// Volumes names the volumes of the deployed VirtualMachine the disk
	// serves.
	Volumes []string
	// Disk is the position of this disk in the deploy, counting from one,
	// and Disks is how many there are.
	Disk  int
	Disks int
	// Transferred is how many bytes have been sent so far.
	Transferred int64
	// Done reports the disk as fully sent.
	Done bool
}

// Outcome is what became of one object a deploy tried to create.
type Outcome string

// Outcomes
const (
	// OutcomeCreated is an object the target now holds.
	OutcomeCreated Outcome = "created"
	// OutcomeFailed is an object the deploy tried to create and could
	// not.
	OutcomeFailed Outcome = "failed"
	// OutcomeSkipped is an object the deploy did not try to create,
	// because what it would have created it from was not there to use.
	OutcomeSkipped Outcome = "skipped"
)

// Record is what a deploy reports about what it did: every object it
// attempted and what became of each one, and every requirement of the
// bundle's it mapped onto the target's resources — because nothing maps
// silently — or could only report.
type Record struct {
	// Steps lists every object the deploy attempted, in the order they
	// were attempted.
	Steps []Step
	// Requirements lists every requirement the bundle declared, in the
	// order the deploy resolved them, with what each was mapped onto.
	Requirements []Requirement
}

// Step is one entry of the deploy record: an object a deploy tried to put
// onto the target and what became of it.
type Step struct {
	Kind string
	// Namespace is empty for a cluster-scoped object.
	Namespace string
	Name      string
	Outcome   Outcome
	// Reason is set only on an object that was not created: why the
	// target does not hold it.
	Reason string
}

// Requirement is one of the bundle's requirements as a deploy placed it
// on the target: what the bundle asked for, what the target provides
// for it, and how the two were put together. A requirement a deploy
// cannot place — a container disk image, which the target has to be
// able to pull — is reported with nothing mapped.
type Requirement struct {
	// Kind says what sort of thing the requirement asks the target for:
	// a storage class, a network, or a container disk image.
	Kind string
	// From is the requirement's own name for what it needs: the class
	// the bundle records, the VirtualMachine's name for the network, or
	// the image a container disk is served from.
	From string
	// To is what the target provides for it: the class the disks were
	// placed on, or the NetworkAttachmentDefinition the network was
	// attached to, as the applied VirtualMachine names it. Empty when
	// nothing is mapped.
	To string
	// How says how the mapping was made — the target's resource of the
	// requirement's own name, or the flag that named it — or, when
	// nothing is mapped, what the target has to do for it.
	How string
}
