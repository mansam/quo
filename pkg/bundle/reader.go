package bundle

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"
)

// Read verifies every member against its recorded digest and returns the
// manifest. Use Open to read individual members without checking digests.
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

// readArrived returns the manifest and the paths of any missing members.
// It verifies complete members so inspect can report on partial bundles.
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

// readManifest decodes the first tar member, checks the format version,
// and returns the manifest and its original bytes.
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

// verifyArrived checks the digests of complete members. It lists a
// truncated member and any subsequent indexed members as missing.
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

// cutShort reports whether a read reached the end of the input.
func cutShort(err error) (short bool) {
	short = errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	return
}

// verifyMember verifies one member's content against its recorded digest.
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

// Reader reads bundle members in tar order without checking their digests.
// Advancing to a later member skips intervening members. A member cannot be
// opened again once the reader has reached or passed it.
type Reader struct {
	manifest       *Manifest
	manifestMember []byte
	tr             *tar.Reader
	memberByPath   map[string]Member
	// passed tracks members the reader has reached or skipped.
	passed map[string]bool
}

// Open reads the manifest, checks its format version, and positions a Reader
// before the remaining members.
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

// ManifestBytes returns the original encoded manifest.
func (r *Reader) ManifestBytes() (content []byte) {
	content = r.manifestMember
	return
}

// Read returns the full contents of the member at path. Use OpenStream for
// members too large to buffer in memory.
func (r *Reader) Read(path string) (content []byte, err error) {
	_, err = r.seekTo(path)
	if err != nil {
		return
	}
	content, err = io.ReadAll(r.tr)
	if cutShort(err) {
		err = absentMember(path)
		return
	}
	if err != nil {
		err = fmt.Errorf("read member %q: %w", path, err)
	}
	return
}

// OpenStream opens the member at path for streaming. The stream is valid
// only until the Reader advances to another member.
func (r *Reader) OpenStream(path string) (stream *MemberStream, err error) {
	member, err := r.seekTo(path)
	if err != nil {
		return
	}
	stream = &MemberStream{tr: r.tr, member: member}
	return
}

// seekTo advances to path and returns its manifest entry.
func (r *Reader) seekTo(path string) (member Member, err error) {
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
		return
	}
}

// MemberStream reads one bundle member and exposes its recorded size.
type MemberStream struct {
	tr     *tar.Reader
	member Member
}

// Read reads bytes from the member.
func (r *MemberStream) Read(p []byte) (n int, err error) {
	n, err = r.tr.Read(p)
	return
}

// Size returns the member size recorded in the manifest.
func (r *MemberStream) Size() (size int64) {
	size = r.member.Size
	return
}

// absentMember reports a member the manifest indexes that the bundle does
// not carry as far as this reader has read.
func absentMember(path string) (err error) {
	err = &FormatError{
		Reason: fmt.Sprintf("member %q is indexed by the manifest but absent", path),
	}
	return
}
