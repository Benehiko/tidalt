package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Benehiko/tidalt/internal/mpris"
)

// playLog returns a logger that writes to ~/.local/share/tidalt/play.log.
// Errors opening the file fall back to stderr.
func playLog() *log.Logger {
	dir := filepath.Join(func() string {
		h, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		return h
	}(), ".local", "share", "tidalt")
	_ = os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, "play.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return log.New(os.Stderr, "", 0)
	}
	return log.New(f, "", 0)
}

// runPlay handles the "tidalt play <url>" subcommand.
//
// If a parent tidalt instance is already running, the URL is forwarded over
// D-Bus and the process exits immediately — no terminal needed.
//
// Otherwise a terminal emulator is launched with "tidalt <url>" so the full
// TUI starts in a proper TTY with the URL queued for auto-play.
func runPlay(url string) {
	lg := playLog()
	lg.Printf("[%s] tidalt play invoked with url=%q", time.Now().Format(time.RFC3339), url)

	if url == "" {
		lg.Printf("error: no URL provided")
		fmt.Fprintln(os.Stderr, "usage: tidalt play <tidal://... or https://tidal.com/...>")
		os.Exit(1)
	}

	// If a parent is already running just push the URL and exit.
	c, err := mpris.NewClient()
	if err == nil {
		defer c.Close()
		if sendErr := c.SendURL(url); sendErr != nil {
			lg.Printf("error: failed to send URL to running instance: %v", sendErr)
			fmt.Fprintf(os.Stderr, "tidalt play: failed to send URL to running instance: %v\n", sendErr)
			os.Exit(1)
		}
		lg.Printf("forwarded URL to running instance")
		return
	}
	lg.Printf("no running instance (%v), launching terminal", err)

	// No running instance — launch a terminal with the TUI.
	self, err := os.Executable()
	if err != nil {
		lg.Printf("error: cannot determine executable path: %v", err)
		fmt.Fprintf(os.Stderr, "tidalt play: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}
	lg.Printf("self=%q", self)

	term, args := findTerminal(self, url)
	if term == "" {
		lg.Printf("error: no terminal emulator found")
		fmt.Fprintln(os.Stderr, "tidalt play: no terminal emulator found; set $TERMINAL or install one of: kitty, ghostty, alacritty, foot, wezterm, konsole, xterm")
		os.Exit(1)
	}
	lg.Printf("launching terminal: %q args=%v", term, args)

	cmd := exec.Command(term, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if startErr := cmd.Start(); startErr != nil {
		lg.Printf("error: failed to launch terminal %q: %v", term, startErr)
		fmt.Fprintf(os.Stderr, "tidalt play: failed to launch terminal %q: %v\n", term, startErr)
		os.Exit(1)
	}
	lg.Printf("terminal launched (pid %d)", cmd.Process.Pid)
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
