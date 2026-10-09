# Spec: Capture a stopped VirtualMachine as a diagnostic bundle

Status: ready-for-agent

## Problem Statement

A product team runs a KubeVirt VirtualMachine that is misbehaving. Their own
support engineers need to investigate it, but they cannot reach the cluster it
lives on, and the problem cannot be understood from logs alone — it needs the
machine itself.

Today there is no way to hand someone a VirtualMachine. The pieces exist but
none of them combine:

- `virtctl vmexport` retrieves disk images and a bare VM manifest. It does not
  gather the ConfigMaps, Secrets, instancetypes, preferences or
  ControllerRevisions the VirtualMachine depends on, so what it produces is not
  self-contained.
- `must-gather` collects cluster diagnostics but no disks and no runnable
  machine.
- Copying objects out by hand means knowing every place a VirtualMachine can
  reference something, and the list is long enough that nobody gets it right
  under pressure.

The result is a slow loop: support asks for one more object, the customer
exports it, support discovers another is missing. Each round trip costs a day,
and the engineer never has confidence they are looking at the same machine the
customer has.

## Solution

`quo export` captures a stopped VirtualMachine — its definition, all of its
disks, and its closure — as a single bundle file.

The governing constraint is fidelity: quo changes nothing it is not forced to
change. Identity is preserved exactly. MAC addresses, firmware UUID, serial and
chassis values travel as they are, because any of them could be the thing
causing the problem. The guest is never touched: no sealing, no generalization,
no reconfiguration, and the behaviour is the same whatever the guest operating
system is.

Alongside the VirtualMachine, the bundle carries evidence about the environment
it came from — StorageClasses, CDI StorageProfiles, the bound PersistentVolume
for each claim, KubeVirt's own configuration — because platform configuration
is often where the problem actually is. It records its requirements, the things
it deliberately does not carry, so a reader knows what a target cluster would
need to provide. And it carries a capture record naming everything quo tried to
collect and what became of it, so a support engineer can tell "this did not
exist" apart from "we were not allowed to look".

This spec covers export only. Deploying a bundle into a diagnostic cluster is a
later phase.

## User Stories

### Capturing a bundle

1. As a product team engineer, I want to capture a VirtualMachine with a single
   command, so that I do not have to know which objects it depends on.
2. As a product team engineer, I want to name the VirtualMachine and its
   namespace, so that I can capture a specific machine.
3. As a product team engineer, I want to choose where the bundle is written, so
   that I can put it on a volume with enough space.
4. As a product team engineer, I want quo to refuse to capture a running
   VirtualMachine and tell me to stop it first, so that I never produce a
   bundle with torn disk images.
5. As a product team engineer, I want progress reported while disks transfer,
   so that I can tell a multi-hour capture apart from a hung one.
6. As a product team engineer, I want quo to tell me roughly how much local
   scratch space it needs before it starts, so that a capture does not fail
   part-way for lack of disk.
7. As a product team engineer, I want a capture that fails to leave no
   half-written bundle behind, so that I cannot accidentally send a truncated
   file to support.
8. As a product team engineer, I want to inspect a bundle without a cluster, so
   that I can confirm what I am about to send.

### What the bundle contains

9. As a support engineer, I want every disk attached to the VirtualMachine, so
   that I can examine the machine's actual state.
10. As a support engineer, I want the VirtualMachine definition exactly as it
    was, so that I am looking at what the customer has and not a normalised
    version of it.
11. As a support engineer, I want MAC addresses, firmware UUID and serial
    preserved, so that a problem caused by machine identity still reproduces.
12. As a support engineer, I want the ConfigMaps and Secrets the VirtualMachine
    references, so that its cloud-init and sysprep configuration is available
    to read.
13. As a support engineer, I want the instancetype and preference objects, so
    that I can see what the VirtualMachine was asked to be.
14. As a support engineer, I want the ControllerRevisions that pinned those
    instancetypes, so that I can tell whether the live object has drifted from
    what the VirtualMachine was actually created with.
15. As a support engineer, I want the expanded form of the VirtualMachine
    recorded as evidence, so that I can compare what the source cluster
    resolved against what my own cluster resolves.
