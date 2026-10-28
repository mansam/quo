package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// progressInterval is how often an upload in progress is reported.
const progressInterval = 2 * time.Second

// catchAllClass is the --storage-class key that covers every storage
// class that does not match by name — the classes the target does not
// hold under the requirement's own name, and the unnamed class, which
// names nothing to match.
const catchAllClass = "*"

// plannedDisk is one of the VirtualMachine's disks as a deploy goes about
// putting it back: what the bundle holds of it, what the bundle says it
// needs from storage, and the deploy record's entry for it.
type plannedDisk struct {
	// held is the disk member the bundle carries, zero when it carries
	// none.
	held bundle.Disk
	// required is what the disk needs from the target's storage. Every
	// field but the claim is empty when the capture could not read the
	// claim behind it.
	required bundle.StorageRequirement
	// storageClass is the target's class the disk lands on: the class of
	// the requirement's own name when the target holds one, or the class
	// a mapping places it on.
	storageClass string
	// step is the deploy record's entry, which the phases fill in as the
	// disk proceeds.
	step Step
}

// planDisks pairs each storage requirement the bundle declares with the
// disk member it names, and records up front the disks that create
// nothing.
//
// A requirement with no member — the claim was forbidden, or never existed
// — creates nothing, because there are no bytes to put anywhere. A member
// whose requirement records no size creates nothing either: quo will not
// guess at what the target should provide, because a disk placed by
// guesswork is not the disk the machine ran on. A requirement that records
// no class is placed by the catch-all mapping when one is given, and
// creates nothing without one, for the same reason. Every gap is
// recorded, so the run says what it left out.
func planDisks(manifest *bundle.Manifest, opt Options) (disks []plannedDisk) {
	//
	heldByClaim := make(map[string]bundle.Disk, len(manifest.Disks))
	for i := range manifest.Disks {
		heldByClaim[manifest.Disks[i].Claim] = manifest.Disks[i]
	}
	planned := make(map[string]bool, len(manifest.Requirements.Storage))
	for i := range manifest.Requirements.Storage {
		required := manifest.Requirements.Storage[i]
		planned[required.Claim] = true
		disk := plannedDisk{
			held:     heldByClaim[required.Claim],
			required: required,
			step: Step{
				Kind:      cluster.KindDataVolume,
				Namespace: opt.Namespace,
				Name:      required.Claim,
			},
		}
		switch {
		case disk.held.Path == "":
			disk.step.Outcome = OutcomeSkipped
			disk.step.Reason = "the bundle holds no bytes for it"
		case disk.required.Size == "":
			disk.step.Outcome = OutcomeSkipped
			disk.step.Reason = "the bundle records no size for it"
		case disk.required.StorageClass == "" && opt.StorageClasses[catchAllClass] == "":
			// A class recorded nowhere can be placed only by the
			// catch-all. Without it quo will not guess: a disk placed by
			// guesswork is not the disk the machine ran on.
			disk.step.Outcome = OutcomeSkipped
			disk.step.Reason = "the bundle records no storage class for it"
		}
		disks = append(disks, disk)
	}
	for i := range manifest.Disks {
		if planned[manifest.Disks[i].Claim] {
			continue
		}
		// A member with no requirement is a bundle no capture of quo
		// produces, because a capture records one for every claim it
		// holds a disk of. What it means is the same as a requirement
		// that could say nothing: there is nothing to place the disk by.
		disks = append(disks, plannedDisk{
			held: manifest.Disks[i],
			step: Step{
				Kind:      cluster.KindDataVolume,
				Namespace: opt.Namespace,
				Name:      manifest.Disks[i].Claim,
				Outcome:   OutcomeSkipped,
				Reason:    "the bundle records no storage requirement for it",
			},
		})
	}
	return
}

// resolveDisks checks what a deploy can check about the target before it
// creates anything, and refuses — naming what it found — when a disk
// cannot be placed on it.
//
// CDI has to be there, because a disk goes back onto a cluster only
// through its upload. Each class the bundle records is placed once and
// reported, because nothing maps silently, and the target's own account
// of the class the disks land on — its StorageProfile — must not rule out
// the volume mode they need. A profile that says nothing about the class
// is not a refusal: CDI falls back to its own defaults when the claim is
// created, and the create is what answers.
func resolveDisks(
	ctx context.Context,
	t cluster.Target,
	disks []plannedDisk,
	opt Options) (requirements []Requirement, err error) {
	//
	toUpload := 0
	for i := range disks {
		if disks[i].step.Outcome == OutcomeSkipped {
			continue
		}
		toUpload++
	}
	if toUpload == 0 {
		return
	}
	_, err = t.GetObject(ctx, cluster.ObjectRef{
		Kind: cluster.KindCDIConfig,
		Name: cluster.CDIConfigName,
	})
	if errors.Is(err, cluster.ErrNotFound) || errors.Is(err, cluster.ErrNotInstalled) {
		err = &NoCDIError{Disks: toUpload}
		return
	}
	if err != nil {
		return
	}
	placed := map[string]Requirement{}
	checked := map[string]bool{}
	for i := range disks {
		if disks[i].step.Outcome == OutcomeSkipped {
			continue
		}
		source := disks[i].required.StorageClass
		mapping, found := placed[source]
		if !found {
			mapping, err = placeStorageClass(ctx, t, source, opt)
			if err != nil {
				return
			}
			placed[source] = mapping
			requirements = append(requirements, mapping)
		}
		disks[i].storageClass = mapping.To
		if disks[i].required.VolumeMode == "" || checked[disks[i].storageClass] {
			continue
		}
		checked[disks[i].storageClass] = true
		err = checkVolumeMode(ctx, t, disks[i])
		if err != nil {
			return
		}
	}
	return
}

