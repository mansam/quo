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

- [ ] Every disk becomes a DataVolume named after the claim it backs,
      with source `upload`
- [ ] The DataVolume's size and volumeMode come from the requirement —
      the bound size, not what was requested
- [ ] The storage class is the target's class of the same name, and a
      disk whose class does not exist on the target refuses the deploy
- [ ] The uploaded bytes are the bundle's, streamed unchanged — gzip and
      tar.gz forms pass through as they are
- [ ] `dataVolumeTemplates` are removed from the applied VirtualMachine
      and become standalone DataVolumes; the volume references that named
      them are satisfied unchanged
- [ ] The VirtualMachine's volume stanzas are unchanged: a volume
      referencing a claim by name finds the claim the DataVolume creates
- [ ] A claim serving two volumes produces one DataVolume serving both
- [ ] Disks upload one at a time through the UploadProxy over a
      port-forward, with the upload-token flow and progress reported
- [ ] quo refuses when CDI is absent and there are disks to upload, and
      when the matched storage class cannot provide a disk's volumeMode
- [ ] A disk the bundle holds no bytes for creates nothing; the gap is
      recorded and the run continues
- [ ] A disk member that fails its digest is skipped as it streams and
      recorded
- [ ] A failed upload is recorded and the next disk proceeds
- [ ] Objects are created in order — the DataVolumes, the uploads, the
      remaining objects, the VirtualMachine last
