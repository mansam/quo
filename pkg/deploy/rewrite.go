package deploy

import (
	"crypto/sha256"
	"encoding/base64"
	"time"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
)

// labelManagedBy says quo put this object here. It is the Kubernetes
// convention for the tool managing an object.
const labelManagedBy = "app.kubernetes.io/managed-by"

// labelBundle says which bundle this object was deployed from. Its key is
// frozen: undeploy will select on it, and so will anyone cleaning up with
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
// cleared, the name when renaming, the namespace, and the labels and
// annotations — and nothing else changed. The expanded spec is rewritten
// in place; a deploy uses it once.
func buildVirtualMachine(
	expanded *unstructured.Unstructured,
	opt Options,
	provenance bundle.Provenance,
	identity string) (vm *unstructured.Unstructured) {
	//
	vm = expanded
	clearSourceMetadata(vm)
	if opt.Name != "" {
		vm.SetName(opt.Name)
	}
	vm.SetNamespace(opt.Namespace)
	labels := vm.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[labelManagedBy] = "quo"
	labels[labelBundle] = identity
	vm.SetLabels(labels)
	annotations := vm.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[annotationBundleFile] = opt.Bundle
	annotations[annotationSourceCluster] = provenance.SourceCluster
	annotations[annotationSourceNamespace] = provenance.Namespace
	annotations[annotationSourceVirtualMachine] = provenance.VirtualMachine
	annotations[annotationCapturedAt] = provenance.CapturedAt.UTC().Format(time.RFC3339)
	annotations[annotationDeployedAt] = time.Now().UTC().Format(time.RFC3339)
	vm.SetAnnotations(annotations)
	return
}

// clearSourceMetadata removes the metadata the source cluster assigned the
// expansion, none of which a target can recreate: it mints its own uid,
// creation timestamp, generation and resourceVersion when it creates, and
// records its own managed fields — and it refuses a create that arrives
// carrying a resourceVersion. Where the machine came from is already said
// by the provenance annotations, so none of the source's bookkeeping is
// sent.
func clearSourceMetadata(vm *unstructured.Unstructured) {
	vm.SetResourceVersion("")
	vm.SetUID("")
	vm.SetCreationTimestamp(meta.Time{})
	vm.SetGeneration(0)
	vm.SetManagedFields(nil)
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
