//go:build live

package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"

	"github.com/mansam/quo/pkg/cluster"
)

// The objectgraph subresource, which is KubeVirt's own account of what a
// VirtualMachine depends on.
const (
	// subresourceGroupVersion is where virt-api serves the subresources that
	// belong to a VirtualMachine. They are served by virt-api rather than by
	// the API server, so none of them is a field of the VirtualMachine
	// resource.
	subresourceGroupVersion = "subresources.kubevirt.io/v1"
	// objectGraphResource is how discovery lists the subresource. A
	// subresource is named for the resource it belongs to as well as for
	// itself.
	objectGraphResource = "virtualmachines/objectgraph"
)

// errGateOff reports the ObjectGraph feature gate being off on a cluster that
// otherwise serves the subresource. It is off by default, so this is the
// likeliest reason a suitable-looking cluster cannot answer.
var errGateOff = errors.New("KubeVirt's ObjectGraph feature gate is not enabled")

// Kinds KubeVirt's graph names that quo's traversal does not follow, because
// a stopped VirtualMachine has neither and quo captures nothing else.
var runtimeKinds = map[string]bool{
	"Pod":                    true,
	"VirtualMachineInstance": true,
}

// graphNode is one node of the graph the objectgraph subresource returns: an
// object the VirtualMachine depends on, and what that object depends on in
// turn.
//
// This mirrors the fields of KubeVirt's own ObjectGraphNode that the
// comparison reads, rather than importing the type, because quo takes no
// dependency on KubeVirt's Go API. Pinning the field names here is part of
// what the suite checks: a rename upstream arrives as a graph that decodes to
// nothing.
type graphNode struct {
	ObjectReference objectReference `json:"objectReference"`
	Children        []graphNode     `json:"children,omitempty"`
}

// objectReference names the object one node stands for. Namespace is a
// pointer upstream and empty on a cluster-scoped object, so a node naming no
// namespace is one that names no namespace rather than one that failed to
// decode.
type objectReference struct {
	APIGroup  *string `json:"apiGroup"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Namespace *string `json:"namespace"`
}

// objectGraph returns KubeVirt's own account of what the named VirtualMachine
// depends on.
//
// It goes through the REST client rather than the dynamic one because what
// the subresource returns is a graph node and not a Kubernetes object: it
// carries no apiVersion and no kind, which the dynamic client's decoder
// requires of everything it reads.
func (r *suite) objectGraph(
	ctx context.Context,
	namespace, name string) (root graphNode, err error) {
	//
	path := fmt.Sprintf(
		"/apis/%s/namespaces/%s/virtualmachines/%s/objectgraph",
		subresourceGroupVersion,
		namespace,
		name)
	body, err := r.core.Discovery().RESTClient().Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		// virt-api serves the route whether or not the gate is on and
		// refuses the request with a bad request when it is off, which is
		// the only way this endpoint answers with one.
		if k8serr.IsBadRequest(err) {
			err = fmt.Errorf("%w: %s", errGateOff, err)
			return
		}
		err = fmt.Errorf("read the object graph for %s/%s: %w", namespace, name, err)
		return
	}
	err = json.Unmarshal(body, &root)
	if err != nil {
		err = fmt.Errorf("decode the object graph for %s/%s: %w", namespace, name, err)
		return
	}
	return
}

// refs flattens the graph into the objects it names, in the order it names
// them and without duplicates.
//
// Each one is reported under the kind, namespace and name quo would use for
// it, so that the two traversals can be compared directly.
func (r *graphNode) refs() (refs []cluster.ObjectRef) {
	seen := map[cluster.ObjectRef]bool{}
	r.collect(seen, &refs)
	return
}

// collect adds the object a node names and then the objects its children
// name.
//
// A node that names nothing quo would follow is passed over, but its children
// are still read: a VirtualMachineInstance quo ignores is the parent of a Pod
// in KubeVirt's graph, and nothing says a kind quo does follow will not
// appear under one it does not.
func (r *graphNode) collect(seen map[cluster.ObjectRef]bool, refs *[]cluster.ObjectRef) {
	ref := r.ObjectReference.ref()
	if followed(ref) && !seen[ref] {
		seen[ref] = true
		*refs = append(*refs, ref)
	}
	for i := range r.Children {
		r.Children[i].collect(seen, refs)
	}
}

// followed reports whether a node names an object quo's traversal would also
// name. A node naming nothing is one whose reference did not decode, which
// the comparison has nothing to say about.
func followed(ref cluster.ObjectRef) (follow bool) {
	if ref.Name == "" {
		return
	}
	if runtimeKinds[ref.Kind] {
		return
	}
	follow = true
	return
}

// ref is the reference quo would use for the object a node names.
func (r objectReference) ref() (ref cluster.ObjectRef) {
	ref = cluster.ObjectRef{Kind: r.Kind, Name: r.Name}
	if r.Namespace == nil {
		return
	}
	ref.Namespace = *r.Namespace
	return
}
