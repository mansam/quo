# 03: Fold the dry-run tests into the tables they repeat

**What to build:** Nothing changes for anyone running quo. The dry-run
tests move into the test files whose contracts they restate, as one more
dimension of the tables already there. A contract is then written once,
and a dry run and a real deploy both assert against it.

The duplication is large enough to hide a drift between the two. One
block of expected steps runs to 56 lines and appears twice, word for
word, once in the dry-run tests and once in the closure tests. Three
bundle-corruption recipes appear two and three times each. The dry-run
refusal table restates rows that four other files already hold.

What the dry-run tests say that no other test says is the negative: that
nothing was created, nothing was uploaded, and nothing was recorded
against the target. That assertion is what catches a write path that
forgets its target. It stays, on the dry-run rows of the shared tables.

**Blocked by:** 02 (the two modes have to take one write path before
their tests can share assertions).

**Status:** ready-for-agent

- [x] Each contract the dry-run tests restate is written once, and both
      modes assert against it
- [x] A dry-run row still asserts that nothing reached the target
- [x] `make check` passes, and the dry-run behaviour pinned today stays
      pinned

## Comments

Implemented. `dryrun_test.go` is gone. A dry run is a dimension of the
tables whose contracts its tests restated, held in a shared `runModes`
value beside `testOptions`: a deploy, and a dry run. Every table that
gained the dimension runs each of its rows in both modes, so a
contract is stated once and the two modes assert against it together.

How the pieces landed:

- The six refusal tables — the bundle refusals and the target refusals
  in `deploy_test.go`, the collisions in `closure_test.go`, the
  storage-class and target refusals in `disks_test.go`, the network
  refusals in `requirements_test.go` — run every row in both modes
  with no branch in the body. A refusal fires on the read path, so the
  two modes behave alike, and the folded contract says so in one
  statement: the refusal and its names, an empty record, and nothing
  reached the target. The nine rows the dry-run refusal table copied
  pass unchanged, as rows of the tables they came from. The rows it
  never copied run as dry runs too, which is new coverage, all of it
  true. What ticket 02 pinned by hand is structural now.
- The success-path tables keep a mode branch, because what the two
  modes do with the target differs. The shared part states the
  record's contract: `TestDeployAppliesTheClosure` holds the 56-line
  block of expected steps once now, and asserts the mappings beside it
  for both modes, which the real-deploy side had never stated. The
  real mode's tail asserts what arrived, byte for byte. The dry mode's
  tail asserts the negative through a shared `wroteNothing` helper:
  nothing was created, nothing was uploaded, and nothing was recorded
  against the target. `TestDeploySkipsAClosureMemberThatFailsItsDigest`
  and `TestDeployRecordsADiskTheBundleHoldsNoBytesFor` fold the same
  way. `TestDeployAppliesNoClosureForAMachineThatCannotBeCreated`
  needs no branch at all: the run ends before the first write, so both
  modes carry the negative.
- The three corruption recipes are one fixture writer each:
  `writeCorruptedClosureBundle`, `writeCorruptedExpansionBundle` and
  `writeCorruptedDiskBundle`, beside the other bundle writers in
  `fake_test.go`. The expansion writer takes the base bundle, because
  the plain fixture and the closure fixture both feed it.
- `TestDeployDryRunReadsNoDiskBytes` keeps a test of its own, moved to
  `disks_test.go` beside the real deploy's digest test it shares its
  recipe with. It says what only a dry run says, so it cannot join a
  shared contract: the same corrupted bundle the real deploy reports
  as four of five objects dries clean, every step said as created.
- The mappings rows folded into the two network tests in
  `requirements_test.go`, which gained the dimension and the record's
  `Steps` assertion the dry-run rows carried.

The proof the behaviour stayed pinned: the same assertions pass before
and after, as rows of the shared tables, and `make check` is green. As
a check on the fold's teeth, a dry-run target whose `Create` or
`UploadDisk` writes through to the cluster was tried against the tests
before and after the fold. Both times the same ground fails, now as
dry-run rows of the shared tables.

The package's test files lose 274 lines (4169 to 3895) while the count
of passing subtests rises from 104 to 120.

Judgment calls, for the record:

- `TestUndeployDryRunDeletesNothing` was left alone. It already sits in
  `undeploy_test.go` beside the tests of its contract, it is not part
  of the parallel file this ticket folds, and the duplication the
  ticket counts does not include its deletions block.
- `TestDeploySkipsAnExpandedSpecThatFailsItsDigest` did not gain the
  dimension. No dry-run test restated it; the dry-run table's
  expansion row used the closure fixture, which folded into
  `TestDeployAppliesNoClosureForAMachineThatCannotBeCreated`. It
  shares the corruption recipe and nothing else.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
