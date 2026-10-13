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
	CaptureRecord CaptureRecord `json:"captureRecord,omitempty"`
	Objects       []Object      `json:"objects,omitempty"`
	Members       []Member      `json:"members"`
	Disks         []Disk        `json:"disks,omitempty"`
}

// CaptureRecord lists every object a capture attempted and what became of
// each one, in the order they were attempted.
//
// It lives in the manifest so that it cannot be separated from the bundle it
// describes, and it holds the objects quo could not collect as well as the
// ones it could. An object missing from a bundle with no entry saying so
// would leave a reader unable to tell an object that never existed from one
// quo was not allowed to read.
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

// Object names one carried object and where it sits in the bundle, so that a
// reader learns what a bundle holds from the manifest alone rather than by
// untarring it and inferring kinds from member paths.
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
