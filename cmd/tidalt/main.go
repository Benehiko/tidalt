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

// signalContext returns a context that is cancelled on SIGINT or SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
}

// loadSession opens the secrets store, loads or performs interactive OAuth2
// login, and returns the client and vault ready for use. On error it prints to
// stderr and exits.
func loadSession(ctx context.Context) (*tidal.Client, *store.SecretsStore, tidal.Session) {
	client := tidal.NewClient()
	vault := store.NewSecretsStore(readPassphrase)

	var session tidal.Session
	err := vault.LoadSession(&session)
	if err != nil || session.CountryCode == "" {
		if err == nil {
			fmt.Println("Existing session is incomplete (missing country code).")
		} else {
			fmt.Println("No active session found.")
		}
		newSession, loginErr := client.AuthenticateInteractive(ctx)
		if loginErr != nil {
			if errors.Is(loginErr, context.Canceled) {
				fmt.Println("\nLogin cancelled.")
				os.Exit(0)
			}
			fmt.Printf("Login failed: %v\n", loginErr)
			os.Exit(1)
		}
		session = *newSession
		if saveErr := vault.SaveSession(session); saveErr != nil {
			fmt.Printf("Failed to save session: %v\n", saveErr)
			os.Exit(1)
		}
	} else {
		client.Session = &session
		fmt.Printf("Restored session for User %d (Country: %s)\n", session.UserID, session.CountryCode)
	}
	return client, vault, session
}

func main() {
	if len(os.Args) < 2 {
		runTUI("")
		return
	}

	switch os.Args[1] {
	case "setup":
		if len(os.Args) > 2 && os.Args[2] == "--daemon" {
			runSetupDaemon()
		} else {
			runSetup()
		}
	case "play":
		url := ""
		if len(os.Args) > 2 {
			url = os.Args[2]
		}
		runPlay(url)
	case "daemon":
		runDaemon()
	case "logout":
		runLogout()
	default:
		// Treat os.Args[1] as an optional tidal:// or https://tidal.com/ URL
		// (passed by the OS when the user clicks "Open in desktop app").
		runTUI(os.Args[1])
	}
}

// runTUI starts the full interactive TUI, optionally pre-queuing a URL.
func runTUI(openURL string) {
	ctx, stop := signalContext()
	defer stop()

	client, vault, _ := loadSession(ctx)

	mprisServer, mprisErr := mpris.Start(ctx)
	if errors.Is(mprisErr, mpris.ErrAlreadyRunning) {
		// Another instance is running — open a client-mode TUI that forwards
		// commands over D-Bus.
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

	p := tea.NewProgram(ui.InitialModel(ctx, client, vault, mprisServer, openURL), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
