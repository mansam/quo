package cluster

import (
	"context"
	"fmt"
	"os"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	k8scached "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// Resources
var (
	virtualMachineResource = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachines",
	}
	// namespaceResource serves namespaces. A deploy reads a namespace
	// through it before creating anything. The deploy can then refuse
	// when the namespace it was told to deploy into is not there.
	namespaceResource = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "namespaces",
	}
	// dataVolumeResource serves DataVolumes. A deploy creates DataVolumes
	// to receive a bundle's disks. The deploy reads them back while CDI
	// prepares them.
	dataVolumeResource = schema.GroupVersionResource{
		Group:    "cdi.kubevirt.io",
		Version:  "v1beta1",
		Resource: "datavolumes",
	}
	kubeVirtResource = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "kubevirts",
	}
	// expandSpecResource serves the subresource that resolves a
	// VirtualMachine's instancetype and preference into its spec.
	expandSpecResource = schema.GroupVersionResource{
		Group:    "subresources.kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachines",
	}
)

const (
	// expandSpec is the subresource returning a VirtualMachine's resolved
	// form.
	expandSpec = "expand-spec"
	// expandSpecName is how discovery lists that subresource. A subresource
	// takes its name from the resource it belongs to as well as from
	// itself. The subresource group lists no bare virtualmachines resource
	// at all. Looking for one would report expand-spec as absent from
	// every cluster.
	expandSpecName = "virtualmachines/" + expandSpec
)

// KubeVirt is the adapter over a real cluster, serving both directions quo
// works in. It is a Cluster, for reading a cluster. It is a Target, for
// putting a deployment onto one. It adapts the Kubernetes client and holds
// no logic of its own.
//
// quo reads objects as unstructured so that every field survives the round
// trip into a bundle. That includes fields this build of quo does not know
// about. Decoding into a typed struct would silently drop them. Fidelity
// does not allow that.
type KubeVirt struct {
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	core      kubernetes.Interface
	config    *rest.Config
	host      string
	// mapper finds the resource serving an object's kind. A deploy can
	// then create what a bundle holds, without quo having to name the
	// resource itself.
	mapper apimeta.RESTMapper
}

// NewKubeVirt builds a Cluster from a kubeconfig. An empty path means the
// ambient configuration the Kubernetes tooling would use.
func NewKubeVirt(kubeconfig string) (c *KubeVirt, err error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		err = fmt.Errorf("load kubeconfig: %w", err)
		return
	}
	// Every client built below shares this handler. A cluster can
	// attach the same warning to every answer it gives, and client-go's
	// default prints one line per answer. A wait that polls for minutes
	// would then bury the terminal in the same warning.
	config.WarningHandler = newWarningHandler(os.Stderr)
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		err = fmt.Errorf("build cluster client: %w", err)
		return
	}
	plainDiscovery, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		err = fmt.Errorf("build discovery client: %w", err)
		return
	}
	// The mapper reads discovery lazily and reloads it when a mapping
	// misses. It therefore needs a client that caches between the two.
	discoveryClient := k8scached.NewMemCacheClient(plainDiscovery)
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		err = fmt.Errorf("build core client: %w", err)
		return
	}
	c = &KubeVirt{
		dynamic:   dynamicClient,
		discovery: discoveryClient,
		core:      coreClient,
		config:    config,
		host:      config.Host,
		mapper:    restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient),
	}
	return
}

// GetVirtualMachine returns the named VirtualMachine.
func (r *KubeVirt) GetVirtualMachine(ctx context.Context, namespace, name string) (vm *unstructured.Unstructured, err error) {
	vm, err = r.dynamic.
		Resource(virtualMachineResource).
		Namespace(namespace).
		Get(ctx, name, meta.GetOptions{})
	if err != nil {
		vm = nil
		err = fmt.Errorf("get VirtualMachine %s/%s: %w", namespace, name, err)
		return
	}
	return
}

// GetObject returns the object the reference names.
//
// GetObject reports a failed read as one of this package's own errors.
// None of them is a reason to abandon a capture. The capture record
// records each one differently.
func (r *KubeVirt) GetObject(ctx context.Context, ref ObjectRef) (object *unstructured.Unstructured, err error) {
	served, known := resourceByKind[ref.Kind]
	if !known {
		err = fmt.Errorf("quo does not know how to read a %s", ref.Kind)
		return
	}
	api := r.dynamic.Resource(served.gvr)
	reader := dynamic.ResourceInterface(api)
	if !served.clusterScoped {
		reader = api.Namespace(ref.Namespace)
	}
	object, err = reader.Get(ctx, ref.Name, meta.GetOptions{})
	if err != nil {
		object = nil
		err = r.mapReadError(ref, served.gvr.GroupVersion(), served.gvr.Resource, err)
		return
	}
	return
}

// GetKubeVirt returns KubeVirt's own configuration.
//
// KubeVirt is a singleton whose namespace and name differ between
// installations, so GetKubeVirt lists it rather than reading it by name.
// GetKubeVirt reports a failed read as one of this package's own errors.
// The resource is cluster-scoped and commonly forbidden to the person
// capturing a bundle.
func (r *KubeVirt) GetKubeVirt(ctx context.Context) (kv *unstructured.Unstructured, err error) {
	list, err := r.dynamic.
		Resource(kubeVirtResource).
		Namespace(meta.NamespaceAll).
		List(ctx, meta.ListOptions{})
	if err != nil {
		err = r.mapListError(err)
		return
	}
	if len(list.Items) == 0 {
		err = fmt.Errorf("%s: %w", KindKubeVirt, ErrNotFound)
		return
	}
	kv = &list.Items[0]
	return
}

