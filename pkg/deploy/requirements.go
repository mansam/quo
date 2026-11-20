package deploy

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// attachNetworks attaches each multus network the bundle declares to a
// NetworkAttachmentDefinition of the target's. The applied
// VirtualMachine then names where its networks attach on the target.
// attachNetworks refuses, naming the network and what was tried, when
// one maps to nothing. The machine is not created whose networks cannot
// be attached.
//
// A network maps automatically when the deploy namespace holds a
// NetworkAttachmentDefinition of the attachment's name. That is the
// mapping a deploy makes on its own. A network maps explicitly when the
// options name one for it. That mapping overrides whatever the deploy
// namespace holds. The pod network is the cluster's own, and every
// target has one. The pod network is never mapped.
func attachNetworks(
	ctx context.Context,
	t cluster.Target,
	required []bundle.NetworkRequirement,
	opt Options) (networks []Requirement, err error) {
	//
	for i := range required {
		if required[i].Type != bundle.NetworkMultus {
			// The pod network is the cluster's own: every target has one,
			// and it names nothing to map.
			continue
		}
		var attachment, how string
		if named, found := opt.Networks[required[i].Name]; found {
			attachment, how, err = attachNetwork(ctx, t, required[i].Name, named, opt)
		} else {
			attachment, how, err = attachNetworkByName(ctx, t, required[i], opt)
		}
		if err != nil {
			return nil, err
		}
		networks = append(networks, Requirement{
			Kind: KindNetwork,
			From: required[i].Name,
			To:   attachment,
			How:  how,
		})
	}
	return
}

// attachNetwork attaches one of the machine's networks to the
// NetworkAttachmentDefinition the command line named for it.
//
// The name may carry a namespace, for a definition outside the deploy
// namespace. What the mapping names, and what the applied
// VirtualMachine's networkName carries, is namespace-qualified only when
// the definition lives outside the deploy namespace. An unqualified name
// means the namespace of the VirtualMachine itself.
func attachNetwork(
	ctx context.Context,
	t cluster.Target,
	network, named string,
	opt Options) (attachment, how string, err error) {
	//
	namespace, name := opt.Namespace, named
	if given, rest, split := strings.Cut(named, "/"); split {
		namespace, name = given, rest
	}
	held, err := holds(ctx, t, cluster.ObjectRef{
		Kind:      cluster.KindNetworkAttachmentDefinition,
		Namespace: namespace,
		Name:      name,
	})
	if err != nil {
		return
	}
	if !held {
		err = &UnmappedRequirementError{
			Kind:        KindNetwork,
			Requirement: network,
			Tried: fmt.Sprintf(
				"no NetworkAttachmentDefinition %q on the target cluster. Use --network %s=<namespace>/<attachment> to select an existing attachment",
				namespace+"/"+name, network),
		}
		return
	}
	attachment = name
	if namespace != opt.Namespace {
		attachment = namespace + "/" + name
	}
	how = "by --network"
	return
}

// attachNetworkByName attaches one of the machine's networks to the
// NetworkAttachmentDefinition of the attachment's name in the deploy
// namespace. That is the mapping a deploy makes on its own. A network
// that names no attachment, or one the deploy namespace holds no
// definition of, maps to nothing unless the command line mapped it.
// That is a refusal: the machine is not created whose networks cannot
// be attached.
func attachNetworkByName(
	ctx context.Context,
	t cluster.Target,
	required bundle.NetworkRequirement,
	opt Options) (attachment string, how string, err error) {
	//
	if required.Attachment == "" {
		err = &UnmappedRequirementError{
			Kind:        KindNetwork,
			Requirement: required.Name,
			Tried: fmt.Sprintf(
				"the bundle does not name an attachment. Set --network %s=<namespace>/<attachment>",
				required.Name),
		}
		return
	}
	held, err := holds(ctx, t, cluster.ObjectRef{
		Kind:      cluster.KindNetworkAttachmentDefinition,
		Namespace: opt.Namespace,
		Name:      required.Attachment,
	})
	if err != nil {
		return
	}
	if !held {
		err = &UnmappedRequirementError{
			Kind:        KindNetwork,
			Requirement: required.Name,
			Tried: fmt.Sprintf(
				"no NetworkAttachmentDefinition %q on the target cluster. Use --network %s=<namespace>/<attachment> to select an existing attachment",
				opt.Namespace+"/"+required.Attachment,
				required.Name),
		}
		return
	}
	attachment, how = required.Attachment, "by name"
	return
}

// reportedContainerDisks states the container disk images the bundle's
// volumes are served from. Each one is a requirement of the target's: it
// has to be able to pull the image. quo neither maps it onto anything
// nor pull-checks it, because it holds no registry credentials.
func reportedContainerDisks(
	required []bundle.ContainerDiskRequirement) (reported []Requirement) {
	//
	for i := range required {
		reported = append(reported, Requirement{
			Kind: KindContainerDisk,
			From: required[i].Image,
			How:  "the target has to be able to pull it",
		})
	}
	return
}

// unmatchedMappings reports the mappings the command line named that no
// requirement of the bundle's matched. A --network naming no network of
// the machine is one. A --storage-class naming a class no disk records
// is another. Nothing is applied and nothing is refused for them. The
// record says so, because a flag that looks like it took effect is
// a quiet mistake. The catch-all is not among them: it names no
// requirement of its own.
func unmatchedMappings(opt Options, manifest *bundle.Manifest) (unmatched []Requirement) {
	recorded := make(map[string]bool, len(manifest.Requirements.Storage))
	for i := range manifest.Requirements.Storage {
		recorded[manifest.Requirements.Storage[i].StorageClass] = true
	}
	for _, source := range slices.Sorted(maps.Keys(opt.StorageClasses)) {
		if source == catchAllClass || recorded[source] {
			continue
		}
		unmatched = append(unmatched, Requirement{
			Kind:   KindStorageClass,
			From:   source,
			To:     opt.StorageClasses[source],
			How:    "the bundle has no matching storage class",
			Unused: true,
		})
	}
	declared := make(map[string]bool, len(manifest.Requirements.Networks))
	for i := range manifest.Requirements.Networks {
		if manifest.Requirements.Networks[i].Type != bundle.NetworkMultus {
			// The pod network is never mapped, so a flag naming it has
			// nothing to match.
			continue
		}
		declared[manifest.Requirements.Networks[i].Name] = true
	}
	for _, network := range slices.Sorted(maps.Keys(opt.Networks)) {
		if declared[network] {
			continue
		}
		unmatched = append(unmatched, Requirement{
			Kind:   KindNetwork,
			From:   network,
			To:     opt.Networks[network],
			How:    "the bundle has no matching Multus network",
			Unused: true,
		})
	}
	return
}