// placeStorageClass says where the disks of one of the bundle's storage
// classes land, as the requirement the deploy record holds for the class:
// what the bundle asked for, the target's class its disks land on, and how
// the choice was made. An explicit --storage-class overrides whatever the
// target holds under the requirement's own name; short of that, the
// target's class of that name is the mapping a deploy makes on its own,
// and the catch-all covers what does not match by name — a class the
// target does not hold, and the unnamed class, which names nothing to
// match. A class that resolves to nothing is a refusal, because the disks
// are not uploaded that cannot be placed.
func placeStorageClass(
	ctx context.Context,
	t cluster.Target,
	source string,
	opt Options) (mapping Requirement, err error) {
	//
	mapping.Kind = "storage class"
	mapping.From = source
	if named, found := opt.StorageClasses[source]; found {
		// The command line said where these disks go, which overrides
		// whatever the target holds under the requirement's own name.
		mapping.To, mapping.How = named, "by --storage-class"
		err = verifyStorageClass(ctx, t, source, mapping.To,
			fmt.Sprintf("--storage-class %s=%s", source, named))
		return
	}
	var held bool
	if source != "" {
		held, err = holds(ctx, t, cluster.ObjectRef{
			Kind: cluster.KindStorageClass,
			Name: source,
		})
		if err != nil {
			return
		}
	}
	if held {
		// The target's class of the requirement's own name, and the read
		// was the check that it is there.
		mapping.To, mapping.How = source, "by name"
		return
	}
	catchAll, found := opt.StorageClasses[catchAllClass]
	if !found {
		err = &UnresolvedRequirementError{
			Kind:        "storage class",
			Requirement: source,
			Tried: fmt.Sprintf(
				"no storage class of that name is there and no mapping names another; "+
					"place it with --storage-class %s=<target>, or every class that does not match by name with --storage-class *=<target>",
				source),
		}
		return
	}
	mapping.To, mapping.How = catchAll, "by --storage-class *"
	err = verifyStorageClass(ctx, t, source, mapping.To,
		fmt.Sprintf("--storage-class *=%s", catchAll))
	return
}

// verifyStorageClass refuses a mapping that names a storage class the
// target does not hold, because the disks land on the class it names.
// named says the mapping as the command line gives it.
func verifyStorageClass(
	ctx context.Context,
	t cluster.Target,
	source, target, named string) (err error) {
	//
	held, err := holds(ctx, t, cluster.ObjectRef{
		Kind: cluster.KindStorageClass,
		Name: target,
	})
	if err != nil {
		return
	}
	if held {
		return
	}
	err = &UnresolvedRequirementError{
		Kind:        "storage class",
		Requirement: source,
		Tried: fmt.Sprintf(
			"the mapping %s names a class the target does not have", named),
	}
	return
}

// holds says whether the target cluster holds the object the reference
// names. A kind the target has not installed holds nothing, the same as
// a name it has no object under: both leave a mapping nothing to point
// at, and only a read that failed for another reason is an error.
func holds(ctx context.Context, t cluster.Target, ref cluster.ObjectRef) (held bool, err error) {
	_, err = t.GetObject(ctx, ref)
	if errors.Is(err, cluster.ErrNotFound) || errors.Is(err, cluster.ErrNotInstalled) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	held = true
	return
}

// checkVolumeMode refuses a disk whose volume mode the target's own
// account of the class it was mapped onto rules out.
func checkVolumeMode(ctx context.Context, t cluster.Target, disk plannedDisk) (err error) {
	profile, readErr := t.GetObject(ctx, cluster.ObjectRef{
		Kind: cluster.KindStorageProfile,
		Name: disk.storageClass,
	})
	if readErr != nil {
		// A profile that cannot be read says nothing. The refusal this
		// package makes is for a class that says the disk cannot go
		// there, not for one quo could not ask.
		return nil
	}
	sets, _, _ := unstructured.NestedSlice(profile.Object, "status", "claimPropertySets")
	offered := false
	for i := range sets {
		set, cast := sets[i].(map[string]any)
		if !cast {
			continue
		}
		mode, _, _ := unstructured.NestedString(set, "volumeMode")
		if mode == disk.required.VolumeMode {
			offered = true
			break
		}
	}
	if len(sets) > 0 && !offered {
		err = &UnsupportedVolumeModeError{
			Claim:        disk.required.Claim,
			StorageClass: disk.storageClass,
			VolumeMode:   disk.required.VolumeMode,
		}
	}
	return
}

