package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	core "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Resources
var virtualMachineExportResource = schema.GroupVersionResource{
	Group:    "export.kubevirt.io",
	Version:  "v1beta1",
	Resource: "virtualmachineexports",
}

// Export settings
const (
	// exportTTL is how long the cluster keeps a VirtualMachineExport alive.
	// KubeVirt defaults to two hours. A large transfer can outlast two hours.
	// An export that expires part-way through would waste the whole capture.
	exportTTL = 48 * time.Hour
	// exportReadyTimeout bounds the wait for the export server to become
	// ready.
	exportReadyTimeout = 10 * time.Minute
	// exportPollInterval is how often the loop reads the export during the
	// wait.
	exportPollInterval = 2 * time.Second
	// exportTokenHeader carries the export token on a download request.
	exportTokenHeader = "x-kubevirt-export-token"
	// exportTokenKey is the key that holds the token in the secret.
	exportTokenKey = "token"
	// exportServicePrefix is the prefix of the name KubeVirt gives an
	// export's Service.
	exportServicePrefix = "virt-export-"
	// exportNameLimit keeps the derived Service name within the 63
	// characters that Kubernetes allows a name.
	exportNameLimit = 63 - len(exportServicePrefix)
)

// Export phases
const (
	phaseReady      = "Ready"
	phaseSkipped    = "Skipped"
	phaseTerminated = "Terminated"
)

// Export is one VirtualMachine's disks, asked of the cluster as a
// VirtualMachineExport.
type Export struct {
	// Namespace and Name say which VirtualMachine the export serves.
	Namespace string
	Name      string
	// Waiting, when set, is called each time the cluster checks whether
	// the export is ready to serve its disks. said is what the capture
	// waits on, in words a person can read. No bytes have moved.
	Waiting func(said string)
}

// vmExport is a VirtualMachineExport, its token secret, and the connection
// to the export server that serves its disks.
type vmExport struct {
	dynamic     dynamic.Interface
	core        kubernetes.Interface
	config      *rest.Config
	namespace   string
	vm          string
	name        string
	// waiting, when set, is called each time the export is checked for
	// readiness, saying what it waits on.
	waiting     func(said string)
	token       string
	offered     []ExportedDisk
	linkByClaim map[string]string
	client      *http.Client
	forward     *forward
}

// newVMExport builds an export's name from the VirtualMachine it exports
// and a random suffix. The suffix keeps two captures of one VirtualMachine
// from colliding.
func newVMExport(c *KubeVirt, export Export) (created *vmExport, err error) {
	suffix, err := newRandom(4)
	if err != nil {
		return
	}
	name := "quo-" + export.Name
	limit := exportNameLimit - len(suffix) - 1
	if len(name) > limit {
		name = name[:limit]
	}
	created = &vmExport{
		dynamic:     c.dynamic,
		core:        c.core,
		config:      c.config,
		namespace:   export.Namespace,
		vm:          export.Name,
		name:        name + "-" + suffix,
		waiting:     export.Waiting,
		linkByClaim: map[string]string{},
	}
	return
}

// start creates the export and waits for the export server to be ready to
// serve its disks.
func (r *vmExport) start(ctx context.Context) (err error) {
	err = r.createToken(ctx)
	if err != nil {
		return
	}
	err = r.create(ctx)
	if err != nil {
		return
	}
	ready, err := r.awaitReady(ctx)
	if err != nil {
		return
	}
	err = r.connect(ctx, ready)
	return
}

// Disks returns what the export offers, in the order its status lists it.
func (r *vmExport) Disks() (disks []ExportedDisk) {
	disks = r.offered
	return
}

// OpenDisk opens the byte stream carrying the disk that backs the named
// PersistentVolumeClaim.
func (r *vmExport) OpenDisk(ctx context.Context, claim string) (stream io.ReadCloser, err error) {
	link, found := r.linkByClaim[claim]
	if !found {
		err = fmt.Errorf("the export offers no disk for PersistentVolumeClaim %q", claim)
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		err = fmt.Errorf("build a request for disk %q: %w", claim, err)
		return
	}
	request.Header.Set(exportTokenHeader, r.token)
	response, err := r.client.Do(request)
	if err != nil {
		err = fmt.Errorf("download disk %q: %w", claim, err)
		return
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		err = fmt.Errorf(
			"download disk %q: the export server answered %s",
			claim,
			response.Status)
		return
	}
	stream = response.Body
	return
}

// Close deletes the export and its token secret from the cluster and stops
// the port forward. Close attempts both deletions. The result of one does
// not stop the other.
func (r *vmExport) Close() (err error) {
	if r.forward != nil {
		close(r.forward.stop)
		r.forward = nil
	}
	ctx := context.TODO()
	exportErr := r.dynamic.
		Resource(virtualMachineExportResource).
		Namespace(r.namespace).
		Delete(ctx, r.name, meta.DeleteOptions{})
	if k8serr.IsNotFound(exportErr) {
		exportErr = nil
	}
	secretErr := r.core.CoreV1().
		Secrets(r.namespace).
		Delete(ctx, r.name, meta.DeleteOptions{})
	if k8serr.IsNotFound(secretErr) {
		secretErr = nil
	}
	err = errors.Join(exportErr, secretErr)
	if err != nil {
		err = fmt.Errorf("remove export %s/%s: %w", r.namespace, r.name, err)
		return
	}
	return
}

