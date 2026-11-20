package main

import (
	"bytes"
	"testing"

	"github.com/onsi/gomega"
)

func TestExportAcceptsFlagsOnEitherSideOfTheName(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantNamespace string
		wantVM        string
		wantOutput    string
		wantDryRun    bool
	}{
		{
			name:          "the documented form, with the output flag after the name",
			args:          []string{"-n", "support-cases", "db-01", "-o", "/tmp/db-01.bundle"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantOutput:    "/tmp/db-01.bundle",
		},
		{
			name:          "every flag before the name",
			args:          []string{"-n", "support-cases", "-o", "/tmp/db-01.bundle", "db-01"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantOutput:    "/tmp/db-01.bundle",
		},
		{
			name:          "the name first, every flag after it",
			args:          []string{"db-01", "-n", "support-cases", "-o", "/tmp/db-01.bundle"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantOutput:    "/tmp/db-01.bundle",
		},
		{
			name:          "long flag names",
			args:          []string{"--namespace", "support-cases", "db-01", "--output", "/tmp/db-01.bundle"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantOutput:    "/tmp/db-01.bundle",
		},
		{
			name:          "no output path, which is allowed",
			args:          []string{"-n", "support-cases", "db-01"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantOutput:    "",
		},
		{
			name:          "a dry run, with the flag after the name",
			args:          []string{"-n", "support-cases", "db-01", "--dry-run"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantDryRun:    true,
		},
		{
			name:          "a dry run, with the flag before the name",
			args:          []string{"--dry-run", "-n", "support-cases", "db-01"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
			wantDryRun:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			parsed, err := parseExportArgs(tt.args)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(parsed.opt.Namespace).To(gomega.Equal(tt.wantNamespace))
			g.Expect(parsed.opt.Name).To(gomega.Equal(tt.wantVM))
			g.Expect(parsed.output).To(gomega.Equal(tt.wantOutput))
			g.Expect(parsed.dryRun).To(gomega.Equal(tt.wantDryRun))
		})
	}
}

func TestExportRejectsACommandLineItCannotActOn(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no namespace is rejected",
			args: []string{"db-01", "-o", "/tmp/db-01.bundle"},
		},
		{
			name: "no VirtualMachine name is rejected",
			args: []string{"-n", "support-cases", "-o", "/tmp/db-01.bundle"},
		},
		{
			name: "a second VirtualMachine name is rejected",
			args: []string{"-n", "support-cases", "db-01", "db-02", "-o", "/tmp/db-01.bundle"},
		},
		{
			name: "an output path with a dry run is rejected, because a dry run writes no bundle",
			args: []string{"-n", "support-cases", "db-01", "-o", "/tmp/db-01.bundle", "--dry-run"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			_, err := parseExportArgs(tt.args)

			g.Expect(err).To(gomega.HaveOccurred())
		})
	}
}

func TestInspectTakesOneBundlePath(t *testing.T) {
	tests := []struct {
		name       string
		positional []string
		want       string
		wantErr    bool
	}{
		{
			name:       "one path is accepted",
			positional: []string{"/tmp/db-01.bundle"},
			want:       "/tmp/db-01.bundle",
		},
		{
			name:       "no path is rejected",
			positional: nil,
			wantErr:    true,
		},
		{
			name:       "a second path is rejected",
			positional: []string{"/tmp/db-01.bundle", "/tmp/db-02.bundle"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			path, err := onlyBundle(tt.positional)

			if tt.wantErr {
				g.Expect(err).To(gomega.HaveOccurred())
				return
			}
			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(path).To(gomega.Equal(tt.want))
		})
	}
}

// The flag package says a command's flags when it reads -h, and says a bad
// flag when it cannot parse the command line. parseFlags tells the two
// apart: one asks for help, so it is not a usage error.
func TestParseFlagsTellsHelpFromAUsageError(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want error
	}{
		{
			name: "a help flag, after a positional argument, is help",
			args: []string{"db-01", "-h"},
			want: errHelp,
		},
		{
			name: "an unknown flag is a usage error",
			args: []string{"--not-a-flag"},
			want: errUsage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			var out bytes.Buffer
			fs := newCommandFlags("export", "-n <namespace> <virtualmachine> [flags]")
			fs.SetOutput(&out)
			_, err := parseFlags(fs, tt.args)

			g.Expect(err).To(gomega.Equal(tt.want))
			g.Expect(out.String()).To(gomega.ContainSubstring("Usage: quo export -n <namespace> <virtualmachine> [flags]"))
		})
	}
}
