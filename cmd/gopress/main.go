// Command gopress is the GoPress orchestrator CLI.
//
// It scans themes/ and plugins/ directories, regenerates the
// internal/autoload package, and either runs (`serve`) or builds
// (`build`) the underlying server. Use `gen` to refresh autoload
// without running anything.
package main

import (
	"fmt"
	"os"
)

const usage = `gopress — GoPress orchestrator

Usage:
  gopress serve [server-flags...]   Regenerate autoload and run the server.
                                    All flags are passed through to cmd/server.
  gopress build [-o path]           Regenerate autoload and produce a server
                                    binary (default: build/gopress-server).
  gopress gen                       Regenerate internal/autoload only.
  gopress help                      Show this help.

Examples:
  gopress serve
  gopress serve -config ./sites/foo.toml
  gopress serve -seed
  gopress build -o build/myserver
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	sub, rest := os.Args[1], os.Args[2:]

	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
		os.Exit(1)
	}

	switch sub {
	case "serve":
		os.Exit(runServe(root, rest))
	case "build":
		os.Exit(runBuild(root, rest))
	case "gen":
		if err := runGen(root); err != nil {
			fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, usage)
	default:
		fmt.Fprintf(os.Stderr, "gopress: unknown subcommand %q\n\n%s", sub, usage)
		os.Exit(2)
	}
}

// runGen regenerates internal/autoload/autoload_gen.go and reports what changed.
func runGen(root string) error {
	themes, plugins, err := scanModules(root)
	if err != nil {
		return err
	}
	changed, err := writeAutoload(root, themes, plugins)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("gopress: autoload regenerated (themes=%d, plugins=%d)\n", len(themes), len(plugins))
	} else {
		fmt.Printf("gopress: autoload up to date (themes=%d, plugins=%d)\n", len(themes), len(plugins))
	}
	return nil
}

