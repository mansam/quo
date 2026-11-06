# 03: Fold the dry-run tests into the tables they repeat

**What to build:** Nothing changes for anyone running quo. The dry-run
tests move into the test files whose contracts they restate, as one more
dimension of the tables already there. A contract is then written once,
and a dry run and a real deploy both assert against it.

The duplication is large enough to hide a drift between the two. One
block of expected steps runs to 56 lines and appears twice, word for
word, once in the dry-run tests and once in the closure tests. Three
bundle-corruption recipes appear two and three times each. The dry-run
refusal table restates rows that four other files already hold.

What the dry-run tests say that no other test says is the negative: that
nothing was created, nothing was uploaded, and nothing was recorded
against the target. That assertion is what catches a write path that
forgets its target. It stays, on the dry-run rows of the shared tables.

**Blocked by:** 02 (the two modes have to take one write path before
their tests can share assertions).

**Status:** ready-for-agent

- [ ] Each contract the dry-run tests restate is written once, and both
      modes assert against it
- [ ] A dry-run row still asserts that nothing reached the target
- [ ] `make check` passes, and the dry-run behaviour pinned today stays
      pinned