16. As a support engineer, I want the NetworkAttachmentDefinitions referenced by
    the VirtualMachine, so that I can see how its networking was declared.
17. As a support engineer, I want the PersistentVolumeClaims and DataVolumes, so
    that I can see what storage was requested.
18. As a support engineer, I want any ServiceAccount the VirtualMachine
    references, so that the closure is complete.
19. As a support engineer, I want access-credential Secrets captured, so that I
    can see what keys and passwords were being injected.

### Evidence about the source environment

20. As a support engineer, I want the StorageClass behind each claim, so that I
    can see the provisioner and its parameters.
21. As a support engineer, I want the CDI StorageProfile for each StorageClass,
    so that I can see what volume mode and access modes the disks were actually
    provisioned with.
22. As a support engineer, I want the bound PersistentVolume for each claim, so
    that I can see the real capacity, CSI driver and volume attributes rather
    than what was requested.
23. As a support engineer, I want KubeVirt's configuration from the source
    cluster, above all its enabled feature gates, so that I can account for
    behaviour my own cluster does not reproduce.
24. As a support engineer, I want the KubeVirt and Kubernetes versions
    recorded, so that I can judge whether a version difference explains what I
    am seeing.
25. As a support engineer, I want the source cluster, namespace, VirtualMachine
    name and capture time recorded, so that I can tie a bundle back to the case
    it came from.

### Requirements

26. As a support engineer, I want the bundle to list the networks it expects, so
    that I know what a target cluster would have to provide.
27. As a support engineer, I want the bundle to list the storage classes it
    expects, along with the size and volume mode each disk needs, so that I can
    tell in advance whether my cluster can satisfy them.
28. As a support engineer, I want the bundle to list container disk images it
    references, so that I know which images I need access to.
29. As a support engineer, I want to read all of that without downloading or
    extracting the disks, so that I can plan before committing to a large
    transfer.

### The capture record

30. As a support engineer, I want a machine-readable record of everything quo
    tried to capture, so that I can tell what is absent from what was never
    collected.
31. As a support engineer, I want a reason attached to anything that was
    skipped, so that I can distinguish a permissions problem from a missing
    object.
32. As a support engineer, I want to know when an optional dependency simply did
    not exist, so that I do not go looking for something that was never there.
33. As a product team engineer, I want the capture to continue when it cannot
    read something, so that one forbidden cluster-scoped object does not waste
    an hours-long disk transfer.
34. As a product team engineer, I want to see what a capture would and would not
    collect before running it, so that I can fix a permissions problem first.

### Integrity and format

35. As a support engineer, I want a digest for every member of the bundle, so
    that I can verify nothing was corrupted in transit.
36. As a support engineer, I want the manifest at the front of the bundle, so
    that I can read what a bundle needs by reading only its first few kilobytes.
37. As a support engineer, I want the bundle to declare a format version, so
    that tooling can refuse a bundle it does not understand.
38. As a support engineer, I want to open a bundle with ordinary tools, so that
    I am not blocked when quo is unavailable or will not run.
39. As a support engineer, I want the carried objects to be readable as
    Kubernetes YAML, so that I can apply or inspect them by hand.

### Operating constraints

40. As a product team engineer, I want quo to work from my laptop without an
    ingress or route on the cluster, so that I can capture from a cluster that
    does not expose its export endpoints.
41. As a product team engineer, I want quo to need no more local disk than the
    largest single compressed disk, so that capturing a multi-disk
    VirtualMachine does not require space for all of it at once.
42. As a product team engineer, I want disks transferred in compressed form, so
    that a sparsely used large volume does not transfer at its provisioned size.
43. As a product team engineer, I want the capture to survive a disk transfer
    that takes longer than the export's default lifetime, so that large
    VirtualMachines do not fail near the end.
44. As a product team engineer, I want to run a capture with only namespace-level
    permissions, so that I do not need a cluster administrator to collect a
    bundle.

## Implementation Decisions

### Shape

quo is a CLI. Phase 1 provides capture and inspection; there is no deploy
command.

