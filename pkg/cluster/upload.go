package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Upload is one disk's transfer into the claim a DataVolume creates.
type Upload struct {
	// Namespace is the namespace the DataVolume was created in.
	Namespace string
	// Name is the DataVolume's own name. CDI names the claim after the
	// DataVolume itself.
	Name string
	// Content is the disk's bytes. UploadDisk sends them unchanged,
	// exactly as it reads them.
	Content io.Reader
	// Size is the count the bytes will add up to. UploadDisk is told
	// the count, so that a transfer that yields fewer bytes than the
	// count fails rather than ending early.
	Size int64
	// Waiting, when set, is called each time the target checks whether
	// the claim is ready. said is what the target is waiting on, in
	// words a person can read. No bytes have moved.
	Waiting func(said string)
}

// quo asks for the upload token that authorizes one upload through a
// resource of CDI's own.
var uploadTokenRequestResource = schema.GroupVersionResource{
	Group:    "upload.cdi.kubevirt.io",
	Version:  "v1beta1",
	Resource: "uploadtokenrequests",
}

// Upload settings
const (
	// uploadPath is where CDI's upload proxy takes a synchronous upload.
	// The connection stays open until CDI finishes processing the bytes.
	// A response that arrived therefore means a disk that arrived.
	uploadPath = "/v1beta1/upload"
	// uploadTokenPrefix names the token requests quo makes, so that a
	// cluster administrator reading them knows what asked.
	uploadTokenPrefix = "quo-upload-"
	// uploadReadyTimeout bounds the wait for a fresh DataVolume to become
	// ready to receive its bytes. A claim has to be bound and an upload
	// pod placed before CDI takes the bytes. A cluster short on room or
	// images can take a while about either.
	uploadReadyTimeout = 10 * time.Minute
	// uploadPollInterval is how often the loop reads the DataVolume during
	// the wait.
	uploadPollInterval = 2 * time.Second
	// uploadProxyService is the name CDI gives the Service that publishes
	// its upload proxy. uploadProxyBundle is the ConfigMap in the same
	// namespace. It holds the authority that signed the proxy's
	// certificate.
	uploadProxyService = "cdi-uploadproxy"
	uploadProxyBundle  = "cdi-uploadproxy-signer-bundle"
	uploadProxyCA      = "ca-bundle.crt"
	// uploadSaidLimit is how much of the upload proxy's answer quo reads
	// into an error. That answer is where the proxy says why it refused.
	uploadSaidLimit = 1024
)

// DataVolume phases
const (
	phaseUploadReady = "UploadReady"
	phaseFailed      = "Failed"
	phaseSucceeded   = "Succeeded"
)

