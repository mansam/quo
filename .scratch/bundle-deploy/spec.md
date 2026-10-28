# Spec: Deploy a bundle into a diagnostic cluster

Status: ready-for-agent

## Problem Statement

A support engineer holds a bundle and can inspect it, but cannot use it.
Reproducing the VirtualMachine by hand means untarring the bundle, working
out object by object which pieces a target would take and which only
describe the source cluster, recreating storage, uploading disks through
CDI, and rewriting references for a new namespace. It is a long procedure
done under time pressure, and at the end of it the engineer has no reason
to believe the machine they built is the machine the customer has.

Phase 1 named this gap and accepted it: "would this bundle actually
reproduce the VirtualMachine?" cannot be answered until a deploy path
exists, and the set of related resources was expected to turn out
incomplete in ways no phase-1 test can reveal.

## Solution

`quo deploy` puts a bundle onto a cluster in one command.

Every disk becomes a DataVolume populated through CDI's upload API with the
bytes the bundle holds. The VirtualMachine that lands on the cluster is
the source cluster's own resolution of it — the bundle's expanded spec —
with a short, fixed list of forced rewrites and nothing else changed.
Instancetypes, preferences and ControllerRevisions are never created; they
are in the bundle to be read.

The requirements recorded at capture are mapped onto the target's
resources: networks onto NetworkAttachmentDefinitions, disks onto storage
classes. Where the target has a resource of the same name the mapping is
automatic and reported; everything else is named with a flag.

Deploy is best-effort, like capture. quo refuses before creating anything
when a check it can make up front says the deploy cannot succeed — a
missing namespace, a name already taken, a mapping that cannot be resolved,
CDI absent, a bundle with no expanded spec. Once past those refusals, the
failure of any single step is recorded and the run continues. What exists
at the end is itemized in the printed deploy record, and `quo undeploy`
removes a deployment in one command.

## User Stories

### Deploying a bundle

1. As a support engineer, I want to deploy a bundle with a single command,
   so that reproducing a machine is not an afternoon of kubectl.
2. As a support engineer, I want to choose the namespace a bundle deploys
   into, so that diagnostic machines sit apart from everything else.
3. As a support engineer, I want to rename the deployed VirtualMachine, so
   that its name says what it is for.
4. As a support engineer, I want to see what a deploy would create before
   it creates anything, so that I can check the mappings first.
5. As a support engineer, I want progress reported while disks upload, so
   that I can tell a multi-hour transfer from a hung one.
6. As a support engineer, I want a deploy to continue when one step fails,
   so that a single refusal does not waste the whole run.
7. As a support engineer, I want a record of everything created, failed and
   skipped, each with a reason, so that I know exactly what the target
   holds.
8. As a support engineer, I want a non-zero exit code when anything failed,
   so that a script can tell a complete deploy from a partial one.
9. As a support engineer, I want Ctrl-C to stop the deploy immediately and
   leave everything as it is, so that stopping is always my decision.
10. As a support engineer, I want one command that removes a deployment, so
    that closing a case does not mean deleting objects by hand.

### What a deploy creates

11. As a support engineer, I want every disk populated with the bytes the
    bundle holds, so that I examine the machine's actual state.
12. As a support engineer, I want the deployed VirtualMachine to be the
    source cluster's resolution of it, so that the target's own defaults
    cannot shift the machine.
13. As a support engineer, I want the ConfigMaps, Secrets and
    ServiceAccounts applied unchanged, so that the machine boots with its
    own configuration.
14. As a support engineer, I want the deployed VirtualMachine left stopped,
    so that I can inspect the disks before anything runs.
15. As a support engineer, I want MAC addresses, firmware UUID and serial
    preserved, so that a problem caused by machine identity still
    reproduces.
16. As a support engineer, I want to deploy with namespace-level
    permissions on the target, so that I do not need a cluster
    administrator there.
17. As a support engineer, I want the deployed objects to say which bundle
    they came from, so that a running machine can be tied back to its case.

### Mappings

18. As a support engineer, I want a requirement mapped automatically when
    the target has a resource of the same name, so that a typical deploy
    needs no flags.
19. As a support engineer, I want to attach one of the machine's networks
    to a NetworkAttachmentDefinition of my choosing, so that it lands on
    the network I prepared.
20. As a support engineer, I want to place the disks on a storage class of
    my choosing, so that the target's storage is used deliberately.
21. As a support engineer, I want one storage class mapping to cover every
    disk, so that a many-disk machine does not mean a flag per class.
22. As a support engineer, I want to be told what mapped to what, so that
    nothing happens silently.
