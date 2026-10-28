package bundle

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"
)

// Read reads a bundle from in and returns its manifest.
//
// It refuses a bundle whose first member is not the manifest, or whose format
// version this build does not understand, and it verifies that every member
// matches the digest the manifest records for it. Member content is streamed
// through the digest and discarded, so reading a bundle costs no more memory
// than its largest member's header.
//
// A reader that wants the members one at a time, verifying each as it is
// used rather than the whole bundle up front, uses Open instead.
func Read(in io.Reader) (manifest *Manifest, err error) {
	manifest, absent, err := readArrived(in)
	if err != nil {
		return
	}
	if len(absent) == 0 {
		return
	}
	manifest = nil
	err = &FormatError{
		Reason: fmt.Sprintf("member %q is indexed by the manifest but absent", absent[0]),
	}
	return
}

// readArrived reads the manifest at the front of a bundle, verifies the
// members that are there against it, and returns the indexed members that are
// not.
//
// A bundle that ends early is not an error here. The manifest is the first
// member so that a reader can learn what a bundle holds and what it would
// need from a target from the first bytes of a transfer, and a transfer still
// running ends part-way through the file by definition.
func readArrived(in io.Reader) (manifest *Manifest, absent []string, err error) {
	tr := tar.NewReader(in)
	manifest, _, err = readManifest(tr)
	if err != nil {
		return
	}
	absent, err = verifyArrived(tr, manifest)
	if err != nil {
		manifest = nil
		absent = nil
		return
	}
	return
}

// readManifest reads and validates the first member of the bundle, returning
// the manifest and the member's own content. The content is returned because
// no member indexes the manifest, so nothing inside a bundle records its
// digest; a reader that needs to say which bundle it is holding hashes these
// bytes.
func readManifest(tr *tar.Reader) (manifest *Manifest, content []byte, err error) {
	header, err := tr.Next()
	if errors.Is(err, io.EOF) {
		err = &FormatError{Reason: "the bundle is empty"}
		return
	}
	if err != nil {
		err = fmt.Errorf("read bundle: %w", err)
		return
	}
	if header.Name != ManifestPath {
		err = &FormatError{
			Reason: fmt.Sprintf("first member is %q, expected %q", header.Name, ManifestPath),
		}
		return
	}
	content, err = io.ReadAll(tr)
	if err != nil {
		err = fmt.Errorf("read manifest: %w", err)
		return
	}
	manifest = &Manifest{}
	err = yaml.Unmarshal(content, manifest)
	if err != nil {
		manifest = nil
		err = &FormatError{Reason: "the manifest is not valid YAML: " + err.Error()}
		return
	}
	if manifest.FormatVersion != FormatVersion {
		found := manifest.FormatVersion
		manifest = nil
		err = &UnsupportedVersionError{Found: found}
		return
	}
	return
}

// verifyArrived checks every member that is there against the manifest index
// and returns the indexed members that are not.
//
// A bundle that stops part-way is read as far as it goes, whether it stops
// between members or inside one. What follows the cut has not arrived rather
// than failed, and telling a reader that is more use than refusing to say
// anything about the part they have.
func verifyArrived(tr *tar.Reader, manifest *Manifest) (absent []string, err error) {
	indexed := make(map[string]Member, len(manifest.Members))
	for i := range manifest.Members {
		indexed[manifest.Members[i].Path] = manifest.Members[i]
	}
	seen := make(map[string]bool, len(manifest.Members))
	for {
		header, nextErr := tr.Next()
		if cutShort(nextErr) {
			break
		}
		if nextErr != nil {
			err = fmt.Errorf("read bundle: %w", nextErr)
			return
		}
		member, found := indexed[header.Name]
		if !found {
			err = &FormatError{
				Reason: fmt.Sprintf("member %q is not indexed by the manifest", header.Name),
			}
			return
		}
		err = verifyMember(tr, member)
		if cutShort(err) {
			err = nil
			break
		}
		if err != nil {
			return
		}
		seen[header.Name] = true
	}
	for i := range manifest.Members {
		if seen[manifest.Members[i].Path] {
			continue
		}
		absent = append(absent, manifest.Members[i].Path)
	}
	return
}

// cutShort reports a read that ran off the end of a bundle, which is what a
// transfer still running looks like rather than a bundle that is wrong.
func cutShort(err error) (short bool) {
	short = errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	return
}

