package tidal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
	"golang.org/x/oauth2"
)

const (
	ClientID     = "fX2JxdmntZWK0ixT"
	ClientSecret = "1Nn9AfDAjxrgJFJbKNWLeAyKGVGmINuXPPLHVXAvxAg="
	AuthURL      = "https://auth.tidal.com/v1/oauth2"
	BaseURL      = "https://api.tidal.com/v1"
	BaseURLV2    = "https://openapi.tidal.com/v2"
)

type Client struct {
	Session   *Session
	Oauth     *oauth2.Config
	Transport http.RoundTripper // optional; overrides the base transport (used in tests)
}

type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	UserID       int       `json:"user_id"`
	CountryCode  string    `json:"country_code"`
}

func NewClient() *Client {
	return &Client{
		Oauth: &oauth2.Config{
			ClientID:     ClientID,
			ClientSecret: ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:       AuthURL + "/device_authorization",
				DeviceAuthURL: AuthURL + "/device_authorization",
				TokenURL:      AuthURL + "/token",
			},
			Scopes: []string{"r_usr", "w_usr", "w_sub"},
		},
	}
}

// AuthenticateInteractive runs the flow in the terminal before TUI launch
func (c *Client) AuthenticateInteractive(ctx context.Context) (*Session, error) {
	fmt.Println("Initiating Tidal Login...")

	httpClient := &http.Client{Timeout: 10 * time.Second}

	// 1. Request Device Code manually — Tidal's response uses camelCase JSON
	// keys (e.g. "deviceCode") instead of the RFC 8628 snake_case
	// ("device_code"), so oauth2.Config.DeviceAuth cannot parse it directly.
	data := url.Values{}
	data.Set("client_id", ClientID)
	data.Set("scope", "r_usr w_usr w_sub")

	resp, err := httpClient.PostForm(c.Oauth.Endpoint.AuthURL, data)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var da struct {
		DeviceCode      string `json:"deviceCode"`
		UserCode        string `json:"userCode"`
		VerificationURI string `json:"verificationUri"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&da); err != nil {
		return nil, err
	}

	verifyURL := "https://" + da.VerificationURI + "?user_code=" + url.QueryEscape(da.UserCode)

	fmt.Printf("\n1. Go to: %s\n", verifyURL)
	fmt.Printf("2. Enter Code: %s\n\n", da.UserCode)
	fmt.Println("Opening browser... or visit the link above to log in.")

	_ = browser.OpenURL(verifyURL)

	// 2. Poll for Token
	standardDA := &oauth2.DeviceAuthResponse{
		DeviceCode:      da.DeviceCode,
		UserCode:        da.UserCode,
		VerificationURI: da.VerificationURI,
		Interval:        int64(da.Interval),
	}

	token, err := c.Oauth.DeviceAccessToken(ctx, standardDA)
	if err != nil {
		return nil, err
	}

	// 3. Fetch Session Info to get UserID and CountryCode reliably
	// Tidal provides a /sessions endpoint that returns the current session info
	req, err := http.NewRequestWithContext(ctx, "GET", BaseURL+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	sResp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch session info: %w", err)
	}
	defer func() { _ = sResp.Body.Close() }()

	var sessionInfo struct {
		UserID      int    `json:"userId"`
		CountryCode string `json:"countryCode"`
	}
	if err := json.NewDecoder(sResp.Body).Decode(&sessionInfo); err != nil {
		// Fallback to token extras if session endpoint fails
		if user, ok := token.Extra("user").(map[string]interface{}); ok {
			if id, ok := user["id"].(float64); ok {
				sessionInfo.UserID = int(id)
			}
		}
		if cc, ok := token.Extra("countryCode").(string); ok {
			sessionInfo.CountryCode = cc
		}
	}

	c.Session = &Session{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		UserID:       sessionInfo.UserID,
		CountryCode:  sessionInfo.CountryCode,
	}

	if c.Session.CountryCode == "" {
		// Final fallback to a common country if still missing,
		// but the above should work.
		c.Session.CountryCode = "US"
	}

	fmt.Printf("Successfully authenticated! (User: %d, Country: %s)\n", c.Session.UserID, c.Session.CountryCode)
	return c.Session, nil
}

func (c *Client) TokenSource(ctx context.Context) oauth2.TokenSource {
	t := &oauth2.Token{
		AccessToken:  c.Session.AccessToken,
		RefreshToken: c.Session.RefreshToken,
		TokenType:    c.Session.TokenType,
		Expiry:       c.Session.Expiry,
	}
	return c.Oauth.TokenSource(ctx, t)
}

// RevokeToken revokes the given token via the Tidal OAuth2 revocation endpoint.
// Errors are logged but not fatal — the caller should still delete the local
// session regardless.
func (c *Client) RevokeToken(ctx context.Context, token string) error {
	data := url.Values{}
	data.Set("token", token)
	data.Set("client_id", ClientID)
	data.Set("client_secret", ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AuthURL+"/revoke",
		strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("revoke returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetAuthClient(ctx context.Context) *http.Client {
	if c.Transport != nil {
		// Wrap the custom transport in an oauth2.Transport so the Authorization
		// header is still added, but the actual round-trip goes through our
		// injected transport (useful for tests).
		return &http.Client{
			Transport: &oauth2.Transport{
				Source: c.TokenSource(ctx),
				Base:   c.Transport,
			},
		}
	}
	return oauth2.NewClient(ctx, c.TokenSource(ctx))
}
