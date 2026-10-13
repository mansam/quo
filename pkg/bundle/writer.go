package bundle

import (
	"archive/tar"
	"fmt"
	"io"
	"math"
	"path"
	"strings"

	"sigs.k8s.io/yaml"
)

// placeholderDigest stands in for a digest that is not known yet. Every
// SHA-256 digest renders at this width, so a manifest holding placeholders is
// exactly as wide as the manifest that replaces it.
var placeholderDigest = "sha256:" + strings.Repeat("0", 64)

// Writer writes a bundle to a seekable destination.
//
// The manifest is the first member and indexes every other one, but a disk's
// size and digest are not known until it has arrived, and a disk is far too
// large to hold on to until the manifest can be written. The writer therefore
// reserves room for the manifest at the front of the bundle, writes each
// member as it becomes available, and fills the manifest in at the end. The
// reservation is sized from a manifest holding the widest value every unknown
// field can take, so the real manifest always fits.
//
// A bundle is written in three steps: collect the objects and declare the
// disks, Open, then stage each disk in turn and Close.
type Writer struct {
	out        io.WriteSeeker
	tw         *tar.Writer
	provenance Provenance
	pending    []content
	members    []Member
	objects    []Object
	disks      []Disk
	indexOf    map[string]int
	offset     int64
	reserved   int
}

// content is one member held until the bundle is opened.
type content struct {
	path string
	data []byte
}

// NewWriter returns a Writer that writes a bundle to out.
func NewWriter(out io.WriteSeeker) (w *Writer) {
	w = &Writer{
		out:     out,
		indexOf: map[string]int{},
	}
	return
}

// Add collects a member, which is held in memory until the bundle is opened.
// Objects are small enough for that; disks are not and go through Expect and
// Stage instead.
func (r *Writer) Add(path string, data []byte) {
	r.pending = append(r.pending, content{path: path, data: data})
}

// Carry collects one carried object: a member of the objects grouping that
// the manifest also names by kind and name. The caller supplies the kind,
// namespace and name; where the object lands is this package's concern. Like
// Add, the content is held in memory until the bundle is opened.
//
// It reports an object that would land on top of one already collected. A
// member path holds a kind and a name but not a namespace, so two objects of
// one kind and name taken from different namespaces collide, and writing both
// would produce a bundle whose manifest disagrees with its own members.
func (r *Writer) Carry(object Object, data []byte) (err error) {
	object.Path = ObjectPath(object.Kind, object.Name)
	for i := range r.objects {
		if r.objects[i].Path != object.Path {
			continue
		}
		err = fmt.Errorf(
			"%s %q and %s %q both belong at %q",
			r.objects[i].Kind,
			path.Join(r.objects[i].Namespace, r.objects[i].Name),
			object.Kind,
			path.Join(object.Namespace, object.Name),
			object.Path)
		return
	}
	r.objects = append(r.objects, object)
	r.Add(object.Path, data)
	return
}

// Reserve declares a disk that will be staged into the bundle later, so that
// the manifest can keep room for it.
func (r *Writer) Reserve(disk Disk) {
	r.disks = append(r.disks, disk)
}

// Open reserves room for the manifest and writes every collected member.
// Nothing may be collected after it.
func (r *Writer) Open(provenance Provenance) (err error) {
	r.provenance = provenance
	r.buildIndex()
	widest, err := r.encode()
	if err != nil {
		return
	}
	r.reserved = len(widest)
	r.tw = tar.NewWriter(r.out)
	err = r.writeHeader(ManifestPath, int64(r.reserved))
	if err != nil {
		return
	}
	// Where the manifest goes. The header is written straight through to out,
	// and PAX adds a record of its own when the capture time carries
	// sub-second precision, so the position is read rather than assumed.
	r.offset, err = r.out.Seek(0, io.SeekCurrent)
	if err != nil {
		err = fmt.Errorf("reserve room for the manifest: %w", err)
		return
	}
	_, err = r.tw.Write(widest)
	if err != nil {
		err = fmt.Errorf("write member %q: %w", ManifestPath, err)
		return
	}
	for i := range r.pending {
		err = r.writeMember(r.pending[i].path, r.pending[i].data)
		if err != nil {
			return
		}
	}
	r.pending = nil
	return
}

