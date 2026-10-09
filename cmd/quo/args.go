package main

import "flag"

// parseFlags parses flags that appear before or after positional arguments
// and returns the positional ones.
//
// The flag package stops at the first argument that is not a flag, so
// "export -n ns db-01 -o bundle" would otherwise leave -o unparsed and quo
// would report a missing output path. Parsing resumes after each positional
// argument instead.
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
