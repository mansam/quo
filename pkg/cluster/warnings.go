package cluster

import (
	"io"

	"k8s.io/client-go/rest"
)

// newWarningHandler returns the handler for the warnings a cluster's
// answers carry. Each distinct warning is printed once. A cluster can
// attach the same warning to every answer, and a wait that polls for
// minutes would print it again on every read.
func newWarningHandler(out io.Writer) rest.WarningHandler {
	return rest.NewWarningWriter(out, rest.WarningWriterOptions{
		Deduplicate: true,
	})
}
