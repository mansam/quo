# 01: Deploy a bundle's VirtualMachine

**What to build:** A support engineer runs `quo deploy <bundle> -n
<namespace>` and the bundle's VirtualMachine appears on the target cluster,
stopped, labeled as deployed by quo, and carrying the provenance that ties
it back to the case it came from. `--name` renames it.

The applied VirtualMachine is the bundle's expanded spec — the source
cluster's own resolution of the machine — not the captured object, which
stays the declaration of record. Instancetypes, preferences and
ControllerRevisions are never created. The forced rewrites at this stage
are the name when renaming, the namespace, and the labels and annotations;
everything else is byte-for-byte. See ADR-0003 and ADR-0004.

This is the tracer bullet: it establishes the target interface — create an
object — with one real adapter and one recording fake, the deploy library
entry point the CLI wraps, and the deploy record. Refusals at this stage:
the bundle cannot be read or its format version is unknown, it holds no
expanded spec, the namespace does not exist, the VirtualMachine already
exists. Each names what it found. Bundle members are digest-verified as
they are read; one that fails is skipped and recorded.

The deploy is best-effort from the start: every attempted step lands in
the record with an outcome — created, failed, or skipped — and a reason,
the record is printed however the run ended, and the exit code is
non-zero when anything failed. Ctrl-C stops the deploy immediately and
leaves everything in place.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] `quo deploy <bundle> -n <namespace>` applies the bundle's expanded
      spec as a stopped VirtualMachine on the target
- [x] The applied VirtualMachine carries only the forced rewrites — the
      name when `--name` is given, the namespace, the labels and
      annotations — and everything else byte-for-byte
- [x] `--name` renames the VirtualMachine and forces no other rewrite
- [x] The captured VirtualMachine object, instancetypes, preferences and
      ControllerRevisions are never created
- [x] Every created object is labeled `app.kubernetes.io/managed-by: quo`
      and `quo.mansam.dev/bundle: sha256-<base64url of the manifest's
      SHA-256>`
- [x] Annotations carry the exact bundle filename, and on the
      VirtualMachine the source cluster, namespace, VirtualMachine name,
      capture time and deploy time
- [x] The namespace is required; quo never falls back to the kubeconfig's
      current namespace
- [x] quo refuses, naming what it found, when the bundle cannot be read or
      its format version is unknown, when it holds no expanded spec, when
      the namespace does not exist, and when the VirtualMachine already
      exists
- [x] Bundle members are digest-verified as they are read; one that fails
      is skipped and recorded
- [x] The deploy record lists every attempted step with its outcome —
      created, failed, or skipped — and a reason for anything not created
- [x] The record is printed however the run ended, and the exit code is
      non-zero when anything failed
- [x] Ctrl-C stops the deploy immediately and leaves everything in place

## Comments

Implemented. `quo deploy <bundle> -n <namespace> [--name NAME]` puts the
bundle's expanded spec onto the target as a stopped VirtualMachine, and
the same run works through the library entry point against the fake
target.

How the pieces landed:

- The `Target` interface sits beside `Cluster` in `pkg/cluster`, served by
  the same `KubeVirt` adapter: `GetNamespace` and `Create` are new methods
  on it, and `GetObject` is the existing read, now shared by both
  interfaces. `VirtualMachine` joined `resourceByKind` for the name-taken
  check — the first time anything reads a VirtualMachine by reference.
- `Create` finds the resource to write to from the object's own kind
  through discovery (a deferred REST mapper over the now memory-cached
  discovery client), so it needs no kind table of its own and will create
  whatever later tickets add without change.
- `bundle.Open` returns a `Reader` that hands members back one at a time,
  verifying each against its digest as it is read. A member that fails its
  digest is an error the caller acts on rather than a fatal one: a deploy
  records the skip and can go on reading what follows.
- The deploy record lives in `pkg/deploy` as `Record`/`Step` with outcomes
  created, failed, skipped. `Deploy` returns it however the run ended, and
  a `NotCreatedError` is what makes the exit code non-zero. The CLI prints
  the record to stdout and the error to stderr, so both are visible
  however the run ended. Ctrl-C is wired through `signal.NotifyContext`,
  so a cancelled run prints its record and leaves what was created in
  place.

One deliberate deviation from the spec's testing decision, flagged for
review: deploy test bundles are built through the exported `bundle.Writer`
rather than by running a capture against the existing fake cluster. The
fake lives in `pkg/capture`'s test files and cannot be imported by another
package's tests, and moving it is a large diff through the capture suite.
The writer is the code that builds real bundles, so the fixtures are still
built by it; the capture-run approach stays open to a later ticket if the
fixtures turn out to drift.

Things ticket 02 inherits:

- `bundle.Reader.Read` returns whole members as bytes, which is right for
  YAML and wrong for disks. Disk streaming wants a variant that hands
  back a reader wrapping the digest, with the mismatch surfacing at the
  end of the stream.
- `Step.Reason` is a plain string built from the underlying error. If
  skipped steps grow reasons a reader selects on (the way the capture
  record's `SkipReason` does), it wants a type.
- The record's steps are created in one place in `Deploy`; ticket 02's
  create order (DataVolumes, uploads, objects, VirtualMachine last) will
  want that spread into the ordered list of creators the spec describes.
- `Create` classifies nothing about why a create failed. The TOCTOU case —
  a name taken between the refusal check and the create — lands as a
  failed step carrying the API server's message, which is honest but
  unclassified.

Status left at `ready-for-agent`, as the export ticket before it: the
tracker's five triage labels have no value meaning "implemented".

The forced rewrites gained an eighth entry after this landed: the source
cluster's own metadata — `resourceVersion`, `uid`, `creationTimestamp`,
`generation`, `managedFields` — is cleared from the expanded spec before
it is created. The expanded spec is what the source cluster returned, so
it carries the source's bookkeeping; a target mints its own of each when
it creates, and refuses outright a create that arrives carrying a
`resourceVersion`. Where the machine came from is still said, by the
provenance annotations. The spec, ADR-0004 and the glossary entry carry
the addition.
