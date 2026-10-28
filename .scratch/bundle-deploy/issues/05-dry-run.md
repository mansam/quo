# 05: Dry-run a deploy

**What to build:** A support engineer runs `quo deploy <bundle> -n
<namespace> --dry-run` and sees exactly what the deploy would do — the
objects that would be created, the forced rewrites, the mappings —
without anything touching the target.

Dry-run runs the complete refusal set — the bundle checks, the namespace,
the collisions, the mappings, CDI, the volumeModes — and prints the deploy
record the real deploy would print. A deploy that would be refused says
so, naming what it found. Nothing is created, nothing is uploaded, no
labels land anywhere.

**Blocked by:** 02, 03, 04 (needs the complete refusal set and the full
deploy record).

**Status:** ready-for-agent

- [ ] `--dry-run` runs every refusal the real deploy runs, and a deploy
      that would be refused says so, naming what it found
- [ ] It prints the deploy record the real deploy would print — the
      objects that would be created, the forced rewrites, the mappings
- [ ] Nothing is created, nothing is uploaded, and no labels land
      anywhere on the target
