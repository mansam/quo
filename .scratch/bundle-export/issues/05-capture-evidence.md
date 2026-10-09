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

- [ ] Captures the StorageClass behind each PersistentVolumeClaim
- [ ] Captures the CDI StorageProfile for each of those StorageClasses, which is
      what actually decided the volume mode and access modes a disk got
- [ ] Captures the bound PersistentVolume for each claim, showing real capacity,
      CSI driver and volume attributes rather than what was requested
- [ ] Captures KubeVirt's own configuration, above all its enabled feature gates
- [ ] Calls the expand-spec subresource and records the result as evidence
- [ ] quo does **not** use the expanded form to rewrite the VirtualMachine; it
      is captured because expansion is version-dependent and a difference
      between the source cluster's resolution and the target's is a plausible
      cause of the problem being investigated
- [ ] Evidence is stored separately from carried objects in the bundle and
      distinguished in the manifest
- [ ] CDI not being installed is recorded as **not installed** in the capture
      record, not treated as an error
- [ ] Forbidden cluster-scoped reads are recorded and the capture proceeds
- [ ] `quo inspect` presents evidence distinctly from carried objects
- [ ] Tests cover each evidence source, CDI being absent, and a namespace-scoped
      caller who can read none of it