All capture logic lives behind a library entry point that takes a cluster
interface, options, and a writer. The CLI is a thin wrapper over it. This is
what makes the behaviour testable without a cluster, and it is the single point
at which tests attach.

### The cluster interface

Everything quo needs from outside the process sits behind one interface:
fetching the VirtualMachine, fetching an arbitrary object by typed reference,
calling the expand-spec subresource, reading the evidence objects, creating and
awaiting a VirtualMachineExport, and opening a byte stream for a disk.

There is exactly one real implementation, a thin adapter over KubeVirt's client,
and one fake used by tests. Disk streaming belongs in this interface rather than
alongside it, so that a test never needs an HTTP server.

### Preconditions

The VirtualMachine must be powered off. quo checks this and refuses otherwise
with a message naming the VirtualMachine and telling the user to stop it. There
is no snapshot path and no flag to override.

### Computing the closure

quo performs its own traversal rather than calling KubeVirt's `objectgraph`
subresource, because that subresource is behind an Alpha feature gate that is
off by default and therefore cannot be relied upon. KubeVirt's server-side
implementation is the specification for what to traverse.

The traversal covers: VirtualMachine, VirtualMachineInstancetype,
VirtualMachineClusterInstancetype, VirtualMachinePreference,
VirtualMachineClusterPreference, DataVolume, ControllerRevision, ConfigMap,
PersistentVolumeClaim, ServiceAccount, Secret, and NetworkAttachmentDefinition.
It must follow volumes, access credentials, and instancetype matchers including
their status references — a matcher may name an object that has since been
deleted, in which case the ControllerRevision is the only complete record.

VirtualMachineInstance and Pod appear in KubeVirt's graph but are irrelevant
here, because a stopped VirtualMachine has neither.

Each discovered object is classified as a carried object, evidence, or a
requirement. KubeVirt's own dependency-type vocabulary maps onto this directly:
its config dependencies become carried objects, its storage and network
dependencies become requirements, with the underlying disk data carried
separately as bundle members.

### Evidence

Beyond the closure, quo captures: the StorageClass for each claim, the CDI
StorageProfile for each of those StorageClasses, the bound PersistentVolume for
each claim, and KubeVirt's own configuration. It also calls the expand-spec
subresource and records the result.

The expanded form is evidence, not resolution. quo does not use it to rewrite
the VirtualMachine. Every input to resolution — the instancetype objects, the
ControllerRevisions, KubeVirt's configuration — is already in the bundle, so the
expansion is derivable; it is captured because expansion is version-dependent
and a difference between the source cluster's resolution and the target's is a
plausible cause of the problem being investigated.

### Requirements

The bundle records the networks, storage classes and container disk images it
expects a target to supply, each with the constraints needed to check a
candidate: for storage, the size and volume mode each disk needs.

Nothing in this phase consumes these records. They are written at capture
because the information is free while connected to the source cluster and
expensive to reconstruct afterwards.

Container disk images are a requirement rather than a carried object: quo does
not repackage container images.

### Capture record

Collection is best effort. A failure to read any single object does not abort
the capture.

Every object quo attempted appears in the capture record with its disposition —
carried, evidence, requirement, or skipped — and anything skipped carries a
reason distinguishing at minimum: forbidden, not found, and not installed. The
last covers an optional component such as CDI being absent.

Much of the evidence set is cluster-scoped, and the person capturing a bundle is
frequently a namespace owner without cluster read. Failing the whole capture in
that case would be unacceptable; silently omitting would be worse, because the
reader could not tell the difference.

A separate check operation reports what a capture would collect and what it
would skip, without transferring any disks.

### Acquiring disks

Disks are retrieved through the VirtualMachineExport API. quo creates an export
referencing the VirtualMachine with a token secret, waits for it to become
ready, reads the per-volume links from its status, and downloads each volume
using the export token header. Port forwarding is used where no external route
exists, so no ingress is required.

quo requests the compressed format rather than raw. A sparsely used volume
transfers at roughly its used size rather than its provisioned size, which is an
order of magnitude on typical disks.

