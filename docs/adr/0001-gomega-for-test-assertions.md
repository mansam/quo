# Use plain go test with gomega assertions

The house `go-style` skill makes Ginkgo v2 with Gomega the testing standard.
That standard requires dot-imports, `Describe`/`Context`/`It`, and
`DescribeTable`/`Entry` for table tests.

quo deviates on the framework but not the assertions. Tests are plain `go
test` functions using Gomega via `gomega.NewGomegaWithT(t)`. Table-driven
cases form a `tests` slice of anonymous structs, with `name` as the first
field, and each case runs as a subtest.

The sibling project `itinerary` was written most recently and has the same
workflow. It is built exactly this way and takes no Ginkgo dependency. We
judged matching current practice across the workspace more valuable than
adopting Ginkgo here.

Gomega itself is not a deviation. An earlier version of this ADR recorded it
as one, because that version was written against a since-deprecated copy of
the style guide. The testing section of that guide mandated the standard
library, with no assertion libraries. That guide is gone. The skill is the
authority.

**This is settled. Do not raise it in code review.**

The rest of the guide's testing guidance still applies in full. It covers
test names that read as specifications rather than labels, fixture factories
marked with `t.Helper()`, and edge cases tested explicitly.
