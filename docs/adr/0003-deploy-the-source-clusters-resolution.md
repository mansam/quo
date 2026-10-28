# Deploy the source cluster's resolution

A deploy applies the bundle's expanded spec as the deployed VirtualMachine.
The expanded spec is the source cluster's own resolution of the
VirtualMachine. The deploy does not create instancetypes or preferences. It
does not let the target resolve the declaration itself.

## What this replaces

The glossary entry for the expanded spec ended: "no object on the cluster
has that name and nothing will apply it to a target." That sentence is
superseded. The expectation it recorded was never built and never will be.
The expectation was that the expanded spec would be read and never applied.
A deploy would instead recreate the instancetype and preference objects and
apply the captured VirtualMachine with its matchers.

## Why the resolution and not the declaration

The expanded spec carries the source cluster's defaults folded in. Those
defaults are machine type, architecture, CPU topology and eviction strategy.
They come from the source's KubeVirt resource. Recreating the declaration
and letting the target resolve it would produce a machine shaped by the
*target's* defaults. Fidelity is to the machine the customer had, so the
resolution is what deploys.

Two things follow. The matchers are absent from the applied object. So no
ControllerRevision is involved, and the deploy creates none. The revisions a
bundle holds are read-only diagnostics.

And a deploy never creates instancetypes or preferences. That means nothing
it creates is cluster-scoped. The support engineer needs only namespace-level
permissions on the target. That is the mirror of the namespace owner who
captures on the source.

## What it costs

Anyone who reads the deployed VirtualMachine sees inlined values where the
customer had a matcher. The target shows the resolution, not the declaration.
The captured VirtualMachine object remains in the bundle as the declaration
of record.

A bundle whose expansion could not be read cannot be deployed at all. Deploy
refuses. Capture warns at export time when it happens. The person capturing
therefore learns it before the bundle is sent.

If the source's instancetype drifted from its pinned revision, the expansion
records what the source actually resolved, and that is what deploys. The
bundle's ControllerRevision and expanded spec are the record of the
difference.

## Status

Settled.
