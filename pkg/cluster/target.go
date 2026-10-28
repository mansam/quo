package cluster

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// Target is everything a deploy needs from the cluster it deploys onto. It
// is the mirror of the Cluster interface: everything quo writes outside its
// own process passes through it, so that a test can substitute a fake and
// never needs a running cluster.
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
