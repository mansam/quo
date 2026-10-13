package capture

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/kvo/pkg/cluster"
)

// evidence collects the objects that explain the environment a VirtualMachine
// came from, in the order they were discovered and without duplicates.
//
// None of them is an object a target will recreate. Platform configuration is
// frequently where the problem actually is — a CSI parameter, a volume binding
// mode, a feature gate — and a support engineer who has to go back to the
// source cluster for it has lost a day.
type evidence struct {
	c    cluster.Cluster
	co   *collector
	seen map[cluster.ObjectRef]bool
}

// captureEvidence collects what the bundle records about the source
// environment: the storage behind each PersistentVolumeClaim, KubeVirt's own
// configuration, and the form the source cluster expands the VirtualMachine
// to.
//
// Almost all of it is cluster-scoped, which is exactly where a capture run by
// a namespace owner runs out of permission, so a read that failed is recorded
// and the capture goes on. A capture that has been cancelled or has run out of
// time ends there instead, for the same reason the closure traversal does: a
// record saying each object was unreadable would describe something that did
// not happen.
func captureEvidence(
	ctx context.Context,
	c cluster.Cluster,
	co *collector,
	vm *unstructured.Unstructured,
	claims []*unstructured.Unstructured) (err error) {
	//
	r := &evidence{
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

// addStorage collects what the cluster put behind one PersistentVolumeClaim:
// the StorageClass it was provisioned from, the CDI StorageProfile that
// decided the volume mode and access modes the disk actually got, and the
// PersistentVolume it is bound to, which holds the real capacity, CSI driver
// and volume attributes rather than what was asked for.
//
// A StorageProfile carries the name of the StorageClass it describes, so the
// two are read under one name.
func (r *evidence) addStorage(ctx context.Context, claim *unstructured.Unstructured) (err error) {
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
// gates it has enabled, which is what accounts for behaviour a target cluster
// does not reproduce.
//
// KubeVirt is a singleton whose namespace and name differ between
// installations, so quo does not know what to call it until it has read it:
// the reference names the kind alone, and collect fills the rest in from the
// object that arrives.
func (r *evidence) addKubeVirt(ctx context.Context) (err error) {
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
// quo does not use it to rewrite the VirtualMachine. Every input to the
// expansion — the instancetype objects, the ControllerRevisions, KubeVirt's
// configuration — is already in the bundle, so the expanded form is
// derivable. It is captured because expansion is version-dependent, and a
// difference between what the source cluster resolved and what a target
// resolves is a plausible cause of the problem being investigated.
//
// It is recorded under the VirtualMachine's own kind and name, because that is
// what it is. The evidence grouping is what keeps it apart from the
// VirtualMachine the bundle carries.
func (r *evidence) addExpandedForm(ctx context.Context, vm *unstructured.Unstructured) (err error) {
	ref := cluster.ObjectRef{
		Kind:      cluster.KindVirtualMachine,
		Namespace: vm.GetNamespace(),
		Name:      vm.GetName(),
	}
	err = r.collect(
		ctx,
		ref,
		func() (expanded *unstructured.Unstructured, readErr error) {
			expanded, readErr = r.c.ExpandSpec(ctx, ref.Namespace, ref.Name)
			return
		})
	return
}

// add reads the object a reference names and collects it as evidence.
//
// An empty name is a reference the cluster does not make — a claim provisioned
// without a storage class names none, and an unbound claim names no volume —
// and an object already collected is left alone, so a StorageClass two claims
// share is captured once.
func (r *evidence) add(ctx context.Context, ref cluster.ObjectRef) (err error) {
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

// collect puts into the bundle as evidence whatever read returns, recorded
// against ref. A reference quo could not name in advance is named from the
// object once it has arrived.
//
// This is the one place the best-effort rule lives. A read that failed is
// recorded and the capture goes on, because failing a capture that may have
// taken hours over one object the person running it was never entitled to see
// would be unacceptable, and omitting it in silence would be worse. A capture
// that has been cancelled or has run out of time ends there instead: quo
// stopped asking, which is not the same as the cluster refusing.
func (r *evidence) collect(
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
	err = r.co.addEvidence(ref, object)
	return
}
