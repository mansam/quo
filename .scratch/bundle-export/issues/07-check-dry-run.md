# 07: `quo check` dry run

**What to build:** A product team engineer runs `quo check -n <namespace>
<name>` and sees what a capture would collect and what it would skip, without
transferring a byte of disk. A permissions problem is then found in seconds
rather than discovered at the end of an hours-long transfer.

**Blocked by:** 04 (shares the disposition and reason vocabulary), 05 (the
cluster-scoped evidence is the most likely thing to be unreadable, so a check
that omitted it would miss the common failure).

**Status:** ready-for-agent

- [x] `quo check` reports every object a capture would collect, with the
      disposition it would get
- [x] It reports everything that would be skipped, with the same reason
      vocabulary as the capture record — forbidden, not found, not installed
- [x] It transfers no disk data and creates no VirtualMachineExport
- [x] It reports the local scratch space a capture would need, which is the
      largest single disk rather than the total
- [x] It reports the total provisioned disk size, so the engineer knows the
      scale of what they are about to transfer
- [x] It reports whether the VirtualMachine is currently running, since that
      would make a capture refuse
- [x] It exits zero when it could reach the cluster and report, even if it found
      things that would be skipped; non-zero only when it could not report at
      all
- [x] Output is readable by a person at a terminal, not only machine-parseable
- [x] Tests cover a clean check, a check with forbidden evidence, and a check
      against a running VirtualMachine

## Comments

The vocabulary in this ticket is superseded. A bundle no longer sorts what it
collects into carried objects and evidence: it holds one set of resources, and
the capture record's outcomes are collected, required and skipped. The source
cluster's expansion of the VirtualMachine is a member of its own rather than a
resource, recorded under its own kind. See
`docs/adr/0002-one-set-of-resources-in-a-bundle.md` and the rewritten sections
of `spec.md`.
