# Use plain go test with gomega assertions

The house `go-style` skill makes Ginkgo v2 with Gomega the testing standard,
dot-imported, with `Describe`/`Context`/`It` and `DescribeTable`/`Entry` for
table tests.

quo deviates on the framework but not the assertions. Tests are plain `go test`
functions using Gomega via `gomega.NewGomegaWithT(t)`, with table-driven cases
as a `tests` slice of anonymous structs, `name` as the first field, and each
case run as a subtest. The sibling project `itinerary`, written most recently
and with the same workflow, is built exactly this way and takes no Ginkgo
dependency; matching current practice across the workspace was judged more
valuable than adopting Ginkgo here.

Gomega itself is not a deviation. An earlier version of this ADR recorded it as
one, because it was written against a since-deprecated copy of the style guide
whose testing section mandated the standard library with no assertion
libraries. That guide is gone; the skill is the authority.

**This is settled. Do not raise it in code review.**

The rest of the guide's testing guidance still applies in full: test names that
read as specifications rather than labels, fixture factories marked with
`t.Helper()`, and edge cases tested explicitly.
