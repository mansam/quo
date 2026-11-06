// Package deploy puts a bundle onto a cluster as a stopped VirtualMachine,
// with its disks populated through CDI's upload. It takes a bundle's
// deployment off a cluster again.
//
// Deploy and Undeploy are the library entry points. The CLI only parses
// a command line and calls one of them. Tests call them directly. Given
// a fake target and a bundle, a test receives the deploy record or the
// list of deletions a real run would print.
//
// The VirtualMachine a deploy creates is the bundle's expanded spec, the
// source cluster's own resolution of the machine. It carries the forced
// rewrites and nothing else. The captured VirtualMachine object is never
// applied: it stays the declaration of record. Instancetypes, preferences
// and ControllerRevisions are never created either. They are in the
// bundle to be read.
//
// The ConfigMaps, Secrets and ServiceAccounts a bundle holds are applied
// unchanged apart from the forced rewrites. The machine's cloud-init,
// sysprep and access credentials are therefore present when it is
// examined. Everything else a bundle holds, the network and storage
// objects above all, is never written, because it describes the source
// cluster rather than the machine.
//
// Every disk becomes a DataVolume named after the claim it backs, with
// source upload. The bundle's bytes are streamed into it unchanged. The
// VirtualMachine's volume stanzas stay as they are: a volume referencing
// a claim by name finds the claim the DataVolume creates. The
// dataVolumeTemplates the declaration carried are removed, because the
// DataVolumes they describe are created on their own.
//
// The bundle's requirements, its storage classes and its networks, are
// mapped onto the target's resources, automatically by name or as the
// options name. Every mapping is reported in the deploy record, because
// nothing maps silently.
package deploy

// Options is a deploy once the command line is parsed.
type Options struct {
	// Bundle is the path of the bundle to deploy, exactly as the command
	// line named it. The exact filename is written into an annotation on
	// every created object. A running machine can therefore be traced to
	// the bundle, and the case, it came from.
	Bundle string
	// Namespace is the namespace to deploy into. It is required. quo
	// never uses the kubeconfig's current namespace in its place. A
	// diagnostic VirtualMachine that arrives in someone's default
	// namespace by accident is a quiet mistake.
	Namespace string
	// Name renames the deployed VirtualMachine. Empty deploys the machine
	// under the name it was captured with. Nothing in a VirtualMachine's
	// spec references its own name, so a rename forces no other rewrite.
	Name string
	// StorageClasses places the disks of the bundle's storage classes.
	// The key is the class the bundle records, or *, which covers every
	// class that does not match by name, the unnamed class included. The
	// value is the target's class that receives those disks. A class
	// that maps to nothing, by name or by flag, refuses the deploy.
	StorageClasses map[string]string
	// Networks attaches the machine's multus networks. The key is the
	// VirtualMachine's own name for the network. The value names the
	// NetworkAttachmentDefinition of the target's it attaches to. The
	// value is namespace-qualified when the definition lives outside the
	// deploy namespace. A network that maps to nothing, by the
	// attachment's name or by a flag, refuses the deploy.
	Networks map[string]string
	// DryRun reports what the deploy would do without doing any of it.
	// Every refusal still runs. The record still says every object the
	// deploy would create and every mapping it would apply. Nothing is
	// created, nothing is uploaded, and no labels reach anything. The
	// target is only read.
	DryRun bool
	// Progress, when set, is called while a disk uploads: while the
	// claim it needs becomes ready, and as its bytes move. A caller can
	// then distinguish a deploy that takes hours from one that is hung.
	Progress func(p Progress)
}

// Progress describes how far along a disk upload is.
type Progress struct {
	// Claim is the PersistentVolumeClaim the disk returns to.
	Claim string
	// Volumes names the volumes of the deployed VirtualMachine the disk
	// serves.
	Volumes []string
	// Disk is the position of this disk in the deploy, counting from one,
	// and Disks is how many there are.
	Disk  int
	Disks int
	// State is what the upload is doing: waiting for the claim to be
	// ready, transferring the disk, or done.
	State State
	// Transferred is the count of bytes sent so far. It counts only
	// bytes that have moved.
	Transferred int64
	// Said is what the upload waits on, in the target's words. It is
	// set only while the upload waits.
	Said string
}

// State is what a disk's upload is doing.
type State string

// States
const (
	// StateWaiting is an upload whose claim is not ready to receive its
	// bytes yet. Said says what it waits on.
	StateWaiting State = "waiting"
	// StateTransferring is an upload whose bytes are moving.
	StateTransferring State = "transferring"
	// StateDone is an upload whose bytes have all arrived.
	StateDone State = "done"
)

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

// Record is what a deploy reports about what it did. It holds every
// object the deploy attempted and what became of each one. It also holds
// every requirement of the bundle's the deploy mapped onto the target's
// resources, or could only report. Nothing maps silently.
type Record struct {
	// Steps lists every object the deploy attempted, in the order they
	// were attempted.
	Steps []Step
	// Requirements lists every requirement the bundle declared, in the
	// order the deploy mapped them, with what each was mapped onto.
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

// dryRunStep puts on a step what a dry run did with the object it stands
// for. Nothing is created. The step still says created, because the
// record a dry run returns is the record a real deploy would print.
func dryRunStep(step *Step) {
	step.Outcome = OutcomeCreated
}

// Kind is the sort of thing a requirement asks the target for.
type Kind string

// Kinds
const (
	// KindStorageClass is the storage a disk asks to be placed on.
	KindStorageClass Kind = "storage class"
	// KindNetwork is the network one of the machine's networks asks to
	// be attached to.
	KindNetwork Kind = "network"
	// KindContainerDisk is the container disk image a volume is served
	// from, which the target has to be able to pull.
	KindContainerDisk Kind = "container disk"
)

// Requirement is one of the bundle's requirements as a deploy placed it
// on the target. It says what the bundle asked for, what the target
// provides for it, and how the two were put together. A requirement a
// deploy cannot place is reported with nothing mapped. A container disk
// image is such a requirement: the target has to be able to pull it.
type Requirement struct {
	// Kind says what sort of thing the requirement asks the target for:
	// a storage class, a network, or a container disk image.
	Kind Kind
	// From is the requirement's own name for what it needs. It is the
	// class the bundle records, the VirtualMachine's name for the
	// network, or the image a container disk is served from.
	From string
	// To is what the target provides for it: the class the disks were
	// placed on, or the NetworkAttachmentDefinition the network was
	// attached to. The value is as the applied VirtualMachine names it.
	// To is empty when nothing is mapped.
	To string
	// How says how the mapping was made: the target's resource of the
	// requirement's own name, or the flag that named it. When nothing is
	// mapped, How says what the target has to do for it.
	How string
}
