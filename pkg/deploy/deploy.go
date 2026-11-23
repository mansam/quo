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

// Deploy creates a stopped VirtualMachine and its resources on the target.
// The record includes object outcomes, requirement mappings, unused mappings,
// and container disk images. Deploy does not try to pull those images.
//
// Before creating objects, Deploy reads the manifest, decodes the related
// resources and expanded spec, and checks the namespace, object names,
// storage, and network mappings. Use quo inspect to verify member digests.
//
// Deploy creates DataVolumes, attempts disk uploads, creates related resources,
// and creates the VirtualMachine last. After the preflight checks, failures
// are recorded and subsequent steps still run. A partial deploy returns a
// non-nil error. Deploy does not roll back, even if the context is canceled.
//
// Options.DryRun runs the preflight checks and returns the planned record
// without writing to the target or uploading disks.
func Deploy(ctx context.Context, t cluster.Target, opt Options) (record Record, err error) {
	//
	if opt.DryRun {
		t = &dryRunTarget{wrapped: t}
	}
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
	// The reader moves forward only: read resources and the expanded spec
	// in tar order and hold them until after the disk uploads.
	err = readClosure(reader, closure)
	if err != nil {
		return
	}
	expanded, err := readVirtualMachine(reader, manifest, opt)
	if err != nil {
		return
	}
	storage, err := placeDisks(ctx, t, disks, opt)
	if err != nil {
		return
	}
	networks, err := attachNetworks(ctx, t, manifest.Requirements.Networks, opt)
	if err != nil {
		return
	}
	identity := bundleIdentity(reader.ManifestBytes())
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
		ctx, t, expanded, opt, manifest, identity, networks))
	record.Requirements = append(record.Requirements, storage...)
	record.Requirements = append(record.Requirements, networks...)
	record.Requirements = append(
		record.Requirements, reportedContainerDisks(manifest.Requirements.ContainerDisks)...)
	record.Requirements = append(record.Requirements, unmatchedMappings(opt, manifest)...)
	err = notCreated(record, opt.DryRun)
	return
}

// validateTarget checks that the namespace exists and reports all planned
// objects whose names are already taken. It uses the manifest before
// reading object or disk members.
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
// the order it would create them. The objects are the DataVolumes for the
// disks it will place, the closure objects it will apply, and the
// VirtualMachine. A disk planned as skipped is not among them, because it
// creates nothing.
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

// readVirtualMachine reads and decodes the bundle's expanded spec.
func readVirtualMachine(
	reader *bundle.Reader,
	manifest *bundle.Manifest,
	opt Options) (expanded *unstructured.Unstructured, err error) {
	//
	content, err := reader.Read(manifest.ExpandedSpec)
	if err != nil {
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

// createVirtualMachine creates the VirtualMachine from the expanded spec
// with the forced rewrites and records the result.
func createVirtualMachine(
	ctx context.Context,
	t cluster.Target,
	expanded *unstructured.Unstructured,
	opt Options,
	manifest *bundle.Manifest,
	identity string,
	networks []Requirement) (finished Step) {
	//
	step := Step{
		Kind:      cluster.KindVirtualMachine,
		Namespace: opt.Namespace,
		Name:      virtualMachineName(opt, manifest),
	}
	vm := buildVirtualMachine(expanded, opt, manifest.Provenance, identity, networks)
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
// what became of it. This error is what makes the exit code non-zero, so
// that a script can tell a complete deploy from a partial one.
func notCreated(record Record, dryRun bool) (err error) {
	missing := 0
	for i := range record.Steps {
		if record.Steps[i].Outcome != OutcomeCreated {
			missing++
		}
	}
	if missing > 0 {
		err = &NotCreatedError{NotCreated: missing, Attempted: len(record.Steps), DryRun: dryRun}
	}
	return
}
