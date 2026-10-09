// Package cluster is quo's boundary with the outside world. Everything quo
// reads from outside its own process passes through the Cluster interface, so
// that a test can substitute a fake and never needs a running cluster or an
// HTTP server.
package cluster

import (
	"context"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Cluster is everything quo needs from outside its own process.
type Cluster interface {
	// GetVirtualMachine returns the named VirtualMachine.
	GetVirtualMachine(ctx context.Context, namespace, name string) (vm *unstructured.Unstructured, err error)
	// GetInfo returns what a bundle records about the cluster it came from.
	GetInfo(ctx context.Context) (info Info, err error)
	// OpenDisk opens a byte stream carrying the disk that backs the named
	// PersistentVolumeClaim. The caller closes it.
	OpenDisk(ctx context.Context, namespace, claim string) (stream io.ReadCloser, err error)
}

// Info describes the cluster a bundle was captured from.
//
// A version is empty when quo was not allowed to read it. Capturing with only
// namespace-level permissions is expected, and a missing version is not a
// reason to abandon a capture.
type Info struct {
	SourceCluster     string
	KubeVirtVersion   string
	KubernetesVersion string
}
