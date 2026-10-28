// Command quo captures a stopped KubeVirt VirtualMachine as a diagnostic
// bundle, says what a capture would collect before one is run, inspects
// bundles it has captured, and deploys a bundle onto a cluster as a
// stopped VirtualMachine.
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
	case "check":
		err = runCheck(args[1:])
	case "inspect":
		err = runInspect(args[1:])
	case "deploy":
		err = runDeploy(args[1:])
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	return
}

// usage writes the command line quo accepts.
func usage(out *os.File) {
	_, _ = fmt.Fprint(out, `quo captures a stopped KubeVirt VirtualMachine as a diagnostic bundle, and
puts a captured bundle onto another cluster.

Usage:
  quo export -n <namespace> <name> [-o <path>] capture a VirtualMachine
  quo check -n <namespace> <name>              say what a capture would collect
  quo inspect <path>                           describe a bundle
  quo deploy <bundle> -n <namespace> [--name <name>]
       [--network <network>=<attachment>] [--storage-class <source>=<target>]
                                               put a bundle onto a cluster

The export path may be a directory or the name of the bundle file. A directory,
or no -o at all, gets a bundle named after the VirtualMachine; with no -o that
is <name>.quo in the current directory.

The deploy namespace is required: quo never falls back to the kubeconfig's
current namespace, because a diagnostic VirtualMachine landing in someone's
default namespace by accident is a quiet mistake. The deployed VirtualMachine
is the source cluster's own resolution of the captured one and is left stopped;
starting it is virtctl start. Each disk the bundle holds is uploaded into a
DataVolume through the target cluster's CDI, which has to be installed.

A deploy maps the bundle's requirements onto what the target has. A storage
class or a multus network maps onto the target's resource of the same name on
its own; --storage-class <source>=<target> places the disks of one class, with
--storage-class *=<target> covering every class that does not match by name,
the unnamed class included; and --network <network>=<attachment> attaches one
of the machine's networks to a NetworkAttachmentDefinition of the target's,
named with its namespace when it lives outside the deploy namespace. Every
mapping is reported in the deploy record, and a requirement that resolves to
nothing refuses the deploy before anything is created. Container disk images
are reported as requirements and never pull-checked.

A bundle contains plaintext Secrets from the source cluster. Treat it with the
same care as the cluster it was captured from.

quo check creates no VirtualMachineExport and transfers no disk data, so
permission to create one is the one thing it does not test.
`)
}
