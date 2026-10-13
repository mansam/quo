package bundle

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// Inspect reads the bundle from in, verifies it, and writes a description of
// what it holds to out. It contacts no cluster.
func Inspect(in io.Reader, out io.Writer) (err error) {
	manifest, err := Read(in)
	if err != nil {
		return
	}
	err = describe(out, manifest)
	return
}

// describe writes a manifest in a form meant to be read by a person.
//
// Write errors are discarded per line because tabwriter reports them again
// from Flush, which is checked.
func describe(out io.Writer, manifest *Manifest) (err error) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	provenance := manifest.Provenance
	_, _ = fmt.Fprintf(tw, "Format version:\t%s\n", manifest.FormatVersion)
	_, _ = fmt.Fprintf(tw, "\nProvenance\n")
	_, _ = fmt.Fprintf(tw, "  Source cluster:\t%s\n", orUnknown(provenance.SourceCluster))
	_, _ = fmt.Fprintf(tw, "  Namespace:\t%s\n", provenance.Namespace)
	_, _ = fmt.Fprintf(tw, "  VirtualMachine:\t%s\n", provenance.VirtualMachine)
	_, _ = fmt.Fprintf(tw, "  Captured at:\t%s\n", provenance.CapturedAt.UTC().Format(time.RFC3339))
	_, _ = fmt.Fprintf(tw, "  KubeVirt version:\t%s\n", orUnknown(provenance.KubeVirtVersion))
	_, _ = fmt.Fprintf(tw, "  Kubernetes version:\t%s\n", orUnknown(provenance.KubernetesVersion))
	describeObjects(tw, manifest)
	describeDisks(tw, manifest)
	describeRecord(tw, manifest)
	_, _ = fmt.Fprintf(tw, "\nMembers (%d), digests verified\n", len(manifest.Members))
	for i := range manifest.Members {
		member := manifest.Members[i]
		_, _ = fmt.Fprintf(tw, "  %s\t%d bytes\t%s\n", member.Path, member.Size, member.Digest)
	}
	err = tw.Flush()
	if err != nil {
		err = fmt.Errorf("write description: %w", err)
		return
	}
	return
}

// describeObjects writes the objects a bundle carries, by kind and name, in
// the order they were discovered.
func describeObjects(out io.Writer, manifest *Manifest) {
	if len(manifest.Objects) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\nCarried objects (%d)\n", len(manifest.Objects))
	for i := range manifest.Objects {
		object := manifest.Objects[i]
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\n",
			object.Kind,
			qualified(object.Namespace, object.Name, manifest.Provenance.Namespace))
	}
}

// qualified renders an object's name. Its namespace is shown only when it is
// not the one the VirtualMachine was captured from, which is how a reader
// spots an object borrowed from elsewhere in the cluster.
func qualified(namespace, name, captured string) (rendered string) {
	rendered = name
	if namespace == "" || namespace == captured {
		return
	}
	rendered = namespace + "/" + name
	return
}

// describeDisks writes the disks a bundle carries and the volumes each one
// serves. A bundle carrying no disks produces no output here.
func describeDisks(out io.Writer, manifest *Manifest) {
	if len(manifest.Disks) == 0 {
		return
	}
	sizeOf := make(map[string]int64, len(manifest.Members))
	for i := range manifest.Members {
		sizeOf[manifest.Members[i].Path] = manifest.Members[i].Size
	}
	_, _ = fmt.Fprintf(out, "\nDisks (%d)\n", len(manifest.Disks))
	for i := range manifest.Disks {
		disk := manifest.Disks[i]
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%d bytes\t%s\tvolumes: %s\n",
			disk.Claim,
			sizeOf[disk.Path],
			disk.Format,
			strings.Join(disk.Volumes, ", "))
	}
}

// describeRecord writes the capture record: how many objects got each
// disposition, and then every skipped object in a section of its own.
//
// Skipped objects are what a reader has to act on, so they are listed rather
// than counted and sit under their own heading instead of among the dozens of
// objects that were collected without incident.
func describeRecord(out io.Writer, manifest *Manifest) {
	if len(manifest.CaptureRecord) == 0 {
		return
	}
	countOf := map[Disposition]int{}
	for i := range manifest.CaptureRecord {
		countOf[manifest.CaptureRecord[i].Disposition]++
	}
	_, _ = fmt.Fprintf(out, "\nCapture record (%d)\n", len(manifest.CaptureRecord))
	for _, disposition := range dispositions {
		if countOf[disposition] == 0 {
			continue
		}
		_, _ = fmt.Fprintf(out, "  %s\t%d\n", disposition, countOf[disposition])
	}
	skipped := manifest.CaptureRecord.Skipped()
	if len(skipped) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\nSkipped (%d), absent from this bundle\n", len(skipped))
	for i := range skipped {
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\t%s\t%s\n",
			skipped[i].Kind,
			qualified(skipped[i].Namespace, skipped[i].Name, manifest.Provenance.Namespace),
			skipped[i].Reason,
			skipped[i].Message)
	}
}

// orUnknown renders a value quo was not able to read.
func orUnknown(value string) (rendered string) {
	rendered = value
	if rendered == "" {
		rendered = "(unknown)"
	}
	return
}
