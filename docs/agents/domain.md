# Domain Docs

This page explains how the engineering skills should consume this repo's domain documentation when they explore the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root: the glossary of domain terms.
- **`docs/adr/`**: read ADRs that touch the area you are about to work in.

If any of these files do not exist, **proceed silently**. Do not flag their absence. Do not suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

This is a **single-context** repo:

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-<decision-slug>.md
│   └── 0002-<decision-slug>.md
└── (Go packages)
```

If this repo ever grows into several bounded contexts, switch to a multi-context layout. That layout has a root `CONTEXT-MAP.md` that points at one `CONTEXT.md` per context. It has context-scoped ADRs alongside each. Update this file when that happens.

## Use the glossary's vocabulary

Use the term as defined in `CONTEXT.md` when your output names a domain concept. This applies to an issue title, a refactor proposal, a hypothesis, or a test name. Do not drift to synonyms that the glossary explicitly avoids.

If the concept you need is not in the glossary yet, that is a signal. Either you are inventing language that the project does not use (reconsider). Or there is a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface the conflict explicitly rather than overriding it silently:

> _Contradicts ADR-0007 (event-sourced orders), but worth reopening because…_
