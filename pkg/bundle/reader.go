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
	tr := tar.NewReader(in)
	manifest, err = readManifest(tr)
	if err != nil {
		return
	}
	err = verifyMembers(tr, manifest)
	if err != nil {
		manifest = nil
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

// verifyMembers checks every remaining member against the manifest index.
func verifyMembers(tr *tar.Reader, manifest *Manifest) (err error) {
	indexed := make(map[string]Member, len(manifest.Members))
	for i := range manifest.Members {
		indexed[manifest.Members[i].Path] = manifest.Members[i]
	}
	seen := make(map[string]bool, len(manifest.Members))
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
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
		if err != nil {
			return
		}
		seen[header.Name] = true
	}
	for i := range manifest.Members {
		if !seen[manifest.Members[i].Path] {
			err = &FormatError{
				Reason: fmt.Sprintf("member %q is indexed by the manifest but absent", manifest.Members[i].Path),
			}
			return
		}
	}
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
