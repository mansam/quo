package cluster

import (
	"context"
	"fmt"
	"io"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Target is everything a deploy needs from the cluster it deploys onto,
// and everything an undeploy needs from the one it takes a deployment
// off. It is the mirror of the Cluster interface: everything quo writes
// outside its own process passes through it, so that a test can
// substitute a fake and never needs a running cluster.
//
// The reads are for the refusals a deploy makes before it creates anything:
// a namespace that is not there, a name that is already taken. Finding
// nothing is often the answer a deploy went looking for, which is the
// opposite of what a capture reads for.
type Target interface {
	// GetNamespace returns the named namespace. A read that failed is
	// reported as ErrNotFound where that is what happened, so a deploy
	// can refuse rather than create objects half-way into a namespace
	// that was never there.
	GetNamespace(ctx context.Context, name string) (namespace *unstructured.Unstructured, err error)
	// GetObject returns the object the reference names. A read that failed
	// is reported as ErrNotFound, ErrForbidden or ErrNotInstalled where
	// that is what happened.
	GetObject(ctx context.Context, ref ObjectRef) (object *unstructured.Unstructured, err error)
	// Create puts one object onto the cluster. The object is sent exactly
	// as the caller built it, so that what a bundle holds reaches the
	// cluster unchanged wherever a deploy is not forced to change it.
	Create(ctx context.Context, object *unstructured.Unstructured) (err error)
	// UploadDisk puts the bytes readable from content into the claim the
	// named DataVolume creates in namespace, which CDI names after the
	// DataVolume itself. The bytes are sent unchanged, exactly as they
	// were read; size is the count they will add up to, which the upload
	// is told so that a transfer which comes up short fails rather than
	// ending early.
	//
	// The upload waits for CDI to make the claim ready to receive the
	// bytes, so a caller that creates the DataVolume and uploads into it
	// at once needs no readiness loop of its own.
	UploadDisk(ctx context.Context, namespace, name string, content io.Reader, size int64) (err error)
	// ListByLabel returns the objects namespace holds that carry the
	// label key=value. A deploy labels everything it creates with the
	// bundle's identity, so one label lists one whole deployment, which
	// is the selection an undeploy makes. Only DeployedKinds are
	// scanned, because they are the only kinds that can carry the label,
	// and a kind the cluster serves no resource for is skipped, because
	// nothing was ever deployed of it. Each object returned carries its
	// own kind, so a Delete can find the resource to delete from the
	// way a Create finds the one to write to.
	ListByLabel(ctx context.Context, namespace, key, value string) (objects []*unstructured.Unstructured, err error)
	// Delete removes one object from the cluster — one a ListByLabel
	// returned. It returns once the API server accepts the deletion and
	// does not wait for the object to be gone: a finalizer can hold one
	// for as long as it likes, and a deletion that hangs is what
	// kubectl is for.
	Delete(ctx context.Context, object *unstructured.Unstructured) (err error)
}

// The KubeVirt adapter serves both directions quo works in.
var _ Target = &KubeVirt{}

// GetNamespace returns the named namespace.
//
// A namespace is served by the API server itself, so a not-found needs no
// confirming against discovery the way an object read does: it means the
// namespace is not there.
func (r *KubeVirt) GetNamespace(ctx context.Context, name string) (namespace *unstructured.Unstructured, err error) {
	//
	namespace, err = r.dynamic.
		Resource(namespaceResource).
		Get(ctx, name, meta.GetOptions{})
	if err != nil {
		namespace = nil
		switch {
		case k8serr.IsNotFound(err):
			err = fmt.Errorf("namespace %q: %w", name, ErrNotFound)
		default:
			err = fmt.Errorf("get namespace %q: %w", name, err)
		}
		return
	}
	return
}

// Create puts one object onto the cluster.
//
// The resource to write to is found from the object's own kind through
// discovery, so that an object quo knows nothing about is created anyway:
// what a bundle holds is what lands on the cluster.
func (r *KubeVirt) Create(ctx context.Context, object *unstructured.Unstructured) (err error) {
	//
	gvk := object.GroupVersionKind()
	mapping, err := r.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		err = fmt.Errorf("find the resource serving a %s: %w", gvk.Kind, err)
		return
	}
	api := r.dynamic.Resource(mapping.Resource)
	writer := dynamic.ResourceInterface(api)
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		writer = api.Namespace(object.GetNamespace())
	}
	_, err = writer.Create(ctx, object, meta.CreateOptions{})
	if err != nil {
		named := object.GetName()
		if object.GetNamespace() != "" {
			named = object.GetNamespace() + "/" + named
		}
		err = fmt.Errorf("create %s %s: %w", gvk.Kind, named, err)
		return
	}
	return
}

