# 04: Capture record and best-effort collection

**What to build:** A capture no longer gives up when it cannot read something.
The person capturing a bundle is frequently a namespace owner without cluster
read, and failing an hours-long capture over one forbidden object would be
unacceptable — but silently omitting it would be worse, because the support
engineer receiving the bundle could not tell an absent object from an
unreadable one.

Every object quo attempted is accounted for in a machine-readable record in the
manifest.

**Blocked by:** 03 (needs a closure to account for).

**Status:** ready-for-agent

- [ ] A failure to read any single object does not abort the capture
- [ ] Every object quo attempted appears in the capture record with its
      disposition: carried, evidence, requirement, or skipped
- [ ] Anything skipped carries a reason distinguishing at minimum **forbidden**,
      **not found**, and **not installed**
- [ ] The capture record is machine-readable and lives in the manifest, so it
      cannot be separated from the bundle it describes
- [ ] A reader can tell "this object did not exist" apart from "we were not
      allowed to look at it"
- [ ] A capture that skipped something still succeeds, but says plainly on
      completion what it could not collect
- [ ] Failures that genuinely prevent a useful bundle — the VirtualMachine
      itself being unreadable, the output path being unwritable — still abort
- [ ] `quo inspect` prints the capture record, grouped so that skipped items are
      obvious rather than buried
- [ ] Tests drive the fake to return forbidden and not-found for chosen objects
      and assert the resulting record, including that the capture still
      completes and the bundle is valid
