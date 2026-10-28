package capture

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// environment collects what describes the cluster a VirtualMachine came from,
// in the order it was discovered and without duplicates.
//
// Platform configuration is frequently where the problem actually is: a CSI
// parameter, a volume binding mode, a feature gate. Collecting it with the
// VirtualMachine saves a support engineer from having to ask the source
// cluster for it afterwards, which can take a day.
type environment struct {
	c    cluster.Cluster
	co   *collector
	seen map[cluster.ObjectRef]bool
}

// captureEnvironment collects what the bundle records about the source
// cluster. That is the storage behind each PersistentVolumeClaim, KubeVirt's
// own configuration, and the form the source cluster expands the VirtualMachine
// to.
//
// Almost all of it is cluster-scoped. That is exactly where a capture run by
// a namespace owner no longer has permission. A read that failed is recorded
// and the capture continues. A capture that was cancelled, or whose deadline
// passed, ends there instead. It ends for the same reason the reference
// traversal does. A record saying each object was unreadable would describe
// something that did not happen.
func captureEnvironment(
	ctx context.Context,
	c cluster.Cluster,
	co *collector,
	vm *unstructured.Unstructured,
	claims []*unstructured.Unstructured) (err error) {
	//
	r := &environment{
		c:    c,
		co:   co,
		seen: map[cluster.ObjectRef]bool{},
	}
	for i := range claims {
		err = r.addStorage(ctx, claims[i])
		if err != nil {
			return
		}
	}
	err = r.addKubeVirt(ctx)
	if err != nil {
		return
	}
	err = r.addExpandedForm(ctx, vm)
	return
}

// addStorage collects what the cluster put behind one
// PersistentVolumeClaim. It collects the StorageClass the claim was
// provisioned from. It collects the CDI StorageProfile, which decided the
// volume mode and access modes the disk actually got. It also collects the
// PersistentVolume the claim is bound to. That object holds the real
// capacity, CSI driver and volume attributes rather than what was asked
// for.
//
// A StorageProfile carries the name of the StorageClass it describes, so the
// two are read under one name.
func (r *environment) addStorage(ctx context.Context, claim *unstructured.Unstructured) (err error) {
	class := nestedString(claim.Object, "spec", "storageClassName")
	err = r.add(ctx, cluster.ObjectRef{Kind: cluster.KindStorageClass, Name: class})
	if err != nil {
		return
	}
	err = r.add(ctx, cluster.ObjectRef{Kind: cluster.KindStorageProfile, Name: class})
	if err != nil {
		return
	}
	bound := nestedString(claim.Object, "spec", "volumeName")
	err = r.add(ctx, cluster.ObjectRef{Kind: cluster.KindPersistentVolume, Name: bound})
	return
}

// addKubeVirt collects KubeVirt's own configuration, above all the feature
// gates it has enabled. Those gates account for behaviour a target cluster
// does not reproduce.
//
// KubeVirt is a singleton whose namespace and name differ between
// installations. quo therefore does not know what to call it until it has
// read it. The reference names the kind alone, and collect completes the
// name from the object that arrives.
func (r *environment) addKubeVirt(ctx context.Context) (err error) {
	err = r.collect(
		ctx,
		cluster.ObjectRef{Kind: cluster.KindKubeVirt},
		func() (kv *unstructured.Unstructured, readErr error) {
			kv, readErr = r.c.GetKubeVirt(ctx)
			return
		})
	return
}

// addExpandedForm collects the form the source cluster resolves the
// VirtualMachine to.
//
// quo does not use it to rewrite the VirtualMachine. It is captured because
// KubeVirt applies the source cluster's own defaults while expanding. Those
// defaults are the machine type, the architecture, and the CPU topology.
// They come from a KubeVirt configuration a namespace owner is often not
// allowed to read. The subresource is namespaced, so for that capture this
// is the only record of them.
//
// It is stored as a member of its own rather than among the resources.
// Nothing on the cluster has that name, and nothing applies it to a
// target. The read is best effort for the same reason the rest of this file
// is: a capture without it is still useful.
func (r *environment) addExpandedForm(ctx context.Context, vm *unstructured.Unstructured) (err error) {
	ref := cluster.ObjectRef{
		Kind:      bundle.KindExpandedSpec,
		Namespace: vm.GetNamespace(),
		Name:      vm.GetName(),
	}
	expanded, err := r.c.ExpandSpec(ctx, ref.Namespace, ref.Name)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.co.skip(ref, err)
		err = nil
		return
	}
	err = r.co.addExpandedSpec(ref, expanded)
	return
}

// add reads the object a reference names and collects it.
//
// An empty name is a reference the cluster does not make. A claim
// provisioned without a storage class names none, and an unbound claim
// names no volume. An object already collected is left alone, so a
// StorageClass two claims share is captured once.
func (r *environment) add(ctx context.Context, ref cluster.ObjectRef) (err error) {
	if ref.Name == "" {
		return
	}
	if r.seen[ref] {
		return
	}
	r.seen[ref] = true
	err = r.collect(
		ctx,
		ref,
		func() (object *unstructured.Unstructured, readErr error) {
			object, readErr = r.c.GetObject(ctx, ref)
			return
		})
	return
}

// collect puts whatever read returns into the bundle, recorded against ref. A
// reference quo could not name in advance is named from the object once it has
// arrived.
//
// A read that failed is recorded and the capture continues. The person
// running a capture is frequently not entitled to read these objects. A
// capture that may have taken hours should not fail over one of them.
// Leaving it out without saying so would be worse, because the reader could
// not tell it from an object that was never there.
//
// A capture that was cancelled, or whose deadline passed, ends instead of
// recording. The remaining objects were never asked for, so recording them
// as unreadable would describe something that did not happen.
func (r *environment) collect(
	ctx context.Context,
	ref cluster.ObjectRef,
	read func() (object *unstructured.Unstructured, readErr error)) (err error) {
	//
	object, err := read()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.co.skip(ref, err)
		err = nil
		return
	}
	if ref.Name == "" {
		ref.Namespace = object.GetNamespace()
		ref.Name = object.GetName()
	}
	err = r.co.add(ref, object)
	return
}
