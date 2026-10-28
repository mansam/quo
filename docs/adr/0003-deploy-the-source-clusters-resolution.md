# Deploy the source cluster's resolution

A deploy applies the bundle's expanded spec — the source cluster's own
resolution of the VirtualMachine — as the deployed VirtualMachine. It does
not create instancetypes or preferences and does not let the target resolve
the declaration itself.

## What this replaces

The glossary entry for the expanded spec ended: "no object on the cluster
has that name and nothing will apply it to a target." That sentence is
superseded. The expectation it recorded — that the expanded spec would be
read and never applied, and that a deploy would instead recreate the
instancetype and preference objects and apply the captured VirtualMachine
with its matchers — was never built, and is not going to be.

## Why the resolution and not the declaration

The expanded spec carries the source cluster's defaults folded in — machine
type, architecture, CPU topology, eviction strategy — which come from the
source's KubeVirt resource. Recreating the declaration and letting the
target resolve it would produce a machine shaped by the *target's* defaults.
Fidelity is to the machine the customer had, so the resolution is what
deploys.

Two things follow. The matchers are absent from the applied object, so no
ControllerRevision is involved and none is created: the revisions a bundle
holds are read-only diagnostics. And instancetypes and preferences are never
created — which means nothing a deploy creates is cluster-scoped, and the
support engineer needs only namespace-level permissions on the target, the
mirror of the namespace owner who captures on the source.

## What it costs

Anyone reading the deployed VirtualMachine sees inlined values where the
customer had a matcher: the target shows the resolution, not the
declaration. The captured VirtualMachine object remains in the bundle as the
declaration of record.

A bundle whose expansion could not be read cannot be deployed at all.
Deploy refuses, and capture warns at export time when it happens, so the
person capturing learns it before the bundle is sent.

If the source's instancetype had drifted from its pinned revision, the
expansion records what the source actually resolved, and that is what
deploys. The bundle's ControllerRevision and expanded spec are the record of
the difference.

## Status

Settled.