// DeployedKinds are the kinds of object a deploy can leave in a
// namespace: the VirtualMachine it applies, the DataVolumes it creates
// for the bundle's disks, and the ConfigMaps, Secrets and ServiceAccounts
// it applies unchanged — the by-kind table's created rows. Everything
// else a bundle holds is read at most and never written, so nothing else
// carries a deploy's label.
//
// The claim a DataVolume creates is not among them: CDI owns the claim
// through an owner reference and removes it with the DataVolume, so an
// undeploy never selects the claim on its own.
var DeployedKinds = []string{
	KindConfigMap,
	KindDataVolume,
	KindSecret,
	KindServiceAccount,
	KindVirtualMachine,
}

// ListByLabel returns the objects namespace holds that carry the label
// key=value.
//
// Each of DeployedKinds is listed with the label as its selector. A kind
// the cluster serves no resource for reads back from a list as a
// not-found — a resource the cluster serves would return an empty list —
// so that kind is skipped: nothing was ever deployed of it.
func (r *KubeVirt) ListByLabel(
	ctx context.Context,
	namespace, key, value string) (objects []*unstructured.Unstructured, err error) {
	//
	selector := key + "=" + value
	for _, kind := range DeployedKinds {
		served := resourceByKind[kind]
		list, listErr := r.dynamic.
			Resource(served.gvr).
			Namespace(namespace).
			List(ctx, meta.ListOptions{LabelSelector: selector})
		if listErr != nil {
			if k8serr.IsNotFound(listErr) {
				continue
			}
			err = fmt.Errorf("list %s in namespace %q: %w", kind, namespace, listErr)
			return
		}
		// The kind is stamped on each object, so that what a list returns
		// names its own kind and a Delete can find the resource from it
		// the way a Create does.
		gvk := schema.GroupVersionKind{
			Group:   served.gvr.Group,
			Version: served.gvr.Version,
			Kind:    kind,
		}
		for i := range list.Items {
			list.Items[i].SetGroupVersionKind(gvk)
			objects = append(objects, &list.Items[i])
		}
	}
	return
}

// Delete removes one object from the cluster.
//
// The resource to delete from is found from the object's own kind
// through discovery, the way Create's is, so the objects a ListByLabel
// returned can be handed straight back. The call returns once the API
// server accepts the deletion; whether the object is gone yet — a
// finalizer can hold one — is the cluster's business.
func (r *KubeVirt) Delete(ctx context.Context, object *unstructured.Unstructured) (err error) {
	//
	gvk := object.GroupVersionKind()
	mapping, err := r.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		err = fmt.Errorf("find the resource serving a %s: %w", gvk.Kind, err)
		return
	}
	api := r.dynamic.Resource(mapping.Resource)
	writer := dynamic.ResourceInterface(api)
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		writer = api.Namespace(object.GetNamespace())
	}
	err = writer.Delete(ctx, object.GetName(), meta.DeleteOptions{})
	if err != nil {
		named := object.GetName()
		if object.GetNamespace() != "" {
			named = object.GetNamespace() + "/" + named
		}
		err = fmt.Errorf("delete %s %s: %w", gvk.Kind, named, err)
		return
	}
	return
}
