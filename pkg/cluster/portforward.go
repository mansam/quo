package cluster

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// Port forwarding settings
const (
	// forwardReadyTimeout bounds the wait for a forwarded port to open.
	forwardReadyTimeout = time.Minute
	// dataServicePort is the port the data services quo reaches publish.
	// The export server's Service and CDI's upload proxy both serve on
	// 443.
	dataServicePort = 443
)

// forward is a local port forwarded to a pod inside the cluster.
type forward struct {
	address string
	stop    chan struct{}
}

// server is a pod serving data and the port it listens on.
type server struct {
	pod  string
	port int32
}

// forwardTo forwards a local port to the pod behind a cluster's Service.
// A caller can then reach a server inside the cluster, without the cluster
// publishing a route to it. The forward is the caller's to stop.
func forwardTo(
	ctx context.Context,
	core kubernetes.Interface,
	config *rest.Config,
	namespace, service string) (f *forward, err error) {
	//
	found, err := getServer(ctx, core, namespace, service)
	if err != nil {
		return
	}
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		err = fmt.Errorf("build a port forward to %s/%s: %w", namespace, found.pod, err)
		return
	}
	target := core.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(found.pod).
		SubResource("portforward").
		URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, target)
	stop := make(chan struct{})
	ready := make(chan struct{})
	ports := []string{fmt.Sprintf("0:%d", found.port)}
	forwarder, err := portforward.New(dialer, ports, stop, ready, io.Discard, io.Discard)
	if err != nil {
		err = fmt.Errorf("build a port forward to %s/%s: %w", namespace, found.pod, err)
		return
	}
	failed := make(chan error, 1)
	go func() {
		failed <- forwarder.ForwardPorts()
	}()
	select {
	case <-ready:
	case err = <-failed:
		err = fmt.Errorf("forward a port to %s/%s: %w", namespace, found.pod, err)
		return
	case <-ctx.Done():
		close(stop)
		err = ctx.Err()
		return
	case <-time.After(forwardReadyTimeout):
		close(stop)
		err = fmt.Errorf("forwarding a port to %s/%s did not open", namespace, found.pod)
		return
	}
	opened, err := forwarder.GetPorts()
	if err != nil {
		close(stop)
		err = fmt.Errorf("forward a port to %s/%s: %w", namespace, found.pod, err)
		return
	}
	f = &forward{
		address: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(opened[0].Local))),
		stop:    stop,
	}
	return
}

// forwardTo forwards a local port to the pod behind the export's Service.
// quo can then download a disk from a cluster that publishes no route to
// it.
func (r *vmExport) forwardTo(ctx context.Context, service string) (err error) {
	r.forward, err = forwardTo(ctx, r.core, r.config, r.namespace, service)
	return
}

// getServer returns the pod behind a Service and the port the server
// listens on inside it.
//
// A port forward goes to a pod rather than to a Service. The port to ask
// for is therefore the one the Service forwards to, not the one it
// publishes.
func getServer(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, service string) (found server, err error) {
	//
	published, err := client.CoreV1().Services(namespace).Get(ctx, service, meta.GetOptions{})
	if err != nil {
		err = fmt.Errorf("get service %s/%s: %w", namespace, service, err)
		return
	}
	if len(published.Spec.Selector) == 0 {
		err = fmt.Errorf("service %s/%s selects no pods", namespace, service)
		return
	}
	pods, err := client.CoreV1().Pods(namespace).List(ctx, meta.ListOptions{
		LabelSelector: k8slabels.SelectorFromSet(published.Spec.Selector).String(),
	})
	if err != nil {
		err = fmt.Errorf("list the pods of service %s/%s: %w", namespace, service, err)
		return
	}
	for i := range pods.Items {
		if pods.Items[i].Status.Phase != core.PodRunning {
			continue
		}
		found.pod = pods.Items[i].Name
		found.port, err = targetPort(published, &pods.Items[i])
		return
	}
	err = fmt.Errorf("service %s/%s has no running pod", namespace, service)
	return
}

// targetPort returns the port on the pod that the Service forwards the
// data port to. Where the Service names a port rather than numbering it,
// targetPort resolves the name against the pod.
func targetPort(service *core.Service, pod *core.Pod) (port int32, err error) {
	published := servicePort(service)
	if published == nil {
		err = fmt.Errorf(
			"service %s/%s publishes no ports",
			service.Namespace,
			service.Name)
		return
	}
	switch published.TargetPort.Type {
	case intstr.String:
		port, err = namedPort(pod, published.TargetPort.StrVal)
		return
	default:
		port = int32(published.TargetPort.IntValue())
	}
	if port == 0 {
		// A target port left unset means the published port itself.
		port = published.Port
	}
	return
}

// servicePort returns the port the Service publishes for the server.
func servicePort(service *core.Service) (found *core.ServicePort) {
	for i := range service.Spec.Ports {
		if service.Spec.Ports[i].Port != dataServicePort {
			continue
		}
		found = &service.Spec.Ports[i]
		return
	}
	if len(service.Spec.Ports) > 0 {
		found = &service.Spec.Ports[0]
	}
	return
}

// namedPort returns the container port a name refers to.
func namedPort(pod *core.Pod, name string) (port int32, err error) {
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		for j := range container.Ports {
			if container.Ports[j].Name != name {
				continue
			}
			port = container.Ports[j].ContainerPort
			return
		}
	}
	err = fmt.Errorf("pod %s/%s has no port named %q", pod.Namespace, pod.Name, name)
	return
}
