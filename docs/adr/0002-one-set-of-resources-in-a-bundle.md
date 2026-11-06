# Hold one set of resources in a bundle

A bundle lists the resources it holds in one place. It says nothing about
which of them a target cluster would recreate. A reader who wants to know
that has to decide it from the kind.

## What this replaces

The format used to sort what it collected into two groups. Carried objects
were said to be the ones a target would recreate. Evidence was said to be
there only to be read. The two groups had separate manifest fields and
separate directories in the tar.

The split failed. What actually decided the group was which code path read
the object. The two paths were the traversal of the VirtualMachine's
references and the fixed list of reads about the source cluster. That is a
fact about quo rather than about the object.

Three kinds were in the wrong group under the format's own rule. A
PersistentVolumeClaim, a DataVolume and a NetworkAttachmentDefinition were
all carried. None of them would be applied to a target as captured. The
attachment definition was already recorded twice, once as carried and once as
a requirement. That is what the contradiction looks like from the outside.

## Why a bundle does not mark what is safe to apply

The obvious fix is to keep two groups with the right membership: mark each
resource with what a deploy should do with it. We decided against it.

A capture cannot know. Whether a resource is applied, recreated from
scratch, or ignored depends on the target cluster and on the mappings
supplied at deploy time. That covers what storage class is available, which
network the bundle is attached to, and whether the VirtualMachine is renamed.
None of that is visible while capturing.

A mark written at capture would be a guess. The deploy would have to
re-derive the answer anyway. So the mark would be one more thing that can be
wrong.

Deciding by kind is what a deploy will do. The set of kinds is small, fixed,
and already written down. It is the traversal in `pkg/capture`.

## What it costs

A reader who untars a bundle by hand gets no warning. Applying a
StorageClass from someone else's cluster is a bad idea. We accept that. The
objects are plain YAML in a directory. A support engineer who applies them
one by one is already outside what quo supports.

The live comparison against KubeVirt's objectgraph subresource loses the
signal it used to tell the traversal's reads from quo's own. It now filters
by kind through a hand-maintained list. That list has to be updated when a
new read about the source cluster is added. The list is in
`test/live/difference_test.go`.

## Status

Settled. Reopen only if a deploy implementation finds it genuinely cannot
decide by kind.
