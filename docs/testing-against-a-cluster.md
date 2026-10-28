# Testing against a cluster

Almost every quo test runs against a fake cluster and needs nothing but Go.
There are three things a fake cluster cannot check. They live in `test/live`,
behind the `live` build tag:

- **quo's closure traversal against KubeVirt's own.** quo traverses a
  VirtualMachine's closure itself. It does not call KubeVirt's `objectgraph`
  subresource, because that subresource sits behind a feature gate that is off
  by default. KubeVirt's server-side implementation is the specification
  quo's traversal follows. Nothing keeps quo's copy in step with the original.
  KubeVirt can introduce a reference type that quo does not know. Asking both
  the same question about the same VirtualMachine is the only thing that
  catches it.
- **the real cluster adapter.** The everyday suite substitutes a fake for
  `cluster.Cluster`, so it never calls `pkg/cluster`. A thin adapter is
  exactly where a wrong field name or a wrong subresource path survives every
  test that does not make a real request.
- **the round trip.** The suite captures its fixture into a bundle and deploys
  it into a second namespace on the same cluster. It compares what the target
  holds against the fixture. Phase 1 left open whether a bundle actually
  reproduces the VirtualMachine, and only a real cluster answers it. The
  question asks whether CDI's upload accepts the export's compressed bytes
  unchanged, whether the deploy applies its rewrites correctly, and whether
  the mappings resolve. The suite diffs the target's own expand-spec of the
  deployed machine against the bundle's expansion. It reports the
  differences, because two clusters resolving the machine differently is
  information.

The default test run excludes the suite. `go test ./...` does not build it,
`go vet ./...` does not see it, and local development never needs a cluster.

## Running it

```sh
go test -tags live ./test/live/...
```

Expect it to take several minutes. It provisions a volume through CDI. It
waits for a real VirtualMachineExport server to start. It then deploys the
captured bundle back onto the cluster through CDI's upload. It removes the
deployment after that.

With no usable cluster, every test that needs one skips. The skip message
says what the test needs from the cluster. Nothing fails. The comparison
logic that does not need a cluster still runs. So `-tags live` is worth
running on a laptop to check that the suite still builds.

## What the cluster must have

- **KubeVirt**, new enough to serve the `objectgraph` subresource. It arrived
  in KubeVirt v1.6.0.
- **KubeVirt's `ObjectGraph` feature gate enabled.** The gate is Alpha and off
  by default. It is the likeliest reason a cluster that looks suitable is not
  suitable. Nothing in discovery reports whether it is on. virt-api serves the
  route either way and refuses the request when the gate is off. So the
  comparison asks, and skips if it has to.

  ```sh
  kubectl patch kubevirt kubevirt -n kubevirt --type merge -p \
    '{"spec":{"configuration":{"developerConfiguration":{"featureGates":["ObjectGraph"]}}}}'
  ```

  On a cluster managed by the HyperConverged Cluster Operator, set it on the
  HyperConverged resource instead. A direct patch to the KubeVirt resource
  will be reverted.

- **CDI**, and a storage class that can provision a 1Gi block volume. The
  fixture disk is blank and therefore sparse. It compresses to very little on
  the way into a bundle. But it has to be large enough that any provisioner
  will take it.

  A class that binds on first consumer is fine. CDI would otherwise park the
  DataVolume in `WaitForFirstConsumer` until a pod mounted the claim. The
  fixture VirtualMachine is never started, so nothing would ever mount the
  claim. The fixture asks for immediate binding to avoid that.

  The round trip covers the gzip form alone. The export produces a tar.gz of
  a filesystem volume. On a CDI running under OpenShift's restricted security
  context, that tar.gz cannot be uploaded again. The export tars the volume's
  own root entry. The upload's untar cannot apply that entry's mode to the
  destination volume. The deploy suite's disk fixtures cover the tar.gz
  bytes' pass-through instead.
- **Multus** is optional. Without it, the suite builds the fixture
  VirtualMachine with no secondary network. The comparison covers one
  reference type fewer.
- **Permissions** beyond a namespace owner's. The suite needs them to create
  a namespace and to forward a port to the export server's pod. It needs them
  to read cluster-scoped StorageClasses and PersistentVolumes. It needs them
  to read the KubeVirt resource. quo itself needs only namespace-level
  permissions to capture, and it records what the cluster refused. The suite
  needs the extra permissions because it has to build what it captures and
  then check that everything arrived.

  The round-trip test needs a namespace of its own to deploy into.
  Deploying into the fixture's namespace would collide with the fixture. The
  test therefore always creates a namespace and removes it again. A kubeconfig
  that may not create a namespace runs the capture side of the suite. It
  skips the round trip and says so.

## Settings

| Variable | Meaning |
| --- | --- |
| `QUO_LIVE_KUBECONFIG` | The kubeconfig to connect with. Empty means the ambient configuration the Kubernetes tooling would use. |
| `QUO_LIVE_NAMESPACE` | An existing namespace to build the fixture in. Empty means the suite creates one and removes it again. |
| `QUO_LIVE_STORAGE_CLASS` | The storage class that provisions the fixture disk. Empty means whichever one the cluster defaults to. |

## What it creates

The suite creates one VirtualMachine and everything it refers to. That
includes a DataVolume and the claim behind it, a ConfigMap, a cloud-init
Secret, an access-credential Secret, and a ServiceAccount. It also includes
an instancetype, a preference, and a NetworkAttachmentDefinition where Multus
is installed. The VirtualMachine also names a container disk image. Nothing
pulls that image, because the VirtualMachine is never started.

The round-trip test then deploys the captured bundle of that fixture into a
second namespace. The deployed objects are the VirtualMachine, its disk as a
DataVolume populated through CDI's upload, and the closure objects. The test
removes the deployment again with quo's own undeploy. That is also the first
place the undeploy runs against a real API server. The test removes the
namespace after that. That is the sure end, however the test went.

Every name comes from one random suffix. Two runs that share a namespace
therefore do not collide. The suite removes everything it created in the
reverse order, whether the run passed or failed. The suite reports a failed
removal on standard error. The failure does not stop the rest.

When quo gains a reference type, extend the fixture. Add the reference to the
VirtualMachine there. The comparison covers it.

## Reading a failure

The comparison reports both directions and names the objects on each side.

**Found by the subresource and missed by quo** is a failure. KubeVirt follows
an object that quo's traversal did not find. A bundle would therefore not
carry it. Either the traversal has a gap, or KubeVirt follows something new.

**Found by quo and not by the subresource** is logged and is not a failure.
quo deliberately follows references that KubeVirt's graph does not follow.
Examples are a sysprep volume's ConfigMap and Secret, the claim behind an
ephemeral volume, and a memory dump's claim. quo also follows the Secret for
pulling a container disk. Finding more than the graph does is the traversal
working.

An object that quo found and was then not allowed to read still counts as
found. A refused read is a different problem with a different fix. quo
records it in the capture record instead of skipping it.

**The round trip** fails when the deploy did not create everything it tried.
It also fails when the target lacks an object of a kind that a deploy creates
and that the fixture has. The failure names the object. The expand-spec
differences are logged and are not failures. The two clusters resolving the
machine differently is information. That is above all true where the deploy's
own rewrites appear (the hoisted dataVolumeTemplates, the qualified network
name).

## In CI

`.github/workflows/test.yml` runs the default suite on every push and pull
request. It runs the live suite in a separate job. That job runs only when
the repository has a `KUBECONFIG` secret. The secret holds credentials for a
cluster that meets the requirements above.

Where there is a cluster, the live job has to pass. Where there is not, the
job does not run. Nothing about everyday local work depends on it.
