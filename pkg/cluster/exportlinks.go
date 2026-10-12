package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// offer is one volume's compressed download link, as the export published it.
type offer struct {
	claim  string
	format DiskFormat
	link   string
}

// getLinks returns the links the export published, preferring an external
// route where the cluster has one.
func getLinks(export *unstructured.Unstructured) (links map[string]any, external bool, err error) {
	links, found, _ := unstructured.NestedMap(export.Object, "status", "links", "external")
	if found && len(links) > 0 {
		external = true
		return
	}
	links, found, _ = unstructured.NestedMap(export.Object, "status", "links", "internal")
	if !found || len(links) == 0 {
		links = nil
		err = errors.New("the export is ready but published no links to its disks")
		return
	}
	return
}

// getCompressed returns the compressed download link for every volume the
// links cover.
//
// A volume holding a block device is offered raw and gzip; a volume holding
// files is offered dir and tar.gz. quo always takes the compressed one, so
// that a sparsely used volume transfers at roughly its used size.
func getCompressed(links map[string]any) (offers []offer, err error) {
	volumes, _, _ := unstructured.NestedSlice(links, "volumes")
	for i := range volumes {
		volume, cast := volumes[i].(map[string]any)
		if !cast {
			continue
		}
		claim, _, _ := unstructured.NestedString(volume, "name")
		found, matched := getFormat(volume)
		if !matched {
			offers = nil
			err = fmt.Errorf("the export offers no compressed format for volume %q", claim)
			return
		}
		found.claim = claim
		offers = append(offers, found)
	}
	if len(offers) == 0 {
		err = errors.New("the export is ready but offers no volumes")
		return
	}
	return
}

// getFormat returns the compressed format one volume is offered in.
func getFormat(volume map[string]any) (found offer, matched bool) {
	formats, _, _ := unstructured.NestedSlice(volume, "formats")
	for i := range formats {
		offered, cast := formats[i].(map[string]any)
		if !cast {
			continue
		}
		name, _, _ := unstructured.NestedString(offered, "format")
		link, _, _ := unstructured.NestedString(offered, "url")
		if link == "" {
			continue
		}
		switch DiskFormat(name) {
		case FormatGzip, FormatArchiveGzip:
			found = offer{format: DiskFormat(name), link: link}
			matched = true
			return
		}
	}
	return
}

// buildCertPool returns the authority the export server's certificate is
// signed by, nil where the link carries none and the system authorities
// apply.
func buildCertPool(links map[string]any) (pool *x509.CertPool, err error) {
	encoded, _, _ := unstructured.NestedString(links, "cert")
	if encoded == "" {
		return
	}
	pool = x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(encoded)) {
		pool = nil
		err = errors.New("the export published a certificate authority that cannot be read")
		return
	}
	return
}

// buildClient returns the client that downloads disks. It has no timeout of
// its own: a single disk can take hours, and the context governs instead.
func buildClient(certs *x509.CertPool, server string) (client *http.Client) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		RootCAs: certs,
		// A forwarded port makes the address local, but the certificate the
		// export server presents is still the one the cluster issued it.
		ServerName: server,
		MinVersion: tls.VersionTLS12,
	}
	client = &http.Client{Transport: transport}
	return
}

// redirect points a link at an address, leaving the rest of it alone.
func redirect(link, address string) (redirected string, err error) {
	parsed, err := url.Parse(link)
	if err != nil {
		err = fmt.Errorf("parse export link %q: %w", link, err)
		return
	}
	parsed.Host = address
	redirected = parsed.String()
	return
}
