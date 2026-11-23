# quo

A tool that captures a stopped KubeVirt VirtualMachine as a diagnostic
bundle. The bundle holds the machine's disks, its definition, and everything
the machine depends on. A support engineer can deploy the bundle in a
different cluster and reproduce the problem that led to the capture.

## Language

### The artifact

**Bundle**:
The archive that quo produces. The bundle holds a stopped VirtualMachine,
all of its disks, and every resource the machine depends on. The capture is
faithful enough that a deploy of the bundle in another cluster reproduces
the problem that led to the capture.
_Avoid_: appliance, image, export, backup, snapshot, archive, package

**Fidelity**:
The governing constraint on quo. A bundle must reproduce the captured VM
closely enough that a problem present in the original is present in the
deployment. For this reason, quo changes nothing that it is not forced to
change. When fidelity to the original and convenience conflict, quo is
faithful.
_Avoid_: accuracy, exactness, parity

**Related resources**:
The resources that a VirtualMachine depends on. The VirtualMachine can name
a resource itself. The VirtualMachine can also reach the resource through
another resource. Every related resource must be accounted for before a
bundle is self-contained.
_Avoid_: closure, dependency graph, transitive dependencies

**Manifest**:
The first member of a bundle. The manifest holds the bundle's format
version, provenance, requirements, and capture record. It also holds an
index of every other member, with sizes and digests. quo writes the
manifest first, so that a reader knows what a bundle needs before any disk
arrives.
_Avoid_: index, header, metadata, descriptor, table of contents

**Capture record**:
The part of the manifest that lists every object quo tried to capture and
what became of it. It lets a reader tell the difference between "this
did not exist" and "we were not allowed to look".
_Avoid_: report, log, inventory, audit, summary, results

### What became of each object

**Outcome**:
What a capture record says happened to one object: collected, required, or
skipped. Anything skipped carries a reason.
_Avoid_: disposition, status, result, state

**Collected**:
An object the bundle holds. A bundle does not say which of these objects a
target would recreate. This depends on the target and on the mappings given
at deploy time. It does not depend on anything that the capture can see.
_Avoid_: carried object, included object, embedded object, copy

**Requirement**:
Something that a bundle declares it needs from the target but does not
hold. Examples are a network, a storage class, and a container disk image.
The bundle names a requirement abstractly. A deploy maps it at deploy time.
_Avoid_: dependency, placeholder, abstract resource, slot

**Mapping**:
The assignment of a concrete cluster resource to a requirement, supplied at
deploy time.
_Avoid_: binding, resolution, match, wiring

**Parameter**:
A value supplied at deploy time that names or places the deployed
VirtualMachine. Examples are its name and its namespace. A parameter is
distinct from a requirement. A parameter invents something. A mapping
points at something that the target already has.
_Avoid_: option, setting, flag, argument

### What the bundle remembers

**Provenance**:
What the bundle records about where it came from and about the cluster that
quo takes it from. It records the source cluster, the namespace and VM
name, the capture time, and the KubeVirt and Kubernetes versions. It also
records the platform context that a support engineer needs to interpret
what they see.
_Avoid_: metadata, history, origin, source details

**Expanded spec**:
The source cluster's own resolution of the VirtualMachine. The resolution
includes the VirtualMachine's instancetype and preference. The resolution
also includes the cluster's defaults. A bundle holds the expanded spec as
one of its own members. The bundle does not hold it as a resource.

A deploy applies the expanded spec, with the forced rewrites, as the
deployed VirtualMachine. The target runs the machine that the source
resolved. The target does not run what it would resolve from the same
declaration. The captured VirtualMachine object remains the declaration of
record.
_Avoid_: expanded VirtualMachine, resolved spec, rendered spec

### The deployment

**Deploy record**:
What a deploy prints when it ends. The deploy record lists every object
with its outcome: created, failed, or skipped. Each outcome carries a
reason. The deploy record also lists the forced rewrites performed, the
mappings used, and instructions for removing the deployment. It is the
deploy-side mirror of the capture record.
_Avoid_: report, log, summary, results, output

**Forced rewrite**:
A change that a deploy makes to a bundle's object because the target leaves
no choice. The forced rewrites are:

- the name when renaming
- the namespace
- the storage class from a mapping
- the upload source of a DataVolume
- the network name from a mapping
- the removal of dataVolumeTemplates
- the clearing of the source cluster's own metadata
- the labels

Everything else deploys byte-for-byte.
_Avoid_: mutation, transformation, patch
