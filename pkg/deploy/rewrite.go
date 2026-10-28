package deploy

import (
	"crypto/sha256"
	"encoding/base64"
	"time"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// labelManagedBy says quo put this object here. It is the Kubernetes
// convention for the tool managing an object.
const labelManagedBy = "app.kubernetes.io/managed-by"

// labelBundle says which bundle this object was deployed from. Its key is
// frozen: undeploy selects on it, and so does anyone cleaning up with
// kubectl.
const labelBundle = "quo.mansam.dev/bundle"

// Annotations a deploy records on what it creates, carrying what a support
// engineer needs to tie a running machine back to the case it was
// captured for.
const (
	// annotationBundleFile is the exact filename the bundle was deployed
	// from, on every created object.
	annotationBundleFile = "quo.mansam.dev/bundle-file"
	// The rest land on the VirtualMachine alone, saying where it came
	// from and when.
	annotationSourceCluster        = "quo.mansam.dev/source-cluster"
	annotationSourceNamespace      = "quo.mansam.dev/source-namespace"
	annotationSourceVirtualMachine = "quo.mansam.dev/source-virtualmachine"
	annotationCapturedAt           = "quo.mansam.dev/captured-at"
	annotationDeployedAt           = "quo.mansam.dev/deployed-at"
)

// annotationBindImmediate asks CDI to bind a DataVolume's claim at once
// rather than wait for a first consumer.
const annotationBindImmediate = "cdi.kubevirt.io/storage.bind.immediate.requested"

// dataVolumeAPIVersion is the API version a created DataVolume carries.
const dataVolumeAPIVersion = "cdi.kubevirt.io/v1beta1"

// virtualMachineName is the name the deployed VirtualMachine takes: the
// one the command line gave, or the machine's own name when it gave none.
// Nothing in a VirtualMachine's spec references its own name, so a rename
// forces no other rewrite.
func virtualMachineName(opt Options, manifest *bundle.Manifest) (name string) {
	name = opt.Name
	if name == "" {
		name = manifest.Provenance.VirtualMachine
	}
	return
}

// buildVirtualMachine returns the VirtualMachine to create: the expanded
// spec carrying the forced rewrites — the source cluster's metadata
// cleared, the name when renaming, the namespace, the instancetype and
// preference matchers removed, the multus network names from the mapping,
// the dataVolumeTemplates removed, and the labels and annotations — and
// nothing else changed. The expanded spec is rewritten in place; a deploy
// uses it once.
func buildVirtualMachine(
	expanded *unstructured.Unstructured,
	opt Options,
	provenance bundle.Provenance,
	identity string,
	networks []Requirement) (vm *unstructured.Unstructured) {
	//
	vm = expanded
	clearSourceMetadata(vm)
	if opt.Name != "" {
		vm.SetName(opt.Name)
	}
	vm.SetNamespace(opt.Namespace)
	rewriteNetworks(vm, networks)
	// The expansion is the source cluster's own resolution of the machine,
	// so what the instancetype and the preference named is already folded
	// into the spec. A matcher that survives the expansion is removed,
	// because the applied VirtualMachine names none: no ControllerRevision
	// is created on the target, and none is needed.
	unstructured.RemoveNestedField(vm.Object, "spec", "instancetype")
	unstructured.RemoveNestedField(vm.Object, "spec", "preference")
	// The templates became standalone DataVolumes, created before the
	// VirtualMachine, so the volume stanzas that named them are satisfied
	// unchanged and the machine creates no disks of its own.
	unstructured.RemoveNestedField(vm.Object, "spec", "dataVolumeTemplates")
	labelAsDeployed(vm, opt, identity)
	annotations := vm.GetAnnotations()
	annotations[annotationSourceCluster] = provenance.SourceCluster
	annotations[annotationSourceNamespace] = provenance.Namespace
	annotations[annotationSourceVirtualMachine] = provenance.VirtualMachine
	annotations[annotationCapturedAt] = provenance.CapturedAt.UTC().Format(time.RFC3339)
	annotations[annotationDeployedAt] = time.Now().UTC().Format(time.RFC3339)
	vm.SetAnnotations(annotations)
	return
}

// rewriteNetworks puts the network mappings onto the multus networks of
// the VirtualMachine being applied: each one's networkName names the
// NetworkAttachmentDefinition it was mapped onto, as the mapping says —
// namespace-qualified only when the definition lives outside the deploy
// namespace, because an unqualified name means the namespace of the
// VirtualMachine itself.
//
// The pod network names no object and is never mapped. A network the
// deploy resolved no mapping for keeps what the expansion carries.
func rewriteNetworks(vm *unstructured.Unstructured, networks []Requirement) {
	attached := make(map[string]string, len(networks))
	for i := range networks {
		attached[networks[i].From] = networks[i].To
	}
	// The slice is taken without a copy, because what it holds is
	// rewritten in place: NestedSlice hands back a copy of what it
	// finds, and rewriting a copy would rewrite nothing.
	declared, _, _ := unstructured.NestedFieldNoCopy(
		vm.Object, "spec", "template", "spec", "networks")
	declaredNetworks, cast := declared.([]any)
	if !cast {
		return
	}
	for i := range declaredNetworks {
		network, cast := declaredNetworks[i].(map[string]any)
		if !cast {
			continue
		}
		if _, found, _ := unstructured.NestedMap(network, "multus"); !found {
			// The pod network, which every cluster has and which names
			// nothing to map.
			continue
		}
		name, _, _ := unstructured.NestedString(network, "name")
		attachment, found := attached[name]
		if !found {
			continue
		}
		err := unstructured.SetNestedField(network, attachment, "multus", "networkName")
		if err != nil {
			// A network stanza is a map of strings; the field always
			// takes.
			continue
		}
	}
}

// buildDataVolume returns the DataVolume that receives one of the bundle's
// disks.
//
// The DataVolume is named after the claim the disk backs, so the
// VirtualMachine's volume stanzas find the claim CDI creates without a
// rewrite of their own: CDI names the claim after the DataVolume. Its
// source is always upload, because a claim cannot be populated on its
// own; its size and volume mode come from the requirement recorded at
// capture; its storage class is the target's class the requirement was
// mapped onto; and its content type follows the compressed form the
// bundle holds the disk in, because the target's CDI reads the bytes
// as what they are.
//
// The DataVolume also asks CDI to bind its claim at once. The machine it
// belongs to is deployed stopped, so nothing will ever be the first
// consumer a WaitForFirstConsumer class waits for, and the upload cannot
// start until the claim is bound.
func buildDataVolume(
	disk plannedDisk,
	opt Options,
	identity string) (dataVolume *unstructured.Unstructured) {
	//
	contentType := "kubevirt"
	if disk.held.Format == bundle.DiskArchiveGzip {
		// A tar of a filesystem volume's content, which CDI unpacks
		// rather than writes as a disk image.
		contentType = "archive"
	}
	storage := map[string]any{
		"resources": map[string]any{
			"requests": map[string]any{"storage": disk.required.Size},
		},
		"storageClassName": disk.storageClass,
	}
	if disk.required.VolumeMode != "" {
		storage["volumeMode"] = disk.required.VolumeMode
	}
	dataVolume = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": dataVolumeAPIVersion,
		"kind":       cluster.KindDataVolume,
		"metadata": map[string]any{
			"name":      disk.held.Claim,
			"namespace": opt.Namespace,
		},
		"spec": map[string]any{
			"source":      map[string]any{"upload": map[string]any{}},
			"contentType": contentType,
			"storage":     storage,
		},
	}}
	labelAsDeployed(dataVolume, opt, identity)
	annotations := dataVolume.GetAnnotations()
	annotations[annotationBindImmediate] = ""
	dataVolume.SetAnnotations(annotations)
	return
}

