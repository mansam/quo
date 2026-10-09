// Command quo captures a stopped KubeVirt VirtualMachine as a diagnostic
// bundle, and inspects bundles it has captured.
package main

import (
	"errors"
	"fmt"
	"os"
)

// errUsage reports a command line quo cannot act on. It is reported without
// the "quo:" prefix, because usage text follows it.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:])
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
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	return
}

// usage writes the command line quo accepts.
func usage(out *os.File) {
	_, _ = fmt.Fprint(out, `quo captures a stopped KubeVirt VirtualMachine as a diagnostic bundle.

Usage:
  quo export -n <namespace> <name> -o <path>   capture a VirtualMachine
  quo inspect <path>                           describe a bundle

A bundle contains plaintext Secrets from the source cluster. Treat it with the
same care as the cluster it was captured from.
`)
}
