# 02: Capture disks through VirtualMachineExport

**What to build:** The bundle now contains every disk attached to the
VirtualMachine. A support engineer who receives it has the machine's actual
state, not just its definition.

Disks come through the VirtualMachineExport API rather than any helper pod quo
builds. They are staged one at a time so that capturing a multi-disk
VirtualMachine never needs local space for all of it at once.

**Blocked by:** 01 (needs the bundle writer, manifest and cluster interface).

**Status:** ready-for-agent

- [x] Creates a VirtualMachineExport referencing the VirtualMachine with a token
      secret, waits for it to become ready, and reads the per-volume links from
      its status
- [x] Sets a time-to-live well beyond the two-hour default, so a large transfer
      does not expire near the end
- [x] Requests the **compressed** format, so a sparsely used volume transfers at
      roughly its used size rather than its provisioned size
- [x] Uses port forwarding where no external route exists, so no ingress is
      required on the cluster
- [x] Downloads one disk at a time to a temporary file, writes it into the
      bundle, and deletes it before starting the next — peak scratch is the
      largest single compressed disk, not the total
- [x] Stores the bytes received verbatim; quo performs no compression or
      recompression of its own
- [x] Disk members are keyed by PersistentVolumeClaim, not by volume name
- [x] Two volumes referencing the same claim produce **one** member, downloaded
      once, with the manifest recording both volume names against it
- [x] The manifest maps volume names onto disk members as a list, and records
      each member's digest and size
- [x] The VirtualMachineExport is cleaned up when the capture finishes or fails
- [x] Transfer progress is reported, so a multi-hour capture is distinguishable
      from a hung one
- [x] `quo inspect` lists the disks and the volumes each serves
- [x] Tests supply canned disk bytes through the fake and assert on member
      naming, the shared-claim case, digests and manifest mappings

## Comments

Implemented. `quo export` now captures disks, and `quo inspect` lists them
with the volumes each serves.

### Two requirements that pull against each other

Ticket 01 fixed the manifest as the strictly first member of the bundle, and
it records a size and digest for every other member. This ticket asks for
peak scratch of one compressed disk. A disk's compressed size and digest are
only known once it has arrived, so those two cannot both hold while the
bundle is written front to back in one pass: either every disk waits on local
disk until the manifest can be written, or the manifest moves after the
disks.

Neither was acceptable, so the writer reserves room for the manifest at the
front of the bundle, streams the members past it, and writes the real
manifest into that room at the end. The reservation is sized from a manifest
holding the widest value each unknown field can take, so the real one always
fits, and the remainder is padded with newlines that parse away. A bundle
still opens with `tar` and still answers from its first few kilobytes.

The cost is that `capture.Export` now takes an `io.WriteSeeker` rather than
an `io.Writer`. The CLI already wrote to a file; tests capture into one.

### The cluster interface changed after all

Ticket 01 left `OpenDisk(ctx, namespace, claim)` on `cluster.Cluster` and
called its shape settled. It was not: the export has a lifecycle — create,
wait, read links, clean up — and all of it belongs behind the interface, as
the spec says. `OpenDisk` is now a method on a `cluster.DiskExport` returned
by `Cluster.ExportDisks`, which the fake serves as before. Nothing needs an
HTTP server.

### Choices worth knowing about

- A claim the VirtualMachine references but the export does not offer fails
  the capture rather than being recorded and skipped. A bundle quietly
  missing a disk is worse than no bundle. When the capture record exists this
  may want revisiting.
- Cleaning up the export is best effort. An export that outlives a capture
  expires on its own, and failing a transfer that took hours over a delete
  that did not land would be the wrong trade.
- Volumes resolve to a claim through `persistentVolumeClaim`, `dataVolume`
  and `memoryDump`. An `ephemeral` volume also has a claim behind it, but
  KubeVirt's export does not offer those, and treating one as a disk would
  turn a capture that works today into a hard failure.

### Inherited by a later ticket

- `bundle.Read` still verifies every member before returning, so `quo
  inspect` hashes every disk in the bundle. Ticket 01 raised this; it is left
  alone here because ticket 01 also asked `inspect` to report digest
  mismatches, and separating describing from verifying changes that
  behaviour. It wants a ticket of its own.
- Nothing in `pkg/cluster` is covered by the main suite, by design — it is
  the one part the fake bypasses. The VirtualMachineExport flow, the choice
  of compressed format, and the port forward to the export server's pod have
  been written against KubeVirt's API but never run against a cluster.
  Ticket 08 is where they get exercised.

Status left at `ready-for-agent`, as ticket 01 did: the tracker has no label
meaning "implemented".
