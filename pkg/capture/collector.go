package capture

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// collector puts objects into a bundle and builds the capture record as it
// goes, so that an object quo asked for is accounted for whether or not it
// arrived.
type collector struct {
	writer *bundle.Writer
	record bundle.CaptureRecord
}

// carry puts one object into the bundle as Kubernetes YAML, exactly as the
// cluster returned it, and records it as carried.
func (r *collector) carry(ref cluster.ObjectRef, object *unstructured.Unstructured) (err error) {
	encoded, err := yaml.Marshal(object.Object)
	if err != nil {
		err = fmt.Errorf("marshal %s %q: %w", ref.Kind, ref.Name, err)
		return
	}
	err = r.writer.Carry(
		bundle.Object{
			Kind:      ref.Kind,
			Namespace: ref.Namespace,
			Name:      ref.Name,
		},
		encoded)
	if err != nil {
		return
	}
	r.record = append(r.record, bundle.Attempt{
		Kind:        ref.Kind,
		Namespace:   ref.Namespace,
		Name:        ref.Name,
		Disposition: bundle.DispositionCarried,
	})
	return
}

// skip records an object quo tried to read and could not. The reason is what
// lets a reader tell an object that was never there from one quo was not
// allowed to look at, and the message carries what the cluster said, which is
// where a detail such as the missing permission appears.
func (r *collector) skip(ref cluster.ObjectRef, failed error) {
	r.record = append(r.record, bundle.Attempt{
		Kind:        ref.Kind,
		Namespace:   ref.Namespace,
		Name:        ref.Name,
		Disposition: bundle.DispositionSkipped,
		Reason:      skipReason(failed),
		Message:     failed.Error(),
	})
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
