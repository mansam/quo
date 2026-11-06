// Command quo captures a stopped KubeVirt VirtualMachine as a diagnostic
// bundle. It says what a capture would collect before one is run. It
// inspects bundles it has captured. It puts a bundle onto a cluster as a
// stopped VirtualMachine. It removes a bundle's deployment from a cluster
// again.
package main

import (
	"errors"
	"fmt"
	"os"
)

// errUsage reports a command line quo cannot act on. It is reported without
// the "quo:" prefix, because usage text follows it.
var errUsage = errors.New("usage")

// errHelp reports a command line that asked for a command's flags. The flag
// package says the flags itself. So quo says nothing more, and the exit code
// is zero.
var errHelp = errors.New("help")

func main() {
	err := run(os.Args[1:])
	if errors.Is(err, errHelp) {
		return
	}
	if errors.Is(err, errUsage) {
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quo: %s\n", err)
		os.Exit(1)
	}
}

// run dispatches to the named subcommand.
func run(args []string) (err error) {
	if len(args) == 0 {
		err = errUsage
		return
	}
	switch args[0] {
	case "export":
		err = runExport(args[1:])
	case "inspect":
		err = runInspect(args[1:])
	case "deploy":
		err = runDeploy(args[1:])
	case "undeploy":
		err = runUndeploy(args[1:])
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	return
}

// usage writes the command line quo accepts. What each command does is in
// the README. usage stays short, because a person who asks quo for help
// wants the command line, not the whole manual.
func usage(out *os.File) {
	_, _ = fmt.Fprint(out, `quo captures a stopped KubeVirt VirtualMachine as a diagnostic bundle, and
puts a captured bundle onto another cluster.

Usage:
  quo <command> [flags]

Commands:
  export     capture a VirtualMachine as a bundle
  inspect    describe a bundle
  deploy     put a bundle onto a cluster
  undeploy   take a bundle off a cluster

Run "quo <command> -h" for a command's flags.
`)
}
