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

- [ ] A dry run runs every refusal a real deploy runs, and reaches the
      target only to read
- [ ] A dry run prints the record the real deploy would print
- [ ] A dry run reads no disk bytes
- [ ] An undeploy dry run lists what it would delete and deletes nothing
- [ ] Each write path has one form, which a dry run and a real deploy
      both take
- [ ] ADR-0005 records the decision, what it replaces, and what it costs