// buildClosureObject returns the closure object to create: the bundle's
// member carrying the forced rewrites that apply to every created object
// — the source cluster's metadata cleared, the namespace, and the labels
// and annotations — and nothing else changed. The member is rewritten in
// place; a deploy uses it once.
func buildClosureObject(
	member *unstructured.Unstructured,
	opt Options,
	identity string) (object *unstructured.Unstructured) {
	//
	object = member
	clearSourceMetadata(object)
	object.SetNamespace(opt.Namespace)
	labelAsDeployed(object, opt, identity)
	return
}

// labelAsDeployed marks one object as quo's work from one bundle: the
// managed-by label, the bundle identity label, and the annotation naming
// the exact bundle file. What the VirtualMachine additionally carries —
// where it came from and when — is set where the VirtualMachine is built.
func labelAsDeployed(object *unstructured.Unstructured, opt Options, identity string) {
	labels := object.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[labelManagedBy] = "quo"
	labels[labelBundle] = identity
	object.SetLabels(labels)
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[annotationBundleFile] = opt.Bundle
	object.SetAnnotations(annotations)
}

// clearSourceMetadata removes the metadata the source cluster assigned an
// object the bundle holds, none of which a target can recreate: it mints
// its own uid, creation timestamp, generation and resourceVersion when it
// creates, and records its own managed fields — and it refuses a create
// that arrives carrying a resourceVersion.
func clearSourceMetadata(object *unstructured.Unstructured) {
	object.SetResourceVersion("")
	object.SetUID("")
	object.SetCreationTimestamp(meta.Time{})
	object.SetGeneration(0)
	object.SetManagedFields(nil)
}

// bundleIdentity is the value that labels every object created from one
// bundle: the SHA-256 of the manifest member, encoded to fit a label
// value. A hex SHA-256 is 64 characters and label values are limited to
// 63; base64url is 43, and the prefix makes 50.
//
// The manifest carries the digest of every other member, so its own hash
// identifies the bundle: two objects carrying the same value came from
// byte-for-byte the same bundle.
func bundleIdentity(manifest []byte) (identity string) {
	summed := sha256.Sum256(manifest)
	identity = "sha256-" + base64.RawURLEncoding.EncodeToString(summed[:])
	return
}
