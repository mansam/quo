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
// record: every object it attempted and what became of each one, and every
// requirement it mapped onto the target's resources.
//
// The bundle's requirements are mapped onto the target's resources: a
// storage class or a multus network maps automatically when the target
// holds one of the requirement's own name, and Options names the rest.
// Every mapping is reported in the record, because nothing maps
// silently. The pod network is never mapped, and a container disk image
// is reported as a requirement and never pull-checked — quo holds no
// registry credentials.
//
// quo refuses, before creating anything, when the bundle cannot be read or
// its format version is unknown, when it holds no expanded spec, when the
// namespace does not exist, and when any object it would create — a
// DataVolume, a closure object, the VirtualMachine — is already there, one
// refusal naming them all. A bundle holding disks to upload is refused as
// well when the target has no CDI and when the target's own account of the
// class a disk's storage was mapped onto rules out the volume mode the disk
// needs. A requirement — a storage class or a multus network — that resolves
// to nothing is a refusal too, naming what was tried: nothing is created
// whose needs cannot be placed.
//
// A dry run — Options.DryRun — runs the complete refusal set and returns
// the record the real deploy would print: the objects it would create, in
// the order it would create them, and the mappings it would apply. Nothing
// is created, nothing is uploaded, and no labels land anywhere; the target
// is only read. The members are still read and verified as the real deploy
// reads them, so one that fails its digest is a skip in the record, and the
// error is the one the real deploy's would be.
//
// Past the refusals the deploy is best-effort: a step that fails is
// recorded and the run continues, and a bundle member that fails its
// digest is skipped rather than applied, so corruption never reaches the
// target. The objects go on in order — the DataVolumes, then the uploads,
// then the closure objects, then the VirtualMachine last — so that nothing
// in the namespace looks like a VirtualMachine until its disks are in
// place, and the machine's cloud-init, sysprep and access credentials are
// present when it is examined. The record is returned however the run
// ended, and the error is non-nil when anything was not created, so that a
// caller can tell a complete deploy from a partial one.
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
	closure := planClosure(manifest, opt)
	disks := planDisks(manifest, opt)
	err = validateTarget(ctx, t, opt, manifest, closure, disks)
	if err != nil {
		return
	}
	// A bundle is written with its resources first, its expansion after
	// them and its disks last, and the reader walks forward only. The
	// closure members are read before the expansion is asked for — asking
	// for the expansion walks past every resource member and leaves them
	// unreadable — and the expansion is read before the first disk is
	// asked for. Both are read whole and held, because they are small,
	// and applied after the disks, each at its turn.
	err = readClosure(reader, closure)
	if err != nil {
		return
	}
	expanded, step, err := readVirtualMachine(reader, manifest, opt)
	if err != nil {
		return
	}
	if step.Outcome == OutcomeSkipped {
		// The member failed its digest, so the machine it names cannot be
		// created. The disks are not uploaded and the closure objects are
		// not applied for a machine that cannot be, which is the same
		// rule that refuses a bundle holding no expansion at all.
		record.Steps = append(record.Steps, step)
		err = &NotCreatedError{NotCreated: 1, Attempted: 1}
		return
	}
	storage, err := resolveDisks(ctx, t, disks, opt)
	if err != nil {
		return
	}
	networks, err := resolveNetworks(ctx, t, manifest.Requirements.Networks, opt)
	if err != nil {
		return
	}
	identity := bundleIdentity(reader.ManifestBytes())
	// The DataVolumes, then the uploads, then the closure objects, then
	// the VirtualMachine last.
	createDataVolumes(ctx, t, disks, opt, identity)
	uploadDisks(ctx, t, reader, disks, opt)
	createClosure(ctx, t, closure, opt, identity)
	for i := range disks {
		record.Steps = append(record.Steps, disks[i].step)
	}
	for i := range closure {
		record.Steps = append(record.Steps, closure[i].step)
	}
	record.Steps = append(record.Steps, createVirtualMachine(
		ctx, t, expanded, step, opt, manifest.Provenance, identity, networks))
	// Every requirement the bundle declared is reported with what it was
	// mapped onto, or with what the target has to do for it: nothing
	// maps silently, and what is only reported is said too.
	record.Requirements = append(record.Requirements, storage...)
	record.Requirements = append(record.Requirements, networks...)
	record.Requirements = append(
		record.Requirements, reportedContainerDisks(manifest.Requirements.ContainerDisks)...)
	err = notCreated(record)
	return
}

