# 01: Capture a stopped VirtualMachine into a bundle

**What to build:** A product team engineer runs `quo export -n <namespace> <name>
-o <path>` and gets a bundle file containing the VirtualMachine. They run `quo
inspect <path>` and see what is in it, without needing a cluster. If the
VirtualMachine is running, quo refuses and tells them to stop it.

This is the tracer bullet: it establishes the bundle format, the cluster
interface and its fake, and both commands. No disks and no closure yet — those
are later tickets. The read-back through `inspect` is the round-trip check that
substitutes for the deploy path phase 1 does not have.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] `quo export` writes a tar whose **first** member is the manifest
- [x] The manifest is YAML and records the format version, provenance (source
      cluster, namespace, VirtualMachine name, capture time, KubeVirt and
      Kubernetes versions), and an index of every member with a SHA-256 digest
- [x] The VirtualMachine is stored as YAML exactly as read, with no
      normalisation; MAC addresses, firmware UUID, serial and chassis values are
      untouched
- [x] YAML marshalling goes through the Kubernetes YAML library, so a version
      string is not reinterpreted as a number and a bare `no` does not become
      boolean false
- [x] `quo export` refuses a running VirtualMachine, names it, tells the user to
      stop it, exits non-zero, and writes no bundle
- [x] A capture that fails part-way leaves no bundle file behind
- [x] `quo inspect` prints format version, provenance and members without
      contacting a cluster
- [x] `quo inspect` verifies every member digest and reports a mismatch
- [x] `quo inspect` refuses a bundle whose format version it does not understand
- [x] Everything quo needs from outside the process sits behind a single
      interface, with one fake implementation for tests — including the future
      disk stream, so no test needs an HTTP server
- [x] Tests run through the library entry point against the fake and assert on
      the produced bundle, never on traversal or writer internals
- [x] Tests use gomega, are table-driven with `name` as the first field, and
      have names that read as specifications

## Comments

Implemented. `quo export` and `quo inspect` both work; the bundle round-trips
through `inspect` without a cluster.

Two things a later ticket inherits:

- `cluster.Cluster.OpenDisk` is part of the interface and is served by the
  fake, but the real adapter returns "not implemented". Disk capture is the
  next ticket; the interface shape is settled so nothing has to move.
- `GetInfo` returns an empty KubeVirt or Kubernetes version when it is not
  allowed to read one, rather than failing the capture. Nothing records *why*
  it was empty yet — that is the capture record ticket.

Two more inherited by the disk ticket, found in review:

- `bundle.Writer.Add` takes `[]byte`, so every member is held in memory until
  the bundle is written. Correct for manifest-first ordering and fine while
  members are small objects, but disks cannot be buffered — peak scratch is
  meant to be the largest single compressed disk. Ticket 02 will have to let a
  member be backed by a file path plus a known size, which changes the
  writer's signature rather than just adding a caller.
- `bundle.Read` verifies every member before returning, and treats a member
  that the manifest indexes but the tar lacks as malformed. That is what this
  ticket asked for, but it is at odds with inspecting a partial transfer and
  with reading a bundle's header without hashing hundreds of gigabytes.
  Describing and verifying probably want to be separate operations once disks
  exist.

Status left at `ready-for-agent`: the tracker's five triage labels have no
value meaning "implemented", so inventing one seemed worse than leaving it.