23. As a support engineer, I want the container disk images listed as
    requirements, so that I know what the target has to be able to pull.

### Refusals and failures

24. As a support engineer, I want quo to refuse before creating anything
    when the target namespace does not exist, so that nothing is created
    half-way into a mistake.
25. As a support engineer, I want quo to refuse when any object it would
    create already exists, so that someone else's work is neither
    overwritten nor built around.
26. As a support engineer, I want quo to refuse when a mapping cannot be
    resolved, so that disks are not uploaded that cannot be placed.
27. As a support engineer, I want quo to refuse when CDI is absent and
    there are disks to upload, so that the run does not fail object by
    object for an hour.
28. As a support engineer, I want quo to refuse a bundle that holds no
    expanded spec, so that disks are not uploaded for a machine that cannot
    be created.
29. As a product team engineer, I want quo to warn at capture time when
    the expansion could not be read, so that I learn the bundle cannot be
    deployed before sending it.
30. As a support engineer, I want quo inspect to say when a bundle holds no
    expanded spec, so that I can tell it cannot be deployed before I try.
31. As a support engineer, I want a member that fails its digest skipped
    and recorded, so that corruption in transit never reaches the target.

### Cleaning up

32. As a support engineer, I want undeploy to remove exactly one
    deployment, so that other work in the namespace is untouched.
33. As a support engineer, I want undeploy to print what it deleted, so
    that I can confirm the namespace is clear.
34. As a support engineer, I want to see what undeploy would delete before
    it deletes anything, so that I can check I am pointing it at the right
    deployment.
35. As a support engineer, I want the objects labeled so that cleanup is
    possible with kubectl alone, so that I am not blocked when the bundle
    file is gone.

## Implementation Decisions

### Shape

quo gains two commands: `quo deploy <bundle> -n <namespace> [--name NAME]
[--dry-run] [mapping flags]` and `quo undeploy <bundle> -n <namespace>
[--dry-run]`.

Deploy logic lives behind a library entry point that takes a bundle, a
target interface and options; the CLI is a thin wrapper over it, the same
arrangement as capture. The target interface — create an object, upload a
disk into a DataVolume, delete the objects carrying a label — sits beside
the cluster interface, with one real adapter over the Kubernetes and CDI
clients and one fake that records what was created, used by tests.
Undeploy uses the same interface.

The namespace is required; quo never falls back to the kubeconfig's current
namespace, because a diagnostic VirtualMachine landing in someone's default
namespace by accident is a quiet mistake. `--name` renames the
VirtualMachine and nothing else: nothing in a VirtualMachine's spec
references its own name, so a rename forces no other rewrite. `--dry-run`
runs the refusals and prints the deploy record without creating anything.

### What deploy creates

Deploy decides by kind. The table:

| Kind | What deploy does |
| --- | --- |
| the expanded spec member | applied as the VirtualMachine, with the forced rewrites |
| the captured VirtualMachine | never applied — the declaration of record |
| ConfigMap, Secret, ServiceAccount | applied unchanged |
| DataVolume | created with source `upload`, named after the claim |
| PersistentVolumeClaim | never written — the DataVolume created in its place covers it |
| NetworkAttachmentDefinition | never written — the network is mapped |
| VirtualMachineInstancetype and VirtualMachinePreference, namespaced and cluster-scoped | never written — read-only |
| ControllerRevision | never written — read-only |
| StorageClass, StorageProfile, PersistentVolume, KubeVirt | never written — evidence about the source |

Nothing deploy creates is cluster-scoped. The support engineer needs only
namespace-level permissions on the target, mirroring the namespace owner
who captures on the source. See ADR-0004.

### The applied VirtualMachine is the expanded spec

The VirtualMachine a deploy creates comes from the bundle's expanded spec
member, not from the captured VirtualMachine object. The expansion is the
source cluster's own resolution: instancetype and preference folded in, the
source's defaults — machine type, architecture, CPU topology, eviction
strategy — applied. Deploying it means the target runs the machine the
source resolved, not what the target would resolve from the same
declaration; a target whose defaults differ cannot shift the machine. The
captured VirtualMachine object is never applied and remains the declaration
of record. See ADR-0003.

The applied VirtualMachine names no instancetype or preference matcher, so
no ControllerRevision is created on the target and none is needed. Whether
the expansion itself still carries the matcher fields — and whether
`dataVolumeTemplates` survive it — are facts the live round-trip settles;
anything that survives against this is removed.

### The forced rewrites

The complete list of changes deploy makes to a bundle's objects. Everything
else is applied byte-for-byte.

