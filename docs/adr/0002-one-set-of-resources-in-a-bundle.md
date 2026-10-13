# Hold one set of resources in a bundle

A bundle lists the resources it holds in one place and says nothing about
which of them a target cluster would recreate. A reader who wants to know
that has to decide it from the kind.

## What this replaces

The format used to sort what it collected into two groups: carried objects,
said to be the ones a target would recreate, and evidence, said to be there
only to be read. The two had separate manifest fields and separate
directories in the tar.

The split did not hold up. What actually decided the group was which code
path had read the object — the traversal of the VirtualMachine's references,
or the fixed list of reads about the source cluster — and that is a fact
about quo rather than about the object. Three kinds were in the wrong group
under the format's own rule: a PersistentVolumeClaim, a DataVolume and a
NetworkAttachmentDefinition were all carried, and none of them would be
applied to a target as captured. The attachment definition was already
recorded twice, once as carried and once as a requirement, which is what the
contradiction looks like from the outside.

## Why a bundle does not mark what is safe to apply

The obvious repair is to keep two groups and fix the membership: mark each
resource with what a deploy should do with it. We decided against it.

A capture cannot know. Whether a resource is applied, recreated from scratch,
or ignored depends on the target cluster and on the mappings supplied at
deploy time — what storage class is available, which network the bundle is
being attached to, whether the VirtualMachine is being renamed. None of that
is visible while capturing. A mark written at capture would be a guess, and
the deploy would have to re-derive the answer anyway, so the mark would be
one more thing that can be wrong.

Deciding by kind is what a deploy will do, and the set of kinds is small,
fixed, and already written down: it is the traversal in `pkg/capture`.

## What it costs

A reader untarring a bundle by hand gets no warning that applying a
StorageClass from someone else's cluster is a bad idea. We accept that: the
objects are plain YAML in a directory and a support engineer applying them
one by one is already outside what quo supports.

The live comparison against KubeVirt's objectgraph subresource loses the
signal it used to tell the traversal's reads from quo's own. It now filters
by kind through a hand-maintained list, which has to be updated when a new
read about the source cluster is added. That list is in
`test/live/difference_test.go`.

## Status

Settled. Reopen only if a deploy implementation finds it genuinely cannot
decide by kind.
