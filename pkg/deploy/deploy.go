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
// already there. A bundle holding disks to upload is refused as well when
// the target has no CDI, when a disk's storage class is not on the target
// under the name the bundle records, and when the target's own account of
// that class rules out the volume mode the disk needs. Each refusal names
// what it found.
//
// Past the refusals the deploy is best-effort: a step that fails is
// recorded and the run continues, and a bundle member that fails its
// digest is skipped rather than applied, so corruption never reaches the
// target. The objects go on in order — the DataVolumes, then the uploads,
// then the VirtualMachine last — so that nothing in the namespace looks
// like a VirtualMachine until its disks are in place. The record is
// returned however the run ended, and the error is non-nil when anything
// was not created, so that a caller can tell a complete deploy from a
// partial one.
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
	// A bundle is written with its resources first and its disks last,
	// and the reader walks forward only, so the expansion is read before
	// the first disk is asked for. It is read whole — the expansion is one
	// small member — and applied last.
	expanded, step, err := readVirtualMachine(reader, manifest, opt)
	if err != nil {
		return
	}
	if step.Outcome == OutcomeSkipped {
		// The member failed its digest, so the machine it names cannot be
		// created. The disks are not uploaded for a machine that cannot
		// be, which is the same rule that refuses a bundle holding no
		// expansion at all.
		record = append(record, step)
		err = &NotCreatedError{NotCreated: 1, Attempted: 1}
		return
	}
	disks := planDisks(manifest, opt)
	err = resolveDisks(ctx, t, disks)
	if err != nil {
		return
	}
	identity := bundleIdentity(reader.ManifestBytes())
	// The DataVolumes, then the uploads, then the VirtualMachine last.
	createDataVolumes(ctx, t, disks, opt, identity)
	uploadDisks(ctx, t, reader, disks, opt)
	for i := range disks {
		record = append(record, disks[i].step)
	}
	record = append(record, createVirtualMachine(
		ctx, t, expanded, step, opt, manifest.Provenance, identity))
	err = notCreated(record)
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

// readVirtualMachine reads and decodes the bundle's expanded spec, and
// returns the step the deploy record holds for the VirtualMachine.
//
// A member that fails its digest is a skipped step rather than an error —
// the run records it and ends — while a member that does not decode is a
// refusal: a bundle whose expansion cannot be read as an object cannot be
// deployed at all.
func readVirtualMachine(
	reader *bundle.Reader,
	manifest *bundle.Manifest,
	opt Options) (expanded *unstructured.Unstructured, step Step, err error) {
	//
	step = Step{
		Kind:      cluster.KindVirtualMachine,
		Namespace: opt.Namespace,
		Name:      virtualMachineName(opt, manifest),
	}
	content, readErr := reader.Read(manifest.ExpandedSpec)
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
	expanded = &unstructured.Unstructured{}
	err = yaml.Unmarshal(content, &expanded.Object)
	if err != nil {
		err = fmt.Errorf("decode the expanded spec: %w", err)
		return
	}
	return
}

// createVirtualMachine creates the VirtualMachine a deploy is for and
// finishes the deploy record's entry for it: the bundle's expanded spec —
// the source cluster's own resolution of the machine — carrying the forced
// rewrites and nothing else.
//
// The step is the one readVirtualMachine built, so the entry a skipped
// expansion would have recorded and the entry a created one records name
// the same object. It is recorded whatever became of it.
func createVirtualMachine(
	ctx context.Context,
	t cluster.Target,
	expanded *unstructured.Unstructured,
	step Step,
	opt Options,
	provenance bundle.Provenance,
	identity string) (finished Step) {
	//
	vm := buildVirtualMachine(expanded, opt, provenance, identity)
	createErr := t.Create(ctx, vm)
	if createErr != nil {
		step.Outcome = OutcomeFailed
		step.Reason = createErr.Error()
		return step
	}
	step.Outcome = OutcomeCreated
	return step
}

// notCreated returns the error that says a run did not create everything
// it tried to, nil when it did. The deploy record names each object and
// what became of it; this error is what makes the exit code non-zero, so
// that a script can tell a complete deploy from a partial one.
func notCreated(record Record) (err error) {
	missing := 0
	for i := range record {
		if record[i].Outcome != OutcomeCreated {
			missing++
		}
	}
	if missing > 0 {
		err = &NotCreatedError{NotCreated: missing, Attempted: len(record)}
	}
	return
}
