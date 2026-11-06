# A dry run is a target, not a flag

A deploy or an undeploy asked for a dry run works through a target that
writes nothing. Deploy and Undeploy wrap the target they were given once,
at entry. The wrapping target's reads go to the cluster behind it. Its
writes keep nothing and report success. One write path therefore serves
a dry run and a real deploy, so the two cannot drift apart.

`Options.DryRun` and `UndeployOptions.DryRun` remain. The command layer
reads them for its wording, and nothing else.

## What this replaces

The bundle-deploy work settled dry runs as a flag. Ticket 05 of that
work, `.scratch/bundle-deploy/issues/05-dry-run.md`, gated the four
write paths on `Options.DryRun`: the DataVolume creates, the uploads,
the closure creates, the VirtualMachine. Each path carried a branch that
stopped short of the target and marked its step created. Undeploy
carried a branch of its own around its delete loop.

The ticket weighed one alternative, a wrapping target, and passed over
it. What it weighed was a target that refuses writes, which would have
blocked the reads the refusals and the mappings rest on. This target
accepts writes and lets reads through, so the objection does not reach
it. The ticket cited undeploy's own flag as precedent, which is the
feature citing itself. And it never weighed the arrangement capture had
already settled on.

Capture's check is that arrangement. `capture.Check` runs the real read
path against a sink that keeps nothing, so a check cannot write at all.
No branch in the read path knows a check is running. The `quo check`
command that wrapped it was folded into `quo export --dry-run` in commit
`45add8c`. The library entry point is intact, and the deploy side now
matches it.

## Why

Under the flag, every write path had two forms. A dry run took the
branch, stopped at the target, and asserted the outcome a real deploy
would have earned. The branch could drift from the path it
short-circuited, and nothing would notice, because no dry run ever ran
the real half.

The wrapper makes a dry run and a real deploy take the same path. The
only difference is what sits at the end of it. That is also what makes
discarding the writes safe. Deploy performs every read before any write,
and Undeploy lists before it deletes. Nothing reads back what it wrote,
so a write that keeps nothing cannot mislead a read.

The record a dry run returns is still the record a real deploy would
print. The writes report success, so each object the deploy would create
is said as created. The command says on the line before the record that
none of it happened.

The reads are delegated by name rather than by embedding. A write method
added to `cluster.Target` later cannot reach a cluster by default. It is
missing from the wrapping target, and the gap does not compile. Whoever
adds one decides what a dry run does with it.

## What it costs

A dry run no longer reads the bundle's disk bytes. It no longer reports
a disk member that fails its digest. The small members, the closure
objects and the expansion, are still read and still verified, because a
deploy reads them before it writes anything.

`quo inspect` is what verifies a bundle, and it needs no cluster. A dry
run of a large bundle stops reading and hashing every byte the bundle
holds. That is the point: it answers from the manifest and the cluster,
not from the bytes.

## Status

Settled.
