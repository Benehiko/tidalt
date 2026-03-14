package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Benehiko/tidalt/internal/mpris"
)

// runPlay handles the "tidalt play <url>" subcommand.
//
// If a parent tidalt instance is already running, the URL is forwarded over
// D-Bus and the process exits immediately — no terminal needed.
//
// Otherwise a terminal emulator is launched with "tidalt <url>" so the full
// TUI starts in a proper TTY with the URL queued for auto-play.
func runPlay(url string) {
	if url == "" {
		fmt.Fprintln(os.Stderr, "usage: tidalt play <tidal://... or https://tidal.com/...>")
		os.Exit(1)
	}

	// If a parent is already running just push the URL and exit.
	c, err := mpris.NewClient()
	if err == nil {
		defer c.Close()
		if sendErr := c.SendURL(url); sendErr != nil {
			fmt.Fprintf(os.Stderr, "tidalt play: failed to send URL to running instance: %v\n", sendErr)
			os.Exit(1)
		}
		return
	}

	// No running instance — launch a terminal with the TUI.
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tidalt play: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}

	term, args := findTerminal(self, url)
	if term == "" {
		fmt.Fprintln(os.Stderr, "tidalt play: no terminal emulator found; set $TERMINAL or install one of: kitty, ghostty, alacritty, foot, wezterm, konsole, xterm")
		os.Exit(1)
	}

	cmd := exec.Command(term, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if startErr := cmd.Start(); startErr != nil {
		fmt.Fprintf(os.Stderr, "tidalt play: failed to launch terminal %q: %v\n", term, startErr)
		os.Exit(1)
	}
	// Detach — let the terminal own the child process.
	_ = cmd.Process.Release()
}

// findTerminal returns the terminal binary and argument list needed to run
// "tidalt <url>" in a new window. Returns ("", nil) if nothing is found.
//
// Lookup order:
//  1. $TERMINAL env var (assumed to accept "-e <cmd> [args...]")
//  2. Well-known terminals, each with their correct flag convention.
func findTerminal(self, url string) (string, []string) {
	type candidate struct {
		bin  string
		args func(self, url string) []string
	}

	// Standard "-e cmd args..." convention.
	withE := func(bin, self, url string) []string { return []string{bin, "-e", self, url} }

	candidates := []candidate{
		// $TERMINAL — honour the user's explicit preference first.
		{os.Getenv("TERMINAL"), func(s, u string) []string { return withE(os.Getenv("TERMINAL"), s, u) }},
		// Terminals that use "-e":
		{"kitty", func(s, u string) []string { return []string{"kitty", s, u} }},
		{"ghostty", func(s, u string) []string { return []string{"ghostty", "-e", s, u} }},
		{"alacritty", func(s, u string) []string { return []string{"alacritty", "-e", s, u} }},
		{"foot", func(s, u string) []string { return []string{"foot", s, u} }},
		{"wezterm", func(s, u string) []string { return []string{"wezterm", "start", "--", s, u} }},
		{"konsole", func(s, u string) []string { return []string{"konsole", "-e", s, u} }},
		{"xfce4-terminal", func(s, u string) []string { return []string{"xfce4-terminal", "-e", s + " " + u} }},
		{"gnome-terminal", func(s, u string) []string { return []string{"gnome-terminal", "--", s, u} }},
		{"xterm", func(s, u string) []string { return []string{"xterm", "-e", s, u} }},
	}

	for _, c := range candidates {
		if c.bin == "" {
			continue
		}
		path, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		full := c.args(self, url)
		// Replace bare name with resolved path.
		full[0] = path
		return path, full[1:]
	}
	return "", nil
}
