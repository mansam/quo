# 05: Capture evidence about the source environment

**What to build:** The bundle now carries objects that explain the environment
the VirtualMachine came from, rather than objects anything will recreate.
Platform configuration is often where the problem actually is — a CSI parameter,
a volume binding mode, a feature gate — and a support engineer who has to go
back and ask for it has lost a day.

Evidence is stored apart from carried objects, so nothing suggests it should be
applied to a target.

**Blocked by:** 03 (needs the traversal to know which claims exist), 04 (nearly
all of this is cluster-scoped, so it is exactly where permission failures land;
without best-effort collection the first forbidden StorageClass aborts the
capture).

**Status:** ready-for-agent

- [x] Captures the StorageClass behind each PersistentVolumeClaim
- [x] Captures the CDI StorageProfile for each of those StorageClasses, which is
      what actually decided the volume mode and access modes a disk got
- [x] Captures the bound PersistentVolume for each claim, showing real capacity,
      CSI driver and volume attributes rather than what was requested
- [x] Captures KubeVirt's own configuration, above all its enabled feature gates
- [x] Calls the expand-spec subresource and records the result as evidence
- [x] quo does **not** use the expanded form to rewrite the VirtualMachine; it
      is captured because expansion is version-dependent and a difference
      between the source cluster's resolution and the target's is a plausible
      cause of the problem being investigated
- [x] Evidence is stored separately from carried objects in the bundle and
      distinguished in the manifest
- [x] CDI not being installed is recorded as **not installed** in the capture
      record, not treated as an error
- [x] Forbidden cluster-scoped reads are recorded and the capture proceeds
- [x] `quo inspect` presents evidence distinctly from carried objects
- [x] Tests cover each evidence source, CDI being absent, and a namespace-scoped
      caller who can read none of it

## Comments

The vocabulary in this ticket is superseded. A bundle no longer sorts what it
collects into carried objects and evidence: it holds one set of resources, and
the capture record's outcomes are collected, required and skipped. The source
cluster's expansion of the VirtualMachine is a member of its own rather than a
resource, recorded under its own kind. See
`docs/adr/0002-one-set-of-resources-in-a-bundle.md` and the rewritten sections
of `spec.md`.

The acceptance item "`quo inspect` presents evidence distinctly from carried
objects" no longer applies: there is nothing to present distinctly. What
replaced it is that inspect names the member holding the expanded spec.
