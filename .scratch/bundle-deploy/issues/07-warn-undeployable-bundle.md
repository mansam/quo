# 07: Warn when a bundle cannot be deployed

**What to build:** A bundle that holds no expanded spec cannot be
deployed, and the people who need to know learn it as early as possible.

`quo export` warns at capture time when the expansion could not be read,
naming the reason. The capture still succeeds — a bundle without an
expansion is still diagnostic material — but the person capturing it
learns before the bundle is sent that it will not deploy. `quo inspect`
says when a bundle holds no expanded spec, so a recipient can tell before
trying.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] `quo export` warns when the expansion could not be read, naming the
      reason, and the capture still succeeds
- [x] `quo inspect` says when a bundle holds no expanded spec
- [x] Both say that the bundle cannot be deployed

## Comments

Implemented. The two statements landed where the ticket pointed them:
`quo export` says it after the capture has succeeded, `quo inspect`
says it from the manifest alone.

How the pieces landed:

- `runExport` prints the warning after the skipped objects, on a line
  of its own: "The capture could not read the expanded spec
  (forbidden: what the cluster said), so the bundle cannot be
  deployed." The reason comes straight out of the capture record —
  the skipped `ExpandedSpec` attempt already carries the classified
  reason and the cluster's message, so nothing had to be threaded out
  of the capture; `warnUndeployable` scans the record for it. The
  cluster's message is included because it is where the missing
  permission appears, and export's skipped list names the reason
  alone. The capture still succeeds and the exit code is unchanged:
  the warning is threaded exactly like `printSkipped`, so only a write
  failure is an error.
- `describeExpandedSpec` no longer stays silent. A bundle holding no
  expansion gets "none, so the bundle cannot be deployed" under the
  heading, in the words the deploy side's refusal already uses
  (`NoExpandedSpecError`: "holds no expanded spec, so it cannot be
  deployed"), so the capture, the inspection and the refusal all say
  the same thing. The statement is said from `manifest.ExpandedSpec`
  alone, so a partial transfer still says it. The test that pinned
  the silence was renamed to pin the statement, and the
  manifest-alone test gained the assertion.
- A bundle with an expansion gets no warning from either command:
  inspect's section names the member as before, and the export
  warning keys on the skipped attempt alone — a skipped Secret says
  nothing about deploying.

Things tickets 05 and 08 inherit:

- Nothing structural. Ticket 05's dry run refuses a bundle with no
  expansion through `NoExpandedSpecError` before any record exists,
  exactly as a real deploy does; the refusal path is unchanged here.
- Ticket 08's round-trip captures a fixture whose expand-spec read
  succeeds, so neither statement fires on the happy path. Should a
  live capture ever produce no expansion, the export warning is on
  stdout and "cannot be deployed" is the phrase to catch; the
  round-trip's expand-spec diff has no bundle side to diff against in
  that case, which the warning names up front.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