// ExpandSpec returns the form this cluster resolves the named VirtualMachine
// to.
//
// virt-api serves the subresource, not the API server itself. ExpandSpec
// therefore reads it through its own resource, rather than as a field of
// the VirtualMachine.
func (r *KubeVirt) ExpandSpec(
	ctx context.Context,
	namespace, name string) (expanded *unstructured.Unstructured, err error) {
	//
	ref := ObjectRef{Kind: KindVirtualMachine, Namespace: namespace, Name: name}
	expanded, err = r.dynamic.
		Resource(expandSpecResource).
		Namespace(namespace).
		Get(ctx, name, meta.GetOptions{}, expandSpec)
	if err != nil {
		expanded = nil
		err = r.mapReadError(ref, expandSpecResource.GroupVersion(), expandSpecName, err)
		return
	}
	return
}

// GetInfo returns what a bundle records about this cluster.
//
// The versions are best effort. GetInfo reads both from places a namespace
// owner is often not allowed to reach. Losing a version string is not a
// reason to abandon a capture that may take hours.
func (r *KubeVirt) GetInfo(ctx context.Context) (info Info, err error) {
	info = Info{
		SourceCluster:     r.host,
		KubernetesVersion: r.kubernetesVersion(),
		KubeVirtVersion:   r.kubeVirtVersion(ctx),
	}
	return
}

// ExportDisks creates a VirtualMachineExport for the named VirtualMachine and
// waits for it to become ready.
func (r *KubeVirt) ExportDisks(ctx context.Context, namespace, name string) (export DiskExport, err error) {
	created, err := newVMExport(r, namespace, name)
	if err != nil {
		return
	}
	err = created.start(ctx)
	if err != nil {
		// ExportDisks deletes anything created before the failure.
		_ = created.Close()
		return
	}
	export = created
	return
}

// mapReadError maps a failure to read an object onto the error a capture acts
// on and records.
//
// A kind no resource serves reads back from the API as the object simply
// not being there. mapReadError therefore confirms a not-found against
// discovery before it reports one. The two are not interchangeable to a
// reader. An object that was deleted is part of the VirtualMachine's
// history. A component that was never installed says something about the
// cluster.
//
// The name is the one discovery lists for the resource. The caller supplies
// it, because a subresource is not named for itself alone.
func (r *KubeVirt) mapReadError(
	ref ObjectRef,
	gv schema.GroupVersion,
	name string,
	err error) (mapped error) {
	//
	switch {
	case k8serr.IsForbidden(err):
		mapped = fmt.Errorf("%s %q: %w", ref.Kind, ref.Name, ErrForbidden)
	case k8serr.IsNotFound(err):
		mapped = fmt.Errorf("%s %q: %w", ref.Kind, ref.Name, ErrNotFound)
		if !r.serves(gv, name) {
			mapped = fmt.Errorf("%s: %w", ref.Kind, ErrNotInstalled)
		}
	default:
		mapped = fmt.Errorf("get %s %q: %w", ref.Kind, ref.Name, err)
	}
	return
}

// mapListError maps a failure to list the KubeVirt singleton onto the error
// a capture acts on and records. A list names no object, so neither does
// the error it produces.
//
// A not-found needs no confirming against discovery the way a read does. A
// list of a resource the cluster serves returns an empty list rather than
// a not-found. A not-found here already means the resource is not served.
func (r *KubeVirt) mapListError(err error) (mapped error) {
	switch {
	case k8serr.IsForbidden(err):
		mapped = fmt.Errorf("%s: %w", KindKubeVirt, ErrForbidden)
	case k8serr.IsNotFound(err):
		mapped = fmt.Errorf("%s: %w", KindKubeVirt, ErrNotInstalled)
	default:
		mapped = fmt.Errorf("list %s: %w", KindKubeVirt, err)
	}
	return
}

// serves reports whether the cluster has the named resource at all. The
// name is the one discovery lists. For a subresource, that name includes
// the resource it belongs to.
//
// When discovery cannot be read, serves treats the resource as served. An
// answer quo does not have is then never reported as a component being
// absent.
func (r *KubeVirt) serves(gv schema.GroupVersion, name string) (served bool) {
	list, err := r.discovery.ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		served = !k8serr.IsNotFound(err)
		return
	}
	for i := range list.APIResources {
		if list.APIResources[i].Name != name {
			continue
		}
		served = true
		return
	}
	return
}

// kubernetesVersion returns the cluster's version, empty when it cannot be
// read.
func (r *KubeVirt) kubernetesVersion() (version string) {
	found, err := r.discovery.ServerVersion()
	if err != nil {
		return
	}
	version = found.GitVersion
	return
}

// kubeVirtVersion returns the version KubeVirt reports for itself, empty when
// it cannot be read. The KubeVirt resource is cluster-scoped and commonly
// forbidden to the person capturing a bundle.
func (r *KubeVirt) kubeVirtVersion(ctx context.Context) (version string) {
	kv, err := r.GetKubeVirt(ctx)
	if err != nil {
		return
	}
	version, _, _ = unstructured.NestedString(kv.Object, "status", "observedKubeVirtVersion")
	return
}
