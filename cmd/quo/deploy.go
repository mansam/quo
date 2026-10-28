package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/mansam/quo/pkg/cluster"
	"github.com/mansam/quo/pkg/deploy"
)

// deployArgs is a deploy command line, once parsed.
type deployArgs struct {
	opt        deploy.Options
	kubeconfig string
}

// runDeploy puts a bundle onto a cluster as a stopped VirtualMachine.
//
// The deploy record is printed however the run ended, so that what the
// target holds is the last thing said, and the error is what makes the
// exit code non-zero when anything failed.
func runDeploy(args []string) (err error) {
	parsed, err := parseDeployArgs(args)
	if err != nil {
		return
	}
	t, err := cluster.NewKubeVirt(parsed.kubeconfig)
	if err != nil {
		return
	}
	// Ctrl-C stops the deploy where it stands. What was created stays
	// where it is, because a deploy never rolls back, and the record
	// prints all the same, so what the target holds is still said.
	ctx, stop := signal.NotifyContext(context.TODO(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	record, deployErr := deploy.Deploy(ctx, t, parsed.opt)
	printErr := printRecord(os.Stdout, record)
	err = deployErr
	if err == nil {
		err = printErr
	}
	return
}

// printRecord writes the deploy record: every object a deploy attempted
// and what became of each one. A run that refused before attempting
// anything has no record, so nothing is printed for one.
//
// Write errors are discarded per line because tabwriter reports them
// again from Flush, which is checked.
func printRecord(out io.Writer, record deploy.Record) (err error) {
	if len(record) == 0 {
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Deploy record (%d)\n", len(record))
	for i := range record {
		line := fmt.Sprintf(
			"  %s\t%s\t%s",
			record[i].Kind,
			named(record[i]),
			record[i].Outcome)
		if record[i].Reason != "" {
			line += "\t" + record[i].Reason
		}
		_, _ = fmt.Fprintf(tw, "%s\n", line)
	}
	err = tw.Flush()
	if err != nil {
		err = fmt.Errorf("write deploy record: %w", err)
		return
	}
	return
}

// named renders a step's object with its namespace, dropping it for a
// cluster-scoped object.
func named(step deploy.Step) (rendered string) {
	rendered = step.Name
	if step.Namespace != "" {
		rendered = step.Namespace + "/" + step.Name
	}
	return
}

// parseDeployArgs parses and validates a deploy command line. It contacts
// nothing, so the whole command line is testable without a cluster.
func parseDeployArgs(args []string) (parsed deployArgs, err error) {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.StringVar(&parsed.opt.Namespace, "n", "", "namespace to deploy into")
	fs.StringVar(&parsed.opt.Namespace, "namespace", "", "namespace to deploy into")
	fs.StringVar(&parsed.opt.Name, "name", "", "name for the deployed VirtualMachine")
	fs.StringVar(&parsed.kubeconfig, "kubeconfig", "", "path to a kubeconfig file")
	positional, err := parseFlags(fs, args)
	if err != nil {
		err = errUsage
		return
	}
	parsed.opt.Bundle, err = onlyBundle(positional)
	if err != nil {
		return
	}
	err = validateDeploy(parsed.opt)
	return
}

// validateDeploy checks a deploy command line names a namespace to deploy
// into. The namespace is required and quo never falls back to the
// kubeconfig's current namespace, because a diagnostic VirtualMachine
// landing in someone's default namespace by accident is a quiet mistake.
func validateDeploy(opt deploy.Options) (err error) {
	if opt.Namespace == "" {
		err = errors.New("a namespace is required: -n <namespace>")
		return
	}
	return
}
