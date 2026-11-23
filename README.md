# quo

quo helps reproduce problems with KubeVirt virtual machines. It captures a stopped
`VirtualMachine`, its disk data, and related Kubernetes resources in a diagnostic
bundle. You can inspect the bundle locally or deploy it to a separate cluster.

Bundles are not encrypted. They can contain Kubernetes Secrets and sensitive
disk data. Restrict access to bundle files and deploy them only to trusted clusters.

## Commands

`export`, `deploy`, and `undeploy` require an explicit namespace (`-n`). Use
`--kubeconfig <path>` to specify the kubeconfig for the source or target cluster.

### `quo export`

Capture a stopped `VirtualMachine` from the source cluster.

```text
quo export -n <namespace> <name> [-o <path>] [--dry-run]
```

By default, `quo export -n workloads my-vm` writes `my-vm.quo` in the current
directory. If `-o` names an existing directory, quo writes `<name>.quo` there.
Otherwise, `-o` specifies the bundle file path.

Use `--dry-run` to preview what the export would collect, require, or skip. The
report lists provisioned disk sizes, their total, and an estimate of local
scratch space. It also reports whether the VM must be stopped before export.
A dry run does not create a `VirtualMachineExport`, transfer disks, or write a
bundle. Do not combine `--dry-run` with `-o`.

A dry run exits with status 0 if it produces a report, even if the export would
skip resources or refuse to capture a running VM. An error that prevents the
report produces a nonzero exit status.

### `quo inspect`

List a bundle's contents, target requirements, and capture record without
contacting a cluster. The capture record lists skipped resources and their reasons.

```text
quo inspect <bundle>
```

`inspect` verifies the digests of complete bundle members and lists any missing
or incomplete members. Use this command to verify a bundle before deployment.
`deploy` does not verify member digests.

### `quo deploy`

Deploy a bundle as a stopped `VirtualMachine` in the target namespace.

```text
quo deploy <bundle> -n <namespace> [--name <name>] [--dry-run]
     [--network <network>=<attachment>] [--storage-class <source>=<target>]
```

quo uses the expanded spec from the source cluster rather than resolving the
VM's instancetype and preference on the target. It uploads captured disks to
`DataVolume` objects. The target cluster needs the Containerized Data Importer
(CDI) for disk uploads.

The deploy record lists the target requirements and any mappings:

- **Storage:** quo matches storage classes by name unless you specify
  `--storage-class <source>=<target>`. Use `--storage-class '*=<target>'` for
  classes that do not match by name, including disks with no recorded class.
- **Multus:** quo looks for a `NetworkAttachmentDefinition` with the source
  attachment's name in the deployment namespace. Use
  `--network <network>=<attachment>` to override the mapping. For an attachment
  in another namespace, specify `<namespace>/<attachment>`.
- **Container disk images:** the target must be able to pull the images listed
  in the deploy record. quo does not attempt to pull them.

Preflight checks report missing storage classes, missing network attachments,
and object name conflicts before quo creates anything. Use `--dry-run` to review
planned actions and mappings without creating objects or uploading disks.

Start the deployed VM with `virtctl start -n <namespace> <name>`. Deployments
from the same bundle can share MAC addresses, firmware UUIDs, serial numbers,
and Windows machine SIDs. Use isolated networks if you need to run multiple
copies at the same time.

### `quo undeploy`

Remove a deployment using the same bundle, namespace, and target cluster.

```text
quo undeploy <bundle> -n <namespace> [--dry-run]
```

quo requests deletion of objects in the specified namespace that carry the
bundle's label. It reports whether the target accepted each request without
waiting for the objects to disappear. Use `--dry-run` to list matching objects
without requesting deletion.

## Build and install

Build the executable at `bin/quo`:

```bash
make build
```

Install to the Go binary directory:

```bash
make install
```

## Bundle format

A `.quo` bundle is an uncompressed tar file. Its first member, `manifest.yaml`,
contains:

- Format version
- Provenance: source cluster, VM name and namespace, capture time, and cluster versions
- Target requirements: networks, storage, and container images
- Capture record: the outcome of each capture attempt
- Member index: paths, sizes, and digests of the other members

The bundle stores Kubernetes resources by kind under `resources/`, disk data
under `disks/`, and the source cluster's expanded VM spec in `expanded-spec.yaml`.
Resources can include source cluster configuration such as storage classes and
the `KubeVirt` custom resource.