Disks are staged one at a time: download to a temporary file, which establishes
the compressed size and digest, write that file into the bundle, delete it,
proceed to the next. Peak scratch is the largest single compressed disk, not the
total.

The bytes received are stored verbatim. quo performs no compression of its own —
the export server has already compressed the stream, and decompressing to
recompress would cost significant processor time across hundreds of gigabytes
for marginal gain.

The export's time-to-live must be set generously, well beyond its two-hour
default, since a large transfer can exceed it.

Secrets are included in full. The bundle is intended for a team's own support
engineers, so confidentiality between the parties is not a concern — but a
bundle does contain plaintext credentials, which the documentation must state.

### Bundle format

A bundle is an uncompressed tar whose members are compressed individually. This
allows streaming and lets a reader skip disks it does not want.

The manifest is strictly the first member. A reader learns the format version,
provenance, requirements, capture record and member index before any disk byte
arrives, which is what makes inspection possible on a partial transfer.

The manifest is YAML, and so is every carried object and evidence object. A
single format throughout is more pleasant for anyone who untars a bundle by
hand. Marshalling goes through the Kubernetes YAML library rather than a general
YAML library, so that encoding follows JSON semantics and avoids the failure
modes that bite machine-written YAML — a version string parsed as a number, a
bare `no` becoming boolean false.

Every member carries a SHA-256 digest recorded in the manifest, so integrity
data cannot be separated from the bundle.

Disks are stored under a disks grouping, carried objects under an objects
grouping, and evidence separately from carried objects so that a reader is never
misled into applying something that was collected only to be read.

Disk members are keyed by PersistentVolumeClaim, not by volume name. A
VirtualMachine's volumes are a named list and nothing prevents two of them
referencing the same claim; keying by volume name would download and store the
same bytes twice. Keying by claim also lines members up one-to-one with what is
downloaded, since the export server addresses volumes by claim name.

The manifest therefore maps volume names onto disk members as a list, so a
member serving two volumes records both. KubeVirt's own OCI export rejects a
VirtualMachine in this situation, because an OCI layer annotation holds a single
string and there is no way to name two volumes in it. That is a limitation of
that format rather than of the problem, and a YAML manifest has no such
difficulty — quo captures these VirtualMachines normally.

### Naming

The tool is `quo`. The repository, module path and working directory still say
`kvo` and need renaming; this is not a blocker for implementation.

## Testing Decisions

### What makes a good test here

A test exercises the capture through its library entry point and asserts on the
bundle that comes out. It does not reach into traversal, classification,
manifest construction or tar writing. Those are implementation and will change;
the bundle is the contract.

Test names read as specifications rather than labels — "forbidden StorageClass
is recorded as skipped, not omitted", "instancetype matcher naming a deleted
object still captures the ControllerRevision" — following the house style guide.
Edge cases are explicit: missing fields, absent optional components, empty
closures, objects referenced from more than one place.

### The main suite

A fake implementation of the cluster interface, built as a plain struct holding
objects and canned disk bytes. Tests construct a cluster state, run a capture
into a buffer, read the bundle back, and assert on it. No Kubernetes fake
clientset, no mock framework, no HTTP test server.

This covers: closure traversal across every reference type; classification into
carried objects, evidence and requirements; capture record contents including
each skip reason; manifest correctness and member ordering; digests; refusal to
capture a running VirtualMachine; and behaviour when an object is forbidden,
absent, or references something deleted.

Assertions use gomega, following `itinerary`. Note that `go-style-guide.md`
currently forbids assertion libraries; `itinerary` is the more recent practice
and was chosen deliberately. The style guide should be reconciled separately —
its table-driven structure, helper conventions and naming guidance still apply.

Table-driven tests with a `tests` slice of anonymous structs, `name` as the
first field and each case run as a subtest, per the style guide. Fixture
factories are marked with `t.Helper()`.

### The cluster suite

A second suite, build-tagged and excluded from the default test run, requiring a
live KubeVirt cluster with the ObjectGraph feature gate enabled. It asserts that
quo's own traversal produces the same object set as KubeVirt's `objectgraph`
subresource.

