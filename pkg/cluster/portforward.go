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
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// Port forwarding settings
const (
	// forwardReadyTimeout bounds the wait for a forwarded port to open.
	forwardReadyTimeout = time.Minute
	// exportServicePort is the port the export's Service publishes.
	exportServicePort = 443
)

// forward is a local port forwarded to the export server's pod.
type forward struct {
	address string
	stop    chan struct{}
}

// server is the pod serving an export and the port it listens on.
type server struct {
	pod  string
	port int32
}

// forwardTo forwards a local port to the pod behind the export's Service, so
// that a disk can be downloaded from a cluster that publishes no route to it.
func (r *vmExport) forwardTo(ctx context.Context, service string) (err error) {
	found, err := r.getServer(ctx, service)
	if err != nil {
		return
	}
	transport, upgrader, err := spdy.RoundTripperFor(r.config)
	if err != nil {
		err = fmt.Errorf("build a port forward to %s/%s: %w", r.namespace, found.pod, err)
		return
	}
	target := r.core.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(r.namespace).
		Name(found.pod).
		SubResource("portforward").
		URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, target)
	stop := make(chan struct{})
	ready := make(chan struct{})
	ports := []string{fmt.Sprintf("0:%d", found.port)}
	forwarder, err := portforward.New(dialer, ports, stop, ready, io.Discard, io.Discard)
	if err != nil {
		err = fmt.Errorf("build a port forward to %s/%s: %w", r.namespace, found.pod, err)
		return
	}
	failed := make(chan error, 1)
	go func() {
		failed <- forwarder.ForwardPorts()
	}()
	select {
	case <-ready:
	case err = <-failed:
		err = fmt.Errorf("forward a port to %s/%s: %w", r.namespace, found.pod, err)
		return
	case <-ctx.Done():
		close(stop)
		err = ctx.Err()
		return
	case <-time.After(forwardReadyTimeout):
		close(stop)
		err = fmt.Errorf("forwarding a port to %s/%s did not open", r.namespace, found.pod)
		return
	}
	opened, err := forwarder.GetPorts()
	if err != nil {
		close(stop)
		err = fmt.Errorf("forward a port to %s/%s: %w", r.namespace, found.pod, err)
		return
	}
	r.forward = &forward{
		address: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(opened[0].Local))),
		stop:    stop,
	}
	return
}

// getServer returns the pod behind the export's Service and the port the
// export server listens on inside it.
//
// A port is forwarded to a pod rather than to a Service, so the port to ask
// for is the one the Service forwards to and not the one it publishes.
func (r *vmExport) getServer(ctx context.Context, service string) (found server, err error) {
	published, err := r.core.CoreV1().Services(r.namespace).Get(ctx, service, meta.GetOptions{})
	if err != nil {
		err = fmt.Errorf("get export service %s/%s: %w", r.namespace, service, err)
		return
	}
	if len(published.Spec.Selector) == 0 {
		err = fmt.Errorf("export service %s/%s selects no pods", r.namespace, service)
		return
	}
	pods, err := r.core.CoreV1().Pods(r.namespace).List(ctx, meta.ListOptions{
		LabelSelector: k8slabels.SelectorFromSet(published.Spec.Selector).String(),
	})
	if err != nil {
		err = fmt.Errorf("list the pods of export service %s/%s: %w", r.namespace, service, err)
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
	err = fmt.Errorf("export service %s/%s has no running pod", r.namespace, service)
	return
}

// targetPort returns the port on pod that service forwards the export port
// to, resolving a port named rather than numbered against the pod itself.
func targetPort(service *core.Service, pod *core.Pod) (port int32, err error) {
	published := servicePort(service)
	if published == nil {
		err = fmt.Errorf(
			"export service %s/%s publishes no ports",
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

// servicePort returns the port the export server is published on.
func servicePort(service *core.Service) (found *core.ServicePort) {
	for i := range service.Spec.Ports {
		if service.Spec.Ports[i].Port != exportServicePort {
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
