package deploy

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// appliedKinds is the by-kind table's applied half: the kinds of resource
// a deploy creates as the bundle holds them, because the VirtualMachine
// refers to them by name and the references have to resolve on the target.
// Everything else a bundle holds is read-only — the instancetypes,
// preferences and ControllerRevisions are folded into the expansion the
// deploy applies, and the rest describes the source cluster — so it is
// never created.
var appliedKinds = map[string]bool{
	cluster.KindConfigMap:      true,
	cluster.KindSecret:         true,
	cluster.KindServiceAccount: true,
}

// plannedClosure is one of the bundle's closure objects as a deploy goes
// about applying it: what the manifest says about the member, the member
// decoded once it is read, and the deploy record's entry for it.
type plannedClosure struct {
	// resource is what the manifest says about the member: its kind, its
	// namespace and name on the source cluster, and where it sits.
	resource bundle.Resource
	// object is the member decoded, nil until it is read and nil for good
	// when the member failed its digest.
	object *unstructured.Unstructured
	// step is the deploy record's entry, which the phases fill in as the
	// object proceeds.
	step Step
}

// planClosure picks out the closure objects a deploy applies, in the order
// the bundle holds them. A bundle is written with its resources in the
// order they were collected, so what comes back is the order a capture
// walked the machine's references.
func planClosure(manifest *bundle.Manifest, opt Options) (closure []plannedClosure) {
	for i := range manifest.Resources {
		resource := manifest.Resources[i]
		if !appliedKinds[resource.Kind] {
			continue
		}
		closure = append(closure, plannedClosure{
			resource: resource,
			step: Step{
				Kind:      resource.Kind,
				Namespace: opt.Namespace,
				Name:      resource.Name,
			},
		})
	}
	return
}

// readClosure reads and decodes the closure members a deploy applies. It
// runs before the expansion is asked for: a bundle is written with its
// resources first and the expansion after them, and the reader walks
// forward only, so asking for the expansion first would walk past every
// resource member and leave these unreadable.
//
// A member that fails its digest is a skipped step rather than an error —
// the run records the gap and goes on — while a member that does not
// decode, or never arrived, is a refusal, as it is for the expansion: a
// bundle whose member cannot be read as an object is not a bundle a
// capture of quo produces.
func readClosure(reader *bundle.Reader, closure []plannedClosure) (err error) {
	for i := range closure {
		content, readErr := reader.Read(closure[i].resource.Path)
		if readErr != nil {
			var mismatch *bundle.DigestMismatchError
			if !errors.As(readErr, &mismatch) {
				err = readErr
				return
			}
			// A member that fails its digest never reaches the target.
			// The object is skipped, recorded, and the run goes on.
			closure[i].step.Outcome = OutcomeSkipped
			closure[i].step.Reason = readErr.Error()
			continue
		}
		closure[i].object = &unstructured.Unstructured{}
		err = yaml.Unmarshal(content, &closure[i].object.Object)
		if err != nil {
			err = fmt.Errorf(
				"decode the %s %s: %w",
				closure[i].resource.Kind,
				closure[i].resource.Name,
				err)
			return
		}
	}
	return
}

// createClosure applies the closure objects a deploy read, each carrying
// the forced rewrites that apply to every created object. They go on after
// the uploads and before the VirtualMachine, so that the machine's
// cloud-init, sysprep and access credentials are present when it is
// examined. An object that was skipped when it was read creates nothing;
// a create that fails is recorded and the run continues to the objects
// behind it and the machine.
func createClosure(
	ctx context.Context,
	t cluster.Target,
	closure []plannedClosure,
	opt Options,
	identity string) {
	//
	for i := range closure {
		if closure[i].step.Outcome == OutcomeSkipped {
			// An object that creates nothing was decided when its member
			// was read; the record already says why.
			continue
		}
		object := buildClosureObject(closure[i].object, opt, identity)
		err := t.Create(ctx, object)
		if err != nil {
			closure[i].step.Outcome = OutcomeFailed
			closure[i].step.Reason = err.Error()
			continue
		}
		closure[i].step.Outcome = OutcomeCreated
	}
}
