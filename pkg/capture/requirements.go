package capture

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// buildRequirements returns what the bundle declares a target cluster would
// have to provide for the VirtualMachine: the networks it expects, what each
// of its disks needs from storage, and the container images it is served
// from.
//
// It reads nothing from the cluster. Everything it needs is on the
// VirtualMachine itself and on the PersistentVolumeClaims the closure already
// collected, so the requirements are recorded whether or not what describes
// the source cluster's storage could be read. A namespace owner
// forbidden the StorageClass still produces a bundle that says what its disks
// need.
func buildRequirements(
	vm *unstructured.Unstructured,
	claims []*unstructured.Unstructured) (required bundle.Requirements) {
	//
	required.Networks = buildNetworks(vm)
	required.Storage = buildStorage(vm, claims)
	required.ContainerDisks = buildContainerDisks(vm)
	return
}

// buildNetworks returns the networks the VirtualMachine expects, in the order
// it declares them.
func buildNetworks(vm *unstructured.Unstructured) (required []bundle.NetworkRequirement) {
	namespace := vm.GetNamespace()
	networks, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "networks")
	for i := range networks {
		network, cast := networks[i].(map[string]any)
		if !cast {
			continue
		}
		required = append(required, buildNetwork(network, namespace))
	}
	return
}

// buildNetwork says what one network asks a target for.
//
// A network is backed either by Multus or by the cluster's own pod network,
// and KubeVirt reads one that names neither as the pod network. Which of the
// two it is turns on the network naming Multus at all rather than on it
// naming an attachment, so a Multus network that names no attachment is not
// reported as something every cluster already has.
func buildNetwork(network map[string]any, namespace string) (required bundle.NetworkRequirement) {
	required = bundle.NetworkRequirement{
		Name: nestedString(network, "name"),
		Type: bundle.NetworkPod,
	}
	multus, found, _ := unstructured.NestedMap(network, "multus")
	if !found {
		return
	}
	required.Type = bundle.NetworkMultus
	attached := nestedString(multus, "networkName")
	if attached == "" {
		return
	}
	required.Namespace, required.Attachment = splitNetworkName(attached, namespace)
	return
}

// buildStorage says what each of the VirtualMachine's disks needs from a
// target's storage.
//
// The disks are the claims its volumes are backed by, which is the same list
// the bundle holds disk members for, so every member has a requirement saying
// what it would take to put it back.
func buildStorage(
	vm *unstructured.Unstructured,
	claims []*unstructured.Unstructured) (required []bundle.StorageRequirement) {
	//
	claimByName := make(map[string]*unstructured.Unstructured, len(claims))
	for i := range claims {
		claimByName[claims[i].GetName()] = claims[i]
	}
	for _, backing := range claimedVolumes(vm) {
		required = append(required, buildDiskStorage(backing.name, claimByName[backing.name]))
	}
	return
}

// buildDiskStorage reads what one disk needs off the claim behind it.
//
// A claim quo could not read leaves everything but the name unknown. The disk
// is in the bundle either way and a target still has to find room for it, so
// the requirement is declared with what is known rather than dropped.
func buildDiskStorage(
	name string,
	claim *unstructured.Unstructured) (required bundle.StorageRequirement) {
	//
	required.Claim = name
	if claim == nil {
		return
	}
	required.StorageClass = nestedString(claim.Object, "spec", "storageClassName")
	required.VolumeMode = nestedString(claim.Object, "spec", "volumeMode")
	// What the claim was bound to rather than what it asked for. A claim
	// requesting 30Gi can be bound to 32Gi, and a target given the smaller of
	// the two has nowhere to put the disk.
	required.Size = nestedString(claim.Object, "status", "capacity", "storage")
	if required.Size != "" {
		return
	}
	required.Size = nestedString(claim.Object, "spec", "resources", "requests", "storage")
	return
}

// buildContainerDisks names the container images the VirtualMachine's volumes
// are served from. quo does not repackage container images, so each one is
// declared as a requirement and never held in a bundle.
func buildContainerDisks(vm *unstructured.Unstructured) (required []bundle.ContainerDiskRequirement) {
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	for i := range volumes {
		volume, cast := volumes[i].(map[string]any)
		if !cast {
			continue
		}
		image := nestedString(volume, "containerDisk", "image")
		if image == "" {
			continue
		}
		required = append(required, bundle.ContainerDiskRequirement{
			Volume: nestedString(volume, "name"),
			Image:  image,
		})
	}
	return
}

// requiredObjects names the object a target has to provide for each
// requirement.
//
// An entry here is not a second account of an object the bundle holds. A
// StorageClass is recorded as collected because quo read it and again as a
// requirement because a target has to have one, and both of those are still
// true when the read was forbidden.
//
// A requirement that names no object produces nothing: every cluster already
// has a pod network, and a claim bound to a volume made by hand names no
// storage class for a target to match.
func requiredObjects(required bundle.Requirements) (refs []cluster.ObjectRef) {
	for _, network := range required.Networks {
		if network.Type != bundle.NetworkMultus {
			continue
		}
		if network.Attachment == "" {
			continue
		}
		refs = append(refs, cluster.ObjectRef{
			Kind:      cluster.KindNetworkAttachmentDefinition,
			Namespace: network.Namespace,
			Name:      network.Attachment,
		})
	}
	for _, storage := range required.Storage {
		if storage.StorageClass == "" {
			continue
		}
		refs = append(refs, cluster.ObjectRef{
			Kind: cluster.KindStorageClass,
			Name: storage.StorageClass,
		})
	}
	for _, disk := range required.ContainerDisks {
		refs = append(refs, cluster.ObjectRef{
			Kind: bundle.KindContainerDisk,
			Name: disk.Image,
		})
	}
	return
}
