package bundle

import "time"

// Manifest is the first member of a bundle. It names the format version, where
// the bundle came from, and indexes every other member.
//
// Fields carry JSON tags because the manifest is marshalled through the
// Kubernetes YAML library, which encodes with JSON semantics.
type Manifest struct {
	FormatVersion string     `json:"formatVersion"`
	Provenance    Provenance `json:"provenance"`
	Members       []Member   `json:"members"`
	Disks         []Disk     `json:"disks,omitempty"`
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
