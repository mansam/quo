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

- [x] A multus network auto-maps when the deploy namespace holds a
      NetworkAttachmentDefinition of the attachment's name, and the
      auto-map is reported
- [x] `--network <network>=<attachment>` attaches a network to a chosen
      NetworkAttachmentDefinition, namespace-qualified when it lives
      outside the deploy namespace
- [x] `--storage-class <source>=<target>` places the disks of one class,
      and `--storage-class *=<target>` covers the classes that do not
      match by name
- [x] The applied VirtualMachine's `networkName` carries the mapping
- [x] A requirement that resolves to nothing refuses the deploy before
      anything is created
- [x] Every mapping, automatic or explicit, is reported in the deploy
      record
- [x] The pod network is never mapped; container disk images are reported
      as requirements, never mapped and not pull-checked

## Comments

Implemented. A deploy now maps the bundle's requirements onto what the
target holds — a storage class or a multus network onto the target's
resource of the same name on its own, or onto whatever the command line
names — and every mapping is reported in the deploy record.

How the pieces landed:

- `deploy.Options` carries the mappings as maps, and the command line
  fills them through `mappingFlag`, a small repeatable `flag.Value` in
  cmd/quo that parses one `<left>=<right>` pair per occurrence and
  rejects what it cannot parse. The plain flag package has no repeatable
  string flag of its own; a repeated left side keeps the last right
  side, the way a repeated plain flag keeps its last value. The flags
  sit on either side of the bundle path like every other deploy flag,
  and the usage text says what each maps.
- Storage resolution lives in `placeStorageClass`: an explicit
  `--storage-class` overrides, the target's class of the requirement's
  own name matches, and the catch-all `*` covers what does not match by
  name — a class the target does not hold, and the unnamed class, which
  names nothing to match. The mapped class is what the created
  DataVolume's `storageClassName` carries and what the volumeMode
  refusal checks against. The catch-all does not take a class the
  target holds under the requirement's own name: it catches only what
  does not match. Ticket 02's recorded gap — a claim bound to a
  hand-made volume, whose requirement records a size but no class — is
  placed by the catch-all when one is given, and stays a gap without
  one, because quo will not guess at placement.
- Network resolution lives in `pkg/deploy/requirements.go`: a multus
  network maps automatically when the deploy namespace holds a
  NetworkAttachmentDefinition of the attachment's name, and explicitly
  with `--network`, whose left side is the VirtualMachine's own name
  for the network and whose right side may carry a namespace, for a
  definition outside the deploy namespace — the target can be asked
  about a definition only where it lives, because the Target interface
  reads by reference and cannot list. The pod network is skipped
  outright: it names no object and is never mapped.
- The applied VirtualMachine's multus `networkName` carries the mapping
  through a new forced rewrite in `buildVirtualMachine`, qualified only
  when the definition lives outside the deploy namespace, because an
  unqualified name means the namespace of the VirtualMachine itself.
  The rewrite touches only the applied machine, which is built after
  the disks are already on the target. One implementation note for
  anything that rewrites an expansion in place again: this version of
  apimachinery's `NestedSlice` hands back a deep copy of what it finds,
  so `rewriteNetworks` takes the networks through `NestedFieldNoCopy`,
  and a rewrite through `NestedSlice` would silently rewrite nothing.
- Every mapping is reported: `Record` became a struct holding the
  steps and a `Requirements` list, each entry saying what the bundle
  asked for (`From`), what the target provides (`To`), and how the two
  were put together (`How` — by name, or by which flag). Container disk
  images are reported there too, with nothing mapped and a `How` that
  says the target has to be able to pull them; they are never
  pull-checked, because quo holds no registry credentials. The printed
  record gained a `Requirements` section beside the deploy record
  proper.
- A requirement that resolves to nothing is a refusal before anything
  is created, naming the requirement and what was tried, including the
  flag that would place it. `UnresolvedRequirementError` covers both
  storage and networks and replaces `MissingStorageClassError`, which
  knew only the by-name case; the by-name refusal keeps saying the
  class and now says the remedy too.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".

Things the next tickets inherit:

- The record is a struct now, not a slice of steps. Ticket 03's closure
  objects append to `record.Steps`, and ticket 05's dry run prints a
  record that carries `Requirements` as well as `Steps` — the mappings
  are exactly what a dry run is for checking before anything is
  created.
- The resolution phase runs between the disk planning and the creates:
  `resolveDisks` (storage classes, CDI, volume modes) then
  `resolveNetworks`. Ticket 03's collision refusal slots into
  `validateTarget`, and whatever else refuses up front goes in the same
  stretch before `createDataVolumes`.
- The fake target's `hold` now serves NetworkAttachmentDefinitions and
  storage classes of any name, and the fixture builders cover the
  requirement shapes a real capture records: a networks fixture
  (pod + multus + attachment-less multus), a container-disk fixture,
  and `writeStorageFixtureBundle`, which takes its requirements — the
  unnamed class included — as arguments.
- Ticket 08's live round-trip can drive a deploy whose classes and
  networks do not match by name through `--storage-class` and
  `--network`, and should: the round-trip is where the qualification
  rule — bare inside the deploy namespace, qualified outside — meets a
  real multus.
