package main

import (
	"bytes"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/kvo/pkg/bundle"
)

func TestExportSaysOnCompletionWhatItCouldNotCollect(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := bundle.CaptureRecord{
		{
			Kind:        "VirtualMachine",
			Name:        "db-01",
			Disposition: bundle.DispositionCarried,
		},
		{
			Kind:        "Secret",
			Name:        "db-01-cloudinit",
			Disposition: bundle.DispositionSkipped,
			Reason:      bundle.SkipForbidden,
		},
		{
			// KubeVirt's configuration is a singleton whose name quo only
			// learns by reading it.
			Kind:        "KubeVirt",
			Disposition: bundle.DispositionSkipped,
			Reason:      bundle.SkipForbidden,
		},
	}
	reported := &bytes.Buffer{}

	err := printSkipped(reported, record)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("2 of the 3 objects"))
	g.Expect(said).To(gomega.ContainSubstring("Secret db-01-cloudinit: forbidden"))
	g.Expect(said).To(gomega.ContainSubstring("KubeVirt: forbidden"))
}

func TestExportSaysNothingAboutSkippedObjectsWhenItCollectedEverything(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := bundle.CaptureRecord{
		{
			Kind:        "VirtualMachine",
			Name:        "db-01",
			Disposition: bundle.DispositionCarried,
		},
	}
	reported := &bytes.Buffer{}

	err := printSkipped(reported, record)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.BeEmpty())
}
