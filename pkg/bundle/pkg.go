// Package bundle defines quo's bundle format and reads and writes it.
//
// A bundle is an uncompressed tar. Its first member is always the manifest, so
// a reader learns the format version, where the bundle came from, and what else
// the bundle holds before any other member arrives. Remaining members are
// grouped by what they are for: carried objects under "objects", evidence under
// "evidence", and disks under "disks", so that a reader is never misled into
// applying something that was collected only to be read.
//
// quo compresses nothing itself. Objects are plain YAML so that a bundle opens
// with ordinary tools, and disk members hold the already-compressed bytes the
// export server produced.
package bundle

import (
	"path"
	"strings"
)

// FormatVersion is the bundle format this build writes, and the only one it
// knows how to read.
const FormatVersion = "v1"

// Member paths
const (
	// ManifestPath is the first member of every bundle.
	ManifestPath = "manifest.yaml"
	// ObjectsDir groups the objects a bundle carries.
	ObjectsDir = "objects"
	// DisksDir groups the disks a bundle carries.
	DisksDir = "disks"
)

// Disposition is what became of one object quo tried to capture.
type Disposition string

// Dispositions
const (
	// DispositionCarried is an object shipped verbatim in the bundle,
	// because its content is the thing that matters.
	DispositionCarried Disposition = "carried"
	// DispositionEvidence is an object captured because it explains the
	// environment the VirtualMachine came from. It is read by a support
	// engineer and never applied to a target.
	DispositionEvidence Disposition = "evidence"
	// DispositionRequirement is something the bundle declares it needs from
	// a target but does not carry.
	DispositionRequirement Disposition = "requirement"
	// DispositionSkipped is an object quo tried to collect and could not.
	DispositionSkipped Disposition = "skipped"
)

// dispositions in the order a reader is shown them.
var dispositions = []Disposition{
	DispositionCarried,
	DispositionEvidence,
	DispositionRequirement,
	DispositionSkipped,
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

// DiskFormat names the form a disk is stored in. quo compresses nothing
// itself, so this records what the export server sent.
type DiskFormat string

// Disk formats
const (
	// DiskGzip is a gzip-compressed raw disk image.
	DiskGzip DiskFormat = "gzip"
	// DiskArchiveGzip is a gzip-compressed tar of a filesystem volume's
	// content, which is the compressed form offered for a volume that holds
	// files rather than a block device.
	DiskArchiveGzip DiskFormat = "tar.gz"
)

// ObjectPath is where a carried object of the given kind sits inside a
// bundle. Grouping by kind is what lets a reader find an object by hand.
//
// Layout is this package's concern, so nothing outside it assembles a member
// path of its own.
func ObjectPath(kind, name string) (member string) {
	member = path.Join(ObjectsDir, strings.ToLower(kind), name+".yaml")
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
