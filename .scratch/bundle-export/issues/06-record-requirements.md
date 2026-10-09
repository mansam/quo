# 06: Record requirements

**What to build:** The manifest declares what a target cluster would have to
provide for this bundle: the networks it expects, the storage classes it
expects and what each disk needs from them, and the container disk images it
references. A support engineer can then tell, before committing to a large
transfer, whether their cluster can satisfy it.

Nothing in this phase consumes these records. They are written at capture
because the information is free while connected to the source cluster and
expensive to reconstruct afterwards.

**Blocked by:** 03 (needs the traversal to find the networks and claims).

**Status:** ready-for-agent

- [ ] The manifest declares the networks the VirtualMachine expects
- [ ] The manifest declares the storage classes it expects, with the size and
      volume mode each disk needs, so a candidate can be checked rather than
      guessed at
- [ ] The manifest declares container disk images the VirtualMachine references
- [ ] Container disk images are recorded as requirements and never repackaged
      into the bundle
- [ ] Requirements are derived from the VirtualMachine and its claims, so this
      works whether or not the evidence in ticket 05 could be collected
- [ ] Requirements appear in the capture record with the requirement disposition
- [ ] `quo inspect` prints the requirements without extracting or downloading
      any disk — reading only the leading manifest
- [ ] Tests cover a VirtualMachine with multiple networks, a container disk, and
      disks with differing volume modes
