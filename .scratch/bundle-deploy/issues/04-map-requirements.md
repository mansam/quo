# 04: Map requirements onto the target

**What to build:** A bundle's requirements — the networks and storage
classes it expects — are mapped onto the target's resources, and the
mapping is visible.

A mapping is automatic when the target has a resource of the same name: a
NetworkAttachmentDefinition of the attachment's name in the deploy
namespace for a multus network, a StorageClass of the requirement's name
for storage. Everything else is named with a flag: `--network
<network>=<attachment>` attaches one of the machine's networks to a chosen
NetworkAttachmentDefinition, namespace-qualified when it lives outside the
deploy namespace; `--storage-class <source>=<target>` places the disks of
one class, and `--storage-class *=<target>` is a catch-all for the classes
that do not match by name.

The multus `networkName` in the applied VirtualMachine carries the
mapping. A requirement that resolves to nothing refuses the deploy before
anything is created — disks are not uploaded that cannot be placed. Every
mapping, automatic or explicit, is reported in the deploy record; nothing
maps silently. The pod network is never mapped, and container disk images
are reported as requirements, never mapped and not pull-checked.

**Blocked by:** 02 (completes the storage resolution the disks
introduced; the network rewrite needs only the applied VirtualMachine).

**Status:** ready-for-agent

- [ ] A multus network auto-maps when the deploy namespace holds a
      NetworkAttachmentDefinition of the attachment's name, and the
      auto-map is reported
- [ ] `--network <network>=<attachment>` attaches a network to a chosen
      NetworkAttachmentDefinition, namespace-qualified when it lives
      outside the deploy namespace
- [ ] `--storage-class <source>=<target>` places the disks of one class,
      and `--storage-class *=<target>` covers the classes that do not
      match by name
- [ ] The applied VirtualMachine's `networkName` carries the mapping
- [ ] A requirement that resolves to nothing refuses the deploy before
      anything is created
- [ ] Every mapping, automatic or explicit, is reported in the deploy
      record
- [ ] The pod network is never mapped; container disk images are reported
      as requirements, never mapped and not pull-checked