// createToken creates the secret holding the token the export server requires
// on every download.
func (r *vmExport) createToken(ctx context.Context) (err error) {
	r.token, err = newRandom(32)
	if err != nil {
		return
	}
	secret := &core.Secret{
		ObjectMeta: meta.ObjectMeta{
			Name:      r.name,
			Namespace: r.namespace,
		},
		StringData: map[string]string{exportTokenKey: r.token},
	}
	_, err = r.core.CoreV1().Secrets(r.namespace).Create(ctx, secret, meta.CreateOptions{})
	if err != nil {
		err = fmt.Errorf("create export token secret %s/%s: %w", r.namespace, r.name, err)
		return
	}
	return
}

// create submits the VirtualMachineExport.
func (r *vmExport) create(ctx context.Context) (err error) {
	_, err = r.dynamic.
		Resource(virtualMachineExportResource).
		Namespace(r.namespace).
		Create(ctx, r.build(), meta.CreateOptions{})
	if err != nil {
		err = fmt.Errorf("create VirtualMachineExport %s/%s: %w", r.namespace, r.name, err)
		return
	}
	return
}

// build returns the VirtualMachineExport that exports the VirtualMachine's
// disks.
func (r *vmExport) build() (export *unstructured.Unstructured) {
	export = &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": virtualMachineExportResource.GroupVersion().String(),
			"kind":       "VirtualMachineExport",
			"metadata": map[string]any{
				"name":      r.name,
				"namespace": r.namespace,
			},
			"spec": map[string]any{
				"tokenSecretRef": r.name,
				"ttlDuration":    exportTTL.String(),
				"source": map[string]any{
					"apiGroup": "kubevirt.io",
					"kind":     "VirtualMachine",
					"name":     r.vm,
				},
			},
		},
	}
	return
}

// awaitReady waits for the export server to be serving.
func (r *vmExport) awaitReady(ctx context.Context) (export *unstructured.Unstructured, err error) {
	deadline := time.Now().Add(exportReadyTimeout)
	for {
		export, err = r.get(ctx)
		if err != nil {
			return
		}
		phase, _, _ := unstructured.NestedString(export.Object, "status", "phase")
		if phase == phaseReady {
			return
		}
		export = nil
		err = r.refused(phase)
		if err != nil {
			return
		}
		if r.waiting != nil {
			if phase == "" {
				// An export straight from a create carries no phase
				// until the export controller gives it one. That is a
				// wait like any other: it says so rather than saying
				// nothing.
				r.waiting("the VirtualMachineExport has no phase yet")
			} else {
				r.waiting(fmt.Sprintf("the VirtualMachineExport is %q", phase))
			}
		}
		if time.Now().After(deadline) {
			err = fmt.Errorf(
				"VirtualMachineExport %s/%s is %q after %s, expected %q",
				r.namespace,
				r.name,
				phase,
				exportReadyTimeout,
				phaseReady)
			return
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-time.After(exportPollInterval):
		}
	}
}

// refused reports a phase the export never recovers from. A capture then
// fails at once, rather than waiting until the timeout ends.
func (r *vmExport) refused(phase string) (err error) {
	switch phase {
	case phaseSkipped:
		err = fmt.Errorf(
			"VirtualMachineExport %s/%s was skipped: the cluster will not export %s",
			r.namespace,
			r.name,
			r.vm)
	case phaseTerminated:
		err = fmt.Errorf("VirtualMachineExport %s/%s was terminated", r.namespace, r.name)
	}
	return
}

// get reads the export back.
func (r *vmExport) get(ctx context.Context) (export *unstructured.Unstructured, err error) {
	export, err = r.dynamic.
		Resource(virtualMachineExportResource).
		Namespace(r.namespace).
		Get(ctx, r.name, meta.GetOptions{})
	if err != nil {
		export = nil
		err = fmt.Errorf("get VirtualMachineExport %s/%s: %w", r.namespace, r.name, err)
		return
	}
	return
}

// connect finds where the export server is and how to reach it.
//
// connect uses an external link where the cluster publishes one. Where the
// cluster publishes none, quo forwards a port to the export server's pod.
// Capturing then needs no ingress or route on the cluster.
func (r *vmExport) connect(ctx context.Context, export *unstructured.Unstructured) (err error) {
	links, external, err := getLinks(export)
	if err != nil {
		return
	}
	offers, err := getCompressed(links)
	if err != nil {
		return
	}
	server, err := url.Parse(offers[0].link)
	if err != nil {
		err = fmt.Errorf("parse export link %q: %w", offers[0].link, err)
		return
	}
	if !external {
		err = r.forwardTo(ctx, serviceName(export, server.Hostname()))
		if err != nil {
			return
		}
	}
	certs, err := buildCertPool(links)
	if err != nil {
		return
	}
	r.client = buildClient(certs, server.Hostname())
	for i := range offers {
		link := offers[i].link
		if r.forward != nil {
			link, err = redirect(link, r.forward.address)
			if err != nil {
				return
			}
		}
		r.offered = append(r.offered, ExportedDisk{
			Claim:  offers[i].claim,
			Format: offers[i].format,
		})
		r.linkByClaim[offers[i].claim] = link
	}
	return
}

// serviceName returns the Service the export published its internal links
// against. The export names the Service in its status. The host of a link
// is the fallback for a cluster that does not.
func serviceName(export *unstructured.Unstructured, host string) (name string) {
	name, _, _ = unstructured.NestedString(export.Object, "status", "serviceName")
	if name != "" {
		return
	}
	name = strings.SplitN(host, ".", 2)[0]
	return
}

// newRandom returns n random bytes as hexadecimal.
func newRandom(n int) (value string, err error) {
	bytes := make([]byte, n)
	_, err = rand.Read(bytes)
	if err != nil {
		err = fmt.Errorf("read random bytes: %w", err)
		return
	}
	value = hex.EncodeToString(bytes)
	return
}
