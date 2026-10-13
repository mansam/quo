//go:build live

// Package live tests quo against a real KubeVirt cluster.
//
// Two things the everyday suite cannot check happen here. The first is that
// quo's own closure traversal finds what KubeVirt's objectgraph subresource
// finds for the same VirtualMachine: quo traverses for itself because that
// subresource sits behind a feature gate that is off by default, and quo's
// copy of KubeVirt's logic is not maintained alongside the original. The
// second is that the real cluster adapter works against a real API, which the
// everyday suite deliberately bypasses by substituting a fake. A thin adapter
// is mostly field names and subresource paths, and no test that substitutes a
// fake for it can tell a correct one from a wrong one.
//
// The suite is behind the "live" build tag and is excluded from the default
// test run, so local development never needs a cluster. See
// docs/testing-against-a-cluster.md for how to run it and what the cluster
// must have.
package live

import (
	"context"
	"fmt"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/mansam/kvo/pkg/cluster"
)

// Environment the suite reads.
const (
	// envKubeconfig names the kubeconfig to connect with. Empty means the
	// ambient configuration the Kubernetes tooling would use.
	envKubeconfig = "QUO_LIVE_KUBECONFIG"
	// envNamespace names an existing namespace to build the fixture in.
	// Empty means the suite creates one and removes it again, which needs
	// permission to create a namespace.
	envNamespace = "QUO_LIVE_NAMESPACE"
	// envStorageClass names the storage class the fixture disk is
	// provisioned from. Empty means whichever one the cluster defaults to.
	envStorageClass = "QUO_LIVE_STORAGE_CLASS"
)

// started is the cluster the suite runs against, and unavailable is why there
// is none. Exactly one of the two is set by the time any test runs.
var (
	started     *suite
	unavailable string
)

// suite is the cluster the tests run against and the fixture they share.
//
// quo is the adapter under test. The other clients are the suite's own, so
// that what it reads to check quo never goes through quo.
type suite struct {
	dynamic dynamic.Interface
	core    kubernetes.Interface
	quo     *cluster.KubeVirt
	fixture *fixture
	// capture is the one capture of the fixture the tests take between them.
	capture sharedCapture
}

// requirement is one thing the suite needs from a cluster: the resource whose
// presence says it is there, and what to report when it is absent.
type prerequisite struct {
	groupVersion string
	resource     string
	absent       string
}

// requirements are what a cluster must offer before the suite will run, in
// the order they are reported.
//
// The ObjectGraph feature gate is not among them. Nothing in discovery says
// whether it is on, because virt-api serves the route either way and refuses
// the request when the gate is off, so the traversal comparison finds out by
// asking and skips for itself.
var prerequisites = []prerequisite{
	{
		groupVersion: "kubevirt.io/v1",
		resource:     "virtualmachines",
		absent:       "KubeVirt is not installed",
	},
	{
		groupVersion: "cdi.kubevirt.io/v1beta1",
		resource:     "datavolumes",
		absent:       "CDI is not installed, so the fixture has no way to provision a disk",
	},
	{
		groupVersion: subresourceGroupVersion,
		resource:     objectGraphResource,
		absent:       "this build of KubeVirt does not serve the objectgraph subresource",
	},
}

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run connects to the cluster, builds the fixture, runs the suite, and takes
// the fixture down again.
//
// A cluster the suite cannot use is not a failure: the reason is recorded and
// every test skips with it, so that a run on a machine with no cluster says
// what it would have needed. A cluster that meets the requirements and then
// cannot host the fixture is a failure, because something that was supposed
// to work did not.
func run(m *testing.M) (code int) {
	ctx := context.Background()
	s, reason := connect()
	if reason != "" {
		unavailable = reason
		code = m.Run()
		return
	}
	defer s.delete(ctx)
	err := s.fixture.create(ctx, s)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "live suite: build the fixture: %v\n", err)
		code = 1
		return
	}
	started = s
	code = m.Run()
	return
}