1. the VirtualMachine's name, when renaming;
2. the namespace of every namespaced object;
3. `storageClassName` on every created DataVolume, from the mapping;
4. the DataVolume source, always `upload`;
5. the multus `networkName`, from the mapping, namespace-qualified when the
   mapped NetworkAttachmentDefinition lives outside the deploy namespace;
6. `spec.dataVolumeTemplates` removed from the applied VirtualMachine;
7. the labels and annotations on every created object;
8. the source cluster's own metadata — the `resourceVersion`, `uid`,
   `creationTimestamp`, `generation` and `managedFields` — removed, because
   the target assigns its own of each when it creates and refuses outright
   a create that arrives carrying a `resourceVersion`.

### Every disk becomes an upload DataVolume

A claim cannot be populated on its own — CDI's upload works through a
DataVolume — so every disk is created as a DataVolume named after the claim
it backs, whatever it was on the source. The DataVolume's size and
volumeMode come from the requirement recorded at capture (the bound size,
not what was requested); its storage class comes from the mapping.

The VirtualMachine's volume stanzas are unchanged. A volume referencing a
claim by name finds the claim the DataVolume creates, because CDI names the
claim after the DataVolume, and a volume of type `dataVolume` names the
DataVolume directly.

`dataVolumeTemplates` are removed from the applied VirtualMachine and
become standalone DataVolumes like any other disk; the volume references
that named them are satisfied unchanged.

A disk the bundle holds no bytes for — the claim was forbidden or never
existed — creates nothing. The gap is recorded and the run continues.

### Mappings

A mapping is automatic when the target has a resource of the same name: a
StorageClass for a storage requirement, a NetworkAttachmentDefinition of
the attachment's name in the deploy namespace for a multus network. Every
automatic mapping is reported; nothing maps silently.

Everything else is named with a flag. `--network <network>=<attachment>`
attaches one of the VirtualMachine's networks to a
NetworkAttachmentDefinition of the target's, namespace-qualified when it
lives outside the deploy namespace. `--storage-class <source>=<target>`
places the disks of one class, and `--storage-class *=<target>` is a
catch-all for the classes that do not match by name.

A requirement that resolves to nothing is a refusal before anything is
created. A disk whose volumeMode the mapped class cannot provide is a
refusal too; no mapping fixes that. The pod network is never mapped. A
container disk image is never mapped and is not pull-checked — quo has no
registry credentials — it is reported as a requirement.

### Labels and annotations

Every object deploy creates is labeled `app.kubernetes.io/managed-by: quo`
and `quo.mansam.dev/bundle: sha256-<base64url>`, where the value is the
unpadded base64url encoding of the SHA-256 of the manifest member, prefixed
`sha256-`. The manifest carries the digest of every other member, so its
hash identifies the bundle. The hash is not written in hex because a hex
SHA-256 is 64 characters and label values are limited to 63; base64url is
43 and the prefix makes 50.

The label key is frozen: undeploy selects on it, and so does anyone
cleaning up with kubectl.

Annotations on every created object carry the exact bundle filename. The
VirtualMachine additionally carries the source cluster, namespace and
VirtualMachine name, the capture time and the deploy time.

### Refusals

Before anything is created, quo refuses — naming what it found — when:

- the bundle cannot be read, or its format version is unknown;
- the bundle holds no expanded spec;
- the target namespace does not exist;
- any object deploy would create already exists;
- a mapping cannot be resolved;
- a disk's volumeMode is one the mapped storage class cannot provide;
- CDI is absent and there are disks to upload.

A bundle whose expansion could not be read is refused rather than deployed
half-way, and the person capturing learns it at capture time: `quo export`
warns when the expansion cannot be read, and `quo inspect` says when a
bundle holds none.

### Best-effort and the deploy record

Once past the refusals, a deploy is best-effort. Every attempted step lands
in the deploy record with an outcome — created, failed, or skipped — and a
reason for anything that failed or was skipped. A member whose digest does
not match is skipped as it is read, so corruption never reaches the target.

Objects are created in order: the DataVolumes, then the uploads, then the
remaining objects, then the VirtualMachine last — nothing in the namespace
looks like a VirtualMachine until its disks are in place.

Ctrl-C stops the deploy immediately and leaves everything as it is;
nothing in quo is uninterruptible. The deploy record is printed however the
run ended, and ends with the undeploy command for what exists. The exit
code is non-zero when anything failed.

### Disk upload

Disks are uploaded through CDI's UploadProxy, reached over a port-forward
so that no ingress is required — the same arrangement as the export side.
The upload-token flow is used; disks upload one at a time with progress
reported, as captures do. The bundle's bytes are streamed unchanged: the
gzip and tar.gz forms the export produced are the forms CDI's upload
accepts, both ends being CDI. The live round-trip verifies this.

