# 01: Say what an upload is waiting for

**What to build:** A support engineer watching a deploy can tell a cluster
still provisioning storage from a transfer that is moving. Today the
progress line says `0 B` and does not change for as long as CDI takes to
bind the claim and place the upload pod, which is bounded only by the
ten-minute readiness timeout. The line says what is happening instead:
that the upload is waiting, and what the DataVolume's phase is while it
waits. Once bytes move, the line counts bytes.

Each report follows something that happened rather than something the code
assumed. The target reports that it is waiting, because the target is what
waits. The reader the bytes pass through reports how many have passed,
because it is what sees them. The two calls that announced a transfer at
its start and at its end, neither of which observed anything, are removed.

`UploadDisk` takes its arguments as one value, which carries the callback
the target calls while it waits:

```go
// Upload is one disk's transfer into the claim a DataVolume creates.
type Upload struct {
	Namespace string
	Name      string
	Content   io.Reader
	Size      int64
	// Waiting, when set, is called each time the target checks whether
	// the claim is ready. said is what the target is waiting on, in
	// words a person can read. No bytes have moved.
	Waiting func(said string)
}
```

A disk's progress is one of three states rather than a done flag: waiting,
transferring, done. The reader reports transferring on its interval and
reports done at the end of the stream. A member that fails its digest is
reported in place of the end of its stream, so it is never reported done.

A progress line that never reaches done is the reporter's to close. It
writes the line ending when it is asked to report a different disk, and
once more when the run ends. The deploy record then never prints onto the
end of an unfinished line.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] A deploy waiting for CDI says so, and says what the DataVolume's
      phase is, for as long as it waits
- [x] A progress line counts bytes only once bytes have moved
- [x] A disk whose upload fails part-way leaves no unfinished progress
      line for the deploy record to print onto
- [x] A target that transfers nothing reports nothing

## Comments

Implemented. A deploy that waits for CDI says so on the progress line,
and says what the DataVolume's phase is, once per readiness check, for
as long as it waits. Once bytes move, the line counts them. A disk
whose upload fails part-way leaves no half-written line for the deploy
record to print onto.

How the pieces landed:

- `cluster.Target.UploadDisk` takes one value instead of five
  parameters. The `cluster.Upload` value carries the disk's placement,
  its bytes, its size, and the `Waiting` callback, each with the doc
  the design settled. The callback is the target's to call, because
  the target is what waits: it fires once per readiness poll, carrying
  the phase in words — `the DataVolume is "ImportInProgress"`, or
  `the DataVolume has no phase yet` for the window between the create
  and CDI's first word. The poll loop already read the phase, so the
  wait had only to say it.
- `deploy.Progress` carries a state — waiting, transferring, done —
  instead of a `Done` flag, and `Said` carries what the upload waits
  on. The command renders the state: the waiting words in place of the
  byte count, the count once bytes move, and the line ending at done.
- `progressReader` is now the only thing that reports transferred
  bytes. It reports on its interval and reports done at the end of the
  stream it wraps. The bundle's `VerifiedStream` reports a digest
  mismatch in place of the end of the stream, so a corrupt member is
  never reported done; that fell out for free. The two bracketing
  calls in `upload()` are deleted: the one that announced a transfer
  at zero bytes before the target was even connected, and the one that
  announced it finished. They asserted things nobody observed.
- On the real path the done report does fire: Go's HTTP transport
  drains a request body after the bytes it counted, so the reader sees
  the stream's end even before the proxy answers. A consumer that
  stops reading part-way leaves the line open, which the closing rule
  below handles.
- A progress line that never reaches done is closed by the reporter
  itself. `newUploadReporter` writes the line ending when it is asked
  to report a different disk, and returns a flush that `runDeploy`
  calls once when the run ends, ahead of the record. The record
  therefore never prints onto the end of an unfinished line.
- The wording of the wait is built where the phase is read, in
  `awaitUploadReady`. It is the one part of the change no test
  reaches, because reaching it needs a cluster. The fake target says
  the same words a real one builds, and the tests assert those words
  arrive through `Progress` and render on the line.

Things ticket 02 inherits:

- A target whose `UploadDisk` reads nothing reports nothing, pinned by
  `TestDeployReportsNothingForATargetThatTransfersNothing` against the
  fake's `uploadReadsNothing` mode. The dry-run target can therefore
  return plain nil from `UploadDisk` and the line stays silent.
- `cluster.Upload` is the value the wrapping target receives. Its
  `Waiting` callback arrives wired to the reporter, and a target that
  never polls never calls it, so the wrapper needs no progress
  plumbing of its own.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
