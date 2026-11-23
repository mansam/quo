package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mansam/quo/pkg/bundle"
)

// runInspect describes a bundle without contacting a cluster.
func runInspect(args []string) (err error) {
	fs := newCommandFlags("inspect", "<bundle>")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return
	}
	path, err := onlyBundle(positional)
	if err != nil {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		err = fmt.Errorf("open bundle: %w", err)
		return
	}
	defer func() {
		_ = file.Close()
	}()
	err = bundle.Inspect(file, os.Stdout)
	return
}

// onlyBundle returns the single bundle path on the command line.
func onlyBundle(positional []string) (path string, err error) {
	if len(positional) == 0 {
		err = errors.New("a bundle path is required")
		return
	}
	if len(positional) > 1 {
		err = fmt.Errorf("expected one bundle path, got %d", len(positional))
		return
	}
	path = positional[0]
	return
}
