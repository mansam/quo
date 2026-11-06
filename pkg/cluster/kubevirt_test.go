package cluster

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
)

// writeKubeconfig writes a kubeconfig that points at a cluster nothing
// runs. NewKubeVirt builds its clients from it without contacting
// anything, so a test can ask how those clients are built.
func writeKubeconfig(t *testing.T) (path string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "kubeconfig")
	content := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: quo-test
contexts:
- context:
    cluster: quo-test
    user: quo-test
  name: quo-test
current-context: quo-test
users:
- name: quo-test
  user:
    token: quo-test
`
	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// TestNewKubeVirtHandlesTheClustersAnswers checks that the adapter gives
// its clients a warning handler of its own. Without one, client-go
// prints one line for every answer that carries a warning. A poll that
// runs for minutes then buries the terminal in the same warning.
func TestNewKubeVirtHandlesTheClustersAnswers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c, err := NewKubeVirt(writeKubeconfig(t))
	if err != nil {
		t.Fatal(err)
	}
	g.Expect(c.config.WarningHandler).NotTo(gomega.BeNil(),
		"NewKubeVirt must give its clients a warning handler, because client-go's default prints one line per answer")
}

// TestEachWarningPrintsOnce checks the handler the adapter installs. A
// cluster can attach the same warning to every answer it gives. The
// export readiness wait reads the export every two seconds for as long
// as ten minutes, so one warning that repeats on every read has to
// print once, not once per read. A distinct warning still prints.
func TestEachWarningPrintsOnce(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	out := &bytes.Buffer{}
	handler := newWarningHandler(out)
	const repeated = "export.kubevirt.io/v1beta1 VirtualMachineExport is deprecated"
	for i := 0; i < 300; i++ {
		handler.HandleWarningHeader(299, "", repeated)
	}
	handler.HandleWarningHeader(299, "", "a different warning")
	g.Expect(out.String()).To(gomega.Equal(
		"Warning: "+repeated+"\n"+
			"Warning: a different warning\n"))
}
