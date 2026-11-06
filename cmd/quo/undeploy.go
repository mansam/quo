package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/mansam/quo/pkg/cluster"
	"github.com/mansam/quo/pkg/deploy"
)

// undeployArgs is the parsed form of an undeploy command line.
type undeployArgs struct {
	opt        deploy.UndeployOptions
	kubeconfig string
}

// runUndeploy deletes one deployment from a cluster. The deployment is what
// a deploy of the bundle put into the namespace.
//
// runUndeploy prints what was deleted however the run ended. So what the
// namespace holds is the last thing said. The error is what makes the exit
// code non-zero when anything was not deleted.
//
// Unlike a deploy, runUndeploy wires no signals. An undeploy has nothing
// long-running to interrupt. Each deletion is one call, and an undeploy
// does not wait for any of them.
func runUndeploy(args []string) (err error) {
	parsed, err := parseUndeployArgs(args)
	if err != nil {
		return
	}
	t, err := cluster.NewKubeVirt(parsed.kubeconfig)
	if err != nil {
		return
	}
	deleted, undeployErr := deploy.Undeploy(context.TODO(), t, parsed.opt)
	printErr := printDeletions(os.Stdout, deleted, parsed.opt.DryRun)
	err = undeployErr
	if err == nil {
		err = printErr
	}
	return
}

// printDeletions writes what an undeploy deleted. On a dry run, it writes
// what the undeploy would delete. A deletion the cluster refused is named
// with why the object is still there. So what the namespace holds is the
// last thing said.
//
// Write errors are discarded per line. tabwriter reports them again from
// Flush. printDeletions checks that error.
func printDeletions(out io.Writer, deleted []deploy.Deletion, dryRun bool) (err error) {
	if len(deleted) == 0 {
		return
	}
	verb := "Deleted"
	if dryRun {
		verb = "Would delete"
	}
	removed := 0
	for i := range deleted {
		if deleted[i].Err == nil {
			removed++
		}
	}
	objects := "objects"
	if len(deleted) == 1 {
		objects = "object"
	}
	heading := fmt.Sprintf("%s %d %s", verb, len(deleted), objects)
	if removed < len(deleted) {
		heading = fmt.Sprintf("%s %d of %d %s", verb, removed, len(deleted), objects)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "%s:\n", heading)
	for i := range deleted {
		line := fmt.Sprintf(
			"  %s\t%s",
			deleted[i].Kind,
			named(deleted[i].Namespace, deleted[i].Name))
		if deleted[i].Err != nil {
			line += "\tnot deleted: " + deleted[i].Err.Error()
		}
		_, _ = fmt.Fprintf(tw, "%s\n", line)
	}
	err = tw.Flush()
	if err != nil {
		err = fmt.Errorf("write what the undeploy deleted: %w", err)
		return
	}
	return
}

// parseUndeployArgs parses and checks an undeploy command line. It
// contacts nothing. So the whole command line is testable without a
// cluster.
func parseUndeployArgs(args []string) (parsed undeployArgs, err error) {
	fs := flag.NewFlagSet("undeploy", flag.ContinueOnError)
	fs.StringVar(&parsed.opt.Namespace, "n", "", "namespace to undeploy from")
	fs.StringVar(&parsed.opt.Namespace, "namespace", "", "namespace to undeploy from")
	fs.BoolVar(&parsed.opt.DryRun, "dry-run", false, "say what would be deleted and delete nothing")
	fs.StringVar(&parsed.kubeconfig, "kubeconfig", "", "path to a kubeconfig file")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return
	}
	parsed.opt.Bundle, err = onlyBundle(positional)
	if err != nil {
		return
	}
	err = validateUndeploy(parsed.opt)
	return
}

// validateUndeploy checks that an undeploy command line names a namespace
// to undeploy from. The rule is the deploy's own, and the reason is the
// same. quo never takes the kubeconfig's current namespace instead,
// because deleting objects from someone's default namespace by accident
// is a quiet mistake.
func validateUndeploy(opt deploy.UndeployOptions) (err error) {
	if opt.Namespace == "" {
		err = errors.New("a namespace is required: -n <namespace>")
		return
	}
	return
}
