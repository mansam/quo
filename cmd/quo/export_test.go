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
	}
	reported := &bytes.Buffer{}

	err := printSkipped(reported, record)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("1 of the 2 objects"))
	g.Expect(said).To(gomega.ContainSubstring("Secret db-01-cloudinit: forbidden"))
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
