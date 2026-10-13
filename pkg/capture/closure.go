package capture

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// closure collects the objects a VirtualMachine depends on, in the order they
// were discovered and without duplicates.
//
// The traversal is quo's own rather than a call to KubeVirt's objectgraph
// subresource, which sits behind an Alpha feature gate that is off by default.
// KubeVirt's server-side implementation is what the traversal follows.
type closure struct {
	namespace string
	refs      []cluster.ObjectRef
	seen      map[cluster.ObjectRef]bool
}

// matcher is one of the two ways a VirtualMachine says what it was asked to
// be: where its spec names the object, where its status records what that
// resolved to, and the namespaced and cluster-scoped kinds the name can mean.
type matcher struct {
	spec          string
	status        string
	namespaced    string
	clusterScoped string
}

// matchers are the instancetype and the preference.
var matchers = []matcher{
	{
		spec:          "instancetype",
		status:        "instancetypeRef",
		namespaced:    cluster.KindVirtualMachineInstancetype,
		clusterScoped: cluster.KindVirtualMachineClusterInstancetype,
	},
	{
		spec:          "preference",
		status:        "preferenceRef",
		namespaced:    cluster.KindVirtualMachinePreference,
		clusterScoped: cluster.KindVirtualMachineClusterPreference,
	},
}

// buildClosure returns the objects the VirtualMachine refers to. It reads
// nothing from the cluster: every reference is on the VirtualMachine itself,
// and a DataVolume's claim carries the DataVolume's own name.
func buildClosure(vm *unstructured.Unstructured) (refs []cluster.ObjectRef) {
	c := &closure{
		namespace: vm.GetNamespace(),
		seen:      map[cluster.ObjectRef]bool{},
	}
	c.addMatchers(vm)
	c.addDataVolumeTemplates(vm)
	c.addVolumes(vm)
	c.addAccessCredentials(vm)
	c.addNetworks(vm)
	refs = c.refs
	return
}

// carryVirtualMachine collects the VirtualMachine itself, which the capture
// already has in hand and so does not read a second time as part of its own
// closure.
func carryVirtualMachine(writer *bundle.Writer, vm *unstructured.Unstructured) (err error) {
	err = carry(
		writer,
		cluster.ObjectRef{
			Kind:      cluster.KindVirtualMachine,
			Namespace: vm.GetNamespace(),
			Name:      vm.GetName(),
		},
		vm)
	return
}

// carryClosure reads the objects the VirtualMachine depends on and collects
// them into the bundle as carried objects.
func carryClosure(
	ctx context.Context,
	c cluster.Cluster,
	writer *bundle.Writer,
	vm *unstructured.Unstructured) (err error) {
	//
	for _, ref := range buildClosure(vm) {
		var object *unstructured.Unstructured
		object, err = c.GetObject(ctx, ref)
		if errors.Is(err, cluster.ErrNotFound) {
			// An instancetype matcher can name an object that has since been
			// deleted. The ControllerRevision it was pinned to is captured in
			// its own right and is then the only complete record of what the
			// VirtualMachine was created with, so the capture goes on.
			err = nil
			continue
		}
		if err != nil {
			return
		}
		err = carry(writer, ref, object)
		if err != nil {
			return
		}
	}
	return
}

// carry collects one object into the bundle as Kubernetes YAML, exactly as the
// cluster returned it.
func carry(writer *bundle.Writer, ref cluster.ObjectRef, object *unstructured.Unstructured) (err error) {
	encoded, err := yaml.Marshal(object.Object)
	if err != nil {
		err = fmt.Errorf("marshal %s %q: %w", ref.Kind, ref.Name, err)
		return
	}
	err = writer.Carry(
		bundle.Object{
			Kind:      ref.Kind,
			Namespace: ref.Namespace,
			Name:      ref.Name,
		},
		encoded)
	return
}

// add records an object in the VirtualMachine's own namespace.
func (r *closure) add(kind, name string) {
	r.addIn(kind, r.namespace, name)
}

// addClusterScoped records a cluster-scoped object, which has no namespace.
func (r *closure) addClusterScoped(kind, name string) {
	r.addIn(kind, "", name)
}

// addIn records an object in the given namespace. An empty name is a
// reference the VirtualMachine does not make, and an object already recorded
// is left alone, so something referred to from more than one place is
// captured once.
func (r *closure) addIn(kind, namespace, name string) {
	if name == "" {
		return
	}
	ref := cluster.ObjectRef{Kind: kind, Namespace: namespace, Name: name}
	if r.seen[ref] {
		return
	}
	r.seen[ref] = true
	r.refs = append(r.refs, ref)
}

// addMatchers follows what the VirtualMachine was asked to be: the
// instancetype and preference its spec names, and whatever its status
// resolved them to.
func (r *closure) addMatchers(vm *unstructured.Unstructured) {
	for _, m := range matchers {
		named, _, _ := unstructured.NestedMap(vm.Object, "spec", m.spec)
		r.addMatcher(named, m)
		resolved, _, _ := unstructured.NestedMap(vm.Object, "status", m.status)
		r.addMatcher(resolved, m)
	}
}

