# 03: Deploy the closure

**What to build:** The ConfigMaps, Secrets and ServiceAccounts a bundle
holds are applied unchanged — byte-for-byte apart from the namespace and
the labels and annotations — so that the machine's cloud-init, sysprep and
access credentials are present when it is examined.

Everything else a bundle holds stays read-only: instancetypes,
preferences, ControllerRevisions, NetworkAttachmentDefinitions,
StorageClasses, StorageProfiles, PersistentVolumes and the KubeVirt
resource are never written. A NetworkAttachmentDefinition is mapped, not
copied; the mapping flags come in ticket 04.

The collision refusal extends to every object deploy would create: if any
of them already exists, quo refuses before creating anything, naming the
colliding objects. Someone else's work is neither overwritten nor built
around.

**Blocked by:** 01 (needs the deploy loop, the target interface and the
deploy record).

**Status:** ready-for-agent

- [ ] The ConfigMaps, Secrets and ServiceAccounts a bundle holds are
      applied unchanged apart from the namespace and the labels and
      annotations
- [ ] Every created object carries the labels and annotations
- [ ] Instancetypes, preferences, ControllerRevisions,
      NetworkAttachmentDefinitions, StorageClasses, StorageProfiles,
      PersistentVolumes and the KubeVirt resource are never created
- [ ] quo refuses before creating anything when any object it would
      create already exists, naming the colliding objects
