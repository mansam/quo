# 01: Capture a stopped VirtualMachine into a bundle

**What to build:** A product team engineer runs `quo export -n <namespace> <name>
-o <path>` and gets a bundle file containing the VirtualMachine. They run `quo
inspect <path>` and see what is in it, without needing a cluster. If the
VirtualMachine is running, quo refuses and tells them to stop it.

This is the tracer bullet: it establishes the bundle format, the cluster
interface and its fake, and both commands. No disks and no closure yet — those
are later tickets. The read-back through `inspect` is the round-trip check that
substitutes for the deploy path phase 1 does not have.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] `quo export` writes a tar whose **first** member is the manifest
- [ ] The manifest is YAML and records the format version, provenance (source
      cluster, namespace, VirtualMachine name, capture time, KubeVirt and
      Kubernetes versions), and an index of every member with a SHA-256 digest
- [ ] The VirtualMachine is stored as YAML exactly as read, with no
      normalisation; MAC addresses, firmware UUID, serial and chassis values are
      untouched
- [ ] YAML marshalling goes through the Kubernetes YAML library, so a version
      string is not reinterpreted as a number and a bare `no` does not become
      boolean false
- [ ] `quo export` refuses a running VirtualMachine, names it, tells the user to
      stop it, exits non-zero, and writes no bundle
- [ ] A capture that fails part-way leaves no bundle file behind
- [ ] `quo inspect` prints format version, provenance and members without
      contacting a cluster
- [ ] `quo inspect` verifies every member digest and reports a mismatch
- [ ] `quo inspect` refuses a bundle whose format version it does not understand
- [ ] Everything quo needs from outside the process sits behind a single
      interface, with one fake implementation for tests — including the future
      disk stream, so no test needs an HTTP server
- [ ] Tests run through the library entry point against the fake and assert on
      the produced bundle, never on traversal or writer internals
- [ ] Tests use gomega, are table-driven with `name` as the first field, and
      have names that read as specifications
