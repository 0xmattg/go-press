package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runServe regenerates autoload then exec's `go run ./cmd/server`, transparently
// forwarding stdio, exit code, and termination signals.
func runServe(root string, serverArgs []string) int {
	themes, plugins, err := scanModules(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
		return 1
	}
	if _, err := writeAutoload(root, themes, plugins); err != nil {
		fmt.Fprintf(os.Stderr, "gopress: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "gopress: starting server (themes=%d, plugins=%d)\n", len(themes), len(plugins))

	args := append([]string{"run", "./cmd/server"}, serverArgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "gopress: failed to start server: %v\n", err)
		return 1
	}

	// Forward termination signals to the child so cmd/server can run its
	// graceful shutdown path.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigCh:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	close(done)
	signal.Stop(sigCh)

	if waitErr == nil {
		return 0
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "gopress: server error: %v\n", waitErr)
	return 1
}
