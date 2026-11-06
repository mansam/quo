package main

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestExportNamesTheBundleWhenTheOutputPathDoesNot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "chosen.quo")
	err := os.WriteFile(file, nil, 0600)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "no output path is the VirtualMachine's name here",
			output: "",
			want:   "db-01.quo",
		},
		{
			name:   "the current directory is named as any other directory is",
			output: ".",
			want:   filepath.Join(".", "db-01.quo"),
		},
		{
			name:   "a directory holds a bundle named after the VirtualMachine",
			output: dir,
			want:   filepath.Join(dir, "db-01.quo"),
		},
		{
			name:   "an existing file is the bundle itself",
			output: file,
			want:   file,
		},
		{
			name:   "a path that does not exist yet is the bundle itself",
			output: filepath.Join(dir, "not-here-yet.quo"),
			want:   filepath.Join(dir, "not-here-yet.quo"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			dest := bundleDest(tt.output, "db-01")

			g.Expect(dest).To(gomega.Equal(tt.want))
		})
	}
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
		State:   capture.StateTransferring,
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

func TestTheExportReporterSaysWhatTheCaptureWaitsFor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	reported := &bytes.Buffer{}
	report := newReporter(reported)

	waitingLine := `waiting for the export server, the VirtualMachineExport is "Pending"`
	report(capture.Progress{
		State: capture.StateWaiting,
		Said:  `the VirtualMachineExport is "Pending"`,
	})
	movingLine := "disk 1/1 disk-0 (vol-0): 512 B"
	report(capture.Progress{
		Claim:       "disk-0",
		Volumes:     []string{"vol-0"},
		Disk:        1,
		Disks:       1,
		State:       capture.StateTransferring,
		Transferred: 512,
	})
	report(capture.Progress{
		Claim:       "disk-0",
		Volumes:     []string{"vol-0"},
		Disk:        1,
		Disks:       1,
		State:       capture.StateDone,
		Transferred: 1024,
	})

	// The line says the capture waits and what it waits on. Once bytes
	// move it counts them, and it says the disk done at the end of the
	// stream. The byte count is shorter than the wait it replaces, so
	// the line is padded over it.
	padding := strings.Repeat(" ", len(waitingLine)-len(movingLine))
	g.Expect(reported.String()).To(gomega.Equal(
		waitingLine + "\r" +
			movingLine + padding + "\r" +
			"disk 1/1 disk-0 (vol-0): 1.0 KiB" + "\n"))
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

func TestExportWarnsThatABundleWithoutAnExpandedSpecCannotBeDeployed(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := bundle.CaptureRecord{
		{
			Kind:    "VirtualMachine",
			Name:    "db-01",
			Outcome: bundle.OutcomeCollected,
		},
		{
			Kind:    bundle.KindExpandedSpec,
			Name:    "db-01",
			Outcome: bundle.OutcomeSkipped,
			Reason:  bundle.SkipForbidden,
			Message: `virtualmachines "db-01" is forbidden`,
		},
	}
	reported := &bytes.Buffer{}

	err := warnUndeployable(reported, record)

	// The capture has still succeeded. So the warning is said plainly
	// rather than as a failure. It names why the read failed, because the
	// missing permission is what a person capturing the bundle can fix
	// before running it again.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.ContainSubstring(
		`The capture could not read the expanded spec ` +
			`(forbidden: virtualmachines "db-01" is forbidden), ` +
			"so the bundle cannot be deployed.\n"))
}

func TestExportSaysNothingAboutDeployingWhenTheCaptureReadTheExpandedSpec(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := bundle.CaptureRecord{
		{
			Kind:    bundle.KindExpandedSpec,
			Name:    "db-01",
			Outcome: bundle.OutcomeCollected,
		},
		{
			// A skipped object of any other kind says nothing about
			// deploying. Only the expansion is what a deploy applies.
			Kind:    "Secret",
			Name:    "db-01-cloudinit",
			Outcome: bundle.OutcomeSkipped,
			Reason:  bundle.SkipForbidden,
		},
	}
	reported := &bytes.Buffer{}

	err := warnUndeployable(reported, record)

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.BeEmpty())
}
