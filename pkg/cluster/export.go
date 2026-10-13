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
	// KubeVirt defaults to two hours, which a large transfer can outlast, and
	// an export that expires part-way through would waste the whole capture.
	exportTTL = 48 * time.Hour
	// exportReadyTimeout bounds the wait for the export server to come up.
	exportReadyTimeout = 10 * time.Minute
	// exportPollInterval is how often the export is read while waiting for it.
	exportPollInterval = 2 * time.Second
	// exportTokenHeader carries the export token on a download request.
	exportTokenHeader = "x-kubevirt-export-token"
	// exportTokenKey is where KubeVirt reads the token out of the secret.
	exportTokenKey = "token"
	// exportServicePrefix is what KubeVirt names an export's Service after.
	exportServicePrefix = "virt-export-"
	// exportNameLimit keeps that derived Service name within the 63
	// characters Kubernetes allows a name.
	exportNameLimit = 63 - len(exportServicePrefix)
)

// Export phases
const (
	phaseReady      = "Ready"
	phaseSkipped    = "Skipped"
	phaseTerminated = "Terminated"
)

// vmExport is a VirtualMachineExport, the token secret that goes with it, and
// the connection to the export server serving its disks.
type vmExport struct {
	dynamic     dynamic.Interface
	core        kubernetes.Interface
	config      *rest.Config
	namespace   string
	vm          string
	name        string
	token       string
	offered     []ExportedDisk
	linkByClaim map[string]string
	client      *http.Client
	forward     *forward
}

// newVMExport names an export after the VirtualMachine it exports, with a
// random suffix so that two captures of one VirtualMachine do not collide.
func newVMExport(c *KubeVirt, namespace, vm string) (export *vmExport, err error) {
	suffix, err := newRandom(4)
	if err != nil {
		return
	}
	name := "quo-" + vm
	limit := exportNameLimit - len(suffix) - 1
	if len(name) > limit {
		name = name[:limit]
	}
	export = &vmExport{
		dynamic:     c.dynamic,
		core:        c.core,
		config:      c.config,
		namespace:   namespace,
		vm:          vm,
		name:        name + "-" + suffix,
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

// Close removes the export and its token secret from the cluster and stops
// the port forward. Both deletions are attempted whatever either one does.
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

// refused reports a phase the export will not come back from, so that a
// capture fails at once rather than waiting out the timeout.
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

// connect works out where the export server is and how to reach it.
//
// An external link is used where the cluster publishes one. Where it does
// not, quo forwards a port to the export server's pod, so that capturing
// needs no ingress or route on the cluster.
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
// against. The export names it in its status; the host of a link is the
// fallback for a cluster that does not.
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
