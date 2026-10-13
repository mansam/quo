package cluster

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Resources
var (
	virtualMachineResource = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachines",
	}
	kubeVirtResource = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "kubevirts",
	}
)

// KubeVirt is the Cluster backed by a real cluster. It is a thin adapter over
// the Kubernetes client and holds no logic of its own.
//
// Objects are read as unstructured so that every field survives the round trip
// into a bundle, including fields this build of quo does not know about.
// Decoding into a typed struct would silently drop them, and fidelity does not
// allow that.
type KubeVirt struct {
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	core      kubernetes.Interface
	config    *rest.Config
	host      string
}

// NewKubeVirt builds a Cluster from a kubeconfig. An empty path uses the
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
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		err = fmt.Errorf("build cluster client: %w", err)
		return
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		err = fmt.Errorf("build discovery client: %w", err)
		return
	}
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
// A read that failed is reported as one of the capture's own signals, because
// none of them is a reason to abandon a capture and each is recorded
// differently in the capture record.
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
		err = r.mapReadError(ref, served.gvr, err)
		return
	}
	return
}

// GetInfo returns what a bundle records about this cluster.
//
// The versions are best effort. Both are read from places a namespace owner is
// often not allowed to reach, and losing a version string is not a reason to
// abandon a capture that may take hours.
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
		// Anything created before the failure is removed again.
		_ = created.Close()
		return
	}
	export = created
	return
}

// mapReadError maps a failure to read an object onto the signal a capture acts
// on and records.
//
// A kind no resource serves reads back from the API as the object simply not
// being there, so a not-found is confirmed against discovery before it is
// reported as one. The two are not interchangeable to a reader: an object
// that was deleted is part of the VirtualMachine's history, while a component
// that was never installed says something about the cluster.
func (r *KubeVirt) mapReadError(ref ObjectRef, gvr schema.GroupVersionResource, err error) (mapped error) {
	switch {
	case k8serr.IsForbidden(err):
		mapped = fmt.Errorf("%s %q: %w", ref.Kind, ref.Name, ErrForbidden)
	case k8serr.IsNotFound(err):
		mapped = fmt.Errorf("%s %q: %w", ref.Kind, ref.Name, ErrNotFound)
		if !r.serves(gvr) {
			mapped = fmt.Errorf("%s: %w", ref.Kind, ErrNotInstalled)
		}
	default:
		mapped = fmt.Errorf("get %s %q: %w", ref.Kind, ref.Name, err)
	}
	return
}

// serves reports whether the cluster has the resource at all. Discovery that
// cannot be read is taken as the resource being served, so that an answer quo
// does not have is never reported as a component being absent.
func (r *KubeVirt) serves(gvr schema.GroupVersionResource) (served bool) {
	list, err := r.discovery.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	if err != nil {
		served = !k8serr.IsNotFound(err)
		return
	}
	for i := range list.APIResources {
		if list.APIResources[i].Name != gvr.Resource {
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
	list, err := r.dynamic.
		Resource(kubeVirtResource).
		Namespace(meta.NamespaceAll).
		List(ctx, meta.ListOptions{})
	if err != nil || len(list.Items) == 0 {
		return
	}
	version, _, _ = unstructured.NestedString(
		list.Items[0].Object,
		"status",
		"observedKubeVirtVersion")
	return
}
