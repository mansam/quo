package main

import (
	"context"
	"flag"
	"os"

	"github.com/mansam/quo/pkg/capture"
	"github.com/mansam/quo/pkg/cluster"
)

// checkArgs is the parsed form of a check command line.
type checkArgs struct {
	opt        capture.Options
	kubeconfig string
}

// runCheck says what a capture would collect and what it would skip. It
// transfers nothing.
//
// A check exists to find the objects a capture would skip. So a check that
// finds some still succeeds. A non-zero exit means the check could not
// report at all.
func runCheck(args []string) (err error) {
	parsed, err := parseCheckArgs(args)
	if err != nil {
		return
	}
	c, err := cluster.NewKubeVirt(parsed.kubeconfig)
	if err != nil {
		return
	}
	err = capture.Check(context.TODO(), c, parsed.opt, os.Stdout)
	return
}

// parseCheckArgs parses and checks a check command line. It contacts
// nothing. So the whole command line is testable without a cluster.
func parseCheckArgs(args []string) (parsed checkArgs, err error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.StringVar(&parsed.opt.Namespace, "n", "", "namespace of the VirtualMachine")
	fs.StringVar(&parsed.opt.Namespace, "namespace", "", "namespace of the VirtualMachine")
	fs.StringVar(&parsed.kubeconfig, "kubeconfig", "", "path to a kubeconfig file")
	positional, err := parseFlags(fs, args)
	if err != nil {
		err = errUsage
		return
	}
	parsed.opt.Name, err = onlyVirtualMachine(positional)
	if err != nil {
		return
	}
	// A check writes no bundle. So it needs nowhere to put one.
	err = validateVirtualMachine(parsed.opt)
	return
}
