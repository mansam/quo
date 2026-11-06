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

- [ ] A deploy waiting for CDI says so, and says what the DataVolume's
      phase is, for as long as it waits
- [ ] A progress line counts bytes only once bytes have moved
- [ ] A disk whose upload fails part-way leaves no unfinished progress
      line for the deploy record to print onto
- [ ] A target that transfers nothing reports nothing
