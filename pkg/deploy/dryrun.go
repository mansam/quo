package deploy

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/cluster"
)

// dryRunTarget is the target a dry run works through. Deploy and
// Undeploy wrap the target they were given in one of these when their
// options ask for a dry run.
//
// Its reads go to the target it wraps, so the refusals and the mappings
// answer from the cluster behind it. Its writes keep nothing and report
// success, so the record a dry run returns is the record a real deploy
// would print: what the deploy would create is said as created, and
// nothing reaches the cluster.
//
// Each read is delegated by name. Nothing is embedded. A write method
// added to cluster.Target later cannot reach a cluster by default. It is
// missing here, and a missing method does not compile.
type dryRunTarget struct {
	// wrapped is the target every read goes to.
	wrapped cluster.Target
}

var _ cluster.Target = &dryRunTarget{}

// GetNamespace returns the namespace the wrapped target holds.
func (r *dryRunTarget) GetNamespace(ctx context.Context, name string) (namespace *unstructured.Unstructured, err error) {
	namespace, err = r.wrapped.GetNamespace(ctx, name)
	return
}

// GetObject returns the object the wrapped target holds.
func (r *dryRunTarget) GetObject(ctx context.Context, ref cluster.ObjectRef) (object *unstructured.Unstructured, err error) {
	object, err = r.wrapped.GetObject(ctx, ref)
	return
}

// Create keeps nothing and reports success. NO-OP.
func (r *dryRunTarget) Create(ctx context.Context, object *unstructured.Unstructured) (err error) {
	return
}

// UploadDisk keeps nothing and reports success. It waits for no claim
// and reads no bytes, so nothing is reported of the upload. NO-OP.
func (r *dryRunTarget) UploadDisk(ctx context.Context, upload cluster.Upload) (err error) {
	return
}

// ListByLabel returns the objects the wrapped target holds.
func (r *dryRunTarget) ListByLabel(
	ctx context.Context,
	namespace, key, value string) (objects []*unstructured.Unstructured, err error) {
	objects, err = r.wrapped.ListByLabel(ctx, namespace, key, value)
	return
}

// Delete keeps nothing and reports success. NO-OP.
func (r *dryRunTarget) Delete(ctx context.Context, object *unstructured.Unstructured) (err error) {
	return
}
