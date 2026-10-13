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
// need from a target off the head of a transfer, and a transfer still in
// flight ends part-way through the file by definition.
func readArrived(in io.Reader) (manifest *Manifest, absent []string, err error) {
	tr := tar.NewReader(in)
	manifest, err = readManifest(tr)
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

// readManifest reads and validates the first member of the bundle.
func readManifest(tr *tar.Reader) (manifest *Manifest, err error) {
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
	encoded, err := io.ReadAll(tr)
	if err != nil {
		err = fmt.Errorf("read manifest: %w", err)
		return
	}
	manifest = &Manifest{}
	err = yaml.Unmarshal(encoded, manifest)
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
// transfer still in flight looks like rather than a bundle that is wrong.
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