// verifyMember checks one member's content against its recorded digest.
func verifyMember(tr *tar.Reader, member Member) (err error) {
	found, err := digestStream(tr)
	if err != nil {
		err = fmt.Errorf("read member %q: %w", member.Path, err)
		return
	}
	if found != member.Digest {
		err = &DigestMismatchError{
			Path: member.Path,
			Want: member.Digest,
			Got:  found,
		}
		return
	}
	return
}

// Reader reads a bundle one member at a time.
//
// The manifest is read and validated by Open. After that a member is read
// only when it is asked for, and it is verified against the digest the
// manifest records for it as it is read. A member whose digest does not
// match is reported rather than fatal to the reader: the caller decides
// what a corrupted member means, and a deploy records the gap and goes on
// to the members that follow.
//
// A member passed over on the way to a later one is not verified. Only
// what is read is checked, which is what keeps a reader from hashing
// every disk a bundle holds before it starts.
type Reader struct {
	manifest       *Manifest
	manifestMember []byte
	tr             *tar.Reader
	memberByPath   map[string]Member
	// passed is the members a walk has already gone by, whether they were
	// read or merely stepped over.
	passed map[string]bool
}

// Open starts reading a bundle from in: it reads the manifest at the
// front, refuses a bundle whose format version this build does not
// understand, and returns a Reader positioned before the first member the
// manifest indexes.
func Open(in io.Reader) (r *Reader, err error) {
	tr := tar.NewReader(in)
	manifest, manifestMember, err := readManifest(tr)
	if err != nil {
		return
	}
	memberByPath := make(map[string]Member, len(manifest.Members))
	for i := range manifest.Members {
		memberByPath[manifest.Members[i].Path] = manifest.Members[i]
	}
	r = &Reader{
		manifest:       manifest,
		manifestMember: manifestMember,
		tr:             tr,
		memberByPath:   memberByPath,
		passed:         make(map[string]bool, len(manifest.Members)),
	}
	return
}

// Manifest returns the manifest the bundle was opened with.
func (r *Reader) Manifest() (manifest *Manifest) {
	manifest = r.manifest
	return
}

// ManifestBytes returns the manifest's own content. No member indexes the
// manifest, so nothing inside the bundle records its digest; a reader that
// needs to say which bundle it is holding — a deploy, labelling what it
// creates — hashes these bytes.
func (r *Reader) ManifestBytes() (content []byte) {
	content = r.manifestMember
	return
}

// Read returns the content of the member at path, verified against the
// digest the manifest records for it.
//
// A bundle is a tar, so members arrive in the order they were written and
// can be read only once: reading a member passes over every member before
// it, and asking for one already gone by is an error. Reading a member
// that failed its digest leaves the reader positioned after it, so a
// caller that records the failure can go on to the members that follow.
func (r *Reader) Read(path string) (content []byte, err error) {
	member, indexed := r.memberByPath[path]
	if !indexed {
		err = &FormatError{
			Reason: fmt.Sprintf("member %q is not indexed by the manifest", path),
		}
		return
	}
	if r.passed[path] {
		err = fmt.Errorf("member %q has already been read", path)
		return
	}
	for {
		header, nextErr := r.tr.Next()
		if cutShort(nextErr) {
			err = absentMember(path)
			return
		}
		if nextErr != nil {
			err = fmt.Errorf("read bundle: %w", nextErr)
			return
		}
		if _, known := r.memberByPath[header.Name]; !known {
			err = &FormatError{
				Reason: fmt.Sprintf("member %q is not indexed by the manifest", header.Name),
			}
			return
		}
		r.passed[header.Name] = true
		if header.Name != path {
			continue
		}
		content, err = io.ReadAll(r.tr)
		if cutShort(err) {
			err = absentMember(path)
			return
		}
		if err != nil {
			err = fmt.Errorf("read member %q: %w", path, err)
			return
		}
		found := digest(content)
		if found != member.Digest {
			err = &DigestMismatchError{Path: path, Want: member.Digest, Got: found}
			return
		}
		return
	}
}

// absentMember reports a member the manifest indexes that the bundle does
// not carry as far as this reader has read.
func absentMember(path string) (err error) {
	err = &FormatError{
		Reason: fmt.Sprintf("member %q is indexed by the manifest but absent", path),
	}
	return
}
