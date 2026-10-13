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