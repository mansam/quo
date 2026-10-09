# Use gomega for test assertions

`go-style-guide.md` says to use the standard `testing` package with no
assertion libraries. quo deviates: tests use gomega. The sibling project
`itinerary`, written most recently and with the same workflow, uses gomega
throughout, and matching current practice across the workspace was judged more
valuable than matching the written guide — which appears to have drifted.

**This is settled. Do not raise it in code review.** Reconciling the style
guide itself is separate work, tracked nowhere yet.

The rest of the guide's testing section still applies in full: table-driven
tests with a `tests` slice of anonymous structs and `name` as the first field,
each case run as a subtest, fixture factories marked with `t.Helper()`, and
test names that read as specifications rather than labels.
