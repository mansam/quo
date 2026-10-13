package capture

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// collector puts objects somewhere and builds the capture record as it goes,
// so that an object quo asked for is accounted for whether or not it arrived.
type collector struct {
	sink   sink
	record bundle.CaptureRecord
}

// sink is where a collector puts the objects it reads. A capture puts them in
// a bundle; a check puts them nowhere, because it reports what a capture
// would collect and keeps none of it.
type sink interface {
	// Collect takes one resource the bundle holds.
	Collect(resource bundle.Resource, data []byte) (err error)
	// AddExpandedSpec takes the source cluster's expansion of the
	// VirtualMachine, which is a member of the bundle but not a resource.
	AddExpandedSpec(data []byte)
}

// collect reads everything a capture collects: the VirtualMachine, the
// resources it depends on, and what describes the source cluster. It returns
// what a target would have to provide, which is derived from the referenced
// resources and recorded as it is derived.
//
// A capture and a check both go through here, so that the two cannot drift
// into accounting for different objects. What they do differ in is where the
// objects go, which is the collector's sink.
func (r *collector) collect(
	ctx context.Context,
	c cluster.Cluster,
	vm *unstructured.Unstructured) (required bundle.Requirements, err error) {
	//
	err = collectVirtualMachine(r, vm)
	if err != nil {
		return
	}
	claims, err := collectReferenced(ctx, c, r, vm)
	if err != nil {
		return
	}
	err = captureEnvironment(ctx, c, r, vm, claims)
	if err != nil {
		return
	}
	required = buildRequirements(vm, claims)
	r.require(required)
	return
}

// add puts one object into the bundle as Kubernetes YAML, exactly as the
// cluster returned it, and records it as collected.
func (r *collector) add(ref cluster.ObjectRef, object *unstructured.Unstructured) (err error) {
	encoded, err := encode(ref, object)
	if err != nil {
		return
	}
	err = r.sink.Collect(bundleResource(ref), encoded)
	if err != nil {
		return
	}
	r.attempt(ref, bundle.OutcomeCollected)
	return
}

// addExpandedSpec puts the source cluster's expansion of the VirtualMachine
// into the bundle as a member of its own.
//
// It is recorded under its own kind rather than the VirtualMachine's, because
// the expansion carries the same kind and name as the VirtualMachine the
// bundle collected and the two would otherwise be one entry repeated.
func (r *collector) addExpandedSpec(
	ref cluster.ObjectRef,
	expanded *unstructured.Unstructured) (err error) {
	//
	encoded, err := encode(ref, expanded)
	if err != nil {
		return
	}
	r.sink.AddExpandedSpec(encoded)
	r.attempt(ref, bundle.OutcomeCollected)
	return
}

// skip records an object quo tried to read and could not. The reason is what
// lets a reader tell an object that was never there from one quo was not
// allowed to look at, and the message carries what the cluster said, which is
// where a detail such as the missing permission appears.
func (r *collector) skip(ref cluster.ObjectRef, failed error) {
	r.record = append(r.record, bundle.Attempt{
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
		Outcome:   bundle.OutcomeSkipped,
		Reason:    skipReason(failed),
		Message:   failed.Error(),
	})
}

// require records everything the bundle declares it needs from a target but
// does not hold: a network, a storage class, a container disk image. Two
// requirements naming one object are recorded once.
func (r *collector) require(required bundle.Requirements) {
	objects := requiredObjects(required)
	seen := make(map[cluster.ObjectRef]bool, len(objects))
	for _, ref := range objects {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		r.attempt(ref, bundle.OutcomeRequired)
	}
}

// attempt records an object and what became of it.
func (r *collector) attempt(ref cluster.ObjectRef, outcome bundle.Outcome) {
	r.record = append(r.record, bundle.Attempt{
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
		Outcome:   outcome,
	})
}

// encode renders one object as the Kubernetes YAML a bundle holds it as.
func encode(ref cluster.ObjectRef, object *unstructured.Unstructured) (encoded []byte, err error) {
	encoded, err = yaml.Marshal(object.Object)
	if err != nil {
		err = fmt.Errorf("marshal %s %q: %w", ref.Kind, ref.Name, err)
		return
	}
	return
}

// bundleResource is how a bundle names the object a reference points at.
func bundleResource(ref cluster.ObjectRef) (resource bundle.Resource) {
	resource = bundle.Resource{
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}
	return
}

// skipReason maps a failure to read an object onto the reason the capture
// record gives for skipping it. A failure quo cannot account for is recorded
// as unreadable rather than as one of the reasons it is not.
func skipReason(failed error) (reason bundle.SkipReason) {
	switch {
	case errors.Is(failed, cluster.ErrNotFound):
		reason = bundle.SkipNotFound
	case errors.Is(failed, cluster.ErrForbidden):
		reason = bundle.SkipForbidden
	case errors.Is(failed, cluster.ErrNotInstalled):
		reason = bundle.SkipNotInstalled
	default:
		reason = bundle.SkipUnreadable
	}
	return
}
