package deploy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// appliedKinds is the by-kind table's applied half: the kinds of resource
// a deploy creates as the bundle holds them. The VirtualMachine refers to
// them by name, and the references have to resolve on the target.
// Everything else a bundle holds is read-only. The instancetypes,
// preferences and ControllerRevisions are folded into the expansion the
// deploy applies. The rest describes the source cluster. Nothing else is
// ever created.
var appliedKinds = map[string]bool{
	cluster.KindConfigMap:      true,
	cluster.KindSecret:         true,
	cluster.KindServiceAccount: true,
}

// plannedClosure is one of the bundle's closure objects as a deploy
// applies it. It holds what the manifest says about the member, the
// member decoded once it is read, and the deploy record's entry for it.
type plannedClosure struct {
	// resource is what the manifest says about the member: its kind, its
	// namespace and name on the source cluster, and where it sits.
	resource bundle.Resource
	// object is the member decoded, nil until it is read.
	object *unstructured.Unstructured
	// step is the deploy record's entry, which the phases update as the
	// object proceeds.
	step Step
}

// planClosure picks the closure objects a deploy applies, in the order
// the bundle holds them. A bundle is written with its resources in the
// order they were collected. What planClosure returns is therefore the
// order in which a capture read the machine's references.
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

// readClosure decodes the related resources that deploy creates. A read or
// decode error stops deployment before any writes.
func readClosure(reader *bundle.Reader, closure []plannedClosure) (err error) {
	for i := range closure {
		content, readErr := reader.Read(closure[i].resource.Path)
		if readErr != nil {
			err = readErr
			return
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

// createClosure creates the related resources with the forced rewrites.
// A create failure is recorded without stopping subsequent creates.
func createClosure(
	ctx context.Context,
	t cluster.Target,
	closure []plannedClosure,
	opt Options,
	identity string) {
	//
	for i := range closure {
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
