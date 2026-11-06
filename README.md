# quo

quo captures a stopped KubeVirt VirtualMachine as a diagnostic bundle. The bundle contains the VirtualMachine definition, its disks, and all the resources it depends on. That includes storage classes and the cluster's KubeVirt CR.

## Commands

**quo export** captures a VirtualMachine to a bundle file.

```
quo export -n <namespace> <name> [-o <path>] [--dry-run]
```

The export path may be a directory or a file path. quo names the bundle after the VirtualMachine when the path is a directory or when there is no `-o`. Without flags, `quo export -n kube-system my-vm` writes `my-vm.quo` in the current directory.

`--dry-run` says what a capture would collect and what it would skip, and transfers nothing. It creates no VirtualMachineExport, so permission to create one is the one thing it does not test. It reports every disk with its size, the local scratch space a capture would need, and the total it would transfer. A VirtualMachine that is not stopped is reported as a refusal, with what a capture would collect once it is stopped. A dry run writes no bundle, so `-o` and `--dry-run` together are refused. It exits zero when it could report, even when it found objects that would be skipped. It exits non-zero only when it could not report at all.

A bundle contains plaintext Secrets from the source cluster.

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

The namespace is required. quo never falls back to the kubeconfig's current namespace, because a diagnostic VirtualMachine landing in someone's default namespace by accident is a quiet mistake.

The deployed VirtualMachine is the source cluster's own resolution of the captured one. The deploy leaves it stopped. Start it with `virtctl start`. The deploy uploads each disk into a DataVolume through the target cluster's CDI. The target cluster must have CDI installed.

A storage class or a multus network maps onto the target's resource of the same name on its own. `--storage-class <source>=<target>` places the disks of one source class onto a target class. `--storage-class *=<target>` places every class that does not match by name, the unnamed class included. `--network <network>=<attachment>` attaches one of the machine's networks to a NetworkAttachmentDefinition of the target's. Name the attachment with its namespace when it lives outside the deploy namespace.

The deploy record reports every mapping. A requirement that maps to nothing refuses the deploy before anything is created. Container disk images are reported as requirements and never pull-checked.

`--dry-run` runs every refusal a deploy runs and prints the record a real deploy would print, the mappings included. Nothing is created and nothing is uploaded.

A bundle contains plaintext Secrets. A deploy writes them onto the target cluster as they are. The cluster you deploy onto is the trust boundary. Treat it with the same care as the source cluster.

Deploying one bundle twice produces two machines with the same identity. This is true whether both deploys go to one cluster or to two. The machines have the same MAC addresses, firmware UUID and serial. Windows guests also have the same machine SID. On a shared network, that is a duplicate machine.

**quo undeploy** removes a bundle's deployment from a cluster.

```
quo undeploy <bundle> -n <namespace> [--dry-run]
```

It removes exactly one deployment: the objects in the namespace carrying the bundle's label, and nothing else. The namespace is required, for the same reason as a deploy's. quo undeploy does not wait for deletions to finish. A deletion that hangs is what `kubectl` is for. `--dry-run` says what it would remove and removes nothing.

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