// addMatcher follows one instancetype or preference reference: the object it
// names and the ControllerRevision the VirtualMachine was pinned to. A spec
// matcher names the revision as revisionName and a resolved status reference
// names it under controllerRevisionRef; both are followed, because either may
// be the only one present.
func (r *closure) addMatcher(named map[string]any, m matcher) {
	if named == nil {
		return
	}
	kind := nestedString(named, "kind")
	name := nestedString(named, "name")
	if matchesKind(kind, m.namespaced) {
		r.add(m.namespaced, name)
	} else {
		r.addClusterScoped(m.clusterScoped, name)
	}
	r.add(cluster.KindControllerRevision, nestedString(named, "revisionName"))
	r.add(cluster.KindControllerRevision, nestedString(named, "controllerRevisionRef", "name"))
}

// addDataVolumeTemplates follows the DataVolumes the VirtualMachine declares
// for itself. They are objects in the cluster like any other once the
// VirtualMachine has been created.
func (r *closure) addDataVolumeTemplates(vm *unstructured.Unstructured) {
	templates, _, _ := unstructured.NestedSlice(vm.Object, "spec", "dataVolumeTemplates")
	for i := range templates {
		template, cast := templates[i].(map[string]any)
		if !cast {
			continue
		}
		r.addDataVolume(nestedString(template, "metadata", "name"))
	}
}

// addVolumes follows every volume of the VirtualMachine.
func (r *closure) addVolumes(vm *unstructured.Unstructured) {
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	for i := range volumes {
		volume, cast := volumes[i].(map[string]any)
		if !cast {
			continue
		}
		r.addVolume(volume)
	}
}

// addVolume follows one volume to whatever is behind it. A volume is backed by
// exactly one of these, so the ones that do not apply name nothing.
//
// A container disk names no object to carry: quo does not repackage container
// images, and the image is recorded as a requirement instead. The Secret the
// image is pulled with is a real object and is carried.
func (r *closure) addVolume(volume map[string]any) {
	r.add(cluster.KindPersistentVolumeClaim, nestedString(volume, "persistentVolumeClaim", "claimName"))
	r.add(cluster.KindPersistentVolumeClaim, nestedString(volume, "ephemeral", "persistentVolumeClaim", "claimName"))
	r.add(cluster.KindPersistentVolumeClaim, nestedString(volume, "memoryDump", "claimName"))
	r.addDataVolume(nestedString(volume, "dataVolume", "name"))
	r.add(cluster.KindConfigMap, nestedString(volume, "configMap", "name"))
	r.add(cluster.KindSecret, nestedString(volume, "secret", "secretName"))
	r.add(cluster.KindServiceAccount, nestedString(volume, "serviceAccount", "serviceAccountName"))
	r.add(cluster.KindConfigMap, nestedString(volume, "sysprep", "configMap", "name"))
	r.add(cluster.KindSecret, nestedString(volume, "sysprep", "secret", "name"))
	for _, source := range []string{"cloudInitNoCloud", "cloudInitConfigDrive"} {
		r.add(cluster.KindSecret, nestedString(volume, source, "secretRef", "name"))
		r.add(cluster.KindSecret, nestedString(volume, source, "networkDataSecretRef", "name"))
	}
	r.add(cluster.KindSecret, nestedString(volume, "containerDisk", "imagePullSecret"))
}

// addDataVolume follows a DataVolume and the PersistentVolumeClaim it
// provisions, which carries the DataVolume's own name.
func (r *closure) addDataVolume(name string) {
	r.add(cluster.KindDataVolume, name)
	r.add(cluster.KindPersistentVolumeClaim, name)
}

// addAccessCredentials follows the Secrets holding the keys and passwords
// being injected into the guest.
func (r *closure) addAccessCredentials(vm *unstructured.Unstructured) {
	credentials, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "accessCredentials")
	for i := range credentials {
		credential, cast := credentials[i].(map[string]any)
		if !cast {
			continue
		}
		r.add(cluster.KindSecret, nestedString(credential, "sshPublicKey", "source", "secret", "secretName"))
		r.add(cluster.KindSecret, nestedString(credential, "userPassword", "source", "secret", "secretName"))
	}
}

// addNetworks follows the NetworkAttachmentDefinitions behind the
// VirtualMachine's Multus networks. A pod network names no object.
func (r *closure) addNetworks(vm *unstructured.Unstructured) {
	networks, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "networks")
	for i := range networks {
		network, cast := networks[i].(map[string]any)
		if !cast {
			continue
		}
		namespace, name := splitNetworkName(nestedString(network, "multus", "networkName"), r.namespace)
		r.addIn(cluster.KindNetworkAttachmentDefinition, namespace, name)
	}
}

// splitNetworkName separates a Multus network name into the namespace holding
// the NetworkAttachmentDefinition and its name. A bare name is one in the
// VirtualMachine's own namespace.
func splitNetworkName(network, namespace string) (held, name string) {
	held = namespace
	name = network
	at := strings.Index(network, "/")
	if at < 0 {
		return
	}
	held = network[:at]
	name = network[at+1:]
	return
}

// matchesKind reports whether an instancetype or preference matcher names the
// given kind. KubeVirt accepts the singular or plural resource name in any
// case, and treats a matcher naming no kind at all as the cluster-scoped one.
func matchesKind(named, kind string) (matched bool) {
	singular := strings.ToLower(kind)
	matched = strings.ToLower(named) == singular || strings.ToLower(named) == singular+"s"
	return
}

// nestedString returns the string at the path, empty when it is absent.
func nestedString(object map[string]any, keys ...string) (value string) {
	value, _, _ = unstructured.NestedString(object, keys...)
	return
}
