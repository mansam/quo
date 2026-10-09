# quo

## Coding standards

Go style follows the `go-k8s-style` skill, a general-purpose Go guide shared
across the Go projects in this workspace. It is installed at
`~/.claude/skills/go-k8s-style/`, so it loads in any session. It is binding
for quo. Read its `references/` files too, not just `SKILL.md`.

The `go-style-guide.md` that used to sit one directory up is deprecated — it
has been renamed `deprecated-go-style-guide-do-not-use.md`. Do not read it.

One deliberate deviation: the guide's testing section calls for Ginkgo v2 with
Gomega, and quo uses plain `go test` with Gomega assertions instead. See
`docs/adr/0001-gomega-for-test-assertions.md`. It is settled; do not flag it.

## Agent skills

### Issue tracker

Issues live as markdown files under `.scratch/<feature>/` in this repo. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, used verbatim as `Status:` strings. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` at the root plus `docs/adr/`. See `docs/agents/domain.md`.
