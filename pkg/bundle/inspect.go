package bundle

import (
	"fmt"
	"io"
	"path"
	"strings"
	"text/tabwriter"
	"time"
)

// Inspect reads the bundle from in, verifies it, and writes a description of
// what it holds to out. It contacts no cluster.
//
// A bundle that ends before its last member is described anyway, out of the
// manifest at its front, and the members that have not arrived are named. The
// manifest is the first member precisely so that a support engineer can read
// what a bundle would need from a target — its networks, its storage, the
// images it is served from — off the head of a transfer, rather than having
// to wait out the hours the disks take.
func Inspect(in io.Reader, out io.Writer) (err error) {
	manifest, absent, err := readArrived(in)
	if err != nil {
		return
	}
	err = describe(out, manifest, absent)
	return
}

// describe writes a manifest in a form meant to be read by a person. The
// members named in absent are the ones the reader does not have.
//
// Write errors are discarded per line because tabwriter reports them again
// from Flush, which is checked.
func describe(out io.Writer, manifest *Manifest, absent []string) (err error) {
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
	describeEvidence(tw, manifest)
	describeRequirements(tw, manifest.Requirements)
	describeDisks(tw, manifest)
	describeRecord(tw, manifest)
	describeMembers(tw, manifest, absent)
	err = tw.Flush()
	if err != nil {
		err = fmt.Errorf("write description: %w", err)
		return
	}
	return
}

// describeMembers writes the member index, saying which members have not
// arrived.
//
// A member with no digest checked against it is one the reader does not have,
// which is not the same as one that failed, so it is named rather than
// counted among the verified.
func describeMembers(out io.Writer, manifest *Manifest, absent []string) {
	missing := make(map[string]bool, len(absent))
	for i := range absent {
		missing[absent[i]] = true
	}
	state := "digests verified"
	if len(absent) > 0 {
		state = fmt.Sprintf("%d not yet arrived", len(absent))
	}
	_, _ = fmt.Fprintf(out, "\nMembers (%d), %s\n", len(manifest.Members), state)
	for i := range manifest.Members {
		member := manifest.Members[i]
		verified := member.Digest
		if missing[member.Path] {
			verified = "(not yet arrived)"
		}
		_, _ = fmt.Fprintf(out, "  %s\t%d bytes\t%s\n", member.Path, member.Size, verified)
	}
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

// describeEvidence writes the evidence a bundle holds, under a heading of its
// own saying what evidence is for.
//
// A reader deciding what to do with a bundle has to be able to tell at a
// glance which objects describe the source cluster and which are the
// VirtualMachine itself, so the two are never listed together.
func describeEvidence(out io.Writer, manifest *Manifest) {
	if len(manifest.Evidence) == 0 {
		return
	}
	_, _ = fmt.Fprintf(
		out,
		"\nEvidence (%d), describes the source cluster and is never applied\n",
		len(manifest.Evidence))
	for i := range manifest.Evidence {
		object := manifest.Evidence[i]
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\n",
			object.Kind,
			evidenceName(object, manifest.Provenance))
	}
}

// evidenceName renders an evidence object's name, saying so when the object is
// the source cluster's expansion of the VirtualMachine the bundle carries.
//
// The two are one object by kind and name, so a reader shown the same line in
// both sections would have no way to tell which was which.
func evidenceName(object Object, provenance Provenance) (rendered string) {
	rendered = qualified(object.Namespace, object.Name, provenance.Namespace)
	if object.Kind != kindVirtualMachine || object.Name != provenance.VirtualMachine {
		return
	}
	rendered += " (as the source cluster expands it)"
	return
}

// describeRequirements writes what a bundle needs a target cluster to
// provide, in three sections of its own.
//
// All of it comes from the manifest, which is the first member of a bundle,
// so a reader learns whether their cluster can satisfy a bundle without any
// disk being read.
func describeRequirements(out io.Writer, required Requirements) {
	describeNetworks(out, required.Networks)
	describeStorage(out, required.Storage)
	describeContainerDisks(out, required.ContainerDisks)
}

// describeNetworks writes the networks a VirtualMachine expects. A Multus
// network is shown with the NetworkAttachmentDefinition it was backed by on
// the source cluster, which is what a target has to be able to offer
// something equivalent to.
func describeNetworks(out io.Writer, networks []NetworkRequirement) {
	if len(networks) == 0 {
		return
	}
	_, _ = fmt.Fprintf(
		out,
		"\nNetworks required (%d), provided by the target and not carried\n",
		len(networks))
	for i := range networks {
		network := networks[i]
		if network.Type != NetworkMultus {
			// The pod network names no object, because every cluster has one.
			_, _ = fmt.Fprintf(out, "  %s\t%s\n", network.Name, network.Type)
			continue
		}
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\t%s\n",
			network.Name,
			network.Type,
			path.Join(network.Namespace, network.Attachment))
	}
}

// describeStorage writes what each disk needs from a target's storage, so
// that a candidate storage class can be checked rather than guessed at.
func describeStorage(out io.Writer, storage []StorageRequirement) {
	if len(storage) == 0 {
		return
	}
	_, _ = fmt.Fprintf(
		out,
		"\nStorage required (%d), provided by the target and not carried\n",
		len(storage))
	for i := range storage {
		required := storage[i]
		_, _ = fmt.Fprintf(
			out,
			"  %s\t%s\t%s\tclass: %s\n",
			required.Claim,
			orUnknown(required.Size),
			orUnknown(required.VolumeMode),
			orUnknown(required.StorageClass))
	}
}

// describeContainerDisks writes the container images the VirtualMachine is
// served from, which a target has to be able to pull.
func describeContainerDisks(out io.Writer, disks []ContainerDiskRequirement) {
	if len(disks) == 0 {
		return
	}
	_, _ = fmt.Fprintf(
		out,
		"\nContainer disk images required (%d), never repackaged into a bundle\n",
		len(disks))
	for i := range disks {
		_, _ = fmt.Fprintf(out, "  %s\t%s\n", disks[i].Volume, disks[i].Image)
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
