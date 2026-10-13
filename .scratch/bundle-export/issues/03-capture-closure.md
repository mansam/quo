# 03: Capture the closure

**What to build:** The bundle now contains the objects the VirtualMachine
depends on, so a support engineer does not have to go back and ask for them one
at a time. quo works out the closure itself rather than calling KubeVirt's
`objectgraph` subresource, because that subresource sits behind an Alpha feature
gate that is off by default. KubeVirt's server-side implementation is the
specification for what to traverse.

**Blocked by:** 01 (needs the bundle writer and the fake cluster).

**Status:** ready-for-agent

- [x] Traverses VirtualMachineInstancetype, VirtualMachineClusterInstancetype,
      VirtualMachinePreference, VirtualMachineClusterPreference, DataVolume,
      ControllerRevision, ConfigMap, PersistentVolumeClaim, ServiceAccount,
      Secret and NetworkAttachmentDefinition
- [x] Follows volumes, access credentials, and instancetype matchers including
      their status references
- [x] An instancetype matcher naming an object that has since been deleted still
      captures the ControllerRevision, which is then the only complete record of
      what the VirtualMachine was created with
- [x] Carried objects are written under the objects grouping as Kubernetes YAML,
      readable and applicable by hand
- [x] Secrets are captured in full, including cloud-init user data, sysprep
      answer files and access credentials
- [x] An object referenced from more than one place appears once in the bundle
- [x] No VirtualMachineInstance and no Pod is sought — a stopped VirtualMachine
      has neither, and looking for them would be an error rather than an absence
- [x] `quo inspect` lists the carried objects by kind and name
- [x] Tests cover each reference type individually, the deleted-matcher case,
      objects referenced twice, and a VirtualMachine with an empty closure

## Comments

Implemented. `quo export` now carries the closure, and `quo inspect` lists
what it carries by kind and name.

### How the traversal is shaped

`cluster.Cluster` gained `GetObject(ctx, ObjectRef)`, the typed-reference read
the spec asked for. The kinds quo reads are constants on the cluster package
and `pkg/cluster/objects.go` maps each onto its API resource and scope, so a
kind quo has no mapping for is reported rather than guessed at.

`buildClosure` is a pure function of the VirtualMachine: everything is on the
VirtualMachine itself, and a DataVolume's claim carries the DataVolume's own
name, so no intermediate object has to be read to find the next reference. The
traversal order is the discovery order, which is what the manifest and
`inspect` then list.

Every field path was checked against `kubevirt.io/api` rather than written
from memory.

### Choices worth knowing about

- **A matcher naming an unrecognised kind is treated as cluster-scoped.**
  KubeVirt's resolver rejects it outright. quo follows KubeVirt's default for
  an empty kind and applies it to anything it does not recognise, so the read
  simply finds nothing. Once the capture record exists that becomes a visible
  "not found" rather than silence.
- **A container disk image names no carried object, but the Secret it is
  pulled with does.** quo does not repackage container images — the image is a
  requirement, ticket 06 — but `imagePullSecret` is a real Secret in the
  namespace and a bundle without it could not pull the disk.
- **`dataVolumeTemplates` are followed as well as volumes.** A templated
  DataVolume is an object in the cluster like any other once the
  VirtualMachine exists, and the ticket asks for DataVolumes.
- **Everything traversed is carried**, including PersistentVolumeClaims,
  DataVolumes and NetworkAttachmentDefinitions. The spec's classification
  sketch reads storage and network dependencies as requirements, but
  requirements in ticket 06 are *derived* records ("derived from the
  VirtualMachine and its claims"), not the objects themselves, and stories 16
  and 17 want these objects readable. Ticket 04 assigns the dispositions and
  is where this should be confirmed or changed.

### Carried objects are not directly re-appliable

They are stored exactly as the cluster returned them, which means
`resourceVersion`, `uid` and `managedFields` travel with them. `kubectl apply`
of one into a fresh cluster is refused by the apiserver until those are
stripped. Fidelity governs here — the glossary is explicit that faithfulness
beats convenience, and rewriting objects for a target is phase 2 — so nothing
is normalised, but "applicable by hand" currently means "after stripping the
server-populated metadata".

### Inherited by a later ticket

- **A member path holds a kind and a name but no namespace.** Two
  NetworkAttachmentDefinitions of the same name in different namespaces, both
  attached to one VirtualMachine, would land on one path. `bundle.Writer.Carry`
  now refuses the second and names both rather than writing a bundle whose
  manifest disagrees with its own members. Qualifying the path with a namespace
  is the real fix and is a format change, so it is left for whoever needs it.
- **A referenced object that does not exist is dropped without a trace.**
  Required by the deleted-matcher case, but it currently applies to every
  kind: a vanished ConfigMap disappears silently. Ticket 04 is where an
  absence becomes a recorded one.
- **Nothing yet distinguishes a forbidden read from a missing object**, so any
  read failure other than not-found still aborts the capture. Also ticket 04.

Status left at `ready-for-agent`, as tickets 01 and 02 did: the tracker has no
label meaning "implemented".

The vocabulary in this ticket is superseded. A bundle no longer sorts what it
collects into carried objects and evidence: it holds one set of resources, and
the capture record's outcomes are collected, required and skipped. The source
cluster's expansion of the VirtualMachine is a member of its own rather than a
resource, recorded under its own kind. See
`docs/adr/0002-one-set-of-resources-in-a-bundle.md` and the rewritten sections
of `spec.md`.
