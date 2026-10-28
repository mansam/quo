# 08: Verify the round-trip on a live cluster

**What to build:** The question phase 1 left open — would a bundle
actually reproduce the VirtualMachine? — gets its test. The live suite
captures its fixture and deploys it into a second namespace on the same
cluster, then compares what the target holds against the fixture.

An object the fixture has that the target lacks is a failure, named. The
target's expand-spec of the deployed VirtualMachine is diffed against the
expansion the bundle holds; differences are reported rather than failed,
because the two resolutions differing is information.

The round-trip also settles three facts the design depends on: that the
expansion carries no instancetype or preference matchers — anything that
survives is removed, and the applied VirtualMachine names none; that
`dataVolumeTemplates` survive the expansion; and that CDI's upload accepts
the gzip and tar.gz bytes the export produced, unchanged. Everything the
round-trip creates is removed again, pass or fail, as the live suite
already does.

**Blocked by:** 02, 03, 04 (deploys disks, closure and mappings).

**Status:** ready-for-agent

- [ ] The live suite captures its fixture and deploys it into a second
      namespace on the same cluster
- [ ] An object the fixture has that the target lacks is a failure, named
- [ ] The target's expand-spec of the deployed VirtualMachine is diffed
      against the bundle's expansion, and differences are reported rather
      than failed
- [ ] The applied VirtualMachine names no instancetype or preference
      matcher, whatever the expansion carries
- [ ] It is established whether `dataVolumeTemplates` survive the
      expansion, and the hoisting behaves correctly for the answer
- [ ] It is verified that CDI's upload accepts the bundle's gzip and
      tar.gz bytes unchanged
- [ ] Everything the round-trip creates is removed again, pass or fail
