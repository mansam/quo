package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
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
	c = &KubeVirt{
		dynamic:   dynamicClient,
		discovery: discoveryClient,
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

// OpenDisk is not implemented in this build. Disks arrive with
// VirtualMachineExport support. The method is part of the interface already so
// that its shape is settled and no test ever needs an HTTP server.
func (r *KubeVirt) OpenDisk(ctx context.Context, namespace, claim string) (stream io.ReadCloser, err error) {
	err = errors.New("capturing disks is not implemented in this build")
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
