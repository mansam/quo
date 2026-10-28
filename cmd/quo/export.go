package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/capture"
	"github.com/mansam/quo/pkg/cluster"
)

// exportArgs is the parsed form of an export command line.
type exportArgs struct {
	opt        capture.Options
	output     string
	kubeconfig string
}

// bundleExtension is what the name of a bundle file ends in.
const bundleExtension = ".quo"

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
	parsed.opt.Progress = newReporter(os.Stderr)
	dest := bundleDest(parsed.output, parsed.opt.Name)
	record, err := capture.ExportToFile(context.TODO(), c, parsed.opt, dest)
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(
		os.Stdout,
		"Captured %s/%s to %s\n",
		parsed.opt.Namespace,
		parsed.opt.Name,
		dest)
	if err != nil {
		return
	}
	err = printSkipped(os.Stdout, record)
	if err != nil {
		return
	}
	err = warnUndeployable(os.Stdout, record)
	return
}

// printSkipped says what a capture could not collect. A capture that skipped
// something still succeeded. So printSkipped says it plainly rather than as
// a failure. It names each object, so that the person capturing the bundle
// can fix a permissions problem and run the capture again.
func printSkipped(out io.Writer, record bundle.CaptureRecord) (err error) {
	skipped := record.Skipped()
	if len(skipped) == 0 {
		return
	}
	_, err = fmt.Fprintf(out, "\nSkipped %d of %d objects:\n", len(skipped), len(record))
	if err != nil {
		return
	}
	for i := range skipped {
		_, err = fmt.Fprintf(out, "  %s: %s\n", subject(skipped[i]), skipped[i].Reason)
		if err != nil {
			return
		}
	}
	return
}

// subject names the object one entry of the capture record is about. subject
// names an entry that carries no name by its kind alone. KubeVirt's
// configuration is a singleton. quo learns that name only by reading the
// configuration. So a read that failed leaves the name empty.
func subject(attempt bundle.Attempt) (named string) {
	named = attempt.Kind
	if attempt.Name == "" {
		return
	}
	named = attempt.Kind + " " + attempt.Name
	return
}

// warnUndeployable says when a capture produced a bundle that cannot be
// deployed. Such a bundle is one whose expanded spec the capture could not
// read. A deploy refuses it. The capture still succeeded. So warnUndeployable
// says this plainly rather than as a failure.
//
// The warning goes on a line of its own, after the skipped objects, rather
// than among them. The person capturing the bundle learns here, before it
// is sent, that it will not deploy. The warning also says why the read
// failed.
func warnUndeployable(out io.Writer, record bundle.CaptureRecord) (err error) {
	for i := range record {
		attempt := record[i]
		if attempt.Kind != bundle.KindExpandedSpec {
			continue
		}
		if attempt.Outcome != bundle.OutcomeSkipped {
			continue
		}
		_, err = fmt.Fprintf(
			out,
			"\nThe capture could not read the expanded spec (%s), so the bundle cannot be deployed.\n",
			whySkipped(attempt))
		return
	}
	return
}

// whySkipped renders why one skipped attempt could not be collected. It gives
// the reason the capture record classifies the attempt under. Then it gives
// what the cluster said. That is where an actionable detail such as the
// missing permission appears.
func whySkipped(attempt bundle.Attempt) (why string) {
	why = string(attempt.Reason)
	if attempt.Message != "" {
		why = why + ": " + attempt.Message
	}
	return
}

// newReporter returns the progress reporter the CLI uses. The reporter
// rewrites one line of out while a disk transfers. It starts a new line when
// the disk finishes. So a person can tell a capture that takes hours from
// one that stopped making progress.
//
// Progress goes to standard error. So standard output stays the result of
// the command.
func newReporter(out io.Writer) (report func(p capture.Progress)) {
	// written is the width of the line last written, so that a shorter line
	// can be padded over the one it replaces. A carriage return moves the
	// cursor back to the start of the line. It removes nothing that is
	// already there. So "1023.5 MiB" followed by "1.1 GiB" would read
	// "1.1 GiBMiB" without the padding.
	written := 0
	report = func(p capture.Progress) {
		line := fmt.Sprintf(
			"disk %d/%d %s (%s): %s",
			p.Disk,
			p.Disks,
			p.Claim,
			strings.Join(p.Volumes, ", "),
			capture.FormatSize(p.Transferred))
		padding := ""
		if len(line) < written {
			padding = strings.Repeat(" ", written-len(line))
		}
		written = len(line)
		ending := "\r"
		if p.Done {
			ending = "\n"
			written = 0
		}
		_, _ = fmt.Fprint(out, line, padding, ending)
	}
	return
}

// parseExportArgs parses and checks an export command line. It contacts
// nothing. So the whole command line is testable without a cluster.
func parseExportArgs(args []string) (parsed exportArgs, err error) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.StringVar(&parsed.opt.Namespace, "n", "", "namespace of the VirtualMachine")
	fs.StringVar(&parsed.opt.Namespace, "namespace", "", "namespace of the VirtualMachine")
	fs.StringVar(&parsed.output, "o", "", "directory or file to write the bundle to")
	fs.StringVar(&parsed.output, "output", "", "directory or file to write the bundle to")
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
	err = validateVirtualMachine(parsed.opt)
	return
}

// onlyVirtualMachine returns the single VirtualMachine name on the command
// line. One bundle holds one VirtualMachine. So more than one name is a
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

// bundleDest returns the path to write the bundle to. It takes the output
// path from the command line and the name of the VirtualMachine.
//
// An output path that names a directory means the bundle is named after the
// VirtualMachine. bundleDest writes the bundle in that directory. No output
// path at all means the same. bundleDest then writes the bundle in the
// current directory. Any other output path is the path of the bundle file
// itself.
//
// bundleDest takes an output path that cannot be read to be a file. So a
// path under a directory that does not exist is reported by the capture.
// bundleDest does not treat the path as a directory name it never had.
func bundleDest(output string, name string) (dest string) {
	dest = output
	if output == "" {
		dest = name + bundleExtension
		return
	}
	info, err := os.Stat(output)
	if err != nil {
		return
	}
	if !info.IsDir() {
		return
	}
	dest = filepath.Join(output, name+bundleExtension)
	return
}
