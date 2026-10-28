package main

import (
	"errors"
	"flag"

	"github.com/mansam/quo/pkg/capture"
)

// validateVirtualMachine checks that a command line names a VirtualMachine.
// Every command that acts on one needs both halves of its name.
func validateVirtualMachine(opt capture.Options) (err error) {
	if opt.Namespace == "" {
		err = errors.New("a namespace is required: -n <namespace>")
		return
	}
	if opt.Name == "" {
		err = errors.New("a VirtualMachine name is required")
		return
	}
	return
}

// parseFlags parses flags that appear before or after positional arguments.
// It returns the positional arguments.
//
// The flag package stops at the first argument that is not a flag. The
// command "export -n ns db-01 -o bundle" would therefore leave -o
// unparsed. quo would then report a missing output path. parseFlags resumes
// parsing after each positional argument instead.
func parseFlags(fs *flag.FlagSet, args []string) (positional []string, err error) {
	for {
		err = fs.Parse(args)
		if err != nil {
			return
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
