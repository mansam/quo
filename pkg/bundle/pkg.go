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

import "path"

// FormatVersion is the bundle format this build writes, and the only one it
// knows how to read.
const FormatVersion = "v1"

// Member paths
const (
	// ManifestPath is the first member of every bundle.
	ManifestPath = "manifest.yaml"
	// ObjectsDir groups the objects a bundle carries.
	ObjectsDir = "objects"
)

// VirtualMachinePath is where the named VirtualMachine sits inside a bundle.
// Layout is this package's concern, so nothing outside it assembles a member
// path of its own.
func VirtualMachinePath(name string) (member string) {
	member = path.Join(ObjectsDir, "virtualmachine", name+".yaml")
	return
}
