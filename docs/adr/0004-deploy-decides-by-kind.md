# Deploy decides by kind

ADR-0002 settled that a bundle holds one set of resources. The bundle says
nothing about which of them a target would recreate: "a deploy decides by
kind." This ADR records what the deploy decided.

## The table

| Kind | What deploy does |
| --- | --- |
| the expanded spec member | applied as the VirtualMachine, with the forced rewrites |
| the captured VirtualMachine | never applied: the declaration of record |
| ConfigMap, Secret, ServiceAccount | applied unchanged |
| DataVolume | created with source `upload`, named after the claim |
| PersistentVolumeClaim | never written: the DataVolume created in its place covers it |
| NetworkAttachmentDefinition | never written: the network is mapped |
| VirtualMachineInstancetype and VirtualMachinePreference, namespaced and cluster-scoped | never written: read-only |
| ControllerRevision | never written: read-only |
| StorageClass, StorageProfile, PersistentVolume, KubeVirt | never written: evidence about the source |

## The forced rewrites

The table says what a deploy creates. The rewrites say what the deploy
changes. The complete list:

1. the VirtualMachine's name, when renaming.
2. the namespace of every namespaced object.
3. `storageClassName` on every created DataVolume, from the mapping.
4. the DataVolume source, always `upload`.
5. the multus `networkName`, from the mapping.
6. `spec.dataVolumeTemplates` removed from the applied VirtualMachine.
7. the labels and annotations on every created object.
8. the source cluster's own metadata, removed. That metadata is the
   `resourceVersion`, `uid`, `creationTimestamp`, `generation` and
   `managedFields`. The target assigns its own of each when it creates. It
   refuses outright a create that arrives carrying a `resourceVersion`.

A deploy applies everything else byte-for-byte.

## Why

Each row follows from what the target can accept. What describes the source
cluster is evidence. A deploy never writes it. It applies what the
VirtualMachine refers to by name unchanged, so that the references resolve.

A bundle holds disks as bytes. A claim cannot be populated on its own. CDI's
upload works through a DataVolume. So every disk becomes one, whatever it was
on the source.

Networks and storage classes belong to the target. A deploy maps them, and
does not copy them. It clears the source cluster's own metadata. The target
mints its own of each when it creates, and refuses one that arrives carrying
a resourceVersion.

## What it costs

A VirtualMachine whose disks were plain claims gets DataVolumes on the
target. The bytes are the same, but the storage management object is
different. The deploy record says so. A reader who untars a bundle by hand
still gets no warning. Applying a StorageClass from someone else's cluster is
a bad idea. ADR-0002 accepted that cost.

## Status

Settled.
