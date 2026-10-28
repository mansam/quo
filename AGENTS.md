# quo

## Coding standards

Go style follows the `go-k8s-style` skill. The skill is a general-purpose Go
guide. The Go projects in this workspace share it. It is installed at
`~/.claude/skills/go-k8s-style/`, so it loads in any session. It is binding
for quo. Read its `references/` files too, not just `SKILL.md`.

The `go-style-guide.md` that used to sit one directory up is deprecated. Its
name is now `deprecated-go-style-guide-do-not-use.md`. Do not read it.

There is one deliberate deviation. The guide's testing section requires
Ginkgo v2 with Gomega. quo uses plain `go test` with Gomega assertions
instead. See `docs/adr/0001-gomega-for-test-assertions.md`. It is settled.
Do not flag it.

## Live clusters

Agents never touch a live cluster. An agent does not read from a live
cluster. An agent does not probe a live cluster. An agent does not run the
live suite. An agent does not create or delete anything.

Read-only access counts as touching. The rule binds every subagent that an
agent dispatches.

`make check` is the whole of an agent's verification. CI runs it. It needs
no cluster.

A human runs the live suite (`make test-live`, see
`docs/testing-against-a-cluster.md`). When a change needs a live run, the
agent writes the change and gives it to a human. The agent does not run it.

## Agent skills

### Issue tracker

Issues are markdown files under `.scratch/<feature>/` in this repo. See
`docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles are used verbatim as `Status:` strings. See
`docs/agents/triage-labels.md`.

### Domain docs

Domain documentation has a single context: one `CONTEXT.md` at the root
plus `docs/adr/`. See `docs/agents/domain.md`.
