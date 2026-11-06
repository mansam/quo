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

// progressInterval is how often the run reports an upload in progress.
const progressInterval = 2 * time.Second

// catchAllClass is the --storage-class key that covers every storage
// class that does not match by name. Those are the classes the target
// does not hold under the requirement's own name, and the unnamed class,
// which names nothing to match.
const catchAllClass = "*"

// plannedDisk is one of the VirtualMachine's disks as a deploy returns it
// to a cluster. It holds what the bundle holds of the disk, what the
// bundle says it needs from storage, and the deploy record's entry for
// it.
type plannedDisk struct {
	// held is the disk member the bundle carries, zero when it carries
	// none.
	held bundle.Disk
	// required is what the disk needs from the target's storage. Every
	// field but the claim is empty when the capture could not read the
	// claim behind it.
	required bundle.StorageRequirement
	// storageClass is the target's class that receives the disk. It is
	// the class of the requirement's own name when the target holds one,
	// or the class a mapping places the disk on.
	storageClass string
	// step is the deploy record's entry, which the phases update as the
	// disk proceeds.
	step Step
}

// planDisks pairs each storage requirement the bundle declares with the
// disk member it names. It records in advance the disks that create
// nothing.
//
// A requirement with no member creates nothing, because there are no
// bytes to put anywhere. The claim was forbidden, or never existed. A
// member whose requirement records no size creates nothing either. quo
// will not guess at what the target should provide, because a disk placed
// by guesswork is not the disk the machine ran on.
//
// A requirement that records no class is placed by the catch-all mapping
// when one is given, and creates nothing without one, for the same
// reason. Every gap is recorded, so the run says what it skipped.
func planDisks(manifest *bundle.Manifest, opt Options) (disks []plannedDisk) {
	//
	heldByClaim := make(map[string]bundle.Disk, len(manifest.Disks))
	for i := range manifest.Disks {
		heldByClaim[manifest.Disks[i].Claim] = manifest.Disks[i]
	}
	plannedByClaim := make(map[string]bool, len(manifest.Requirements.Storage))
	for i := range manifest.Requirements.Storage {
		required := manifest.Requirements.Storage[i]
		plannedByClaim[required.Claim] = true
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
		if plannedByClaim[manifest.Disks[i].Claim] {
			continue
		}
		// A member with no requirement is a bundle no capture of quo
		// produces. A capture records one requirement for every claim it
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

// placeDisks places the bundle's disks on the target's storage classes,
// and refuses, naming what it found, when a disk cannot be placed on the
// target.
//
// CDI has to be there, because a disk returns to a cluster only through
// its upload. Each class the bundle records is placed once and reported,
// because nothing maps silently. The target keeps its own account of the
// class the disks are placed on, its StorageProfile. That account must
// not rule out the volume mode the disks need. A profile that says
// nothing about the class is not a refusal. CDI uses its own defaults
// when the claim is created, and the create is what answers.
func placeDisks(
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
	placedByClass := map[string]Requirement{}
	checkedByClass := map[string]bool{}
	for i := range disks {
		if disks[i].step.Outcome == OutcomeSkipped {
			continue
		}
		source := disks[i].required.StorageClass
		mapping, found := placedByClass[source]
		if !found {
			mapping, err = placeStorageClass(ctx, t, source, opt)
			if err != nil {
				return
			}
			placedByClass[source] = mapping
			requirements = append(requirements, mapping)
		}
		disks[i].storageClass = mapping.To
		if disks[i].required.VolumeMode == "" || checkedByClass[disks[i].storageClass] {
			continue
		}
		checkedByClass[disks[i].storageClass] = true
		err = checkVolumeMode(ctx, t, disks[i])
		if err != nil {
			return
		}
	}
	return
}

// placeStorageClass says where the disks of one of the bundle's storage
// classes go, as the requirement the deploy record holds for the class.
// The requirement says what the bundle asked for, the target's class that
// receives the disks, and how the choice was made.
//
// An explicit --storage-class overrides whatever the target holds under
// the requirement's own name. Short of that, the target's class of that
// name is the mapping a deploy makes on its own. The catch-all covers
// what does not match by name: a class the target does not hold, and the
// unnamed class, which names nothing to match.
//
// A class that maps to nothing is a refusal, because the disks are not
// uploaded that cannot be placed.
func placeStorageClass(
	ctx context.Context,
	t cluster.Target,
	source string,
	opt Options) (mapping Requirement, err error) {
	//
	mapping.Kind = KindStorageClass
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
		err = &UnmappedRequirementError{
			Kind:        KindStorageClass,
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
// target does not hold, because the class it names receives the disks.
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
	err = &UnmappedRequirementError{
		Kind:        KindStorageClass,
		Requirement: source,
		Tried: fmt.Sprintf(
			"the mapping %s names a class the target does not have", named),
	}
	return
}

// holds says whether the target cluster holds the object the reference
// names. A kind the target has not installed holds nothing, the same as
// a name it has no object under. Both leave a mapping with nothing to
// point at. Only a read that failed for another reason is an error.
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
// bundle holds that can be placed, named after the claim it backs. The
// DataVolumes are created before anything uploads and before the
// VirtualMachine. Nothing in the namespace then looks like a
// VirtualMachine until its disks are in place.
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
			// planned. The record already says why.
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

// uploadDisks streams each held disk's bytes into its DataVolume, one at
// a time, and finishes the deploy record's entry for it. A disk whose
// DataVolume was not created is not uploaded. A disk that fails does not
// stop the ones behind it.
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

// upload streams one disk's member into its DataVolume, and records
// what became of it. The outcome is created only when the bytes the
// bundle holds arrived whole. The outcome is skipped when they were
// not the bytes the manifest recorded. The outcome is failed when the
// upload itself failed.
//
// Every progress report follows something that happened rather than
// something the code assumed. The target reports what it waits on,
// because the target is what waits. The reader the bytes pass through
// reports how many have passed, because it is what sees them.
func upload(
	ctx context.Context,
	t cluster.Target,
	reader *bundle.Reader,
	disks []plannedDisk,
	at int,
	opt Options) {
	//
	disk := &disks[at]
	stream, err := reader.OpenStream(disk.held.Path)
	if err != nil {
		// The member is indexed but the bundle does not carry it as far
		// as this reader has read. That is what a transfer that stopped
		// looks like. Nothing was uploaded from it.
		disk.step.Outcome = OutcomeSkipped
		disk.step.Reason = err.Error()
		return
	}
	report, waiting := newReporter(opt, disks, at)
	sent := &progressReader{in: stream, report: report, reported: time.Now()}
	err = t.UploadDisk(ctx, cluster.Upload{
		Namespace: opt.Namespace,
		Name:      disk.held.Claim,
		Content:   sent,
		Size:      stream.Size(),
		Waiting:   waiting,
	})
	if mismatch := stream.Mismatch(); mismatch != nil {
		// The bytes that left the bundle were not the bytes the manifest
		// recorded. What the target holds is therefore not the disk the
		// machine ran on. The step is skipped rather than failed: what
		// was deployed is not what the bundle holds.
		disk.step.Outcome = OutcomeSkipped
		disk.step.Reason = mismatch.Error()
		return
	}
	if err != nil {
		disk.step.Outcome = OutcomeFailed
		disk.step.Reason = "the upload failed: " + err.Error()
		return
	}
	// The step keeps the outcome createDataVolumes gave it: the
	// DataVolume is created and now holds its bytes.
}

// newReporter returns the functions that report one disk's upload
// progress: report says the bytes moving through the reader, and waiting
// says what the target waits on before they move. Both do nothing when
// the caller asked for no progress.
func newReporter(
	opt Options,
	disks []plannedDisk,
	at int) (report func(state State, sent int64), waiting func(said string)) {
	//
	if opt.Progress == nil {
		report = func(state State, sent int64) {}
		waiting = func(said string) {}
		return
	}
	p := Progress{
		Claim:   disks[at].held.Claim,
		Volumes: disks[at].held.Volumes,
		Disk:    at + 1,
		Disks:   len(disks),
	}
	report = func(state State, sent int64) {
		p.State = state
		p.Transferred = sent
		p.Said = ""
		opt.Progress(p)
	}
	waiting = func(said string) {
		p.State = StateWaiting
		p.Transferred = 0
		p.Said = said
		opt.Progress(p)
	}
	return
}

// progressReader reports how much of a disk is sent while it is read.
// Reports are spaced in time, because a disk is read in small pieces and
// a report per piece would be too many. The reader that wraps the member
// starts the clock, so a disk that arrives quickly is reported only as
// finished. The end of the stream is the end of the transfer, so it is
// reported as done. The bundle's VerifiedStream reports a digest
// mismatch in place of the end of the stream. A member that fails its
// digest is therefore never reported done.
type progressReader struct {
	in       io.Reader
	report   func(state State, sent int64)
	sent     int64
	reported time.Time
}

// Read reads from the stream it wraps and reports how much is sent so
// far.
func (r *progressReader) Read(b []byte) (n int, err error) {
	n, err = r.in.Read(b)
	r.sent += int64(n)
	if errors.Is(err, io.EOF) {
		r.report(StateDone, r.sent)
		return
	}
	if time.Since(r.reported) < progressInterval {
		return
	}
	r.reported = time.Now()
	r.report(StateTransferring, r.sent)
	return
}
