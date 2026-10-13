package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mansam/kvo/pkg/bundle"
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
	parsed.opt.Progress = newReporter(os.Stderr)
	record, err := capture.ExportToFile(context.TODO(), c, parsed.opt, parsed.output)
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(
		os.Stdout,
		"Captured %s/%s to %s\n",
		parsed.opt.Namespace,
		parsed.opt.Name,
		parsed.output)
	if err != nil {
		return
	}
	err = printSkipped(os.Stdout, record)
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
	_, err = fmt.Fprintf(
		out,
		"\n%d of the %d objects quo tried to collect were skipped:\n",
		len(skipped),
		len(record))
	if err != nil {
		return
	}
	for i := range skipped {
		_, err = fmt.Fprintf(
			out,
			"  %s %s: %s\n",
			skipped[i].Kind,
			skipped[i].Name,
			skipped[i].Reason)
		if err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(out, "The bundle's capture record says what became of each one.\n")
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
	report = func(p capture.Progress) {
		ending := "\r"
		if p.Done {
			ending = "\n"
		}
		_, _ = fmt.Fprintf(
			out,
			"disk %d/%d %s (%s): %s%s",
			p.Disk,
			p.Disks,
			p.Claim,
			strings.Join(p.Volumes, ", "),
			formatSize(p.Transferred),
			ending)
	}
	return
}

// formatSize renders a byte count for a person.
func formatSize(count int64) (rendered string) {
	const unit = 1024
	if count < unit {
		rendered = fmt.Sprintf("%d B", count)
		return
	}
	divisor := int64(unit)
	at := 0
	for remaining := count / unit; remaining >= unit; remaining /= unit {
		divisor *= unit
		at++
	}
	rendered = fmt.Sprintf("%.1f %ciB", float64(count)/float64(divisor), "KMGTPE"[at])
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
