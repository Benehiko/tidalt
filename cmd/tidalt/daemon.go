package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Benehiko/tidalt/v4/internal/mpris"
	"github.com/Benehiko/tidalt/v4/internal/ui"
)

// systemd user service unit template.
const serviceTemplate = `[Unit]
Description=tidalt — Tidal HiFi music player daemon
Documentation=https://github.com/Benehiko/tidalt
After=graphical-session.target
PartOf=graphical-session.target

[Service]
Type=simple
ExecStart={{.Exec}} daemon
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=graphical-session.target
`

// runDaemon starts tidalt in headless daemon mode: full playback engine and
// MPRIS2 server, but no TUI. Control via client instances or playerctl.
// It returns an error so the caller can exit after deferred cleanup runs.
func runDaemon() error {
	ctx, stop := signalContext()
	defer stop()

	client, vault, session := loadSession(ctx)

	mprisServer, mprisErr := mpris.Start(ctx)
	if errors.Is(mprisErr, mpris.ErrAlreadyRunning) {
		return errors.New("a tidalt instance is already running")
	}
	if mprisErr != nil {
		fmt.Fprintf(os.Stderr, "MPRIS unavailable: %v\n", mprisErr)
	}

	fmt.Printf("tidalt daemon running (user %d, country %s)\n", session.UserID, session.CountryCode)
	fmt.Println("No audio device is opened until playback starts.")
	fmt.Println("Use 'tidalt' (client mode) or playerctl to control playback.")
	fmt.Println("Send SIGTERM or SIGINT to stop.")

	// Run the BubbleTea model without alt-screen and without a TTY.
	// WithoutRenderer suppresses all terminal output — the model drives
	// playback logic only; the UI is provided by client instances.
	p := tea.NewProgram(
		ui.InitialModel(ctx, client, vault, mprisServer, "").WithoutGraphics(),
		tea.WithoutRenderer(),
		tea.WithInput(nil),
	)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("daemon error: %w", err)
	}
	return nil
}

// runSetupDaemon installs a systemd --user service unit for tidalt, then
// enables and starts it.
func runSetupDaemon() {
	self, err := os.Executable()
	if err != nil {
		fatalf("cannot determine executable path: %v", err)
	}

	unitDir := filepath.Join(homeDir(), ".config", "systemd", "user")
	stepf("Creating directory %s", unitDir)
	if err := os.MkdirAll(unitDir, 0o700); err != nil {
		fatalf("mkdir %s: %v", unitDir, err)
	}

	unitPath := filepath.Join(unitDir, "tidalt.service")
	stepf("Writing %s", unitPath)

	tmpl, err := template.New("unit").Parse(serviceTemplate)
	if err != nil {
		fatalf("internal error: bad service template: %v", err)
	}

	//nolint:gosec // G304: unitPath is a fixed filename under the user's own ~/.config/systemd/user dir, not attacker-controlled
	f, err := os.OpenFile(unitPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fatalf("create %s: %v", unitPath, err)
	}
	if execErr := tmpl.Execute(f, struct{ Exec string }{Exec: self}); execErr != nil {
		_ = f.Close()
		fatalf("write %s: %v", unitPath, execErr)
	}
	if err := f.Close(); err != nil {
		fatalf("close %s: %v", unitPath, err)
	}

	run(false, "systemctl", "--user", "daemon-reload")
	run(false, "systemctl", "--user", "enable", "tidalt.service")
	run(false, "systemctl", "--user", "start", "tidalt.service")

	fmt.Println()
	fmt.Println("Daemon installed and started.")
	fmt.Println()
	fmt.Println("  Status : systemctl --user status tidalt")
	fmt.Println("  Logs   : journalctl --user -u tidalt -f")
	fmt.Println("  Stop   : systemctl --user stop tidalt")
	fmt.Println("  Disable: systemctl --user disable --now tidalt")
}
