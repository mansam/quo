# 08: Live-cluster traversal verification

**What to build:** A test suite that runs against a real KubeVirt cluster and
checks two things the fake-based tests cannot: that quo's own closure traversal
finds the same objects as KubeVirt's `objectgraph` subresource, and that the
real cluster adapter works against a real API.

This matters because the everyday tests deliberately bypass the adapter, and
thin adapters are exactly where wrong field names and wrong subresource paths
live. It is also the only thing that will catch KubeVirt introducing a reference
type quo does not know about — the upstream implementation is maintained, quo's
copy of its logic is not.

**Blocked by:** 02 (exercises a real export), 03 (the traversal being compared).

**Status:** ready-for-agent

- [x] A build-tagged suite, excluded from the default test run, so local
      development never needs a cluster
- [x] Asserts that quo's traversal produces the same object set as the
      `objectgraph` subresource for the same VirtualMachine
- [x] Reports a clear, actionable difference when the sets diverge, naming the
      objects on each side rather than just failing
- [x] Captures a real VirtualMachine end to end through a real
      VirtualMachineExport, and verifies the resulting bundle
- [x] Skips with an explanatory message — naming the `ObjectGraph` feature gate
      that must be enabled — when no suitable cluster is available, rather than
      failing
- [x] Documents how to run it and what the cluster must have
- [x] Runs in CI where a cluster is available, and is not required to pass for
      everyday local work

## Comments

The two object sets are compared in both directions and both are reported, but
only one direction fails the suite: an object KubeVirt's graph names and quo's
traversal missed. The other direction is logged and allowed.

This is not a weakening of the ticket. Reading KubeVirt's implementation,
`addVolumeGraph` follows a volume's `persistentVolumeClaim`, `dataVolume`,
`configMap`, `secret`, `serviceAccount` and cloud-init secret references and
nothing else, while quo also follows a sysprep volume's ConfigMap and Secret,
the claim behind an ephemeral volume, a memory dump's claim, and the Secret a
container disk is pulled with. quo also treats an instancetype matcher naming
no kind as the cluster-scoped one and accepts the plural resource name, where
KubeVirt's `getInstanceTypeMatcherResource` emits no node for either. Strict
equality would therefore fail on any VirtualMachine using one of those, for a
traversal that is working.

What the ticket is for is caught by the direction that does fail: KubeVirt
introducing a reference type quo does not know about shows up as an object the
subresource found and quo did not.

What the suite does not cover. The fixture is built rather than found, so it
can only exercise reference types someone has added to it, and today it has
none of the shapes the two implementations disagree about — no sysprep volume,
no ephemeral volume, no memory dump, no container disk pull secret, and two
matchers that each name a singular kind. It also has no cluster-scoped
instancetype or preference, so `VirtualMachineClusterInstancetype` and
`VirtualMachineClusterPreference` are traversed only in the fake-based suite;
covering them needs a second VirtualMachine, since one has a single matcher of
each sort. Nor does it have persistent TPM or EFI, so the backend storage claim
KubeVirt's `storageutils.GetVolumes` yields under `WithAllVolumes` — which quo
does not follow — goes unexercised.

Because the fixture avoids every divergent shape, strict equality would in fact
pass against it today. It is not asserted anyway: the fixture is where someone
adds a reference type when quo gains one, and the first person to add a sysprep
volume would get a failure for a traversal that is working.
