# quo

A tool that captures a stopped KubeVirt VirtualMachine — its disks, its
definition, and everything it depends on — as a diagnostic bundle, so that a
support engineer can deploy it in a different cluster and reproduce the
problem it was captured for.

## Language

### The artifact

**Bundle**:
The archive quo produces: a stopped VirtualMachine, all of its disks, and
every resource it depends on, captured faithfully enough that deploying it
elsewhere reproduces the problem it was captured for.
_Avoid_: appliance, image, export, backup, snapshot, archive, package

**Fidelity**:
The governing constraint on quo. A bundle must reproduce the captured VM
closely enough that a problem present in the original is present in the
deployment, so quo changes nothing it is not forced to change. When being
faithful to the original and being convenient conflict, quo is faithful.
_Avoid_: accuracy, exactness, parity

**Related resources**:
The resources a VirtualMachine depends on, whether it names them itself or
reaches them through another resource, and which must be accounted for before
a bundle is self-contained.
_Avoid_: closure, dependency graph, transitive dependencies

**Manifest**:
The first member of a bundle: its format version, provenance, requirements,
the capture record, and an index of every other member with sizes and
digests. Written first so a reader knows what a bundle needs before any disk
arrives.
_Avoid_: index, header, metadata, descriptor, table of contents

**Capture record**:
The part of the manifest listing every object quo tried to capture and what
became of it. Lets a reader tell "this did not exist" apart from "we were not
allowed to look".
_Avoid_: report, log, inventory, audit, summary, results

### What became of each object

**Outcome**:
What a capture record says happened to one object: collected, required, or
skipped. Anything skipped carries a reason.
_Avoid_: disposition, status, result, state

**Collected**:
An object the bundle holds. A bundle does not say which of them a target
would recreate, because that depends on the target and on the mappings given
at deploy time, not on anything the capture can see.
_Avoid_: carried object, included object, embedded object, copy

**Requirement**:
Something a bundle declares it needs from the target but does not hold — a
network, a storage class, a container disk image. Named abstractly in the
bundle and mapped at deploy time.
_Avoid_: dependency, placeholder, abstract resource, slot

**Mapping**:
The assignment of a concrete cluster resource to a requirement, supplied when
the bundle is deployed.
_Avoid_: binding, resolution, match, wiring

**Parameter**:
A value supplied at deploy time that names or places the deployed
VirtualMachine — its name, its namespace. Distinct from a requirement: a
parameter invents something, a mapping points at something the target already
has.
_Avoid_: option, setting, flag, argument

### What the bundle remembers

**Provenance**:
What the bundle records about where it came from and the cluster it was taken
from: source cluster, namespace and VM name, capture time, KubeVirt and
Kubernetes versions, and the platform context a support engineer needs to
interpret what they are looking at.
_Avoid_: metadata, history, origin, source details

**Expanded spec**:
The source cluster's own resolution of the VirtualMachine, with its
instancetype and preference folded in and the cluster's defaults applied. A
bundle holds it as a member of its own rather than as a resource, and a
deploy applies it, with the forced rewrites, as the deployed
VirtualMachine: the target runs the machine the source resolved, not what
the target would resolve from the same declaration. The captured
VirtualMachine object remains the declaration of record.
_Avoid_: expanded VirtualMachine, resolved spec, rendered spec

### The deployment

**Deploy record**:
What a deploy prints when it ends: every object with its outcome —
created, failed, or skipped, each with a reason — the forced rewrites
performed, the mappings used, and the undeploy command for what exists.
The deploy-side mirror of the capture record.
_Avoid_: report, log, summary, results, output

**Forced rewrite**:
A change a deploy makes to a bundle's object because the target leaves no
choice: the name when renaming, the namespace, the storage class from a
mapping, the upload source of a DataVolume, the network name from a
mapping, the removal of dataVolumeTemplates, the clearing of the source
cluster's own metadata, the labels. Everything else deploys byte-for-byte.
_Avoid_: mutation, transformation, patch
