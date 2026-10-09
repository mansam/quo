# 03: Capture the closure

**What to build:** The bundle now contains the objects the VirtualMachine
depends on, so a support engineer does not have to go back and ask for them one
at a time. quo works out the closure itself rather than calling KubeVirt's
`objectgraph` subresource, because that subresource sits behind an Alpha feature
gate that is off by default. KubeVirt's server-side implementation is the
specification for what to traverse.

**Blocked by:** 01 (needs the bundle writer and the fake cluster).

**Status:** ready-for-agent

- [ ] Traverses VirtualMachineInstancetype, VirtualMachineClusterInstancetype,
      VirtualMachinePreference, VirtualMachineClusterPreference, DataVolume,
      ControllerRevision, ConfigMap, PersistentVolumeClaim, ServiceAccount,
      Secret and NetworkAttachmentDefinition
- [ ] Follows volumes, access credentials, and instancetype matchers including
      their status references
- [ ] An instancetype matcher naming an object that has since been deleted still
      captures the ControllerRevision, which is then the only complete record of
      what the VirtualMachine was created with
- [ ] Carried objects are written under the objects grouping as Kubernetes YAML,
      readable and applicable by hand
- [ ] Secrets are captured in full, including cloud-init user data, sysprep
      answer files and access credentials
- [ ] An object referenced from more than one place appears once in the bundle
- [ ] No VirtualMachineInstance and no Pod is sought — a stopped VirtualMachine
      has neither, and looking for them would be an error rather than an absence
- [ ] `quo inspect` lists the carried objects by kind and name
- [ ] Tests cover each reference type individually, the deleted-matcher case,
      objects referenced twice, and a VirtualMachine with an empty closure
