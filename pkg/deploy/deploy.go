package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// Deploy applies a bundle onto the cluster behind t and returns the deploy
// record: every object it attempted and what became of each one.
//
// quo refuses, before creating anything, when the bundle cannot be read or
// its format version is unknown, when it holds no expanded spec, when the
// namespace does not exist, and when the VirtualMachine it would create is
// already there. Each refusal names what it found.
//
// Past the refusals the deploy is best-effort: a step that fails is
// recorded and the run continues, and a bundle member that fails its
// digest is skipped rather than applied, so corruption never reaches the
// target. The record is returned however the run ended, and the error is
// non-nil when anything was not created, so that a caller can tell a
// complete deploy from a partial one.
//
// The context bounds the run: cancelling it stops the deploy where it
// stands, and everything already created stays in place, because a deploy
// never rolls back.
func Deploy(ctx context.Context, t cluster.Target, opt Options) (record Record, err error) {
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
	manifest := reader.Manifest()
	if manifest.ExpandedSpec == "" {
		err = &NoExpandedSpecError{Bundle: opt.Bundle}
		return
	}
	err = validateTarget(ctx, t, opt, manifest)
	if err != nil {
		return
	}
	step, err := createVirtualMachine(ctx, t, reader, manifest, opt)
	if err != nil {
		return
	}
	record = append(record, step)
	if step.Outcome != OutcomeCreated {
		err = &NotCreatedError{NotCreated: 1, Attempted: len(record)}
	}
	return
}

// validateTarget checks what a deploy can check about the target before it
// creates anything, and refuses — naming what it found — when the deploy
// cannot succeed.
//
// The namespace has to be there, because objects created into a namespace
// that was never made land nowhere. And the name the VirtualMachine will
// take has to be free, because something already holding it is someone
// else's work, which a deploy neither overwrites nor builds around.
func validateTarget(
	ctx context.Context,
	t cluster.Target,
	opt Options,
	manifest *bundle.Manifest) (err error) {
	//
	_, err = t.GetNamespace(ctx, opt.Namespace)
	if errors.Is(err, cluster.ErrNotFound) {
		err = &MissingNamespaceError{Namespace: opt.Namespace}
		return
	}
	if err != nil {
		return
	}
	ref := cluster.ObjectRef{
		Kind:      cluster.KindVirtualMachine,
		Namespace: opt.Namespace,
		Name:      virtualMachineName(opt, manifest),
	}
	_, err = t.GetObject(ctx, ref)
	if errors.Is(err, cluster.ErrNotFound) {
		// A name that is free is what a deploy went looking for.
		err = nil
		return
	}
	if err != nil {
		return
	}
	err = &AlreadyExistsError{Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name}
	return
}

// createVirtualMachine creates the VirtualMachine a deploy is for and
// returns the deploy record's entry for it: the bundle's expanded spec —
// the source cluster's own resolution of the machine — carrying the forced
// rewrites and nothing else.
//
// The step is recorded whatever became of it. Only a bundle that cannot be
// read at all is an error, because that is a refusal rather than a step:
// the member holding the expansion is indexed but absent, or does not
// decode as an object.
func createVirtualMachine(
	ctx context.Context,
	t cluster.Target,
	reader *bundle.Reader,
	manifest *bundle.Manifest,
	opt Options) (step Step, err error) {
	//
	step = Step{
		Kind:      cluster.KindVirtualMachine,
		Namespace: opt.Namespace,
		Name:      virtualMachineName(opt, manifest),
	}
	expanded, readErr := reader.Read(manifest.ExpandedSpec)
	if readErr != nil {
		var mismatch *bundle.DigestMismatchError
		if !errors.As(readErr, &mismatch) {
			err = readErr
			return
		}
		// A member that fails its digest never reaches the target. The
		// object is skipped, recorded, and the run goes on.
		step.Outcome = OutcomeSkipped
		step.Reason = readErr.Error()
		return
	}
	vm := &unstructured.Unstructured{}
	err = yaml.Unmarshal(expanded, &vm.Object)
	if err != nil {
		err = fmt.Errorf("decode the expanded spec: %w", err)
		return
	}
	vm = buildVirtualMachine(vm, opt, manifest.Provenance, bundleIdentity(reader.ManifestBytes()))
	createErr := t.Create(ctx, vm)
	if createErr != nil {
		step.Outcome = OutcomeFailed
		step.Reason = createErr.Error()
		return
	}
	step.Outcome = OutcomeCreated
	return
}
