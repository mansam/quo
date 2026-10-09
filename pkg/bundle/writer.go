package bundle

import (
	"archive/tar"
	"fmt"
	"io"
	"time"

	"sigs.k8s.io/yaml"
)

// Writer collects the members of a bundle and writes them as a tar.
//
// The manifest is the first member and indexes every other one, so a bundle
// cannot be written until all of its members are known. Collect first, write
// once.
type Writer struct {
	members []content
}

// content is one member held until the bundle is written.
type content struct {
	path string
	data []byte
}

// Add collects a member. Members appear in the bundle in the order they were
// added, after the manifest.
func (r *Writer) Add(path string, data []byte) {
	r.members = append(r.members, content{path: path, data: data})
}

// Write writes the collected members to w as a bundle, manifest first.
func (r *Writer) Write(w io.Writer, provenance Provenance) (err error) {
	manifest := r.buildManifest(provenance)
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		err = fmt.Errorf("marshal manifest: %w", err)
		return
	}
	tw := tar.NewWriter(w)
	err = writeMember(tw, ManifestPath, encoded, provenance.CapturedAt)
	if err != nil {
		return
	}
	for i := range r.members {
		err = writeMember(tw, r.members[i].path, r.members[i].data, provenance.CapturedAt)
		if err != nil {
			return
		}
	}
	err = tw.Close()
	if err != nil {
		err = fmt.Errorf("close bundle: %w", err)
		return
	}
	return
}

// buildManifest returns the manifest describing the collected members.
func (r *Writer) buildManifest(provenance Provenance) (manifest *Manifest) {
	manifest = &Manifest{
		FormatVersion: FormatVersion,
		Provenance:    provenance,
	}
	for i := range r.members {
		manifest.Members = append(manifest.Members, Member{
			Path:   r.members[i].path,
			Size:   int64(len(r.members[i].data)),
			Digest: digest(r.members[i].data),
		})
	}
	return
}

// writeMember writes one member into the tar.
func writeMember(tw *tar.Writer, path string, data []byte, modified time.Time) (err error) {
	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     path,
		Mode:     0o644,
		Size:     int64(len(data)),
		ModTime:  modified,
		Format:   tar.FormatPAX,
	}
	err = tw.WriteHeader(header)
	if err != nil {
		err = fmt.Errorf("write header for %q: %w", path, err)
		return
	}
	_, err = tw.Write(data)
	if err != nil {
		err = fmt.Errorf("write member %q: %w", path, err)
		return
	}
	return
}
