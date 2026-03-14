package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"

	"github.com/Benehiko/tidalt/internal/mpris"
	"github.com/Benehiko/tidalt/internal/store"
	"github.com/Benehiko/tidalt/internal/tidal"
	"github.com/Benehiko/tidalt/internal/ui"
)

// readPassphrase reads a passphrase from stdin with echo disabled.
func readPassphrase(_ context.Context, prompt string) ([]byte, error) {
	fmt.Print(prompt + ": ")

	oldState, err := unix.IoctlGetTermios(syscall.Stdin, unix.TCGETS)
	if err != nil {
		// Not a terminal — fall back to plain read.
		var buf [256]byte
		n, err := syscall.Read(syscall.Stdin, buf[:])
		return trimNewline(buf[:n]), err
	}

	noEcho := *oldState
	noEcho.Lflag &^= unix.ECHO
	_ = unix.IoctlSetTermios(syscall.Stdin, unix.TCSETS, &noEcho)

	var buf [256]byte
	n, readErr := syscall.Read(syscall.Stdin, buf[:])

	// Always restore terminal state.
	_ = unix.IoctlSetTermios(syscall.Stdin, unix.TCSETS, oldState)
	fmt.Println()

	return trimNewline(buf[:n]), readErr
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func main() {
	// Handle interrupt signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	client := tidal.NewClient()
	vault := store.NewSecretsStore(readPassphrase)

	// 1. Try to load session from keychain
	var session tidal.Session
	err := vault.LoadSession(&session)

	// If loading fails OR session is incomplete (missing CountryCode)
	if err != nil || session.CountryCode == "" {
		if err == nil {
			fmt.Println("Existing session is incomplete (missing country code).")
		} else {
			fmt.Println("No active session found.")
		}

		// 2. Perform interactive login
		newSession, err := client.AuthenticateInteractive(ctx)
		if err != nil {
			if err == context.Canceled {
				fmt.Println("\nLogin cancelled.")
				os.Exit(0)
			}
			fmt.Printf("Login failed: %v\n", err)
			os.Exit(1)
		}
		session = *newSession
		if err := vault.SaveSession(session); err != nil {
			fmt.Printf("Failed to save session: %v\n", err)
			os.Exit(1)
		}
	} else {
		client.Session = &session
		fmt.Printf("Restored session for User %d (Country: %s)\n", session.UserID, session.CountryCode)
	}

	// 3. Start MPRIS2 server (non-fatal if no session bus)
	mprisServer, mprisErr := mpris.Start(ctx)
	if mprisErr != nil {
		fmt.Printf("MPRIS unavailable: %v\n", mprisErr)
	}

	// 4. Launch TUI — vault is passed in so it is not re-created inside the TUI
	// (re-creating it after the terminal is in raw mode would prevent passphrase prompts).
	// The TUI model takes ownership of vault and closes it on quit.
	p := tea.NewProgram(ui.InitialModel(ctx, client, vault, mprisServer.Commands), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
