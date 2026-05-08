package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	// Find project root (where go.mod lives)
	root := findProjectRoot()
	if root == "" {
		fmt.Fprintln(os.Stderr, "Error: cannot find project root (go.mod not found)")
		os.Exit(1)
	}

	// Locate swag binary
	swagBin := findSwag()
	if swagBin == "" {
		fmt.Fprintln(os.Stderr, "Error: swag CLI not found. Install it with:")
		fmt.Fprintln(os.Stderr, "  go install github.com/swaggo/swag/cmd/swag@latest")
		os.Exit(1)
	}

	fmt.Printf("Project root: %s\n", root)
	fmt.Printf("Swag binary:  %s\n", swagBin)
	fmt.Println("Generating Swagger API documentation...")

	cmd := exec.Command(swagBin,
		"init",
		"-g", "cmd/server/main.go",
		"-o", "docs",
		"--parseDependency",
		"--parseInternal",
	)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nError: swag init failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nDone! Generated files:")
	for _, f := range []string{"docs/docs.go", "docs/swagger.json", "docs/swagger.yaml"} {
		abs := filepath.Join(root, f)
		if info, err := os.Stat(abs); err == nil {
			fmt.Printf("  %-25s %d bytes\n", f, info.Size())
		}
	}
	fmt.Println("\nSwagger UI available at: http://localhost:8080/swagger/index.html")
}

// findProjectRoot walks up from cwd to locate go.mod.
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// findSwag locates the swag binary in GOPATH/bin or PATH.
func findSwag() string {
	// Check GOPATH/bin first
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	bin := "swag"
	if runtime.GOOS == "windows" {
		bin = "swag.exe"
	}
	candidate := filepath.Join(gopath, "bin", bin)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Fall back to PATH
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	return ""
}
