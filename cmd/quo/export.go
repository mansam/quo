package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mansam/kvo/pkg/capture"
	"github.com/mansam/kvo/pkg/cluster"
)

// exportArgs is an export command line, once parsed.
type exportArgs struct {
	opt        capture.Options
	output     string
	kubeconfig string
}

// runExport captures a VirtualMachine into a bundle.
func runExport(args []string) (err error) {
	parsed, err := parseExportArgs(args)
	if err != nil {
		return
	}
	c, err := cluster.NewKubeVirt(parsed.kubeconfig)
	if err != nil {
		return
	}
	err = capture.ExportToFile(context.TODO(), c, parsed.opt, parsed.output)
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(
		os.Stdout,
		"Captured %s/%s to %s\n",
		parsed.opt.Namespace,
		parsed.opt.Name,
		parsed.output)
	return
}

// parseExportArgs parses and validates an export command line. It contacts
// nothing, so the whole command line is testable without a cluster.
func parseExportArgs(args []string) (parsed exportArgs, err error) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.StringVar(&parsed.opt.Namespace, "n", "", "namespace of the VirtualMachine")
	fs.StringVar(&parsed.opt.Namespace, "namespace", "", "namespace of the VirtualMachine")
	fs.StringVar(&parsed.output, "o", "", "path to write the bundle to")
	fs.StringVar(&parsed.output, "output", "", "path to write the bundle to")
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
	err = validateExport(parsed.opt, parsed.output)
	return
}

// onlyVirtualMachine returns the single VirtualMachine name on the command
// line. One bundle holds one VirtualMachine, so more than one name is a
// mistake worth reporting rather than quietly ignoring.
func onlyVirtualMachine(positional []string) (name string, err error) {
	if len(positional) > 1 {
		err = fmt.Errorf(
			"expected one VirtualMachine name, got %d: %s",
			len(positional),
			strings.Join(positional, " "))
		return
	}
	if len(positional) == 1 {
		name = positional[0]
	}
	return
}

// validateExport checks the command line names a VirtualMachine and somewhere
// to put it.
func validateExport(opt capture.Options, output string) (err error) {
	if opt.Namespace == "" {
		err = errors.New("a namespace is required: -n <namespace>")
		return
	}
	if opt.Name == "" {
		err = errors.New("a VirtualMachine name is required")
		return
	}
	if output == "" {
		err = errors.New("an output path is required: -o <path>")
		return
	}
	return
}
