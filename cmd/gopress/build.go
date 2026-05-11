package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const defaultServerBinary = "build/gopress-server"

// runBuild regenerates autoload then runs `go build` to produce a server binary.
func runBuild(root string, args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	out := fs.String("o", defaultServerBinary, "output binary path (relative paths are resolved against the repo root)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	themes, plugins, err := scanModules(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
		return 1
	}
	if _, err := writeAutoload(root, themes, plugins); err != nil {
		fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
		return 1
	}

	outPath := *out
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(root, outPath)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "gopress: building server (themes=%d, plugins=%d) -> %s\n", len(themes), len(plugins), outPath)
	cmd := exec.Command("go", "build", "-o", outPath, "./cmd/server")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "gopress: build failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "gopress: built %s\n", outPath)
	return 0
}
