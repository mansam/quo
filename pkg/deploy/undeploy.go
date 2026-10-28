package deploy

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// UndeployOptions is an undeploy once the command line is parsed.
type UndeployOptions struct {
	// Bundle is the path of the bundle whose deployment the undeploy
	// deletes, exactly as the command line named it. The bundle is read
	// for its identity, the label value every object of its deployment
	// carries, and for nothing else. An undeploy puts nothing on the
	// cluster, so it needs none of what the bundle holds.
	Bundle string
	// Namespace is the namespace the deployment sits in. It is required,
	// the same rule a deploy's namespace obeys. quo never uses the
	// kubeconfig's current namespace in its place, because deleting
	// objects from someone's default namespace by accident is a quiet
	// mistake.
	Namespace string
	// DryRun lists what the undeploy would delete and deletes nothing.
	DryRun bool
}

// Deletion is one object an undeploy deleted from the cluster, or on a
// dry run one it would delete. A deletion the cluster refused leaves
// the entry with Err set, saying why the object is still there.
type Deletion struct {
	Kind      string
	Namespace string
	Name      string
	// Err is why the object is still on the cluster. It is nil for an
	// object the undeploy deleted, and for every entry of a dry run,
	// which attempts nothing.
	Err error
}

// Undeploy deletes one deployment from the cluster behind t. The
// deployment is the objects carrying the bundle's label in the
// namespace. That is everything a deploy of that bundle put there. A
// deployment of another bundle carries a different label and is
// untouched. That is what makes an undeploy delete exactly one
// deployment.
//
// The bundle is opened and its manifest read, because the manifest's own
// bytes are what the label was computed from. A bundle that cannot be
// read is an error naming it.
//
// An undeploy is best-effort, like a deploy. A deletion the cluster
// refuses is recorded and the run continues. The error is non-nil when
// anything was not deleted, so that a caller can tell a complete
// undeploy from a partial one. Each deletion returns once the API server
// accepts it. Whether an object is gone yet is the cluster's business:
// a finalizer can hold one for as long as it likes. An undeploy does
// not wait. A deletion that hangs is what kubectl is for.
func Undeploy(
	ctx context.Context,
	t cluster.Target,
	opt UndeployOptions) (deleted []Deletion, err error) {
	//
	file, err := os.Open(opt.Bundle)
	if err != nil {
		err = fmt.Errorf("open bundle: %w", err)
		return
	}
	defer func() {
		_ = file.Close()
	}()
	reader, err := bundle.Open(file)
	if err != nil {
		return
	}
	identity := bundleIdentity(reader.ManifestBytes())
	held, err := t.ListByLabel(ctx, opt.Namespace, labelBundle, identity)
	if err != nil {
		return
	}
	// The order is fixed, by kind then name, so that what an undeploy
	// says it deleted reads the same from run to run.
	slices.SortFunc(held, func(a, b *unstructured.Unstructured) (order int) {
		order = cmp.Compare(a.GetKind(), b.GetKind())
		if order == 0 {
			order = cmp.Compare(a.GetName(), b.GetName())
		}
		return
	})
	if opt.DryRun {
		// Nothing is attempted, so nothing can fail: the list is what
		// the undeploy would delete, whole.
		for i := range held {
			deleted = append(deleted, deletionOf(held[i]))
		}
		return
	}
	for i := range held {
		entry := deletionOf(held[i])
		entry.Err = t.Delete(ctx, held[i])
		deleted = append(deleted, entry)
	}
	err = notDeleted(deleted)
	return
}

// deletionOf is the entry for one object an undeploy selected.
func deletionOf(object *unstructured.Unstructured) (entry Deletion) {
	entry = Deletion{
		Kind:      object.GetKind(),
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
	return
}

// notDeleted returns the error that says an undeploy did not delete
// everything it tried to, nil when it did. What the undeploy deleted
// names each object and what became of it. This error is what makes the
// exit code non-zero, so that a script can tell a complete undeploy from
// a partial one.
func notDeleted(deleted []Deletion) (err error) {
	remaining := 0
	for i := range deleted {
		if deleted[i].Err != nil {
			remaining++
		}
	}
	if remaining > 0 {
		err = &NotDeletedError{NotDeleted: remaining, Attempted: len(deleted)}
	}
	return
}
