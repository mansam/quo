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

- [x] The ConfigMaps, Secrets and ServiceAccounts a bundle holds are
      applied unchanged apart from the namespace and the labels and
      annotations
- [x] Every created object carries the labels and annotations
- [x] Instancetypes, preferences, ControllerRevisions,
      NetworkAttachmentDefinitions, StorageClasses, StorageProfiles,
      PersistentVolumes and the KubeVirt resource are never created
- [x] quo refuses before creating anything when any object it would
      create already exists, naming the colliding objects

## Comments

Implemented. `quo deploy` now applies the ConfigMaps, Secrets and
ServiceAccounts a bundle holds, between the uploads and the
VirtualMachine, and the same runs work through the library entry point
against the fake target.

How the pieces landed:

- Deploy decides by kind from the manifest's list of resources, and
  the applied half of ADR-0004's table is one map, `appliedKinds` in
  pkg/deploy/closure.go: ConfigMap, Secret, ServiceAccount. Everything
  else a bundle holds is read-only — the instancetypes, preferences
  and ControllerRevisions are folded into the expansion the deploy
  applies, and the NetworkAttachmentDefinitions, storage objects and
  the rest describe the source cluster — so nothing else is ever
  planned, and only what is planned is created. A closure object lands
  in the deploy record as its own step, in creation order, with the
  usual outcomes and reasons.
- Applied closure objects carry exactly the forced rewrites that apply
  to every created object: the source cluster's metadata cleared, the
  namespace, and the labels and annotations. This ticket's body says
  "apart from the namespace and the labels and annotations", but the
  spec's forced-rewrites list is the complete authority and its item 8
  applies to every object a deploy creates: the API server refuses a
  create arriving with a resourceVersion, so byte-for-byte would fail
  on a real cluster. `clearSourceMetadata` serves the closure objects
  as well as the expansion; everything else is byte-for-byte.
- The reading phase of `Deploy` was restructured around the reader's
  forward-only walk. `bundle.Reader` walks members in written order —
  resources, then the expansion, then the disks — and `Deploy` used to
  ask for the expansion first, which walked past every resource member
  and left them unreadable. The closure members are now read (and held
  in memory; they are small) before the expansion is asked for, the
  expansion before the first disk, while creation keeps the spec's
  order: DataVolumes, uploads, closure objects, VirtualMachine last.
- The collision refusal covers every object a deploy would create —
  the DataVolumes for the disks it will place, the closure objects,
  the VirtualMachine — through `wouldCreate`, and one refusal names
  every colliding object in creation order. `AlreadyExistsError` now
  carries the list. The check is made from the manifest alone, before
  any member is read, which keeps a refused run's record empty: what
  the manifest says the bundle holds is what the deploy would create,
  and a member that later fails its digest can only shrink that set,
  never grow it.
- A closure member that fails its digest is a skipped step and the run
  continues, exactly as it is for a disk; a closure create that fails
  is recorded and the run continues to the objects behind it and the
  machine. The expansion's skip still ends the run after recording the
  VirtualMachine's step — the closure objects are not applied for a
  machine that cannot be created — and a closure member that does not
  decode, or never arrived, is a refusal, as it is for the expansion.
- The fake target gained `createErrFor`, a per-object create failure,
  following `uploadErrFor`'s precedent, so a test can make one closure
  create fail while the rest proceed. The fixture bundles gained a
  shared `writeMachineBundle` builder (the disks fixtures are thin
  wrappers over it now) and a closure fixture holding the four applied
  objects alongside the read-only kinds — the declaration, the
  instancetype, the preference, the ControllerRevision, a
  NetworkAttachmentDefinition and a StorageClass — with the expanded
  spec naming the closure objects from its volumes, the way a real
  capture's machine does.

Things the tickets after this inherit:

- Ticket 04's network mapping rewrites only the applied
  VirtualMachine, which `planClosure` now guarantees is the only place
  a bundle's referenced objects land: the NetworkAttachmentDefinition
  in the bundle is never created, so the mapping is the only way the
  network resolves on the target. A kind ticket 04 wants applied joins
  `appliedKinds`.
- `wouldCreate` is the hook ticket 05's `--dry-run` wants: it already
  lists every object a deploy would create, in creation order, and
  the collision refusal is the refusal a dry run reports for a name
  that is taken.
- Ticket 06's undeploy needs no change from this ticket: the closure
  objects carry the bundle label like everything else created, so a
  delete selecting on the label covers them.
- Ticket 08's live round-trip can assert the closure objects arrive:
  the live fixture already creates a ConfigMap, a cloud-init Secret,
  a credential Secret and a ServiceAccount beside the machine, so the
  captured bundle holds them and the comparison against the target
  exercises this ticket's path end to end.
- `Step.Reason` remains a plain string, as ticket 01's inheritance
  note left it.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
