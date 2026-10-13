package bundle

import "time"

// Manifest is the first member of a bundle. It names the format version, where
// the bundle came from, and indexes every other member.
//
// Fields carry JSON tags because the manifest is marshalled through the
// Kubernetes YAML library, which encodes with JSON semantics.
type Manifest struct {
	FormatVersion string        `json:"formatVersion"`
	Provenance    Provenance    `json:"provenance"`
	Requirements  Requirements  `json:"requirements,omitzero"`
	CaptureRecord CaptureRecord `json:"captureRecord,omitempty"`
	Objects       []Object      `json:"objects,omitempty"`
	Evidence      []Object      `json:"evidence,omitempty"`
	Members       []Member      `json:"members"`
	Disks         []Disk        `json:"disks,omitempty"`
}

// Requirements is what a bundle declares a target cluster would have to
// provide for the VirtualMachine it carries: the networks it expects, what
// each of its disks needs from storage, and the container images it is served
// from. None of it travels in the bundle.
//
// It is recorded at capture because every part of it is free to read while
// connected to the source cluster and expensive to reconstruct afterwards. It
// sits in the manifest, which is the first member of a bundle, so that a
// support engineer can tell whether their cluster can satisfy a bundle before
// committing to a transfer that may take hours.
type Requirements struct {
	Networks       []NetworkRequirement       `json:"networks,omitempty"`
	Storage        []StorageRequirement       `json:"storage,omitempty"`
	ContainerDisks []ContainerDiskRequirement `json:"containerDisks,omitempty"`
}

// NetworkType says what a network requirement asks a target for.
type NetworkType string

// Network types
const (
	// NetworkPod is the cluster's own pod network, which every cluster has
	// and which therefore names no object.
	NetworkPod NetworkType = "pod"
	// NetworkMultus is a secondary network attached through Multus, which a
	// target provides under a NetworkAttachmentDefinition of its own.
	NetworkMultus NetworkType = "multus"
)

// NetworkRequirement is one network the VirtualMachine expects.
type NetworkRequirement struct {
	// Name is the VirtualMachine's own name for the network, which is what
	// its interfaces bind to.
	Name string `json:"name"`
	// Type says what has to be provided.
	Type NetworkType `json:"type"`
	// Namespace and Attachment name the NetworkAttachmentDefinition a Multus
	// network is backed by on the source cluster. Both are empty for a pod
	// network.
	Namespace  string `json:"namespace,omitempty"`
	Attachment string `json:"attachment,omitempty"`
}

// StorageRequirement is what one of the VirtualMachine's disks needs from a
// target's storage, so that a candidate storage class can be checked against
// it rather than guessed at.
//
// Every field but the claim is empty when quo could not read the claim it
// describes. The disk still travels in the bundle and a target still has to
// find room for it, so the requirement is declared with what is known rather
// than dropped.
type StorageRequirement struct {
	// Claim is the PersistentVolumeClaim backing the disk, which is also how
	// the bundle names the member holding the disk's content.
	Claim string `json:"claim"`
	// StorageClass is the class the claim was provisioned from. It is empty
	// for a claim bound to a volume that was made by hand rather than
	// provisioned, which names no class, and for a claim quo could not read.
	// A claim that asked for no class in particular is not one of those: the
	// cluster writes the default it chose into the claim.
	StorageClass string `json:"storageClass,omitempty"`
	// Size is the capacity a target has to provide for this disk.
	Size string `json:"size,omitempty"`
	// VolumeMode is Block or Filesystem. A target that can only offer the
	// other one cannot run the VirtualMachine as captured.
	VolumeMode string `json:"volumeMode,omitempty"`
}

// ContainerDiskRequirement is a container image one of the VirtualMachine's
// volumes is served from.
//
// quo does not repackage container images, so the image is declared and never
// carried: a target has to be able to pull it.
type ContainerDiskRequirement struct {
	// Volume is the VirtualMachine's name for the volume served from the
	// image.
	Volume string `json:"volume"`
	// Image is the image reference exactly as the VirtualMachine gives it.
	Image string `json:"image"`
}

// CaptureRecord lists every object a capture attempted and what became of
// each one, in the order they were attempted.
//
// It lives in the manifest so that it cannot be separated from the bundle it
// describes, and it holds the objects quo could not collect as well as the
// ones it could. An object missing from a bundle with no entry saying so
// would leave a reader unable to tell an object that never existed from one
// quo was not allowed to read.
//
// One object can appear twice under different dispositions, because the two
// entries answer different questions. A StorageClass is read as evidence and
// is also something a target has to provide, and the second entry still holds
// when the first says the read was forbidden.
type CaptureRecord []Attempt

// Skipped returns the objects the capture could not collect.
func (r CaptureRecord) Skipped() (skipped []Attempt) {
	for i := range r {
		if r[i].Disposition != DispositionSkipped {
			continue
		}
		skipped = append(skipped, r[i])
	}
	return
}

// Attempt is one entry of the capture record: an object quo tried to capture
// and what became of it.
type Attempt struct {
	Kind string `json:"kind"`
	// Namespace is empty for a cluster-scoped object.
	Namespace   string      `json:"namespace,omitempty"`
	Name        string      `json:"name"`
	Disposition Disposition `json:"disposition"`
	// Reason is set only on a skipped object.
	Reason SkipReason `json:"reason,omitempty"`
	// Message is what the cluster said when the read failed, which is where
	// an actionable detail such as the missing permission appears.
	Message string `json:"message,omitempty"`
}

// Object names one object a bundle holds and where it sits, so that a reader
// learns what a bundle holds from the manifest alone rather than by untarring
// it and inferring kinds from member paths. The manifest lists carried objects
// and evidence separately, so which of the two an entry is follows from the
// list it appears in.
type Object struct {
	Kind string `json:"kind"`
	// Namespace is empty for a cluster-scoped object.
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Path      string `json:"path"`
}

// Provenance is what a bundle records about where it came from, so that a
// support engineer can tie it back to the case it was captured for.
type Provenance struct {
	SourceCluster     string    `json:"sourceCluster"`
	Namespace         string    `json:"namespace"`
	VirtualMachine    string    `json:"virtualMachine"`
	CapturedAt        time.Time `json:"capturedAt"`
	KubeVirtVersion   string    `json:"kubeVirtVersion"`
	KubernetesVersion string    `json:"kubernetesVersion"`
}

// Member indexes one member of a bundle other than the manifest. The digest
// travels inside the bundle so that integrity data cannot be separated from
// what it describes.
type Member struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// Disk maps the volumes of the captured VirtualMachine onto the bundle member
// holding their content.
//
// A disk is keyed by the PersistentVolumeClaim that backs it rather than by
// volume name. A VirtualMachine's volumes are a named list and nothing stops
// two of them referencing one claim, so keying by volume name would store the
// same bytes twice. Volumes is therefore a list: a member serving two volumes
// records both.
//
// Format names what the bytes are. quo stores what the export server sent and
// compresses nothing itself, so a reader needs to be told which form arrived.
type Disk struct {
	Claim   string     `json:"claim"`
	Path    string     `json:"path"`
	Format  DiskFormat `json:"format"`
	Volumes []string   `json:"volumes"`
}
