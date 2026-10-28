package deploy

import (
	"context"
	"errors"
	"io"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// progressInterval is how often an upload in progress is reported.
const progressInterval = 2 * time.Second

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
// whose requirement records no size, or no storage class, creates nothing
// either: quo will not guess at what the target should provide, because a
// disk placed by guesswork is not the disk the machine ran on. Both gaps
// are recorded, so the run says what it left out.
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
		case disk.required.StorageClass == "":
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
// through its upload. The storage class a disk was provisioned from has to
// be there under the same name, and the target's own account of that
// class — its StorageProfile — must not rule out the volume mode the disk
// needs. A profile that says nothing about the class is not a refusal:
// CDI falls back to its own defaults when the claim is created, and the
// create is what answers.
func resolveDisks(ctx context.Context, t cluster.Target, disks []plannedDisk) (err error) {
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
	checked := map[string]bool{}
	for i := range disks {
		if disks[i].step.Outcome == OutcomeSkipped {
			continue
		}
		required := disks[i].required
		_, err = t.GetObject(ctx, cluster.ObjectRef{
			Kind: cluster.KindStorageClass,
			Name: required.StorageClass,
		})
		if errors.Is(err, cluster.ErrNotFound) {
			err = &MissingStorageClassError{
				Claim:        required.Claim,
				StorageClass: required.StorageClass,
			}
			return
		}
		if err != nil {
			return
		}
		if required.VolumeMode == "" || checked[required.StorageClass] {
			continue
		}
		checked[required.StorageClass] = true
		err = checkVolumeMode(ctx, t, disks[i])
		if err != nil {
			return
		}
	}
	return
}

// checkVolumeMode refuses a disk whose volume mode the target's own
// account of its storage class rules out.
func checkVolumeMode(ctx context.Context, t cluster.Target, disk plannedDisk) (err error) {
	profile, readErr := t.GetObject(ctx, cluster.ObjectRef{
		Kind: cluster.KindStorageProfile,
		Name: disk.required.StorageClass,
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
			StorageClass: disk.required.StorageClass,
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
		dataVolume := buildDataVolume(disks[i].held, disks[i].required, opt, identity)
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
