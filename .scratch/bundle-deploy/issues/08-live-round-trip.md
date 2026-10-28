# 08: Verify the round-trip on a live cluster

**What to build:** The question phase 1 left open — would a bundle
actually reproduce the VirtualMachine? — gets its test. The live suite
captures its fixture and deploys it into a second namespace on the same
cluster, then compares what the target holds against the fixture.

An object the fixture has that the target lacks is a failure, named. The
target's expand-spec of the deployed VirtualMachine is diffed against the
expansion the bundle holds; differences are reported rather than failed,
because the two resolutions differing is information.

The round-trip also settles three facts the design depends on: that the
expansion carries no instancetype or preference matchers — anything that
survives is removed, and the applied VirtualMachine names none; that
`dataVolumeTemplates` survive the expansion; and that CDI's upload accepts
the gzip and tar.gz bytes the export produced, unchanged. Everything the
round-trip creates is removed again, pass or fail, as the live suite
already does.

**Blocked by:** 02, 03, 04 (deploys disks, closure and mappings).

**Status:** ready-for-agent

- [x] The live suite captures its fixture and deploys it into a second
      namespace on the same cluster
- [x] An object the fixture has that the target lacks is a failure, named
- [x] The target's expand-spec of the deployed VirtualMachine is diffed
      against the bundle's expansion, and differences are reported rather
      than failed
- [x] The applied VirtualMachine names no instancetype or preference
      matcher, whatever the expansion carries
- [x] It is established whether `dataVolumeTemplates` survive the
      expansion, and the hoisting behaves correctly for the answer
- [x] It is verified that CDI's upload accepts the bundle's gzip and
      tar.gz bytes unchanged
- [x] Everything the round-trip creates is removed again, pass or fail

## Comments

Implemented. The live suite now takes the one capture its tests share and
deploys it into a second namespace on the same cluster, through the library
entry point with the real cluster adapter, then compares what the target
holds against the fixture — and the round trip is green on a real cluster:
this branch was built against one (KubeVirt, CDI, Multus, ODF storage;
ObjectGraph gate off, so the two traversal tests skip and everything else
runs), where the whole round trip passes in well under a minute and leaves
nothing behind.

How the pieces landed:

- The round trip is one test, `TestDeployingTheCapturedBundleReproducesTheVirtualMachine`
  in test/live/roundtrip_test.go, run linearly through its
  phases: deploy, compare, disks, machine, diff, undeploy. One test rather
  than a shared-deploy set of them, because the phases are one lifecycle —
  the undeploy at the end needs the deployment the earlier phases asserted
  on, and Go gives no order guarantee between test functions in a package.
- The second namespace is created by the test itself
  (`quo-live-<suffix>-target`), because deploying into the fixture's own
  namespace would collide with the fixture's objects and a deploy refuses
  a namespace that is not there. A kubeconfig that may not create a
  namespace skips the round trip saying so: the capture side can run
  against `QUO_LIVE_NAMESPACE`, but the round trip cannot, and neither
  says anything about quo. The namespace is deleted in a `t.Cleanup`
  however the test ends, which is the sure end; the undeploy runs inside
  the test first, because what it deletes is asserted on.
- The comparison is `deployedSet` — every object the manifest holds of a
  kind in `cluster.DeployedKinds`, under its captured name, in the target
  namespace — read back through the suite's own dynamic client, so that
  what checks quo never goes through quo. An object the fixture has that
  the target lacks is the failure, named by kind and name. The claim behind
  a disk is not in the set, because CDI makes it rather than the deploy; it
  is checked where the disks are. `deployedSet` and the spec diff below are
  pure logic and unit-tested in the live package, which runs them without
  a cluster the way difference_test.go's comparison tests do.