### Undeploy

`quo undeploy <bundle> -n <namespace>` removes one deployment: the objects
carrying that bundle's label in that namespace. It prints what it deleted
and exits; it does not wait for deletions to complete. `--dry-run` prints
what it would delete.

There is deliberately no flag that removes everything quo manages in a
namespace. When the bundle file is gone, the labels make kubectl enough.

### Simplicity

The governing principle for the implementation: do the simplest thing that
will work. Each piece is simple, readable, and not drowning in guards for
edge cases. It is acceptable to let things fail — a failure is recorded and
the run continues — rather than to anticipate every contingency. Guards
exist for the mistakes a support engineer will actually make, which is what
the refusal list is.

## Testing Decisions

### What makes a good test here

A test drives the deploy through its library entry point with the fake
target and asserts on the created set — which objects, with which rewrites,
labels and annotations — and on the deploy record. The created set is the
contract, as the bundle is for capture. How mappings are resolved, how
rewrites are applied and how the CDI upload is driven are implementation
and are not tested directly.

Test bundles are produced by running a capture against the existing fake
cluster, so a fixture is built by the same code that builds real bundles.

### The main suite

Coverage: every forced rewrite; mappings matched by name and by flag; every
refusal; the best-effort outcomes — created, failed, skipped — each with a
reason; a disk the bundle holds no bytes for; labels and annotations;
undeploy's selection; dry-run creating nothing; a member whose digest does
not match skipped and recorded; renaming; cancellation through the context.

Edge cases are explicit, following the capture suite: a VirtualMachine
with no disks; one claim serving two volumes; a network naming an
attachment in another namespace; a bundle with no expanded spec.

Test names read as specifications, and tests are table-driven with `name`
as the first field, per the house style. Assertions use gomega; ADR-0001 is
settled and not to be raised in review.

### The live suite

The round-trip: build the fixture, capture it, deploy it into a second
namespace on the same cluster, then compare what the target holds against
the fixture. An object the fixture has that the target lacks is a failure.
The target's expand-spec of the deployed VirtualMachine is diffed against
the expansion the bundle holds; differences are reported rather than
failed, because the two clusters resolving differently is information.

The round-trip also settles three facts the design depends on: that the
expansion carries no instancetype or preference matchers; that
`dataVolumeTemplates` survive the expansion; and that CDI's upload accepts
the gzip and tar.gz bytes the export produced, unchanged.

### Prior art

The capture suite: its fake cluster, table structure, fixture factories
marked as helpers, and specification-reading test names.

## Out of Scope

**Starting the deployed VirtualMachine.** Deploy leaves it stopped, as
captured. Starting is `virtctl start`, deliberately not a flag.

**Automatic rollback.** A failed or interrupted deploy leaves what it
created in place and reports it; undeploy is the remedy, and the engineer
decides.

**Resuming a partial deploy.** After a failure, clean up and run again.

**Creating target-side infrastructure.** NetworkAttachmentDefinitions,
instancetypes, preferences, ControllerRevisions, StorageClasses,
StorageProfiles, PersistentVolumes and the KubeVirt resource are never
written. Networks and storage classes are mapped to what the target has.

**Container disk images.** Not repackaged, not mapped, not pull-checked.
Reported as requirements.

**Multi-VM bundles.** One VirtualMachine per bundle, as in phase 1.

**Redaction and secret handling.** Secrets deploy verbatim; a deploy puts
plaintext credentials onto the target cluster, which the documentation must
state.

**Signing.** Per-member digests detect corruption, not tampering, and
deploy verifies exactly that.

## Further Notes

### The completeness gap closes

The live round-trip is the test phase 1 named as its single most likely
source of rework: capture, deploy, compare. Expect the set of related
resources to turn out incomplete in ways only this test reveals.

### Deploying one bundle twice

Two deployments of one bundle — onto one cluster or two — produce two
machines with the same identity: same MAC addresses, same firmware UUID,
same serial. On a shared network that is a duplicate machine, and for
Windows guests a duplicate machine SID, which nothing outside the guest can
fix. This is inherent to fidelity and is a documentation matter, not a
block.

### Secrets land on the target

A bundle holds plaintext credentials, and a deploy writes them into the
target cluster. The diagnostic cluster is the trust boundary, and the
documentation must say so.

### Where the record lives

ADR-0003 records that a deploy applies the source cluster's resolution;
ADR-0004 records the by-kind table and the forced rewrites. The glossary
gains Deploy record and Forced rewrite, and its Expanded spec entry is
rewritten to match.
