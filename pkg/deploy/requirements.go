package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// attachNetworks attaches each multus network the bundle declares to a
// NetworkAttachmentDefinition of the target's, so that the applied
// VirtualMachine names where its networks attach on the target, and
// refuses — naming the network and what was tried — when one maps to
// nothing, because the machine is not created whose networks cannot be
// attached.
//
// A network maps automatically when the deploy namespace holds a
// NetworkAttachmentDefinition of the attachment's name, which is the
// mapping a deploy makes on its own, and explicitly when the options name
// one for it, which overrides whatever the deploy namespace holds. The
// pod network is the cluster's own — every target has one — and is never
// mapped.
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
// namespace. What the mapping names — and what the applied
// VirtualMachine's networkName carries — is qualified only when the
// definition lives there, because an unqualified name means the
// namespace of the VirtualMachine itself.
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
				"--network maps it to %q, which the target cluster does not have",
				named),
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
// namespace, which is the mapping a deploy makes on its own. A network
// that names no attachment, or one the deploy namespace holds no
// definition of, maps to nothing unless the command line mapped it,
// which is a refusal: the machine is not created whose networks cannot
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
				"the bundle names no attachment for it and no --network maps it; "+
					"attach it with --network %s=<namespace>/<attachment>",
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
				"the deploy namespace %s holds no NetworkAttachmentDefinition %q and no --network maps it; "+
					"attach it with --network %s=<namespace>/<attachment>",
				opt.Namespace,
				required.Attachment,
				required.Name),
		}
		return
	}
	attachment, how = required.Attachment, "by name"
	return
}

// reportedContainerDisks states the container disk images the bundle's
// volumes are served from. Each one is a requirement of the target's — it
// has to be able to pull the image — and quo neither maps it onto
// anything nor pull-checks it, because it holds no registry credentials.
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
