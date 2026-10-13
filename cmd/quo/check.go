package main

import (
	"context"
	"flag"
	"os"

	"github.com/mansam/kvo/pkg/capture"
	"github.com/mansam/kvo/pkg/cluster"
)

// checkArgs is a check command line, once parsed.
type checkArgs struct {
	opt        capture.Options
	kubeconfig string
}

// runCheck says what a capture would collect and what it would skip, without
// transferring anything.
//
// A check that found objects a capture would skip has done the job it exists
// for, so it succeeds. The exit status says whether quo could report, not
// whether it liked what it found.
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

// parseCheckArgs parses and validates a check command line. It contacts
// nothing, so the whole command line is testable without a cluster.
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
	// A check writes no bundle, so it needs nowhere to put one.
	err = validateVirtualMachine(parsed.opt)
	return
}
