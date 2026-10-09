# 02: Capture disks through VirtualMachineExport

**What to build:** The bundle now contains every disk attached to the
VirtualMachine. A support engineer who receives it has the machine's actual
state, not just its definition.

Disks come through the VirtualMachineExport API rather than any helper pod quo
builds. They are staged one at a time so that capturing a multi-disk
VirtualMachine never needs local space for all of it at once.

**Blocked by:** 01 (needs the bundle writer, manifest and cluster interface).

**Status:** ready-for-agent

- [ ] Creates a VirtualMachineExport referencing the VirtualMachine with a token
      secret, waits for it to become ready, and reads the per-volume links from
      its status
- [ ] Sets a time-to-live well beyond the two-hour default, so a large transfer
      does not expire near the end
- [ ] Requests the **compressed** format, so a sparsely used volume transfers at
      roughly its used size rather than its provisioned size
- [ ] Uses port forwarding where no external route exists, so no ingress is
      required on the cluster
- [ ] Downloads one disk at a time to a temporary file, writes it into the
      bundle, and deletes it before starting the next — peak scratch is the
      largest single compressed disk, not the total
- [ ] Stores the bytes received verbatim; quo performs no compression or
      recompression of its own
- [ ] Disk members are keyed by PersistentVolumeClaim, not by volume name
- [ ] Two volumes referencing the same claim produce **one** member, downloaded
      once, with the manifest recording both volume names against it
- [ ] The manifest maps volume names onto disk members as a list, and records
      each member's digest and size
- [ ] The VirtualMachineExport is cleaned up when the capture finishes or fails
- [ ] Transfer progress is reported, so a multi-hour capture is distinguishable
      from a hung one
- [ ] `quo inspect` lists the disks and the volumes each serves
- [ ] Tests supply canned disk bytes through the fake and assert on member
      naming, the shared-claim case, digests and manifest mappings
