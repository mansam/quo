// Package bundle defines quo's bundle format and reads and writes it.
//
// A bundle is an uncompressed tar. Its first member is always the manifest.
// Before any other member arrives, a reader learns the format version, where
// the bundle came from, and what else the bundle holds. The bundle groups the
// remaining members by what they are. Collected resources sit under
// "resources". Disks sit under "disks". The source cluster's expansion of the
// VirtualMachine sits in a member of its own.
//
// quo compresses nothing itself. Objects are plain YAML, so a bundle opens
// with ordinary tools. Disk members hold the already-compressed bytes the
// export server produced.
package bundle

import (
	"path"
	"strings"
)

// FormatVersion is the bundle format this build writes. It is the only
// format this build reads.
const FormatVersion = "v1"

// Member paths
const (
	// ManifestPath is the first member of every bundle.
	ManifestPath = "manifest.yaml"
	// ResourcesDir groups the resources a bundle collected.
	ResourcesDir = "resources"
	// DisksDir groups the disks a bundle carries.
	DisksDir = "disks"
	// ExpandedSpecPath holds the source cluster's expansion of the
	// VirtualMachine. It sits outside the resources grouping because no
	// object on the cluster has that name and nothing applies it to a target.
	ExpandedSpecPath = "expanded-spec.yaml"
)

// Kinds the bundle format names for itself, rather than because quo reads
// an object of that kind from a cluster.
const (
	// KindContainerDisk is how the capture record names a container image
	// that serves a volume. No Kubernetes object stands for it. A target
	// has to be able to pull it, so the capture record names it by its
	// image reference.
	KindContainerDisk = "ContainerDisk"
	// KindExpandedSpec is how the capture record names the source cluster's
	// expansion of the VirtualMachine. Recording it under a kind of its own
	// keeps it apart from the VirtualMachine the bundle collected, which
	// carries the same kind and name.
	KindExpandedSpec = "ExpandedSpec"
)

// Outcome is what became of one object quo tried to capture.
type Outcome string

// Outcomes
const (
	// OutcomeCollected is an object the bundle holds.
	OutcomeCollected Outcome = "collected"
	// OutcomeRequired is something the bundle declares it needs from a target
	// and does not hold.
	OutcomeRequired Outcome = "required"
	// OutcomeSkipped is an object quo tried to collect and could not.
	OutcomeSkipped Outcome = "skipped"
)

// outcomes in the order a reader sees them.
var outcomes = []Outcome{
	OutcomeCollected,
	OutcomeRequired,
	OutcomeSkipped,
}

// SkipReason is why a skipped object was not collected. It is what lets a
// reader tell an object that was never there from one quo was not allowed to
// look at.
type SkipReason string

// Skip reasons
const (
	// SkipForbidden is an object the person capturing the bundle was not
	// allowed to read.
	SkipForbidden SkipReason = "forbidden"
	// SkipNotFound is an object the cluster does not have.
	SkipNotFound SkipReason = "not found"
	// SkipNotInstalled is an object of a kind the cluster does not serve at
	// all, because the optional component providing it is absent.
	SkipNotInstalled SkipReason = "not installed"
	// SkipUnreadable is any other failure to read an object.
	SkipUnreadable SkipReason = "unreadable"
)

// DiskFormat names the form a bundle stores a disk in. quo compresses
// nothing itself, so this records what the export server sent.
type DiskFormat string

// Disk formats
const (
	// DiskGzip is a gzip-compressed raw disk image.
	DiskGzip DiskFormat = "gzip"
	// DiskArchiveGzip is a gzip-compressed tar of a filesystem volume's
	// content. It is the compressed form offered for a volume that holds
	// files rather than a block device.
	DiskArchiveGzip DiskFormat = "tar.gz"
)

// ResourcePath is where a resource of the given kind sits inside a bundle.
// Grouping by kind is what lets a reader find an object by hand.
//
// Layout is this package's concern, so nothing outside it assembles a member
// path of its own.
func ResourcePath(kind, name string) (member string) {
	member = path.Join(ResourcesDir, strings.ToLower(kind), name+".yaml")
	return
}

// DiskPath is where the disk backing the named PersistentVolumeClaim sits
// inside a bundle. The extension says which compressed form the bytes are in.
func DiskPath(claim string, format DiskFormat) (member string) {
	extension := ".gz"
	switch format {
	case DiskArchiveGzip:
		extension = ".tar.gz"
	case DiskGzip:
		extension = ".gz"
	}
	member = path.Join(DisksDir, claim+extension)
	return
}
