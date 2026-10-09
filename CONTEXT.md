# quo

A tool that captures a stopped KubeVirt VirtualMachine — its disks, its
definition, and everything it depends on — as a diagnostic bundle, so that a
support engineer can deploy it in a different cluster and reproduce the
problem it was captured for.

## Language

### The artifact

**Bundle**:
The archive quo produces: a stopped VirtualMachine, all of its disks, and its
closure, captured faithfully enough that deploying it elsewhere reproduces
the problem it was captured for.
_Avoid_: appliance, image, export, backup, snapshot, archive, package

**Fidelity**:
The governing constraint on quo. A bundle must reproduce the captured VM
closely enough that a problem present in the original is present in the
deployment, so quo changes nothing it is not forced to change. Where
faithfulness and convenience conflict, faithfulness wins.
_Avoid_: accuracy, exactness, parity

**Closure**:
The set of cluster objects a VirtualMachine transitively depends on and which
must be accounted for before the bundle is self-contained.
_Avoid_: dependencies, references, related objects

**Manifest**:
The first member of a bundle: its format version, provenance, requirements,
the capture record, and an index of every other member with sizes and
digests. Written first so a reader knows what a bundle needs before any disk
arrives.
_Avoid_: index, header, metadata, descriptor, table of contents

**Capture record**:
The part of the manifest listing every object quo tried to capture and what
became of it — carried, evidence, requirement, or skipped — with a reason
for anything skipped. Lets a reader tell "this did not exist" apart from "we
were not allowed to look".
_Avoid_: report, log, inventory, audit, summary, results

### What becomes of each dependency

Every member of the closure is handled in one of these ways.

**Carried object**:
A referenced object shipped verbatim in the bundle and recreated on the
target, because its content is the thing that matters.
_Avoid_: embedded object, bundled resource, copy

**Evidence**:
An object captured because it explains the environment the VirtualMachine
came from, not because anything will recreate it — a StorageClass, the
resolved form of an instancetype. Read by a support engineer; never applied
to a target.
_Avoid_: context, reference copy, informational object, extras

**Requirement**:
Something a bundle declares it needs from the target but does not carry — a
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
