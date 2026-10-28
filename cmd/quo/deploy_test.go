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
		{
			name:          "a dry run, with the flag after the bundle",
			args:          []string{"db-01.quo", "-n", "diagnostics", "--dry-run"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
			wantDryRun:    true,
		},
		{
			name:          "a dry run, with the flag before the bundle",
			args:          []string{"--dry-run", "-n", "diagnostics", "db-01.quo"},
			wantBundle:    "db-01.quo",
			wantNamespace: "diagnostics",
			wantDryRun:    true,
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
			g.Expect(parsed.opt.DryRun).To(gomega.Equal(tt.wantDryRun))
		})
	}
}

func TestDeployAcceptsMappingFlagsOnEitherSideOfTheBundle(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		wantStorageClasses map[string]string
		wantNetworks       map[string]string
	}{
		{
			name:               "one storage class mapping, after the bundle",
			args:               []string{"db-01.quo", "-n", "diagnostics", "--storage-class", "fast-rbd=ceph-rbd"},
			wantStorageClasses: map[string]string{"fast-rbd": "ceph-rbd"},
		},
		{
			name:               "the storage class catch-all, before the bundle",
			args:               []string{"--storage-class", "*=ceph-rbd", "-n", "diagnostics", "db-01.quo"},
			wantStorageClasses: map[string]string{"*": "ceph-rbd"},
		},
		{
			name: "storage class mappings repeat, one per class",
			args: []string{
				"-n", "diagnostics",
				"--storage-class", "fast-rbd=ceph-rbd",
				"--storage-class", "slow-nfs=ceph-rbd",
				"db-01.quo",
			},
			wantStorageClasses: map[string]string{
				"fast-rbd": "ceph-rbd",
				"slow-nfs": "ceph-rbd",
			},
		},
		{
			name:         "a network mapping naming an attachment in the deploy namespace",
			args:         []string{"db-01.quo", "-n", "diagnostics", "--network", "lab=lab-net"},
			wantNetworks: map[string]string{"lab": "lab-net"},
		},
		{
			name: "a network mapping naming an attachment outside the deploy namespace",
			args: []string{
				"-n", "diagnostics",
				"--network", "lab=infrastructure/diagnostic-net",
				"--network", "office=office-net",
				"db-01.quo",
			},
			wantNetworks: map[string]string{
				"lab":    "infrastructure/diagnostic-net",
				"office": "office-net",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			parsed, err := parseDeployArgs(tt.args)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(parsed.opt.StorageClasses).To(gomega.Equal(tt.wantStorageClasses))
			g.Expect(parsed.opt.Networks).To(gomega.Equal(tt.wantNetworks))
		})
	}
}

