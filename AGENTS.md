# quo

## Coding standards

Go style follows `go-style-guide.md`, kept one directory up and shared across
the sibling projects in this workspace. It is binding for quo.

One deliberate deviation: tests use gomega, which that guide forbids. See
`docs/adr/0001-gomega-for-test-assertions.md`. It is settled; do not flag it.

## Agent skills

### Issue tracker

Issues live as markdown files under `.scratch/<feature>/` in this repo. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, used verbatim as `Status:` strings. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` at the root plus `docs/adr/`. See `docs/agents/domain.md`.
