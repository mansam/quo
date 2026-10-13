# 06: Record requirements

**What to build:** The manifest declares what a target cluster would have to
provide for this bundle: the networks it expects, the storage classes it
expects and what each disk needs from them, and the container disk images it
references. A support engineer can then tell, before committing to a large
transfer, whether their cluster can satisfy it.

Nothing in this phase consumes these records. They are written at capture
because the information is free while connected to the source cluster and
expensive to reconstruct afterwards.

**Blocked by:** 03 (needs the traversal to find the networks and claims).

**Status:** ready-for-agent

- [x] The manifest declares the networks the VirtualMachine expects
- [x] The manifest declares the storage classes it expects, with the size and
      volume mode each disk needs, so a candidate can be checked rather than
      guessed at
- [x] The manifest declares container disk images the VirtualMachine references
- [x] Container disk images are recorded as requirements and never repackaged
      into the bundle
- [x] Requirements are derived from the VirtualMachine and its claims, so this
      works whether or not the evidence in ticket 05 could be collected
- [x] Requirements appear in the capture record with the requirement disposition
- [x] `quo inspect` prints the requirements without extracting or downloading
      any disk — reading only the leading manifest
- [x] Tests cover a VirtualMachine with multiple networks, a container disk, and
      disks with differing volume modes

## Comments

Implemented in `pkg/bundle/manifest.go` (the `Requirements` types),
`pkg/capture/requirements.go` (deriving them) and `pkg/bundle/inspect.go`
(printing them).

Four decisions worth recording:

- **`quo inspect` now describes a bundle that has not all arrived.** The
  criterion asks for the requirements to be readable from the leading
  manifest, and `Inspect` used to go through `bundle.Read`, which refuses a
  bundle whose indexed members are not all present — exactly the case a
  reader planning a transfer is in. `Inspect` now reads as far as the bundle
  goes, whether it stops between members or inside one, and names what has
  not arrived. `bundle.Read` is unchanged and still strict, because every
  other caller reads a manifest in order to work with the members it indexes.

- **A requirement can name an object the record already accounts for.** A
  StorageClass appears as evidence because quo read it and again as a
  requirement because a target has to have one. Both entries are still true
  when the read was forbidden, which is the case the requirement record exists
  for. The `CaptureRecord` doc comment says so.
- **Storage requirements are one per disk, not one per storage class.** The
  check a reader makes is per disk — can my cluster provision 32Gi of Block
  from something? — and the set of classes is readable off the entries. A
  disk whose claim could not be read is still declared, with its name alone.
- **Size is the capacity the claim was bound to**, falling back to the one it
  requested. A claim asking for 30Gi can be bound to 32Gi, and a target given
  the smaller of the two has nowhere to put the disk.

Two things deliberately left for later, both worth a ticket of their own:

- **`kernelBoot` container images are not declared.** Requirements come from
  `containerDisk` volumes only. A VirtualMachine can also be given a kernel
  and initrd out of a container image at
  `spec.template.spec.domain.firmware.kernelBoot.container.image`, which is
  an image a target would have to pull. The closure traversal does not follow
  it either, so closing the gap belongs in one change covering both.
- **A network requirement says nothing about how an interface binds to it.**
  SR-IOV and a Multus network declared `default: true` both change what a
  target has to be able to offer, and neither is recorded. This ticket asked
  for the networks and stops there.

The vocabulary in this ticket is superseded. A bundle no longer sorts what it
collects into carried objects and evidence: it holds one set of resources, and
the capture record's outcomes are collected, required and skipped. The source
cluster's expansion of the VirtualMachine is a member of its own rather than a
resource, recorded under its own kind. See
`docs/adr/0002-one-set-of-resources-in-a-bundle.md` and the rewritten sections
of `spec.md`.