This is the only thing that catches KubeVirt introducing a reference type quo
does not know about, and it is also the only coverage of the real cluster
adapter, which the main suite deliberately bypasses. It should run in CI where a
cluster is available, and must not be required for local development.

### Prior art

`itinerary` in the sibling directory is the closest model: a small focused Go
library using gomega, with hand-written fakes defined in a dedicated test file
rather than generated mocks. `go-style-guide.md` governs table structure,
assertion formatting, helper conventions and test naming.

## Out of Scope

**Deploying a bundle.** Everything about getting a bundle onto a cluster is a
later phase: mapping requirements to concrete cluster resources, supplying
mappings, uploading disks through CDI, rewriting references when renaming,
rollback of a partial deploy, and naming the deployed VirtualMachine. Phase 1
records what a deploy will need and stops there.

**Running VirtualMachines.** No snapshot path, no freeze and thaw, no
application-consistent capture. A VirtualMachine must be stopped.

**Guest modification of any kind.** No sealing, no generalization, no sysprep,
no cloud-init cleaning, no credential stripping, no network reconfiguration.
KubeVirt exposes no mechanism to execute inside a guest, and attempting it would
contradict fidelity in any case.

**Appliances.** Producing something deployable repeatedly as a template, with
identity stripped and parameters filled in at deploy, is a different tool with
opposite defaults. It was considered and dropped.

**OCI artifacts.** KubeVirt's VEP-256 defines an OCI artifact format for VM
export. quo uses its own format. The consequence is no registry distribution,
no signing with existing container tooling, and no layer deduplication.

**Multi-VM bundles.** One VirtualMachine per bundle.

**Runtime diagnostics.** VirtualMachineInstance status, events, and the
virt-launcher pod specification are unavailable for a stopped VirtualMachine and
are not collected. Capturing runtime state before shutdown and merging it into a
later capture is a plausible follow-on, not part of this.

**Redaction and secret handling.** Secrets are captured in full. There is no
filtering, masking or encryption.

**Signing bundles.** No integrity guarantee beyond per-member digests, which
detect corruption, not tampering.

**Duplicating must-gather.** quo collects a specific, small set of cluster
objects as evidence. It does not scrape logs or gather general cluster
diagnostics.

## Further Notes

### The completeness gap

Nothing in this phase verifies that a bundle contains *enough*. A write-then-read
round trip catches format and encoding bugs, but the question "would this
actually reproduce the VirtualMachine?" cannot be answered until a deploy path
exists. This is a known and accepted risk, not an oversight. It is the single
most likely source of rework in phase 2, and implementers should expect the
closure to turn out incomplete in ways no phase-1 test can reveal.

### Which problems a bundle can carry

Requiring a stopped VirtualMachine means a bundle only carries problems that
survive a power cycle: on-disk state, configuration, data corruption, anything
reproducible from a cold boot. A wedged process, a memory leak mid-leak, a
transient network fault — all are destroyed by the act of capturing. This should
be stated plainly in user-facing documentation, because "export the broken VM"
naturally reads as "capture it as it is right now".

### Windows

Windows VirtualMachines are captured exactly like any other, and for this use
case that works well — fidelity is what a support engineer wants. What a bundle
cannot do is produce a *distinct* Windows machine on deploy, because that
requires sysprep inside the guest before capture. Since phase 1 does not deploy
and never claims to generalize, this is not a phase-1 concern. It will matter in
phase 2 if anyone deploys the same bundle twice onto one network.

### Dependencies that may be absent

CDI may not be installed, in which case StorageProfiles do not exist and
DataVolumes will not appear. Multus may not be installed, in which case there
are no NetworkAttachmentDefinitions. Neither is an error; both are capture
record entries.

### Things that will bite

The VirtualMachineExport internal CA rotates every seven days. Irrelevant for a
single transfer, but any caching of export links will fail in a way that looks
intermittent.

Resolved values such as machine type and CPU topology are not present on a
stopped VirtualMachine and are only obtainable through expand-spec. Code that
expects to read them from a VirtualMachineInstance will find nothing.
