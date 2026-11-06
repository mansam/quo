# 02: Run a dry run through a target that writes nothing

**What to build:** `quo deploy <bundle> -n <namespace> --dry-run` and
`quo undeploy <bundle> -n <namespace> --dry-run` do what they do today.
Every refusal runs. The record says every object the deploy would create
and every mapping it would apply. Nothing reaches the target.

One thing changes for the person running it. A dry run no longer reads the
bundle's disk bytes, so it no longer reports a disk member that fails its
digest. A dry run of a large bundle stops reading and hashing every byte
the bundle holds. `quo inspect` is what verifies a bundle, and it verifies
one without a cluster. The small members, the closure objects and the
expansion, are still read and still verified, because a deploy reads them
before it writes anything.

Underneath, a dry run is a target rather than a flag. `Deploy` and
`Undeploy` each wrap the target they were given, once. Every write path
below them runs unchanged. The wrapping target's reads go to the real
cluster, so the refusals and the mappings still answer from it. Its writes
keep nothing and return nil. It delegates each read method by name rather
than by embedding, so a write method added to the interface later cannot
reach a cluster by default. The branches each write path carried, and the
helper that marked a step created on their behalf, are removed. One write
path serves a dry run and a real deploy, so the two cannot drift apart.

The record still says created for what a dry run would create. That is the
record a real deploy would print, which is what a dry run is for, and the
command already prints one line ahead of it saying that none of it
happened.

**Blocked by:** 01 (a target that moves no bytes has to report no
progress).

**Status:** ready-for-agent

- [x] A dry run runs every refusal a real deploy runs, and reaches the
      target only to read
- [x] A dry run prints the record the real deploy would print
- [x] A dry run reads no disk bytes
- [x] An undeploy dry run lists what it would delete and deletes nothing
- [x] Each write path has one form, which a dry run and a real deploy
      both take
- [x] ADR-0005 records the decision, what it replaces, and what it costs

## Comments

Implemented. A dry run still runs every refusal, still returns the
record the real deploy would print, and still touches nothing on the
target. An undeploy dry run still lists what it would delete. One thing
changed for the person running it: a dry run reads no disk bytes, so it
no longer reports a disk member that fails its digest.

How the pieces landed:

- A dry run is a target now, not a flag. `Deploy` and `Undeploy` wrap
  the target they were given in a `dryRunTarget` once, at entry, when
  their options ask for a dry run. The wrapper's `GetNamespace`,
  `GetObject` and `ListByLabel` go to the target it wraps, so the
  refusals and the mappings answer from the cluster. Its `Create`,
  `UploadDisk` and `Delete` keep nothing and return nil — plain nil,
  not `ctx.Err()`, because the context is irrelevant for work that
  does not happen.
- The reads are delegated by name, not by embedding. A write method
  added to `cluster.Target` later is missing from the wrapper, and the
  gap does not compile. It cannot reach a cluster by default. Whoever
  adds one decides what a dry run does with it.
- The four write-path branches, `Undeploy`'s branch and the
  `dryRunStep` helper are deleted. One write path serves a dry run and
  a real deploy, so the two cannot drift apart. What makes discarding
  the writes safe is the order the paths already kept: `Deploy`
  performs every read before any write, and `Undeploy` lists before it
  deletes. Nothing reads back what it wrote.
- The record still says created for what a dry run would create. The
  wrapper's writes report success, so `createDataVolumes`,
  `createClosure` and `createVirtualMachine` mark their steps created
  exactly as a real deploy's success does. `notCreated` and
  `printRecord`'s undeploy line therefore serve the dry run unchanged,
  and `Options.DryRun` stays for the command's wording alone.
- The dry-run test's "a disk member that fails its digest" row is
  deleted, by decision: a dry run no longer reads disk bytes, so the
  row asserted a finding the dry run can no longer make. The closure
  and expansion digest rows stay, because those members are read on
  the shared path. A new test pins the other side:
  `TestDeployDryRunReadsNoDiskBytes` gives the dry run a bundle whose
  root disk member fails its digest and asserts the run dries clean,
  every step said as created. Every other dry-run test passes
  unchanged, which is the proof the behaviour is preserved.
- `UploadDisk` on the wrapper waits for nothing and reads no bytes, so
  the progress line stays silent on a dry run. That is the property
  ticket 01 pinned against the fake's `uploadReadsNothing` mode. The
  wrapper is that mode as a target.
- ADR-0005, "A dry run is a target, not a flag", records what ticket
  05 of the bundle-deploy work decided, why this reverses it — that
  ticket weighed a target that refuses writes, cited undeploy's own
  flag as precedent, which is the feature citing itself, and never
  weighed `capture.Check`'s arrangement — and the cost: a dry run no
  longer verifies disk bytes.

Things ticket 03 inherits:

- The dry-run tests still live in `dryrun_test.go` as a parallel file.
  Folding them into the shared tables moves the corruption recipes and
  the negative assertions, not the behaviour: every dry-run test that
  passes today must pass as a row of the shared table.
- `TestDeployDryRunReadsNoDiskBytes` and the table it sits beside share
  one corruption recipe, the root disk's same-length replacement. That
  is one of the recipes the fold is for.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
