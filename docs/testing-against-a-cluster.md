# Testing against a cluster

Almost every quo test runs against a fake cluster and needs nothing but Go.
Two things that cannot be checked that way live in `test/live`, behind the
`live` build tag:

- **quo's closure traversal against KubeVirt's own.** quo traverses a
  VirtualMachine's closure itself rather than calling KubeVirt's `objectgraph`
  subresource, because that subresource sits behind a feature gate that is off
  by default. KubeVirt's server-side implementation is the specification quo's
  traversal follows, and nothing keeps quo's copy in step with the original.
  Asking both the same question about the same VirtualMachine is the only
  thing that catches KubeVirt introducing a reference type quo does not know
  about.
- **the real cluster adapter.** The everyday suite substitutes a fake for
  `cluster.Cluster`, so it never calls `pkg/cluster`. A thin adapter is
  exactly where a wrong field name or a wrong subresource path survives every
  test that does not make a real request.

The suite is excluded from the default test run. `go test ./...` does not
build it, `go vet ./...` does not see it, and local development never needs a
cluster.

## Running it

```sh
go test -tags live ./test/live/...
```

Expect it to take several minutes: it provisions a volume through CDI and
waits for a real VirtualMachineExport server to come up.

With no usable cluster, every test that needs one skips and says what the
cluster would have had to offer. Nothing fails. The comparison logic that does
not need a cluster still runs, so `-tags live` is worth running on a laptop to
check the suite still builds.

## What the cluster must have

- **KubeVirt**, new enough to serve the `objectgraph` subresource. It arrived
  in KubeVirt v1.6.0.
- **KubeVirt's `ObjectGraph` feature gate enabled.** It is Alpha and off by
  default, and it is the likeliest reason a cluster that looks suitable turns
  out not to be. Nothing in discovery reports whether it is on: virt-api
  serves the route either way and refuses the request when the gate is off, so
  the comparison finds out by asking and skips if it has to.

  ```sh
  kubectl patch kubevirt kubevirt -n kubevirt --type merge -p \
    '{"spec":{"configuration":{"developerConfiguration":{"featureGates":["ObjectGraph"]}}}}'
  ```

  On a cluster managed by the HyperConverged Cluster Operator, set it on the
  HyperConverged resource instead; patching the KubeVirt resource directly
  will be reverted.

- **CDI**, and a storage class that can provision a 1Gi volume. The fixture
  disk is blank and therefore sparse, so it compresses to very little on the
  way into a bundle, but it has to be large enough that any provisioner will
  take it.

  A class that binds on first consumer is fine. CDI would otherwise park the
  DataVolume in `WaitForFirstConsumer` until a pod mounted the claim, and the
  fixture VirtualMachine is never started, so nothing ever would; the fixture
  asks for immediate binding to get past that.
- **Multus** is optional. Without it the fixture VirtualMachine is built with
  no secondary network and the comparison covers one reference type fewer.
- **Permissions** beyond a namespace owner's: creating a namespace, reading
  cluster-scoped StorageClasses and PersistentVolumes, reading the KubeVirt
  resource, and forwarding a port to the export server's pod. quo itself is
  built to capture with namespace-level permissions alone and records what it
  was refused; the suite is not, because it has to build what it captures and
  then check that everything arrived.

## Settings

| Variable | Meaning |
| --- | --- |
| `QUO_LIVE_KUBECONFIG` | The kubeconfig to connect with. Empty means the ambient configuration the Kubernetes tooling would use. |
| `QUO_LIVE_NAMESPACE` | An existing namespace to build the fixture in. Empty means the suite creates one and removes it again. |
| `QUO_LIVE_STORAGE_CLASS` | The storage class the fixture disk is provisioned from. Empty means whichever one the cluster defaults to. |

## What it creates

One VirtualMachine and everything it refers to: a DataVolume and the claim
behind it, a ConfigMap, a cloud-init Secret, an access-credential Secret, a
ServiceAccount, an instancetype, a preference, and a
NetworkAttachmentDefinition where Multus is installed. The VirtualMachine also
names a container disk image, which nothing pulls because it is never started.

Every name is derived from one random suffix, so two runs sharing a namespace
do not collide. Everything created is removed again in the reverse order,
whether the run passed or failed. A removal that fails is reported on standard
error and does not stop the rest.

The fixture is where to go when quo gains a reference type: add the reference
to the VirtualMachine there, and the comparison covers it.

## Reading a failure

The comparison reports both directions and names the objects on each side.

**Found by the subresource and missed by quo** is a failure. KubeVirt follows
an object quo's traversal did not find, so a bundle would not carry it. Either the traversal has a gap, or KubeVirt has started following
something new.

**Found by quo and not by the subresource** is logged and is not a failure.
quo deliberately follows references KubeVirt's graph does not — a sysprep
volume's ConfigMap and Secret, the claim behind an ephemeral volume, a memory
dump's claim, the Secret a container disk is pulled with — and finding more
than the graph does is the traversal working.

An object quo found and was then not allowed to read counts as found. Being
refused the read is a different problem with a different fix, and quo records
it in the capture record rather than passing over it.

## In CI

`.github/workflows/test.yml` runs the default suite on every push and pull
request, and the live suite in a separate job that runs only when the
repository has a `KUBECONFIG` secret holding credentials for a cluster that
meets the requirements above. Where there is a cluster, the live job has to
pass. Where there is not, it does not run, and nothing about everyday local
work depends on it.