// validateTarget checks what a deploy can check about the target before it
// creates anything, and refuses — naming what it found — when the deploy
// cannot succeed.
//
// The namespace has to be there, because objects created into a namespace
// that was never made land nowhere. And every name a deploy would take has
// to be free — the DataVolumes it will create, the closure objects it will
// apply, the VirtualMachine — because something already holding one is
// someone else's work, which a deploy neither overwrites nor builds
// around. One refusal names every colliding object.
//
// The check is made from the manifest alone, before any member is read:
// what the manifest says the bundle holds is what the deploy would create,
// and a member that later fails its digest can only shrink that set, never
// grow it.
func validateTarget(
	ctx context.Context,
	t cluster.Target,
	opt Options,
	manifest *bundle.Manifest,
	closure []plannedClosure,
	disks []plannedDisk) (err error) {
	//
	_, err = t.GetNamespace(ctx, opt.Namespace)
	if errors.Is(err, cluster.ErrNotFound) {
		err = &MissingNamespaceError{Namespace: opt.Namespace}
		return
	}
	if err != nil {
		return
	}
	var taken []cluster.ObjectRef
	for _, ref := range wouldCreate(opt, manifest, closure, disks) {
		_, err = t.GetObject(ctx, ref)
		if errors.Is(err, cluster.ErrNotFound) {
			// A name that is free is what a deploy went looking for.
			err = nil
			continue
		}
		if err != nil {
			return
		}
		taken = append(taken, ref)
	}
	if len(taken) > 0 {
		err = &AlreadyExistsError{Objects: taken}
	}
	return
}

// wouldCreate returns every object a deploy would put onto the target, in
// the order it would create them: the DataVolumes for the disks it will
// place, the closure objects it will apply, and the VirtualMachine. A disk
// planned as skipped is not among them, because it creates nothing.
func wouldCreate(
	opt Options,
	manifest *bundle.Manifest,
	closure []plannedClosure,
	disks []plannedDisk) (refs []cluster.ObjectRef) {
	//
	for i := range disks {
		if disks[i].step.Outcome == OutcomeSkipped {
			continue
		}
		refs = append(refs, cluster.ObjectRef{
			Kind:      cluster.KindDataVolume,
			Namespace: opt.Namespace,
			Name:      disks[i].step.Name,
		})
	}
	for i := range closure {
		refs = append(refs, cluster.ObjectRef{
			Kind:      closure[i].step.Kind,
			Namespace: opt.Namespace,
			Name:      closure[i].step.Name,
		})
	}
	refs = append(refs, cluster.ObjectRef{
		Kind:      cluster.KindVirtualMachine,
		Namespace: opt.Namespace,
		Name:      virtualMachineName(opt, manifest),
	})
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
// the same object. It is recorded whatever became of it. A dry run creates
// nothing and still says the machine as created, which is what a real
// deploy would do with it.
//
// networks is what resolveNetworks decided for the machine's multus
// networks; the applied VirtualMachine carries the mappings.
func createVirtualMachine(
	ctx context.Context,
	t cluster.Target,
	expanded *unstructured.Unstructured,
	step Step,
	opt Options,
	provenance bundle.Provenance,
	identity string,
	networks []Requirement) (finished Step) {
	//
	if opt.DryRun {
		// Nothing is created. The step still says created, because the
		// record a dry run returns is the record a real deploy would
		// print.
		step.Outcome = OutcomeCreated
		return step
	}
	vm := buildVirtualMachine(expanded, opt, provenance, identity, networks)
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
	for i := range record.Steps {
		if record.Steps[i].Outcome != OutcomeCreated {
			missing++
		}
	}
	if missing > 0 {
		err = &NotCreatedError{NotCreated: missing, Attempted: len(record.Steps)}
	}
	return
}
