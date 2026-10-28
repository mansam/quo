package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/deploy"
)

func TestDeployAcceptsFlagsOnEitherSideOfTheBundle(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantBundle    string
		wantNamespace string
		wantName      string
	}{
		{
			name:          "the documented form, with the flags after the bundle",
			args:          []string{"db-01.quo", "-n", "diagnostics"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
		},
		{
			name:          "every flag before the bundle",
			args:          []string{"-n", "diagnostics", "--name", "db-01-copy", "db-01.quo"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
			wantName:      "db-01-copy",
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

			parsed, err := parseDeployArgs(tt.args)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(parsed.opt.Bundle).To(gomega.Equal(tt.wantBundle))
			g.Expect(parsed.opt.Namespace).To(gomega.Equal(tt.wantNamespace))
			g.Expect(parsed.opt.Name).To(gomega.Equal(tt.wantName))
		})
	}
}

func TestDeployRejectsACommandLineItCannotActOn(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no namespace is rejected, because quo never falls back to the kubeconfig's current one",
			args: []string{"db-01.quo"},
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

			_, err := parseDeployArgs(tt.args)

			g.Expect(err).To(gomega.HaveOccurred())
		})
	}
}

func TestDeployPrintsTheRecordHoweverTheRunEnded(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		{
			Kind:      "VirtualMachine",
			Namespace: "diagnostics",
			Name:      "db-01",
			Outcome:   deploy.OutcomeCreated,
		},
		{
			Kind:      "DataVolume",
			Namespace: "diagnostics",
			Name:      "db-01-root",
			Outcome:   deploy.OutcomeSkipped,
			Reason:    `member "disks/db-01-root.gz" does not match its digest`,
		},
		{
			Kind:      "Secret",
			Namespace: "diagnostics",
			Name:      "db-01-cloudinit",
			Outcome:   deploy.OutcomeFailed,
			Reason:    "the API server went away",
		},
	}
	reported := &bytes.Buffer{}

	err := printRecord(reported, record, deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("Deploy record (3)"))
	g.Expect(said).To(gomega.ContainSubstring("VirtualMachine"))
	g.Expect(said).To(gomega.ContainSubstring("diagnostics/db-01"))
	g.Expect(said).To(gomega.ContainSubstring("created"))
	g.Expect(said).To(gomega.ContainSubstring("skipped"))
	g.Expect(said).To(gomega.ContainSubstring("failed"))
	// A reason is said for anything not created.
	g.Expect(said).To(gomega.ContainSubstring("does not match its digest"))
	g.Expect(said).To(gomega.ContainSubstring("the API server went away"))
}

func TestDeployPrintsNothingForARunThatRefusedBeforeAttemptingAnything(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	reported := &bytes.Buffer{}

	err := printRecord(reported, nil, deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.BeEmpty())
}

func TestDeployRecordEndsWithTheUndeployCommandForWhatExists(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		{
			Kind:      "DataVolume",
			Namespace: "diagnostics",
			Name:      "db-01-root",
			Outcome:   deploy.OutcomeCreated,
		},
		{
			Kind:      "VirtualMachine",
			Namespace: "diagnostics",
			Name:      "db-01",
			Outcome:   deploy.OutcomeCreated,
		},
	}
	reported := &bytes.Buffer{}

	err := printRecord(
		reported,
		record,
		deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The last thing said is the command that removes what the deploy
	// put there: the exact bundle path and namespace, so it can be run
	// as it reads.
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("Remove it again with:"))
	g.Expect(said).To(gomega.ContainSubstring("quo undeploy db-01.quo -n diagnostics"))
	lines := strings.Split(strings.TrimRight(said, "\n"), "\n")
	g.Expect(lines[len(lines)-1]).To(gomega.Equal("  quo undeploy db-01.quo -n diagnostics"))
}

func TestDeployRecordSaysNoUndeployCommandWhenNothingWasCreated(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		{
			Kind:      "VirtualMachine",
			Namespace: "diagnostics",
			Name:      "db-01",
			Outcome:   deploy.OutcomeFailed,
			Reason:    "the API server went away",
		},
	}
	reported := &bytes.Buffer{}

	err := printRecord(
		reported,
		record,
		deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	// There is nothing on the target, so there is nothing to point an
	// undeploy at.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).ToNot(gomega.ContainSubstring("undeploy"))
}
