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

- [ ] `quo export` warns when the expansion could not be read, naming the
      reason, and the capture still succeeds
- [ ] `quo inspect` says when a bundle holds no expanded spec
- [ ] Both say that the bundle cannot be deployed
