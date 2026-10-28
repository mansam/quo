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
// The ConfigMaps, Secrets and ServiceAccounts a bundle holds are applied
// unchanged apart from the forced rewrites, so that the machine's
// cloud-init, sysprep and access credentials are present when it is
// examined. Everything else a bundle holds — the network and storage
// objects above all — is never written, because it describes the source
// cluster rather than the machine.
//
// Every disk becomes a DataVolume named after the claim it backs, with
// source upload, and the bundle's bytes are streamed into it unchanged.
// The VirtualMachine's volume stanzas are left alone: a volume referencing
// a claim by name finds the claim the DataVolume creates, and the
// dataVolumeTemplates the declaration carried are removed, because the
// DataVolumes they describe are created on their own.
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

// Record lists every object a deploy attempted and what became of each
// one, in the order they were attempted.
//
// It is the deploy-side mirror of the capture record: what a deploy prints
// when it ends, so that the person who ran it knows exactly what the
// target holds.
type Record []Step

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
