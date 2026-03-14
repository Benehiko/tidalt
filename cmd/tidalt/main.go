package main

import (
	"context"
	"errors"
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
	// Dispatch subcommands before anything else so they don't require a session.
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		runSetup()
		return
	}

	// Anything else in os.Args[1] is treated as an optional tidal:// or
	// https://tidal.com/ URL (passed by the OS when the user clicks
	// "Open in desktop app").
	var openURL string
	if len(os.Args) > 1 {
		openURL = os.Args[1]
	}

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

	// 3. Start MPRIS2 server.
	// If another instance is already running, run the TUI in client mode:
	// it can browse and queue tracks but all playback commands are forwarded
	// over D-Bus to the parent instance.
	mprisServer, mprisErr := mpris.Start(ctx)
	if errors.Is(mprisErr, mpris.ErrAlreadyRunning) {
		// Close the vault opened above — the parent holds the DB lock.
		// Open a client-only store that skips the DB.
		vault.Close()
		clientVault := store.NewClientStore(readPassphrase)
		mprisClient, err := mpris.NewClient()
		if err != nil {
			fmt.Printf("Failed to connect to running instance: %v\n", err)
			os.Exit(1)
		}
		defer mprisClient.Close()
		p := tea.NewProgram(
			ui.ClientModel(ctx, client, clientVault, mprisClient, openURL),
			tea.WithAltScreen(),
		)
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if mprisErr != nil {
		fmt.Printf("MPRIS unavailable: %v\n", mprisErr)
	}

	// 4. Launch TUI — vault is passed in so it is not re-created inside the TUI
	// (re-creating it after the terminal is in raw mode would prevent passphrase prompts).
	// The TUI model takes ownership of vault and closes it on quit.
	p := tea.NewProgram(ui.InitialModel(ctx, client, vault, mprisServer, openURL), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
