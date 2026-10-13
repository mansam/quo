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

- [x] A failure to read any single object does not abort the capture
- [x] Every object quo attempted appears in the capture record with its
      disposition: carried, evidence, requirement, or skipped
- [x] Anything skipped carries a reason distinguishing at minimum **forbidden**,
      **not found**, and **not installed**
- [x] The capture record is machine-readable and lives in the manifest, so it
      cannot be separated from the bundle it describes
- [x] A reader can tell "this object did not exist" apart from "we were not
      allowed to look at it"
- [x] A capture that skipped something still succeeds, but says plainly on
      completion what it could not collect
- [x] Failures that genuinely prevent a useful bundle — the VirtualMachine
      itself being unreadable, the output path being unwritable — still abort
- [x] `quo inspect` prints the capture record, grouped so that skipped items are
      obvious rather than buried
- [x] Tests drive the fake to return forbidden and not-found for chosen objects
      and assert the resulting record, including that the capture still
      completes and the bundle is valid

## Comments

Implemented. A capture now collects what it can, records what became of every
object it asked for, and only fails over something that would leave a bundle
not worth having.

### Where the record lives

`bundle.CaptureRecord` is a list of `bundle.Attempt`, carried in the manifest
under `captureRecord` so it travels with the bundle it describes. `Export` and
`ExportToFile` return the same record, which is how the CLI says on completion
what it skipped without reading the bundle back.

`pkg/capture/collector.go` is the one place an object becomes an entry: the
collector writes an object into the bundle and records it as carried, or
records it as skipped. Nothing can now put an object in a bundle without
accounting for it.

The record is handed to the writer before the bundle is opened, because the
room reserved for the manifest is sized from what the manifest holds. That
puts a constraint on later tickets: anything that wants a record entry has to
have it before the first disk is staged.

### The reason vocabulary

`forbidden`, `not found`, `not installed`, and `unreadable` as the catch-all,
with the cluster's own words kept alongside in `message` — the missing
permission is the actionable part of a forbidden read and only the cluster
knows it. Tickets 05 and 07 share this vocabulary and will want the fourth
term as well as the three the ticket names.

`cluster` gained `ErrForbidden` and `ErrNotInstalled` beside `ErrNotFound`,
and the real adapter maps Kubernetes errors onto them. **Not installed is
decided by discovery**: a kind no resource serves reads back from the API as
the object simply not being there, so a not-found is confirmed against
discovery before it is reported as one. That costs one extra request on a path
that has already failed, and it is what makes CDI or Multus being absent
distinguishable in ticket 05 rather than a reason that can never be produced.

### Choices worth knowing about

- **A cancelled capture aborts rather than recording the rest as unreadable.**
  Best effort covers one object quo could not read, not a capture that has
  stopped asking: a record saying every remaining object was unreadable would
  describe something that did not happen.
- **A failure to *write* a readable object still aborts.** The known trigger is
  the member-path collision ticket 03 deferred — two objects of one kind and
  name from different namespaces. A bundle whose manifest disagrees with its
  own members is exactly the "not worth having" case, so it is reported rather
  than recorded.
- **Only carried and skipped are produced.** Evidence and requirement are
  defined here because the ticket names them and 05 and 06 need the shared
  vocabulary, but nothing yields them yet.
- **A leftover VirtualMachineExport is not a record entry.** The deferred
  cleanup in `Export` runs after the manifest has been written, and the record
  names objects quo tried to *collect*, not ones it created. The comment that
  promised otherwise has been corrected.

### Not covered by tests

`KubeVirt.mapReadError` and `serves` have no coverage: the main suite goes
through the fake by design, and the spec puts the real adapter in the
build-tagged cluster suite (ticket 08). The fake is driven with the sentinel
errors the adapter produces, so the two could drift.

Status left at `ready-for-agent`, as tickets 01 to 03 did: the tracker has no
label meaning "implemented".

The vocabulary in this ticket is superseded. A bundle no longer sorts what it
collects into carried objects and evidence: it holds one set of resources, and
the capture record's outcomes are collected, required and skipped. The source
cluster's expansion of the VirtualMachine is a member of its own rather than a
resource, recorded under its own kind. See
`docs/adr/0002-one-set-of-resources-in-a-bundle.md` and the rewritten sections
of `spec.md`.

The note that only carried and skipped are produced is also superseded: the
four dispositions it defined are now three outcomes.
