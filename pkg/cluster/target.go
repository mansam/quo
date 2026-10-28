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
// off. It is the mirror of the Cluster interface. Everything quo writes
// outside its own process passes through it. A test can therefore
// substitute a fake and never needs a running cluster.
//
// The reads are for the refusals a deploy makes before it creates anything:
// a namespace that is not there, a name that is already taken. Finding
// nothing is often the answer a deploy went looking for. That is the
// opposite of what a capture reads for.
type Target interface {
	// GetNamespace returns the named namespace. GetNamespace reports a
	// failed read as ErrNotFound where that is what happened. A deploy
	// can then refuse, rather than create objects half-way into a
	// namespace that was never there.
	GetNamespace(ctx context.Context, name string) (namespace *unstructured.Unstructured, err error)
	// GetObject returns the object the reference names. GetObject reports
	// a failed read as ErrNotFound, ErrForbidden or ErrNotInstalled where
	// that is what happened.
	GetObject(ctx context.Context, ref ObjectRef) (object *unstructured.Unstructured, err error)
	// Create puts one object onto the cluster. Create sends the object
	// exactly as the caller built it. What a bundle holds therefore
	// reaches the cluster unchanged, wherever a deploy is not forced to
	// change it.
	Create(ctx context.Context, object *unstructured.Unstructured) (err error)
	// UploadDisk puts the bytes readable from content into the claim the
	// named DataVolume creates in namespace. CDI names that claim after
	// the DataVolume itself. UploadDisk sends the bytes unchanged, exactly
	// as it read them. size is the count the bytes will add up to.
	// UploadDisk is told the count, so that a transfer that yields fewer
	// bytes than the count fails rather than ending early.
	//
	// The upload waits for CDI to make the claim ready to receive the
	// bytes. A caller that creates the DataVolume and uploads into it at
	// once therefore needs no readiness loop of its own.
	UploadDisk(ctx context.Context, namespace, name string, content io.Reader, size int64) (err error)
	// ListByLabel returns the objects namespace holds that carry the
	// label key=value. A deploy labels everything it creates with the
	// bundle's identity. One label therefore lists one whole deployment.
	// That is the selection an undeploy makes.
	//
	// ListByLabel scans only DeployedKinds, because they are the only
	// kinds that can carry the label. ListByLabel skips a kind the
	// cluster serves no resource for, because nothing was ever deployed
	// of it. Each object returned carries its own kind. A Delete can
	// therefore find the resource to delete from, the way a Create finds
	// the one to write to.
	ListByLabel(ctx context.Context, namespace, key, value string) (objects []*unstructured.Unstructured, err error)
	// Delete deletes one object from the cluster, one a ListByLabel
	// returned. Delete returns once the API server accepts the deletion.
	// Delete does not wait for the object to be gone. A finalizer can
	// hold one for as long as it likes. A deletion that hangs is what
	// kubectl is for.
	Delete(ctx context.Context, object *unstructured.Unstructured) (err error)
}

// The KubeVirt adapter serves both directions quo works in.
var _ Target = &KubeVirt{}

// GetNamespace returns the named namespace.
//
// The API server itself serves a namespace. A not-found therefore
// needs no confirming against discovery the way an object read does. It
// means the namespace is not there.
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
// Create finds the resource to write to from the object's own kind through
// discovery. An object quo knows nothing about is therefore created
// anyway. What a bundle holds is what reaches the cluster.
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
// namespace. They are the by-kind table's created rows. A deploy applies
// the VirtualMachine. It creates the DataVolumes for the bundle's disks.
// It applies the ConfigMaps, Secrets and ServiceAccounts unchanged.
// A deploy reads everything else a bundle holds at most, and never
// writes it. Nothing else therefore carries a deploy's label.
//
// The claim a DataVolume creates is not among them. CDI owns the claim
// through an owner reference and deletes it with the DataVolume. An
// undeploy therefore never selects the claim on its own.
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
// ListByLabel lists each of DeployedKinds with the label as its selector.
// A kind the cluster serves no resource for reads back from a list as a
// not-found. A resource the cluster serves would return an empty list.
// That kind is therefore skipped: nothing was ever deployed of it.
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
		// ListByLabel stamps the kind on each object. What a list
		// returns therefore names its own kind. A Delete can find the
		// resource from it the way a Create does.
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

// Delete deletes one object from the cluster.
//
// Delete finds the resource to delete from through discovery, from the
// object's own kind, the way Create does. A caller can therefore pass the
// objects a ListByLabel returned straight to Delete. Delete returns once
// the API server accepts the deletion. Whether the object is gone yet is
// the cluster's business. A finalizer can hold one.
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
