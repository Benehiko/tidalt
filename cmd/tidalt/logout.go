package main

import (
	"fmt"

	"github.com/Benehiko/tidalt/v3/internal/store"
	"github.com/Benehiko/tidalt/v3/internal/tidal"
)

func runLogout() {
	ctx, stop := signalContext()
	defer stop()

	vault := store.NewSecretsStore(readPassphrase)
	defer vault.Close()

	// Load the session so we can revoke the access token server-side.
	var session tidal.Session
	if err := vault.LoadSession(&session); err == nil && session.AccessToken != "" {
		client := tidal.NewClient()
		if err := client.RevokeToken(ctx, session.AccessToken); err != nil {
			fmt.Printf("Warning: failed to revoke token: %v\n", err)
		}
	}

	if err := vault.DeleteSession(); err != nil {
		fatal("logout: %v", err)
	}
	fmt.Println("Logged out. Run `tidalt` to log in again.")
}
