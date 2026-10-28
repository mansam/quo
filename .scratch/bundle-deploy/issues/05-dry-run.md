# 05: Dry-run a deploy

**What to build:** A support engineer runs `quo deploy <bundle> -n
<namespace> --dry-run` and sees exactly what the deploy would do — the
objects that would be created, the forced rewrites, the mappings —
without anything touching the target.

Dry-run runs the complete refusal set — the bundle checks, the namespace,
the collisions, the mappings, CDI, the volumeModes — and prints the deploy
record the real deploy would print. A deploy that would be refused says
so, naming what it found. Nothing is created, nothing is uploaded, no
labels land anywhere.

**Blocked by:** 02, 03, 04 (needs the complete refusal set and the full
deploy record).

**Status:** ready-for-agent

- [x] `--dry-run` runs every refusal the real deploy runs, and a deploy
      that would be refused says so, naming what it found
- [x] It prints the deploy record the real deploy would print — the
      objects that would be created, the forced rewrites, the mappings
- [x] Nothing is created, nothing is uploaded, and no labels land
      anywhere on the target

## Comments

Implemented. `quo deploy <bundle> -n <namespace> --dry-run` runs the
complete refusal set the real deploy runs and prints the deploy record
the real deploy would print — the objects it would create, in the real
create order, and the mappings — and nothing touches the target: no
Create, no UploadDisk, no labels, no port-forward. The same runs work
through the library entry point against the fake target.

How the pieces landed:

- The mechanism is `Options.DryRun`, gating the four write paths where
  they write: `createDataVolumes`, `upload`, `createClosure` and
  `createVirtualMachine` each stop short of the target and mark their
  step created instead. A wrapping Target that refuses writes was the
  alternative and was passed over: the refusals and the mappings are
  reads — GetNamespace, GetObject, the CDI and StorageProfile lookups —
  and an options field lets those keep flowing through the real target
  untouched, which is also the pattern undeploy's own `--dry-run`
  established on `UndeployOptions`. `Deploy`'s body did not change at
  all; only its doc comment grew.
- The would-be steps carry `OutcomeCreated`, not a dry-run outcome of
  their own. The record a dry run returns is the record the real
  deploy would print — the ticket's own wording — and that choice is
  what makes the settled rules serve the dry run unchanged:
  `notCreated` counts skips and failures exactly as it does for a real
  run, so a dry run that would be refused refuses with the refusal's
  own error, and one whose would-be record holds skips or failures
  exits non-zero through `NotCreatedError`, while a clean dry run
  exits zero — the ticket's exit-code rule, falling out of the real
  deploy's own. `printRecord`'s undeploy-line check looks for a created
  step and finds one. The misreading the choice invites — steps saying
  created for objects that are not there — is answered in the CLI, not
  the library: `printRecord` prints one line ahead of the record
  ("Dry run: nothing is created, nothing is uploaded.") and varies the
  undeploy lead-in to "Remove what it would create with:", which is
  the only wording the dry run changed.
- Digest verification still reads members as the real deploy does. The
  expansion and the closure members go through `readVirtualMachine`
  and `readClosure` unchanged, ahead of any write path, so their skips
  land in the dry-run record for free. A disk member is opened through
  `OpenStream` and read to its end without being sent anywhere — the
  read is the verification, and `Mismatch` hands the verdict over the
  way it does for the uploader — so a disk that fails its digest is a
  skipped step in the dry run's record. A member that ends before it
  is whole, which no fixture produces and which the real deploy records
  as a failed upload, is recorded as failed with "the upload would
  fail", which is the honest prediction.
- The refusal set needed nothing: `validateTarget`, `resolveDisks` and
  `resolveNetworks` run ahead of the write paths, so every refusal —
  bundle readability and format version, no expanded spec, namespace
  missing, name collisions through `wouldCreate`, unresolved storage
  classes and networks, CDI absent with disks to upload, volume modes
  the mapped class cannot provide — fires under `--dry-run` exactly as
  it does without it, naming what it found, and a refused dry run
  prints no record, as a refused real deploy does not. The dry-run
  refusal table covers them all in one place, which is the ticket's
  own ask.
- The CLI is `--dry-run` on `parseDeployArgs` — naming and behavior
  matching undeploy's flag, "say what would be created and create
  nothing" — and a sentence in the usage text; `runDeploy` itself did
  not change, because `printRecord` already receives the options and
  the options already know. The upload progress reporter is wired as
  always and never called, there being no upload to report.

Things ticket 08 inherits:

- The live round-trip can dry-run a deploy against the real cluster
  before it runs one: `Options.DryRun` through the same `suite.quo`
  target, asserting the would-be record and that the second namespace
  holds nothing afterwards. The printed record and the exit code are
  the library's to produce now.
- A dry run reads the target the way a real deploy does — namespaces,
  collisions, classes, profiles, attachment definitions — so a live
  dry run is also a cheap rehearsal of every read path the real
  round-trip takes.
- The "upload would fail" reason for a member that cannot be read
  whole is untested against a real truncated bundle; if the live suite
  ever builds one, that is where its wording meets a real
  `io.ErrUnexpectedEOF`.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
