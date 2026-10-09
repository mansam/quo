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
