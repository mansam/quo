# quo

quo captures a stopped KubeVirt VirtualMachine as a diagnostic bundle. The bundle contains the VirtualMachine definition, its disks, and all the resources it depends on including storage classes and the cluster's KubeVirt CR.

## Commands

**quo export** captures a VirtualMachine to a bundle file.

```
quo export -n <namespace> <name> [-o <path>]
```

The export path may be a directory or a file path. A directory, or no `-o` at all, names the bundle after the VirtualMachine. Without flags, `quo export -n kube-system my-vm` writes `my-vm.quo` in the current directory.

A bundle contains plaintext Secrets from the source cluster.

**quo check** does a dry-run of an export.

```
quo check -n <namespace> <name>
```

**quo inspect** describes the contents of a bundle.

```
quo inspect <path>
```

It reads the bundle format, reports what the bundle contains, and lists any items that could not be exported due to RBAC limitations.

**quo deploy** puts a bundle onto a cluster as a stopped VirtualMachine.

```
quo deploy <bundle> -n <namespace> [--name <name>] [--dry-run]
     [--network <network>=<attachment>] [--storage-class <source>=<target>]
```

The deployed VirtualMachine is the source cluster's own resolution of the captured one and is left stopped; starting it is `virtctl start`. Each disk is uploaded into a DataVolume through the target cluster's CDI, which has to be installed. A storage class or a multus network maps onto the target's resource of the same name on its own; `--storage-class` and `--network` name the rest, and every mapping is reported in the deploy record.

A bundle contains plaintext Secrets, and a deploy writes them onto the target cluster as they are. The cluster you deploy onto is the trust boundary: treat it with the same care as the cluster the bundle was captured from.

Deploying one bundle twice — onto one cluster or two — produces two machines with the same identity: the same MAC addresses, firmware UUID and serial, and for Windows guests the same machine SID. On a shared network that is a duplicate machine.

**quo undeploy** takes a bundle's deployment off a cluster again.

```
quo undeploy <bundle> -n <namespace> [--dry-run]
```

It removes exactly one deployment: the objects in the namespace carrying the bundle's label, and nothing else.

## Build

```
make build
```

The binary is written to `bin/quo`. To install to the Go bin directory:

```
make install
```

## Bundle format

A bundle is an uncompressed tar. Its first member is always the manifest, which reports:

- Format version
- Provenance: where the bundle came from, the source cluster versions
- Requirements: networks, storage, and container images that would be necessary to redeploy the VM
- Capture record: All resources that quo attempted to export
- Member index: sizes and digests of all other members

Resources collected from the cluster are grouped by kind under `resources/`. Disk data is grouped under `disks/`.