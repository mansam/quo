package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/capture"
)

func TestExportSaysOnCompletionWhatItCouldNotCollect(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := bundle.CaptureRecord{
		{
			Kind:    "VirtualMachine",
			Name:    "db-01",
			Outcome: bundle.OutcomeCollected,
		},
		{
			Kind:    "Secret",
			Name:    "db-01-cloudinit",
			Outcome: bundle.OutcomeSkipped,
			Reason:  bundle.SkipForbidden,
		},
		{
			// KubeVirt's configuration is a singleton whose name quo only
			// learns by reading it.
			Kind:    "KubeVirt",
			Outcome: bundle.OutcomeSkipped,
			Reason:  bundle.SkipForbidden,
		},
	}
	reported := &bytes.Buffer{}

	err := printSkipped(reported, record)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("Skipped 2 of 3 objects"))
	g.Expect(said).To(gomega.ContainSubstring("Secret db-01-cloudinit: forbidden"))
	g.Expect(said).To(gomega.ContainSubstring("KubeVirt: forbidden"))
}

func TestExportProgressCoversTheLineItReplaces(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	reported := &bytes.Buffer{}
	report := newReporter(reported)
	at := capture.Progress{
		Claim:   "disk-0",
		Volumes: []string{"vol-0"},
		Disk:    1,
		Disks:   1,
	}

	// 1023.5 MiB, then 1.1 GiB: the second line is shorter than the first.
	at.Transferred = 1073217536
	report(at)
	at.Transferred = 1181116006
	report(at)

	lines := strings.Split(reported.String(), "\r")
	g.Expect(lines[0]).To(gomega.HaveSuffix("1023.5 MiB"))
	g.Expect(lines[1]).To(gomega.HaveSuffix("1.1 GiB   "))
	g.Expect(len(lines[1])).To(gomega.Equal(len(lines[0])))
}

func TestExportSaysNothingAboutSkippedObjectsWhenItCollectedEverything(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := bundle.CaptureRecord{
		{
			Kind:    "VirtualMachine",
			Name:    "db-01",
			Outcome: bundle.OutcomeCollected,
		},
	}
	reported := &bytes.Buffer{}

	err := printSkipped(reported, record)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.BeEmpty())
}