func TestDeployRejectsAMappingItCannotParse(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "a storage class mapping with no equals sign",
			args: []string{"-n", "diagnostics", "--storage-class", "fast-rbd", "db-01.quo"},
		},
		{
			name: "a storage class mapping with no source",
			args: []string{"-n", "diagnostics", "--storage-class", "=ceph-rbd", "db-01.quo"},
		},
		{
			name: "a storage class mapping with no target",
			args: []string{"-n", "diagnostics", "--storage-class", "fast-rbd=", "db-01.quo"},
		},
		{
			name: "a storage class mapping with two equals signs",
			args: []string{"-n", "diagnostics", "--storage-class", "fast-rbd=ceph=rbd", "db-01.quo"},
		},
		{
			name: "a network mapping with no equals sign",
			args: []string{"-n", "diagnostics", "--network", "lab", "db-01.quo"},
		},
		{
			name: "a network mapping with no attachment",
			args: []string{"-n", "diagnostics", "--network", "lab=", "db-01.quo"},
		},
		{
			name: "a network mapping with no network",
			args: []string{"-n", "diagnostics", "--network", "=lab-net", "db-01.quo"},
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
		Steps: []deploy.Step{
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

func TestDeployPrintsTheRequirementsARecordCarries(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		Steps: []deploy.Step{
			{
				Kind:      "VirtualMachine",
				Namespace: "diagnostics",
				Name:      "db-01",
				Outcome:   deploy.OutcomeCreated,
			},
		},
		Requirements: []deploy.Requirement{
			{
				Kind: "storage class",
				From: "fast-rbd",
				To:   "fast-rbd",
				How:  "by name",
			},
			{
				Kind: "storage class",
				From: "slow-nfs",
				To:   "ceph-rbd",
				How:  "by --storage-class *",
			},
			{
				// The unnamed class: a claim bound to a volume made by
				// hand, which the catch-all placed.
				Kind: "storage class",
				To:   "ceph-rbd",
				How:  "by --storage-class *",
			},
			{
				Kind: "network",
				From: "lab",
				To:   "infrastructure/diagnostic-net",
				How:  "by --network",
			},
			{
				Kind: "container disk",
				From: "quay.io/example/support-tools:6.0",
				How:  "the target has to be able to pull it",
			},
			{
				// A mapping the command line named that no requirement
				// matched: said rather than silently ignored.
				Kind: "network",
				From: "office",
				To:   "office-net",
				How:  "the bundle declares no such network",
			},
		},
	}
	reported := &bytes.Buffer{}

	err := printRecord(reported, record, deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	// Every requirement is printed with what it was mapped onto and how.
	// The unnamed class says it is unnamed.
	g.Expect(said).To(gomega.ContainSubstring("Requirements (6)"))
	g.Expect(said).To(gomega.ContainSubstring("storage class"))
	g.Expect(said).To(gomega.ContainSubstring("fast-rbd -> fast-rbd"))
	g.Expect(said).To(gomega.ContainSubstring("by name"))
	g.Expect(said).To(gomega.ContainSubstring("slow-nfs -> ceph-rbd"))
	g.Expect(said).To(gomega.ContainSubstring("by --storage-class *"))
	g.Expect(said).To(gomega.ContainSubstring("(unnamed) -> ceph-rbd"))
	g.Expect(said).To(gomega.ContainSubstring("network"))
	g.Expect(said).To(gomega.ContainSubstring("lab -> infrastructure/diagnostic-net"))
	g.Expect(said).To(gomega.ContainSubstring("by --network"))
	g.Expect(said).To(gomega.ContainSubstring("container disk"))
	g.Expect(said).To(gomega.ContainSubstring("quay.io/example/support-tools:6.0"))
	g.Expect(said).To(gomega.ContainSubstring("the target has to be able to pull it"))
	g.Expect(said).To(gomega.ContainSubstring("office -> office-net"))
	g.Expect(said).To(gomega.ContainSubstring("the bundle declares no such network"))
}

func TestDeployPrintsNothingForARunThatRefusedBeforeAttemptingAnything(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	reported := &bytes.Buffer{}

	err := printRecord(reported, deploy.Record{}, deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.BeEmpty())
}

func TestDeployRecordEndsWithTheUndeployCommandForWhatExists(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		Steps: []deploy.Step{
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
		},
	}
	reported := &bytes.Buffer{}

	err := printRecord(
		reported,
		record,
		deploy.Options{Bundle: "db-01.quo", Namespace: "diagnostics"})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The last thing said is the command that removes what the deploy put
	// there. It gives the exact bundle path and namespace. So it can be
	// run as it reads.
	said := reported.String()
	g.Expect(said).To(gomega.ContainSubstring("Remove it again with:"))
	g.Expect(said).To(gomega.ContainSubstring("quo undeploy db-01.quo -n diagnostics"))
	lines := strings.Split(strings.TrimRight(said, "\n"), "\n")
	g.Expect(lines[len(lines)-1]).To(gomega.Equal("  quo undeploy db-01.quo -n diagnostics"))
}

func TestDeployRecordSaysNoUndeployCommandWhenNothingWasCreated(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		Steps: []deploy.Step{
			{
				Kind:      "VirtualMachine",
				Namespace: "diagnostics",
				Name:      "db-01",
				Outcome:   deploy.OutcomeFailed,
				Reason:    "the API server went away",
			},
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

func TestDeployDryRunPrintsTheRecordItWouldPrint(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	record := deploy.Record{
		Steps: []deploy.Step{
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
		},
		Requirements: []deploy.Requirement{
			{
				Kind: "storage class",
				From: "fast-rbd",
				To:   "fast-rbd",
				How:  "by name",
			},
		},
	}
	reported := &bytes.Buffer{}

	err := printRecord(reported, record, deploy.Options{
		Bundle:    "db-01.quo",
		Namespace: "diagnostics",
		DryRun:    true,
	})

	g.Expect(err).ToNot(gomega.HaveOccurred())
	said := reported.String()
	// The first thing said is that nothing happened. The steps below say
	// created, because the record is the one a real deploy would print.
	// This line keeps them from being read as actions.
	lines := strings.Split(said, "\n")
	g.Expect(lines[0]).To(gomega.Equal("Dry run: nothing is created, nothing is uploaded."))
	g.Expect(said).To(gomega.ContainSubstring("Deploy record (2)"))
	g.Expect(said).To(gomega.ContainSubstring("diagnostics/db-01-root"))
	g.Expect(said).To(gomega.ContainSubstring("created"))
	// The mappings are what a dry run is for checking first.
	g.Expect(said).To(gomega.ContainSubstring("Requirements (1)"))
	g.Expect(said).To(gomega.ContainSubstring("fast-rbd -> fast-rbd"))
	g.Expect(said).To(gomega.ContainSubstring("by name"))
	// The last thing said is the command that removes what a real deploy
	// of this bundle would put there. The dry run gives that command as
	// its own.
	g.Expect(said).To(gomega.ContainSubstring("Remove what it would create with:"))
	lines = strings.Split(strings.TrimRight(said, "\n"), "\n")
	g.Expect(lines[len(lines)-1]).To(gomega.Equal("  quo undeploy db-01.quo -n diagnostics"))
}

func TestDeployDryRunPrintsNothingForARunThatRefusedBeforeAttemptingAnything(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	reported := &bytes.Buffer{}

	err := printRecord(reported, deploy.Record{}, deploy.Options{
		Bundle:    "db-01.quo",
		Namespace: "diagnostics",
		DryRun:    true,
	})

	// A dry run that would be refused says so and prints no record, the
	// same way a refused real deploy does.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reported.String()).To(gomega.BeEmpty())
}
