package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/mansam/quo/pkg/capture"
	"github.com/mansam/quo/pkg/cluster"
	"github.com/mansam/quo/pkg/deploy"
)

// deployArgs is the parsed form of a deploy command line.
type deployArgs struct {
	opt        deploy.Options
	kubeconfig string
}

// runDeploy puts a bundle onto a cluster as a stopped VirtualMachine.
//
// runDeploy prints the deploy record however the run ended. So what the
// target holds is the last thing said. The error is what makes the exit
// code non-zero when anything failed.
func runDeploy(args []string) (err error) {
	parsed, err := parseDeployArgs(args)
	if err != nil {
		return
	}
	t, err := cluster.NewKubeVirt(parsed.kubeconfig)
	if err != nil {
		return
	}
	reportProgress, flushProgress := newUploadReporter(os.Stderr)
	parsed.opt.Progress = reportProgress
	// Ctrl-C stops the deploy where it stands. What was created stays
	// where it is, because a deploy never rolls back. The record prints
	// all the same. So what the target holds is still said.
	ctx, stop := signal.NotifyContext(context.TODO(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	record, deployErr := deploy.Deploy(ctx, t, parsed.opt)
	// The line the last disk left open is closed here, so the record
	// never prints onto the end of it.
	flushProgress()
	printErr := printRecord(os.Stdout, record, parsed.opt)
	err = deployErr
	if err == nil {
		err = printErr
	}
	return
}

// newUploadReporter returns the progress reporter the deploy uses, and
// the flush that closes the line it leaves open. The reporter rewrites
// one line of out while a disk uploads. It starts a new line when the
// disk finishes. So a person can tell a deploy that takes hours from one
// that stopped making progress.
//
// The line says what the disk is doing. While the target waits for the
// claim, the line says so, and says what the target waits on. Once bytes
// move, the line counts them.
//
// A disk whose upload never finishes leaves its line open. The reporter
// closes it when it is asked to report a different disk, and flush closes
// it when the run ends. The deploy record then never prints onto the end
// of an unfinished line.
//
// Progress goes to standard error. So standard output stays the result of
// the command.
func newUploadReporter(out io.Writer) (report func(p deploy.Progress), flush func()) {
	// written is the width of the line last written, so that a shorter line
	// can be padded over the one it replaces. A carriage return moves the
	// cursor back to the start of the line. It removes nothing that is
	// already there. So "1023.5 MiB" followed by "1.1 GiB" would read
	// "1.1 GiBMiB" without the padding.
	written := 0
	// open says a line is being rewritten that no line ending has closed
	// yet. disk is the position of the disk that line reports.
	open := false
	disk := 0
	report = func(p deploy.Progress) {
		if open && disk != p.Disk {
			// The disk before this one never finished. Its line is
			// closed here, so this disk starts on a line of its own.
			_, _ = fmt.Fprint(out, "\n")
			written = 0
			open = false
		}
		var tail string
		switch p.State {
		case deploy.StateWaiting:
			tail = "waiting, " + p.Said
		default:
			tail = capture.FormatSize(p.Transferred)
		}
		line := fmt.Sprintf(
			"disk %d/%d %s (%s): %s",
			p.Disk,
			p.Disks,
			p.Claim,
			strings.Join(p.Volumes, ", "),
			tail)
		padding := ""
		if len(line) < written {
			padding = strings.Repeat(" ", written-len(line))
		}
		written = len(line)
		ending := "\r"
		open = true
		if p.State == deploy.StateDone {
			ending = "\n"
			written = 0
			open = false
		}
		disk = p.Disk
		_, _ = fmt.Fprint(out, line, padding, ending)
	}
	flush = func() {
		if !open {
			return
		}
		_, _ = fmt.Fprint(out, "\n")
		open = false
	}
	return
}

// printRecord writes the deploy record. The record holds every object a
// deploy attempted and what became of each one. It also holds every
// requirement the deploy mapped onto the target's resources. A run that
// refused before attempting anything has no record. So printRecord prints
// nothing for one.
//
// A dry run prints the record the real deploy would print. The steps say
// created, because that is what a real deploy would do with each object.
// One line in front says that none of it happened. So the steps cannot be
// read as actions.
//
// The record ends with the undeploy command for what exists. So the way to
// remove the deployment again is the last thing said. printRecord prints
// the command only when something was created. A deploy that put nothing on
// the target leaves nothing to point an undeploy at.
//
// Write errors are discarded per line. tabwriter reports them again from
// Flush. printRecord checks that error.
func printRecord(out io.Writer, record deploy.Record, opt deploy.Options) (err error) {
	if len(record.Steps) == 0 && len(record.Requirements) == 0 {
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if opt.DryRun {
		_, _ = fmt.Fprintln(tw, "Dry run: nothing is created, nothing is uploaded.")
	}
	if len(record.Steps) > 0 {
		_, _ = fmt.Fprintf(tw, "Deploy record (%d)\n", len(record.Steps))
		for i := range record.Steps {
			line := fmt.Sprintf(
				"  %s\t%s\t%s",
				record.Steps[i].Kind,
				named(record.Steps[i].Namespace, record.Steps[i].Name),
				record.Steps[i].Outcome)
			if record.Steps[i].Reason != "" {
				line += "\t" + record.Steps[i].Reason
			}
			_, _ = fmt.Fprintf(tw, "%s\n", line)
		}
	}
	if len(record.Requirements) > 0 {
		_, _ = fmt.Fprintf(tw, "Requirements (%d)\n", len(record.Requirements))
		for i := range record.Requirements {
			from := record.Requirements[i].From
			if from == "" {
				from = "(unnamed)"
			}
			placed := from
			if record.Requirements[i].To != "" {
				placed += " -> " + record.Requirements[i].To
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\n",
				record.Requirements[i].Kind,
				placed,
				record.Requirements[i].How)
		}
	}
	err = tw.Flush()
	if err != nil {
		err = fmt.Errorf("write deploy record: %w", err)
		return
	}
	created := false
	for i := range record.Steps {
		if record.Steps[i].Outcome == deploy.OutcomeCreated {
			created = true
			break
		}
	}
	if !created {
		return
	}
	// A dry run points the command at what a real deploy would put there.
	// That is not there yet. A real deploy points the command at what is.
	lead := "Remove it again with:"
	if opt.DryRun {
		lead = "Remove what it would create with:"
	}
	_, err = fmt.Fprintf(
		out,
		"\n%s\n  quo undeploy %s -n %s\n",
		lead,
		opt.Bundle,
		opt.Namespace)
	if err != nil {
		err = fmt.Errorf("write the undeploy command: %w", err)
		return
	}
	return
}

// named renders an object with its namespace. It drops the namespace for a
// cluster-scoped object.
func named(namespace, name string) (rendered string) {
	rendered = name
	if namespace != "" {
		rendered = namespace + "/" + name
	}
	return
}

// mappingFlag is a repeatable flag that names mappings. Each occurrence
// gives one <left>=<right> pair. mappingFlag holds the pairs in a map from
// left to right. The flag package has no repeatable string flag of its own.
// So mappingFlag is one. A repeated left side keeps the last right side,
// the way a repeated plain flag keeps its last value.
type mappingFlag struct {
	// pairs holds every pair the command line gave.
	pairs map[string]string
	// want says what a well-formed value looks like, for the error a
	// malformed one produces.
	want string
}

// Set parses one occurrence and adds its pair.
func (f *mappingFlag) Set(value string) (err error) {
	left, right, found := strings.Cut(value, "=")
	if !found || left == "" || right == "" || strings.Contains(right, "=") {
		err = errors.New(f.want)
		return
	}
	if f.pairs == nil {
		f.pairs = map[string]string{}
	}
	f.pairs[left] = right
	return
}

// String renders the pairs the flag holds, so that a flag with a default
// can say what it is. The zero flag has none.
func (f *mappingFlag) String() (rendered string) {
	lefts := slices.Sorted(maps.Keys(f.pairs))
	pairs := make([]string, 0, len(lefts))
	for _, left := range lefts {
		pairs = append(pairs, left+"="+f.pairs[left])
	}
	return strings.Join(pairs, ", ")
}

// parseDeployArgs parses and checks a deploy command line. It contacts
// nothing. So the whole command line is testable without a cluster.
func parseDeployArgs(args []string) (parsed deployArgs, err error) {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.StringVar(&parsed.opt.Namespace, "n", "", "namespace to deploy into")
	fs.StringVar(&parsed.opt.Namespace, "namespace", "", "namespace to deploy into")
	fs.StringVar(&parsed.opt.Name, "name", "", "name for the deployed VirtualMachine")
	fs.BoolVar(&parsed.opt.DryRun, "dry-run", false, "say what would be created and create nothing")
	storageClasses := &mappingFlag{
		want: "a storage class mapping wants <source>=<target>",
	}
	fs.Var(storageClasses, "storage-class",
		"place the disks of one storage class onto a target class; * covers every class that does not match by name")
	networks := &mappingFlag{
		want: "a network mapping wants <network>=<attachment>",
	}
	fs.Var(networks, "network",
		"attach one of the machine's networks to a NetworkAttachmentDefinition")
	fs.StringVar(&parsed.kubeconfig, "kubeconfig", "", "path to a kubeconfig file")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return
	}
	parsed.opt.Bundle, err = onlyBundle(positional)
	if err != nil {
		return
	}
	parsed.opt.StorageClasses = storageClasses.pairs
	parsed.opt.Networks = networks.pairs
	err = validateDeploy(parsed.opt)
	return
}

// validateDeploy checks that a deploy command line names a namespace to
// deploy into. The namespace is required. quo never takes the kubeconfig's
// current namespace instead, because a diagnostic VirtualMachine that lands
// in someone's default namespace by accident is a quiet mistake.
func validateDeploy(opt deploy.Options) (err error) {
	if opt.Namespace == "" {
		err = errors.New("a namespace is required: -n <namespace>")
		return
	}
	return
}
