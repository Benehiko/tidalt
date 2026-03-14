package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed tidalt.desktop
var desktopFileContent []byte

// runSetup installs the .desktop file and registers the tidal:// URL handler.
// Every action is printed before it is executed so the user knows exactly what
// is happening to their system.
func runSetup() {
	appDir := filepath.Join(homeDir(), ".local", "share", "applications")

	step("Creating directory %s", appDir)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		fatal("mkdir %s: %v", appDir, err)
	}

	destPath := filepath.Join(appDir, "tidalt.desktop")
	step("Writing %s", destPath)
	if err := os.WriteFile(destPath, desktopFileContent, 0o644); err != nil {
		fatal("write %s: %v", destPath, err)
	}

	run("xdg-mime", "default", "tidalt.desktop", "x-scheme-handler/tidal")
	run("update-desktop-database", appDir)

	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println("Clicking \"Open in desktop app\" on tidal.com will now open tidalt.")
}

// step prints a human-readable description of the next action.
func step(format string, args ...any) {
	fmt.Printf("  -> %s\n", fmt.Sprintf(format, args...))
}

// run prints the command it is about to execute, runs it, and exits on failure.
// Non-zero exit codes from optional tools (update-desktop-database) are treated
// as warnings rather than hard failures.
func run(name string, args ...string) {
	// Build a printable command string.
	display := name
	for _, a := range args {
		display += " " + a
	}
	step("$ %s", display)

	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// update-desktop-database may not be installed on all systems.
		if name == "update-desktop-database" {
			fmt.Printf("     (warning: %v — continuing)\n", err)
			return
		}
		fatal("%s: %v", display, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		fatal("cannot determine home directory: %v", err)
	}
	return h
}
