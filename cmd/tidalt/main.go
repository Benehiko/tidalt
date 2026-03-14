package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Benehiko/tidalt/internal/store"
	"github.com/Benehiko/tidalt/internal/tidal"
	"github.com/Benehiko/tidalt/internal/ui"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigChan
		cancel()
	}()

	client := tidal.NewClient()
	vault := store.NewSecretsStore()

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
		vault.SaveSession(session)
	} else {
		client.Session = &session
		fmt.Printf("Restored session for User %d (Country: %s)\n", session.UserID, session.CountryCode)
	}

	// 3. Launch TUI
	p := tea.NewProgram(ui.InitialModel(ctx, client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