// UploadDisk puts the bytes readable from upload.Content into the claim
// the DataVolume upload.Name creates in upload.Namespace. CDI names that
// claim after the DataVolume itself. UploadDisk sends the bytes
// unchanged, exactly as it read them.
//
// The upload waits for CDI to make the claim ready to receive the bytes.
// A caller that creates the DataVolume and uploads into it at once
// therefore needs no readiness loop of its own. While the claim is not
// ready, UploadDisk calls upload.Waiting, when set, once per check it
// makes, saying what it waits on. UploadDisk offers the bytes once. A
// transfer that fails part-way is the caller's to report. The person who
// ran the deploy decides what happens next, because nothing in quo
// resumes a partial one.
func (r *KubeVirt) UploadDisk(ctx context.Context, upload Upload) (err error) {
	//
	err = r.awaitUploadReady(ctx, upload.Namespace, upload.Name, upload.Waiting)
	if err != nil {
		return
	}
	proxy, err := r.findUploadProxy(ctx)
	if err != nil {
		return
	}
	tunneled, err := forwardTo(ctx, r.core, r.config, proxy.Namespace, proxy.Name)
	if err != nil {
		return
	}
	defer func() {
		close(tunneled.stop)
	}()
	client := r.uploadClient(ctx, proxy)
	token, err := r.uploadToken(ctx, upload.Namespace, upload.Name)
	if err != nil {
		return
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://"+tunneled.address+uploadPath,
		upload.Content)
	if err != nil {
		err = fmt.Errorf("build the upload of disk %q: %w", upload.Name, err)
		return
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.ContentLength = upload.Size
	response, err := client.Do(request)
	if err != nil {
		err = fmt.Errorf("upload disk %q: %w", upload.Name, err)
		return
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		var said []byte
		said, _ = io.ReadAll(io.LimitReader(response.Body, uploadSaidLimit))
		err = fmt.Errorf(
			"upload disk %q: the upload proxy answered %s: %s",
			upload.Name,
			response.Status,
			strings.TrimSpace(string(said)))
		return
	}
	return
}

// awaitUploadReady waits for CDI to make the named DataVolume ready to
// receive its bytes: the claim bound and the upload pod placed. When the
// DataVolume reaches a phase it never recovers from, awaitUploadReady
// reports at once rather than letting the timeout expire. Each check it
// makes calls waiting, when set, saying what the DataVolume's phase is.
// A person watching the deploy can then tell a wait from a transfer.
func (r *KubeVirt) awaitUploadReady(
	ctx context.Context,
	namespace, name string,
	waiting func(said string)) (err error) {
	//
	deadline := time.Now().Add(uploadReadyTimeout)
	for {
		var dataVolume *unstructured.Unstructured
		dataVolume, err = r.dynamic.
			Resource(dataVolumeResource).
			Namespace(namespace).
			Get(ctx, name, meta.GetOptions{})
		if err != nil {
			err = fmt.Errorf("get DataVolume %s/%s: %w", namespace, name, err)
			return
		}
		phase, _, _ := unstructured.NestedString(dataVolume.Object, "status", "phase")
		switch phase {
		case phaseUploadReady:
			return
		case phaseFailed:
			err = fmt.Errorf(
				"DataVolume %s/%s failed, so it cannot receive the disk; its status says why",
				namespace,
				name)
			return
		case phaseSucceeded:
			err = fmt.Errorf("DataVolume %s/%s is already populated", namespace, name)
			return
		}
		if waiting != nil {
			if phase == "" {
				// A DataVolume straight from a create carries no phase
				// until CDI gives it one. That is a wait like any
				// other, and is said as one.
				waiting("the DataVolume has no phase yet")
			} else {
				waiting(fmt.Sprintf("the DataVolume is %q", phase))
			}
		}
		if time.Now().After(deadline) {
			err = fmt.Errorf(
				"DataVolume %s/%s is %q after %s, expected %q",
				namespace,
				name,
				phase,
				uploadReadyTimeout,
				phaseUploadReady)
			return
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-time.After(uploadPollInterval):
		}
	}
}

// findUploadProxy returns the Service CDI publishes its upload proxy
// behind. CDI's namespace is wherever its operator was installed.
// findUploadProxy therefore looks for the Service by name across the
// cluster, rather than guessing.
func (r *KubeVirt) findUploadProxy(ctx context.Context) (service *core.Service, err error) {
	list, err := r.core.CoreV1().Services(meta.NamespaceAll).List(ctx, meta.ListOptions{
		FieldSelector: "metadata.name=" + uploadProxyService,
	})
	if err != nil {
		err = fmt.Errorf("find CDI's upload proxy Service: %w", err)
		return
	}
	if len(list.Items) == 0 {
		err = fmt.Errorf(
			"find CDI's upload proxy Service: no Service named %q is in the cluster",
			uploadProxyService)
		return
	}
	service = &list.Items[0]
	return
}

// uploadClient returns the client that uploads disks. The client has no
// timeout of its own. A disk can take hours, so the context governs
// instead.
//
// An authority the cluster keeps in a ConfigMap beside it signs the
// proxy's certificate. Where that authority can be read, the client
// verifies the connection against it. Where it cannot, the connection is
// carried unverified: it runs inside a tunnel to the API server that the
// kubeconfig already authenticated. That is the same trust the port
// forward itself rests on.
func (r *KubeVirt) uploadClient(ctx context.Context, proxy *core.Service) (client *http.Client) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{
		// A forwarded port makes the address local. The certificate the
		// upload proxy presents is still the one the cluster issued it,
		// for its Service's name.
		ServerName: proxy.Name + "." + proxy.Namespace + ".svc",
		MinVersion: tls.VersionTLS12,
	}
	certs := r.uploadCertPool(ctx, proxy.Namespace)
	if certs != nil {
		tlsConfig.RootCAs = certs
	} else {
		tlsConfig.InsecureSkipVerify = true
	}
	transport.TLSClientConfig = tlsConfig
	client = &http.Client{Transport: transport}
	return
}

// uploadCertPool returns the authority that signed the upload proxy's
// certificate, nil when it cannot be read. The ConfigMap is CDI's own
// arrangement rather than an API, so a cluster may have moved it. The
// upload then relies on the tunnel's own authentication.
func (r *KubeVirt) uploadCertPool(ctx context.Context, namespace string) (pool *x509.CertPool) {
	bundle, err := r.core.CoreV1().ConfigMaps(namespace).Get(ctx, uploadProxyBundle, meta.GetOptions{})
	if err != nil {
		return
	}
	encoded := bundle.Data[uploadProxyCA]
	if encoded == "" {
		return
	}
	pool = x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(encoded)) {
		return nil
	}
	return
}

// uploadToken asks CDI for the token that authorizes one upload. The token
// names the claim it opens and lives five minutes. Only an upload that
// fails outlives it.
func (r *KubeVirt) uploadToken(ctx context.Context, namespace, name string) (token string, err error) {
	request := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": uploadTokenRequestResource.GroupVersion().String(),
		"kind":       "UploadTokenRequest",
		"metadata": map[string]any{
			"generateName": uploadTokenPrefix,
			"namespace":    namespace,
		},
		"spec": map[string]any{"pvcName": name},
	}}
	granted, err := r.dynamic.
		Resource(uploadTokenRequestResource).
		Namespace(namespace).
		Create(ctx, request, meta.CreateOptions{})
	if err != nil {
		err = fmt.Errorf("request an upload token for %s/%s: %w", namespace, name, err)
		return
	}
	token, _, _ = unstructured.NestedString(granted.Object, "status", "token")
	if token == "" {
		err = fmt.Errorf("request an upload token for %s/%s: CDI granted none", namespace, name)
		return
	}
	return
}
