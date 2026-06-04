package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed tidalt.desktop
var desktopFileContent []byte

// runSetup installs the .desktop file and registers the tidal:// URL handler.
// Every action is printed before it is executed so the user knows exactly what
// is happening to their system.
func runSetup() {
	appDir := filepath.Join(homeDir(), ".local", "share", "applications")

	stepf("Creating directory %s", appDir)
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		fatalf("mkdir %s: %v", appDir, err)
	}

	// Resolve the full path to the tidalt binary so the desktop file works
	// even when the DE launches apps with a minimal PATH.
	self, err := os.Executable()
	if err != nil {
		fatalf("cannot determine executable path: %v", err)
	}

	// Substitute the bare "tidalt" in the Exec line with the absolute path.
	desktop := string(desktopFileContent)
	desktop = strings.ReplaceAll(desktop, "Exec=tidalt ", "Exec="+self+" ")

	destPath := filepath.Join(appDir, "tidalt.desktop")
	stepf("Writing %s (Exec=%s play %%u)", destPath, self)
	if err := os.WriteFile(destPath, []byte(desktop), 0o600); err != nil {
		fatalf("write %s: %v", destPath, err)
	}

	run(false, "xdg-mime", "install", "--novendor", "--mode", "user", destPath)
	run(false, "xdg-mime", "default", "tidalt.desktop", "x-scheme-handler/tidal")
	run(true, "update-desktop-database", appDir)

	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println("Clicking \"Open in desktop app\" on tidal.com will now open tidalt.")
}

// step prints a human-readable description of the next action.
func stepf(format string, args ...any) {
	fmt.Printf("  -> %s\n", fmt.Sprintf(format, args...))
}

// run prints the command it is about to execute, runs it, and exits on failure.
// optional=true means a non-zero exit is printed as a warning rather than a
// hard failure (used for tools that may not be installed on all systems).
func run(optional bool, name string, args ...string) {
	display := name
	var displaySb62 strings.Builder
	for _, a := range args {
		displaySb62.WriteString(" " + a)
	}
	display += displaySb62.String()
	stepf("$ %s", display)

	//nolint:gosec // G204: name and args are hardcoded literals from setup call sites (systemctl, xdg-mime, update-desktop-database), never user input
	cmd := exec.CommandContext(context.Background(), name, args...)
	// Capture stderr so noise from helper tools (e.g. "qtpaths: command not
	// found" from xdg-mime on KDE without qt6-tools) doesn't alarm the user.
	// We print it ourselves only on failure.
	var stderr strings.Builder
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if optional {
			if msg != "" {
				fmt.Printf("     (warning: %s — continuing)\n", msg)
			} else {
				fmt.Printf("     (warning: %v — continuing)\n", err)
			}
			return
		}
		if msg != "" {
			fatalf("%s: %s", display, msg)
		}
		fatalf("%s: %v", display, err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		fatalf("cannot determine home directory: %v", err)
	}
	return h
}
