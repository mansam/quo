package main

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestCheckTakesTheSameVirtualMachineAnExportDoesAndNoOutputPath(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantNamespace string
		wantVM        string
	}{
		{
			name:          "the documented form",
			args:          []string{"-n", "support-cases", "db-01"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
		},
		{
			name:          "the name first, the namespace after it",
			args:          []string{"db-01", "-n", "support-cases"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
		},
		{
			name:          "long flag names",
			args:          []string{"--namespace", "support-cases", "db-01"},
			wantNamespace: "support-cases",
			wantVM:        "db-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			parsed, err := parseCheckArgs(tt.args)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(parsed.opt.Namespace).To(gomega.Equal(tt.wantNamespace))
			g.Expect(parsed.opt.Name).To(gomega.Equal(tt.wantVM))
		})
	}
}

func TestCheckRejectsACommandLineItCannotActOn(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no namespace is rejected",
			args: []string{"db-01"},
		},
		{
			name: "no VirtualMachine name is rejected",
			args: []string{"-n", "support-cases"},
		},
		{
			name: "a second VirtualMachine name is rejected",
			args: []string{"-n", "support-cases", "db-01", "db-02"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			_, err := parseCheckArgs(tt.args)

			g.Expect(err).To(gomega.HaveOccurred())
		})
	}
}
