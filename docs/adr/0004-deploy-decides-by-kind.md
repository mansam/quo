# Deploy decides by kind

ADR-0002 settled that a bundle holds one set of resources and says nothing
about which of them a target would recreate: "a deploy decides by kind."
This records what the deploy decided.

## The table

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

## The forced rewrites

The table says what is created; the rewrites say what is changed. The
complete list:

1. the VirtualMachine's name, when renaming;
2. the namespace of every namespaced object;
3. `storageClassName` on every created DataVolume, from the mapping;
4. the DataVolume source, always `upload`;
5. the multus `networkName`, from the mapping;
6. `spec.dataVolumeTemplates` removed from the applied VirtualMachine;
7. the labels and annotations on every created object;
8. the source cluster's own metadata — the `resourceVersion`, `uid`,
   `creationTimestamp`, `generation` and `managedFields` — removed, because
   the target assigns its own of each when it creates and refuses outright
   a create that arrives carrying a `resourceVersion`.

Everything else is applied byte-for-byte.

## Why

Each row follows from what the target can accept. What describes the source
cluster is evidence and is never written. What the VirtualMachine refers to
by name is applied unchanged so that the references resolve. Disks are held
as bytes, and a claim cannot be populated on its own — CDI's upload works
through a DataVolume — so every disk becomes one, whatever it was on the
source. Networks and storage classes belong to the target and are mapped,
not copied. The metadata the source cluster assigned itself is cleared,
because the target mints its own of each when it creates — and refuses one
that arrives carrying a resourceVersion.

## What it costs

A VirtualMachine whose disks were plain claims gets DataVolumes on the
target: the bytes are the same, the storage management object is not, and
the deploy record says so. A reader untarring a bundle by hand still gets no
warning that applying a StorageClass from someone else's cluster is a bad
idea; that cost was accepted in ADR-0002.

## Status

Settled.