// delete takes down everything a run created: the fixture on the cluster,
// and the directory the bundle the tests share was written to.
func (r *suite) delete(ctx context.Context) {
	r.fixture.delete(ctx, r)
	if r.capture.scratch == "" {
		return
	}
	_ = os.RemoveAll(r.capture.scratch)
}

// live returns the cluster the suite runs against, skipping the test when
// there is none.
func live(t *testing.T) (s *suite) {
	t.Helper()
	if started == nil {
		t.Skip(unavailable)
	}
	s = started
	return
}

// connect builds the clients the suite needs and reports what it needs and
// the cluster does not have.
//
// A reason is not an error. Every way of failing to reach a usable cluster —
// no kubeconfig, an unreachable API, a component that is not installed — is
// something to skip on rather than fail on, because none of them says
// anything about quo.
func connect() (s *suite, reason string) {
	config, err := loadConfig()
	if err != nil {
		reason = unmet(err.Error())
		return
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		reason = unmet("build a cluster client: " + err.Error())
		return
	}
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		reason = unmet("build a core client: " + err.Error())
		return
	}
	quo, err := cluster.NewKubeVirt(os.Getenv(envKubeconfig))
	if err != nil {
		reason = unmet(err.Error())
		return
	}
	built := &suite{
		dynamic: dynamicClient,
		core:    coreClient,
		quo:     quo,
		fixture: newFixture(),
	}
	reason = built.unsupported()
	if reason != "" {
		return
	}
	s = built
	return
}

// unsupported reports what the suite needs and this cluster does not offer,
// empty when the cluster will do.
func (r *suite) unsupported() (reason string) {
	_, err := r.core.Discovery().ServerVersion()
	if err != nil {
		reason = unmet("the cluster is not reachable: " + err.Error())
		return
	}
	for i := range prerequisites {
		if r.serves(prerequisites[i].groupVersion, prerequisites[i].resource) {
			continue
		}
		reason = unmet(prerequisites[i].absent)
		return
	}
	return
}

// writerFor returns the client that reads and writes one resource, in the
// namespace the object is held in. A cluster-scoped object is held in none.
func (r *suite) writerFor(
	gvr schema.GroupVersionResource,
	namespace string) (writer dynamic.ResourceInterface) {
	//
	api := r.dynamic.Resource(gvr)
	writer = api
	if namespace == "" {
		return
	}
	writer = api.Namespace(namespace)
	return
}

// serves reports whether the cluster has the named resource. The name is the
// one discovery lists, which for a subresource includes the resource it hangs
// off.
func (r *suite) serves(groupVersion, name string) (served bool) {
	list, err := r.core.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
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

// loadConfig reads the kubeconfig the suite connects with.
func loadConfig() (config *rest.Config, err error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeconfig := os.Getenv(envKubeconfig)
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		config = nil
		err = fmt.Errorf("load kubeconfig: %w", err)
		return
	}
	return
}

// unmet renders why the suite will not run, followed by what a cluster would
// have to offer.
//
// The ObjectGraph feature gate is named and the command to enable it is given
// because the gate is off by default, which makes it the likeliest reason a
// cluster that looks suitable turns out not to be.
func unmet(detail string) (reason string) {
	reason = "no cluster the live suite can use: " + detail + "\n" +
		"\n" +
		"The live suite needs a cluster with:\n" +
		"  - KubeVirt installed, new enough to serve the objectgraph subresource\n" +
		"  - KubeVirt's ObjectGraph feature gate enabled, which is off by default:\n" +
		"      kubectl patch kubevirt kubevirt -n kubevirt --type merge -p \\\n" +
		"        '{\"spec\":{\"configuration\":{\"developerConfiguration\":" +
		"{\"featureGates\":[\"ObjectGraph\"]}}}}'\n" +
		"  - CDI installed, and a storage class that can provision a " + fixtureDiskSize + " volume\n" +
		"  - permission to create a namespace, or " + envNamespace + " naming one to use\n" +
		"\n" +
		"See docs/testing-against-a-cluster.md.\n"
	return
}
