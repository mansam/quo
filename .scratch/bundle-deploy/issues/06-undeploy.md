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

- [ ] `quo undeploy <bundle> -n <namespace>` deletes the objects carrying
      that bundle's label in that namespace, and prints what it deleted
- [ ] Deleting a DataVolume takes its claim with it
- [ ] Deployments of other bundles in the namespace are untouched
- [ ] Undeploy exits without waiting for deletions to complete
- [ ] `--dry-run` prints what it would delete and deletes nothing
- [ ] The deploy record, real and dry-run alike, ends with the undeploy
      command for what exists