// Stage writes size bytes read from source into the bundle as the disk member
// at path, and records the size and digest in the manifest. The caller may
// discard what it staged as soon as Stage returns, which is what keeps peak
// local disk down to one disk rather than all of them.
func (r *Writer) Stage(path string, source io.Reader, size int64) (err error) {
	at, found := r.indexOf[path]
	if !found {
		err = fmt.Errorf("disk %q was not declared before the bundle was opened", path)
		return
	}
	err = r.writeHeader(path, size)
	if err != nil {
		return
	}
	sum, written, err := copyDigest(r.tw, source)
	if err != nil {
		err = fmt.Errorf("write member %q: %w", path, err)
		return
	}
	if written != size {
		err = fmt.Errorf(
			"disk %q was %d bytes, not the %d it was declared as",
			path,
			written,
			size)
		return
	}
	r.members[at].Size = written
	r.members[at].Digest = sum
	return
}

// Close finishes the bundle and writes the manifest into the room reserved for
// it.
func (r *Writer) Close() (err error) {
	err = r.validateStaged()
	if err != nil {
		return
	}
	err = r.tw.Close()
	if err != nil {
		err = fmt.Errorf("close bundle: %w", err)
		return
	}
	encoded, err := r.encode()
	if err != nil {
		return
	}
	if len(encoded) > r.reserved {
		err = fmt.Errorf(
			"manifest is %d bytes and only %d were reserved for it",
			len(encoded),
			r.reserved)
		return
	}
	padded := make([]byte, r.reserved)
	copy(padded, encoded)
	for i := len(encoded); i < r.reserved; i++ {
		// Trailing newlines are part of the YAML document and parse away.
		padded[i] = '\n'
	}
	_, err = r.out.Seek(r.offset, io.SeekStart)
	if err != nil {
		err = fmt.Errorf("return to the manifest: %w", err)
		return
	}
	_, err = r.out.Write(padded)
	if err != nil {
		err = fmt.Errorf("write manifest: %w", err)
		return
	}
	_, err = r.out.Seek(0, io.SeekEnd)
	if err != nil {
		err = fmt.Errorf("return to the end of the bundle: %w", err)
		return
	}
	return
}

// buildIndex builds the member index: the collected members, whose size and
// digest are known, followed by the declared disks, whose size and digest are
// not.
func (r *Writer) buildIndex() {
	for i := range r.pending {
		r.members = append(r.members, Member{
			Path:   r.pending[i].path,
			Size:   int64(len(r.pending[i].data)),
			Digest: digest(r.pending[i].data),
		})
	}
	for i := range r.disks {
		r.indexOf[r.disks[i].Path] = len(r.members)
		r.members = append(r.members, Member{
			Path:   r.disks[i].Path,
			Size:   math.MaxInt64,
			Digest: placeholderDigest,
		})
	}
}

// validateStaged reports a declared disk that never arrived, which would
// leave the manifest indexing a member the bundle does not carry.
func (r *Writer) validateStaged() (err error) {
	for i := range r.members {
		if r.members[i].Digest != placeholderDigest {
			continue
		}
		err = fmt.Errorf("disk %q was declared but never staged", r.members[i].Path)
		return
	}
	return
}

// encode renders the manifest as it stands.
func (r *Writer) encode() (encoded []byte, err error) {
	manifest := &Manifest{
		FormatVersion: FormatVersion,
		Provenance:    r.provenance,
		Objects:       r.objects,
		Members:       r.members,
		Disks:         r.disks,
	}
	encoded, err = yaml.Marshal(manifest)
	if err != nil {
		err = fmt.Errorf("marshal manifest: %w", err)
		return
	}
	return
}

// writeHeader writes the tar header for one member.
func (r *Writer) writeHeader(path string, size int64) (err error) {
	err = r.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     path,
		Mode:     0o644,
		Size:     size,
		ModTime:  r.provenance.CapturedAt,
		Format:   tar.FormatPAX,
	})
	if err != nil {
		err = fmt.Errorf("write header for %q: %w", path, err)
		return
	}
	return
}

// writeMember writes one member whose content is already in memory.
func (r *Writer) writeMember(path string, data []byte) (err error) {
	err = r.writeHeader(path, int64(len(data)))
	if err != nil {
		return
	}
	_, err = r.tw.Write(data)
	if err != nil {
		err = fmt.Errorf("write member %q: %w", path, err)
		return
	}
	return
}
