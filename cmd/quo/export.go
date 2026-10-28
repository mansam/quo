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

// exportArgs is an export command line, once parsed.
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
// something still succeeded, so this is said plainly rather than as a
// failure, and it names each object so that a permissions problem can be
// fixed and the capture run again.
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

// subject names the object one entry of the capture record is about. An entry
// carrying no name is named by its kind alone: KubeVirt's configuration is a
// singleton whose name quo only learns by reading it, so a read that failed
// leaves the name empty.
func subject(attempt bundle.Attempt) (named string) {
	named = attempt.Kind
	if attempt.Name == "" {
		return
	}
	named = attempt.Kind + " " + attempt.Name
	return
}

// warnUndeployable says when a capture produced a bundle that cannot be
// deployed: one whose expanded spec could not be read, which a deploy refuses.
// The capture has still succeeded, so this is said plainly rather than as a
// failure, and on a line of its own after the skipped objects rather than
// among them: the person capturing the bundle learns here, before it is sent,
// that it will not deploy, and why the read failed.
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

// whySkipped renders why one skipped attempt could not be collected: the
// reason the capture record classifies it under, then what the cluster said,
// which is where an actionable detail such as the missing permission appears.
func whySkipped(attempt bundle.Attempt) (why string) {
	why = string(attempt.Reason)
	if attempt.Message != "" {
		why = why + ": " + attempt.Message
	}
	return
}

// newReporter returns the progress reporter the CLI uses. It rewrites one
// line of out while a disk transfers and starts a new line once the disk has
// finished, so that a capture taking hours can be told apart from one that
// has stopped making progress.
//
// Progress goes to standard error so that standard output stays the result of
// the command.
func newReporter(out io.Writer) (report func(p capture.Progress)) {
	// Width of the line last written, so that a shorter line can be padded
	// over the one it replaces. A carriage return moves the cursor back to
	// the start of the line without removing what is already there, so
	// "1023.5 MiB" followed by "1.1 GiB" would otherwise read "1.1 GiBMiB".
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

// parseExportArgs parses and validates an export command line. It contacts
// nothing, so the whole command line is testable without a cluster.
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

// bundleDest returns the path to write the bundle to, given the output path
// from the command line and the name of the VirtualMachine.
//
// An output path that names a directory, and no output path at all, both mean
// the bundle is named after the VirtualMachine: the first writes it in that
// directory, the second in the current one. Any other output path is the path
// of the bundle file itself.
//
// An output path that cannot be read is taken to be a file, so that a path
// under a directory that does not exist is reported by the capture rather than
// turned into a directory name it never had.
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
