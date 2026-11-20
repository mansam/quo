package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/deploy"
)

func TestUndeployAcceptsFlagsOnEitherSideOfTheBundle(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantBundle    string
		wantNamespace string
		wantDryRun    bool
	}{
		{
			name:          "the documented form, with the flags after the bundle",
			args:          []string{"db-01.quo", "-n", "diagnostics"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
		},
		{
			name:          "every flag before the bundle",
			args:          []string{"-n", "diagnostics", "--dry-run", "db-01.quo"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
			wantDryRun:    true,
		},
		{
			name:          "long flag names",
			args:          []string{"--namespace", "diagnostics", "db-01.quo"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			parsed, err := parseUndeployArgs(tt.args)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(parsed.opt.Bundle).To(gomega.Equal(tt.wantBundle))
			g.Expect(parsed.opt.Namespace).To(gomega.Equal(tt.wantNamespace))
			g.Expect(parsed.opt.DryRun).To(gomega.Equal(tt.wantDryRun))
		})
	}
}

func TestUndeployRejectsACommandLineItCannotActOn(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// message is what the rejection says, when one row's rejection
		// says something worth pinning.
		message string
	}{
		{
			name:    "no namespace is rejected, because quo never falls back to the kubeconfig's current one",
			args:    []string{"db-01.quo"},
			message: "a namespace is required: -n <namespace>",
		},
		{
			name: "no bundle path is rejected",
			args: []string{"-n", "diagnostics"},
		},
		{
			name: "a second bundle path is rejected",
			args: []string{"-n", "diagnostics", "db-01.quo", "db-02.quo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			_, err := parseUndeployArgs(tt.args)

			g.Expect(err).To(gomega.HaveOccurred())
			if tt.message != "" {
				g.Expect(err.Error()).To(gomega.ContainSubstring(tt.message))
			}
		})
	}
}

func TestUndeployPrintsAcceptedDeletionRequests(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	deleted := []deploy.Deletion{
		{
			Kind:      "DataVolume",
			Namespace: "diagnostics",
			Name:      "db-01-root",
		},
		{
			Kind:      "VirtualMachine",
			Namespace: "diagnostics",
			Name:      "db-01",
		},
	}
	reported := &bytes.Buffer{}

	err := printDeletions(reported, deleted, deploy.UndeployOptions{})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("Deletion requested for 2 objects:"))
	g.Expect(said).To(gomega.ContainSubstring("DataVolume"))
	g.Expect(said).To(gomega.ContainSubstring("diagnostics/db-01-root"))
	g.Expect(said).To(gomega.ContainSubstring("VirtualMachine"))
	g.Expect(said).To(gomega.ContainSubstring("diagnostics/db-01"))
}

func TestUndeployDryRunPrintsWhatItWouldDelete(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	deleted := []deploy.Deletion{
		{
			Kind:      "VirtualMachine",
			Namespace: "diagnostics",
			Name:      "db-01",
		},
	}
	reported := &bytes.Buffer{}

	err := printDeletions(reported, deleted, deploy.UndeployOptions{DryRun: true})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	// It says what it would do, and that one thing, not many.
	g.Expect(said).To(gomega.ContainSubstring("Would request deletion for 1 object:"))
	g.Expect(said).To(gomega.ContainSubstring("VirtualMachine"))
	g.Expect(said).To(gomega.ContainSubstring("diagnostics/db-01"))
	g.Expect(said).ToNot(gomega.ContainSubstring("Deleted"))
}

func TestUndeployDistinguishesFailedAndAcceptedRequests(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	deleted := []deploy.Deletion{
		{
			Kind:      "DataVolume",
			Namespace: "diagnostics",
			Name:      "db-01-root",
		},
		{
			Kind:      "Secret",
			Namespace: "diagnostics",
			Name:      "db-01-cloudinit",
			Err:       errors.New("the API server went away"),
		},
	}
	reported := &bytes.Buffer{}

	err := printDeletions(reported, deleted, deploy.UndeployOptions{})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("Deletion results:"))
	g.Expect(said).To(gomega.ContainSubstring("request accepted"))
	g.Expect(said).To(gomega.ContainSubstring("request failed: the API server went away"))
}

func TestUndeployReportsWhenNoObjectsMatch(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	reported := &bytes.Buffer{}

	err := printDeletions(reported, nil, deploy.UndeployOptions{Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.Equal("No objects match this deployment in namespace \"diagnostics\".\n"))
}