- The three facts, as the cluster settled them:
  - **Matchers.** The expansion the bundle held carried no instancetype or
    preference matcher — the expand-spec read leaves them off, on this
    cluster's KubeVirt at least. The design no longer depends on that
    answer: `buildVirtualMachine` now removes `spec.instancetype` and
    `spec.preference` whatever the expansion carries (a pkg/deploy change,
    with `writeExpandedBundle` parameterizing the fixture so a unit test
    covers an expansion that carries them), and the live test asserts the
    deployed VirtualMachine names none.
  - **dataVolumeTemplates.** They survive the expansion: the fixture
    VirtualMachine now declares one, and the captured expansion carried it.
    The fixture's template names the DataVolume the fixture itself creates
    — the shape of a machine that ran once and was stopped, since KubeVirt
    provisions a template's DataVolume at start and leaves it behind at
    stop, and the fixture never starts the machine. The hoisting behaved
    correctly for that answer: the applied VirtualMachine carried no
    templates, its volume stanzas were the expansion's own unchanged, and
    the claim the volume names was bound.
  - **CDI's upload.** The bundle's compressed bytes were accepted
    unchanged: the deployed DataVolume reached Succeeded, and the claim
    behind it was Bound at the recorded size in the recorded volume mode.
    The fixture's disk went round as gzip on this cluster (the log line
    says which form each run checked); the tar.gz form's pass-through is
    covered by the deploy suite's disk fixtures, and only a fixture whose
    claim holds files would exercise it live.
- The mappings: storage mapped by name (source and target are one cluster,
  so the class matches), and the multus network went through `--network`,
  because the fixture's NetworkAttachmentDefinition lives in the fixture's
  namespace and the deploy namespace holds nothing of that name — which is
  ticket 04's qualification rule meeting a real multus, as its comment
  hoped. The expand-spec diff reported exactly that: the source's expansion
  carries the bare attachment name, the target's carries the qualified one
  the deploy wrote.
- The diff itself is `differences`, a structural path-by-path walk over
  two objects, reported through `t.Log` and never failed on. It covers the
  spec alone: the metadata is rewritten by design and each cluster keeps
  its own status, and neither of those is a resolution. On the run, the
  only differences were the two the deploy itself causes — the hoisted
  `dataVolumeTemplates` and the qualified `networkName`.
- Cleanup and undeploy: the undeploy at the end deleted exactly the
  deployed set (`ConsistOf` against `deployedSet`, so a label that selected
  too much or too little fails), which is `ListByLabel` and `Delete`
  running against a real API server for the first time — both facts
  ticket 06 named for the round trip to settle came out in quo's favour:
  the dynamic client's list items carried readable kinds, and CDI took the
  claim with the deleted DataVolume. The round trip waits to see that it
  did (bounded by two minutes) and says so either way, reported rather
  than failed, because the namespace deletion behind the test is the sure
  end whatever the cluster did.
- `fixture.await` moved to the suite as `suite.await(ctx, gvr, namespace,
  name, ...)`, returning the last object it read: the round trip waits in
  a namespace the fixture does not hold, and reading the settled machine
  back once is what the assertions are made on. The fixture's two waits
  call it unchanged.

What a maintainer runs to see it green: `make test-live`, with
`QUO_LIVE_KUBECONFIG` naming a kubeconfig for a cluster with KubeVirt and
CDI (Multus optional — without it the fixture builds no secondary network
and the round trip covers one mapping fewer; the ObjectGraph gate only
gates the two traversal tests). `make check` remains the clusterless gate
and passes: the live package compiles, and its cluster-free tests — the
deployed set, the spec diff, the traversal comparison logic — run under
`-tags live` with no cluster at all.

Left open, deliberately: the round trip runs source and target on one
cluster, so "two clusters resolving differently" can only show up as the
defaults side of the diff, and on one cluster there were none beyond the
deploy's own rewrites. A two-cluster round trip is a harness question
rather than a quo one, and the diff is already built to report what it
would find.

Status left at `ready-for-agent`, as the tickets before it: the tracker's
five triage labels have no value meaning "implemented".
