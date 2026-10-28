# quo

quo captures a stopped KubeVirt VirtualMachine as a diagnostic bundle. The bundle contains the VirtualMachine definition, its disks, and all the resources it depends on. That includes storage classes and the cluster's KubeVirt CR.

## Commands

**quo export** captures a VirtualMachine to a bundle file.

```
quo export -n <namespace> <name> [-o <path>]
```

The export path may be a directory or a file path. quo names the bundle after the VirtualMachine when the path is a directory or when there is no `-o`. Without flags, `quo export -n kube-system my-vm` writes `my-vm.quo` in the current directory.

A bundle contains plaintext Secrets from the source cluster.

**quo check** does a dry-run of an export.

```
quo check -n <namespace> <name>
```

**quo inspect** describes the contents of a bundle.

```
quo inspect <path>
```

It reads the bundle format. It reports what the bundle contains. It lists any items that quo could not export because of RBAC limitations.

**quo deploy** puts a bundle onto a cluster as a stopped VirtualMachine.

```
quo deploy <bundle> -n <namespace> [--name <name>] [--dry-run]
     [--network <network>=<attachment>] [--storage-class <source>=<target>]
```

The deployed VirtualMachine is the source cluster's own resolution of the captured one. The deploy leaves it stopped. Start it with `virtctl start`. The deploy uploads each disk into a DataVolume through the target cluster's CDI. The target cluster must have CDI installed.

A storage class or a multus network maps onto the target's resource of the same name on its own. `--storage-class` and `--network` name the rest. The deploy record reports every mapping.

A bundle contains plaintext Secrets. A deploy writes them onto the target cluster as they are. The cluster you deploy onto is the trust boundary. Treat it with the same care as the source cluster.

Deploying one bundle twice produces two machines with the same identity. This is true whether both deploys go to one cluster or to two. The machines have the same MAC addresses, firmware UUID and serial. Windows guests also have the same machine SID. On a shared network, that is a duplicate machine.

**quo undeploy** removes a bundle's deployment from a cluster.

```
quo undeploy <bundle> -n <namespace> [--dry-run]
```

It removes exactly one deployment: the objects in the namespace carrying the bundle's label, and nothing else.

## Build

```
make build
```

The build writes the binary to `bin/quo`. To install to the Go bin directory:

```
make install
```

## Bundle format

A bundle is an uncompressed tar. Its first member is always the manifest, which reports:

- Format version
- Provenance: where the bundle came from, the source cluster versions
- Requirements: networks, storage, and container images that would be necessary to redeploy the VM
- Capture record: all resources that quo attempted to export
- Member index: sizes and digests of all other members

quo groups the resources it collects by kind under `resources/`. The bundle groups disk data under `disks/`.
