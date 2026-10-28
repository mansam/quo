# 02: Deploy the disks

**What to build:** The bundle's disks land on the target populated with
the bytes the bundle holds, and the deployed VirtualMachine references
claims that hold real data.

Every disk becomes a DataVolume, whatever it was on the source: a claim
cannot be populated on its own, because CDI's upload works through a
DataVolume. The DataVolume is named after the claim it backs, its source
is `upload`, its size and volumeMode come from the requirement recorded at
capture — the bound size, not what was requested — and its storage class
is the target's class of the same name. The VirtualMachine's volume
stanzas are unchanged: a volume referencing a claim by name finds the
claim the DataVolume creates.

Disks upload one at a time through CDI's UploadProxy, reached over a
port-forward so that no ingress is required — the same arrangement as the
export side — with the upload-token flow and progress reported. The
bundle's bytes are streamed unchanged; the gzip and tar.gz forms the
export produced are the forms CDI's upload accepts. `dataVolumeTemplates`
are removed from the applied VirtualMachine and become standalone
DataVolumes like any other disk.

Objects are created in order — the DataVolumes, then the uploads, then
the remaining objects, then the VirtualMachine last — so that nothing in
the namespace looks like a VirtualMachine until its disks are in place.
quo refuses when CDI is absent and there are disks to upload, and when
the matched storage class cannot provide a disk's volumeMode. A disk the
bundle holds no bytes for creates nothing; the gap is recorded and the
run continues. A failed upload is recorded and the next disk proceeds.

**Blocked by:** 01 (needs the deploy command, the target interface and
the deploy record).

**Status:** ready-for-agent

- [x] Every disk becomes a DataVolume named after the claim it backs,
      with source `upload`
- [x] The DataVolume's size and volumeMode come from the requirement —
      the bound size, not what was requested
- [x] The storage class is the target's class of the same name, and a
      disk whose class does not exist on the target refuses the deploy
- [x] The uploaded bytes are the bundle's, streamed unchanged — gzip and
      tar.gz forms pass through as they are
- [x] `dataVolumeTemplates` are removed from the applied VirtualMachine
      and become standalone DataVolumes; the volume references that named
      them are satisfied unchanged
- [x] The VirtualMachine's volume stanzas are unchanged: a volume
      referencing a claim by name finds the claim the DataVolume creates
- [x] A claim serving two volumes produces one DataVolume serving both
- [x] Disks upload one at a time through the UploadProxy over a
      port-forward, with the upload-token flow and progress reported
- [x] quo refuses when CDI is absent and there are disks to upload, and
      when the matched storage class cannot provide a disk's volumeMode
- [x] A disk the bundle holds no bytes for creates nothing; the gap is
      recorded and the run continues
- [x] A disk member that fails its digest is skipped as it streams and
      recorded
- [x] A failed upload is recorded and the next disk proceeds
- [x] Objects are created in order — the DataVolumes, the uploads, the
      remaining objects, the VirtualMachine last

## Comments

Implemented. `quo deploy <bundle> -n <namespace>` now puts the bundle's
disks back as DataVolumes populated with the bundle's bytes, before the
VirtualMachine is created, and the same runs work through the library
entry point against the fake target.

How the pieces landed:

- The unit a deploy plans by is the storage requirement, not the disk
  member: requirements are keyed by claim, cover every claim the
  machine's volumes reference, and say what a target has to provide.
  A requirement with no member (the gap) or with no size or class is
  planned as a skipped step up front and creates nothing — quo will not
  guess at placement, because a disk placed by guesswork is not the disk
  the machine ran on. A member with no requirement is planned the same
  way, though no capture of quo produces one.
- `bundle.Reader` gained `OpenStream`, the variant ticket 01's
  inheritance note asked for: a member as a `VerifiedStream` that
  hashes as it is read and reports a digest mismatch at the end of the
  stream. `Mismatch` hands the verdict to a caller that gave the stream
  to something else which consumed it, so the skip is recorded without
  depending on the uploader to hand the error back recognisably. Read
  and OpenStream share one walk (`seekTo`); only what is read is
  verified, as before.
- The Target interface gained `UploadDisk(namespace, name, content,
  size)`. The real adapter waits for the DataVolume to reach
  `UploadReady`, finds CDI's upload proxy Service by name across the
  cluster (CDI's namespace is wherever its operator was installed),
  forwards a port to its pod, asks CDI for an upload token through an
  `UploadTokenRequest`, and POSTs the bytes to `/v1beta1/upload` with
  the token as a bearer header and the member's size as the content
  length. The connection is verified against the CA in CDI's
  `cdi-uploadproxy-signer-bundle` ConfigMap where that can be read, and
  carried unverified inside the API-server tunnel where it cannot,
  which is the same trust the port forward itself rests on. The port
  forwarding code was lifted from the export side to serve both.
- The refusals run before anything is created: CDI is looked for
  through the `CDIConfig` singleton (absent or not installed means no
  CDI), each created disk's storage class is read by name, and the
  target's StorageProfile for the class must not rule out the disk's
  volumeMode. A profile that says nothing, or cannot be read, is not a
  refusal — the create is what answers there.
- Each created DataVolume asks CDI to bind its claim at once, through
  `cdi.kubevirt.io/storage.bind.immediate.requested`. This is quo's own
  construction rather than a forced rewrite of a bundle object: the
  machine is deployed stopped, so nothing will ever be the first
  consumer a WaitForFirstConsumer class waits for, and the upload
  cannot start until the claim is bound. virtctl makes the same request
  under `--force-bind`.
- A disk's record step is finished by its upload: `created` only when
  the bytes arrived whole, `skipped` when the member failed its digest
  or never arrived, `failed` when the upload itself failed. The
  expansion is read before the disks — members arrive in written order,
  resources then disks, and the reader walks forward only — and a
  corrupt expansion ends the run after recording the VirtualMachine's
  skipped step, because the disks are not uploaded for a machine that
  cannot be created.
- Skipped steps count towards `NotCreatedError` as ticket 01's rule
  already counted them: a deploy that leaves a gap is a partial one,
  and the exit code says so. The record says why, per disk.

Things ticket 03 inherits:

- The record's steps are appended by phase in `Deploy` (DataVolumes,
  uploads, VirtualMachine); the closure objects slot between the
  uploads and the VirtualMachine, where the order comment in `Deploy`
  already says they go.
- The collision refusal still covers the VirtualMachine alone. A
  pre-existing DataVolume of a claim's name lands as a failed step
  carrying the API server's message, which is honest but unclassified,
  the same TOCTOU shape ticket 01 left open.
- The storage-class refusal matches by name only, as this ticket
  specified. An unnamed class (a claim bound to a volume made by hand)
  is a recorded gap rather than a refusal; ticket 04's catch-all flag
  is what places such disks, and its network mapping rewrites only the
  applied VirtualMachine, which is built after the disks are already
  on the target.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
