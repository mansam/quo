# 06: Undeploy a deployment

**What to build:** A support engineer runs `quo undeploy <bundle> -n
<namespace>` and the deployment is gone: the objects carrying that
bundle's label in that namespace are deleted, and what was deleted is
printed.

Undeploy removes exactly one deployment, selected by the bundle label, so
that other work in the namespace — including deployments of other bundles
— is untouched. Deleting a DataVolume takes its claim with it. Undeploy
exits without waiting for deletions to complete; if a deletion hangs,
that is what kubectl is for. `--dry-run` prints what it would delete.

There is deliberately no flag that removes everything quo manages in a
namespace. When the bundle file is gone, the labels make kubectl enough.
The deploy record — real and dry-run alike — ends with the undeploy
command for what exists.

**Blocked by:** 01 (needs the labels on created objects; works on a
VirtualMachine-only deploy).

**Status:** ready-for-agent

- [x] `quo undeploy <bundle> -n <namespace>` deletes the objects carrying
      that bundle's label in that namespace, and prints what it deleted
- [x] Deleting a DataVolume takes its claim with it
- [x] Deployments of other bundles in the namespace are untouched
- [x] Undeploy exits without waiting for deletions to complete
- [x] `--dry-run` prints what it would delete and deletes nothing
- [x] The deploy record, real and dry-run alike, ends with the undeploy
      command for what exists

## Comments

Implemented. `quo undeploy <bundle> -n <namespace> [--dry-run]` removes
one deployment — the objects carrying that bundle's identity label in the
namespace — prints what it deleted, and returns without waiting for any
deletion to complete. The same runs work through the library entry point
against the fake target.

How the pieces landed:

- The library entry point is `Undeploy` in `pkg/deploy`, mirroring
  `Deploy`'s shape: `Undeploy(ctx, t, UndeployOptions)` returns
  `([]Deletion, error)`. It opens the bundle, reads the manifest, and
  computes the identity exactly as deploy does — `bundleIdentity` over
  the manifest bytes — so a deployment is selected by the same value a
  deploy labeled its objects with. Nothing else in the bundle is read:
  an undeploy that had to refuse a bundle with no expanded spec would be
  one that cannot remove a deployment it is holding the label of. A
  bundle that cannot be read is an error naming it, the refusal set
  deploy already opens with.
- The `Target` interface gained two methods, `ListByLabel` and `Delete`,
  both served by the existing `KubeVirt` adapter. `ListByLabel` runs a
  label-selector list over `DeployedKinds` — a new exported table in
  `pkg/cluster` naming the kinds a deploy can leave in a namespace: the
  VirtualMachine, the DataVolumes, and the ConfigMaps, Secrets and
  ServiceAccounts the by-kind table (ADR-0004) says are applied
  unchanged. `Delete` finds the resource from the object's own kind
  through discovery, exactly as `Create` does, and returns once the API
  server accepts the deletion — that is what makes undeploy not wait,
  and no polling loop exists anywhere on the path. A kind the cluster
  does not serve reads back from a list as a not-found and is skipped,
  the same reading `mapListError` already makes of a list.
- The claim a DataVolume creates is deliberately not among
  `DeployedKinds`: CDI owns the claim through an owner reference and
  removes it with the DataVolume, which is the cluster's doing, not
  quo's. The comment on `DeployedKinds` says so, and the fake's `Delete`
  models it — deleting a DataVolume removes the claim of the same name —
  so the tests pin that the claim is never selected on its own, even
  when it carries the label too.
- An undeploy is best-effort like a deploy: a deletion the cluster
  refuses is recorded on its entry (`Deletion.Err` says why the object
  is still there) and the run continues with the rest;
  `NotDeletedError` — the mirror of `NotCreatedError` — is what makes
  the exit code non-zero for a partial undeploy. The order of deletions
  is fixed (by kind, then name), so what an undeploy says it deleted
  reads the same from run to run; the VirtualMachine goes last, as it
  came last in a deploy.
- The CLI is a thin wrapper in `cmd/quo/undeploy.go`: flags `-n`/
  `--namespace` (required, with deploy's own message and for deploy's
  own reason), `--dry-run`, `--kubeconfig`, and exactly one positional
  bundle path. What was deleted prints as a table — `Deleted 5 objects:`
  on a real run, `Would delete 5 objects:` on a dry run, `Deleted 1 of
  2 objects:` naming the refusal when one failed — and nothing prints
  when the namespace holds nothing of that deployment. There is no
  signal wiring, unlike a deploy: nothing in an undeploy runs long
  enough to interrupt. The command is registered in main.go's dispatch
  and the usage text, which also says the one-deployment rule and why
  undeploy does not wait.
- The layering decision for the record's last line, which ticket 05
  inherits: the undeploy command is **CLI-only**, printed by
  `printRecord` from the deploy's own `Options` (bundle path and
  namespace), not part of the `Record` data. A dry run that prints the
  record it would print — ticket 05's whole output — calls
  `printRecord(out, record, opt)` and gets the same line for free,
  because the line depends only on the options it already holds. The
  line prints only when at least one step was created: a deploy that put
  nothing on the target leaves nothing to point an undeploy at, and a
  refused run prints no record at all, as before.

Things tickets 05 and 07–08 inherit:

- Ticket 05's dry run prints through `printRecord(out, record, opt)` and
  needs no change of its own to end with the undeploy command. Its
  `--dry-run` flag will want an `Options.DryRun` field of deploy's own;
  undeploy's `DryRun` lives on `UndeployOptions`, which is a separate
  type because the two commands share only the bundle and the
  namespace.
- Ticket 03's closure objects are already in `DeployedKinds`, so its
  ConfigMaps, Secrets and ServiceAccounts are undeployable the moment
  they are deployable; no undeploy change is needed when it lands.
- The fake target's `Delete` takes the objects a `ListByLabel` returns,
  so any test that wants a deployment on the fake holds one through
  `holdLabeled(kind, name, identity)` — the label value comes from the
  existing `bundleLabelOf(t, path)` helper — and asserts on `holds`.
  `deleteErrFor` fails one object's deletion by name, the mirror of
  `uploadErrFor`.
- Ticket 08's live round-trip is where `ListByLabel` and `Delete` first
  run against a real API server. Two facts it will settle: that the
  dynamic client's list responses leave each item's kind readable (the
  adapter stamps the GVK on every item it returns, so it does not depend
  on the server writing it), and that CDI really does take the claim
  with a deleted DataVolume on the clusters quo supports.

Status left at `ready-for-agent`, as the tickets before it: the
tracker's five triage labels have no value meaning "implemented".
