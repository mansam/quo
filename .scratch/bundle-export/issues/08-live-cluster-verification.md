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

- [ ] A build-tagged suite, excluded from the default test run, so local
      development never needs a cluster
- [ ] Asserts that quo's traversal produces the same object set as the
      `objectgraph` subresource for the same VirtualMachine
- [ ] Reports a clear, actionable difference when the sets diverge, naming the
      objects on each side rather than just failing
- [ ] Captures a real VirtualMachine end to end through a real
      VirtualMachineExport, and verifies the resulting bundle
- [ ] Skips with an explanatory message — naming the `ObjectGraph` feature gate
      that must be enabled — when no suitable cluster is available, rather than
      failing
- [ ] Documents how to run it and what the cluster must have
- [ ] Runs in CI where a cluster is available, and is not required to pass for
      everyday local work