// createDataVolumes puts one DataVolume onto the target for each disk the
// bundle holds that can be placed, named after the claim it backs. They
// are created before anything uploads and before the VirtualMachine, so
// that nothing in the namespace looks like a VirtualMachine until its
// disks are in place.
func createDataVolumes(
	ctx context.Context,
	t cluster.Target,
	disks []plannedDisk,
	opt Options,
	identity string) {
	//
	for i := range disks {
		if disks[i].step.Outcome == OutcomeSkipped {
			// A disk that creates nothing was decided when it was
			// planned; the record already says why.
			continue
		}
		dataVolume := buildDataVolume(disks[i], opt, identity)
		err := t.Create(ctx, dataVolume)
		if err != nil {
			disks[i].step.Outcome = OutcomeFailed
			disks[i].step.Reason = err.Error()
			continue
		}
		disks[i].step.Outcome = OutcomeCreated
	}
}

// uploadDisks streams each held disk's bytes into its DataVolume, one at a
// time, and finishes the deploy record's entry for it. A disk whose
// DataVolume was not created is not uploaded, and a disk that fails does
// not stop the ones behind it.
func uploadDisks(
	ctx context.Context,
	t cluster.Target,
	reader *bundle.Reader,
	disks []plannedDisk,
	opt Options) {
	//
	for i := range disks {
		if disks[i].step.Outcome != OutcomeCreated {
			continue
		}
		upload(ctx, t, reader, disks, i, opt)
	}
}

// upload streams one disk's member into its DataVolume through the
// progress reporter, and records what became of it: created only when the
// bytes the bundle holds arrived whole, skipped when they were not the
// bytes the manifest recorded, failed when the upload itself failed.
func upload(
	ctx context.Context,
	t cluster.Target,
	reader *bundle.Reader,
	disks []plannedDisk,
	at int,
	opt Options) {
	//
	disk := &disks[at]
	report := newReporter(opt, disks, at)
	stream, err := reader.OpenStream(disk.held.Path)
	if err != nil {
		// The member is indexed but the bundle does not carry it as far
		// as this reader has read, which is what a transfer that stopped
		// looks like. Nothing was uploaded from it.
		disk.step.Outcome = OutcomeSkipped
		disk.step.Reason = err.Error()
		return
	}
	report(0, false)
	sent := &progressReader{in: stream, report: report, reported: time.Now()}
	err = t.UploadDisk(ctx, opt.Namespace, disk.held.Claim, sent, stream.Size())
	if mismatch := stream.Mismatch(); mismatch != nil {
		// The bytes that left the bundle were not the bytes the manifest
		// recorded, so what the target holds is not the disk the machine
		// ran on. The step is skipped rather than failed: what was
		// deployed is not what the bundle holds.
		disk.step.Outcome = OutcomeSkipped
		disk.step.Reason = mismatch.Error()
		return
	}
	if err != nil {
		disk.step.Outcome = OutcomeFailed
		disk.step.Reason = "the upload failed: " + err.Error()
		return
	}
	report(stream.Size(), true)
	// The step keeps the outcome createDataVolumes gave it: the
	// DataVolume is created and now holds its bytes.
}

// newReporter returns the function that reports one disk's upload
// progress, which does nothing when the caller asked for no progress.
func newReporter(opt Options, disks []plannedDisk, at int) (report func(sent int64, done bool)) {
	if opt.Progress == nil {
		report = func(sent int64, done bool) {}
		return
	}
	p := Progress{
		Claim:   disks[at].held.Claim,
		Volumes: disks[at].held.Volumes,
		Disk:    at + 1,
		Disks:   len(disks),
	}
	report = func(sent int64, done bool) {
		p.Transferred = sent
		p.Done = done
		opt.Progress(p)
	}
	return
}

// progressReader reports how much of a disk has been sent while it is
// being read. Reports are spaced out in time, because a disk is read in
// small pieces and a report per piece would be too many. The reader that
// wraps the member starts the clock, so a disk that arrives quickly is
// reported only as started and as finished.
type progressReader struct {
	in       io.Reader
	report   func(sent int64, done bool)
	sent     int64
	reported time.Time
}

// Read passes the read through and reports what has been sent so far.
func (r *progressReader) Read(b []byte) (n int, err error) {
	n, err = r.in.Read(b)
	r.sent += int64(n)
	if time.Since(r.reported) < progressInterval {
		return
	}
	r.reported = time.Now()
	r.report(r.sent, false)
	return
}
