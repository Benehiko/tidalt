// Package spotify reads the track list behind a public Spotify track or
// playlist URL without any Spotify credentials or OAuth.
//
// Spotify locked down its Web API in February 2026: the Client Credentials
// flow no longer reads metadata, and even a full user-OAuth token can only read
// playlists the user owns. There is therefore no clean, official, no-login way
// to read an arbitrary public playlist. This package uses the two remaining
// credential-free paths:
//
//   - Track URLs resolve through the official oEmbed endpoint, which returns the
//     track title (no structured artist).
//   - Playlist URLs are read by scraping the server-rendered embed page, whose
//     __NEXT_DATA__ JSON blob carries the full track list (title + artist).
//
// The playlist path is unofficial and brittle: if Spotify changes its embed
// markup the blob shape may change and Resolve will return a clear error. All
// parsing is isolated here so breakage is contained and guarded by tests.
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// SourceTrack is one track read from a Spotify source.
type SourceTrack struct {
	Title   string // song name
	Artists []string
}

// Artist returns the primary artist name, or "" if none is known.
func (t SourceTrack) Artist() string {
	if len(t.Artists) == 0 {
		return ""
	}
	return t.Artists[0]
}

// Source is the resolved Spotify track or playlist.
type Source struct {
	Kind   string // "track" or "playlist"
	Name   string // playlist title, or track title
	Tracks []SourceTrack
}

// userAgent is sent on the embed-page request; Spotify serves the rendered
// page (with the __NEXT_DATA__ blob) to desktop browser agents.
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// idPattern validates a Spotify base-62 entity id.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// IsSpotifyURL reports whether s looks like a Spotify URL or URI we can resolve.
func IsSpotifyURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "open.spotify.com/") || strings.HasPrefix(s, "spotify:")
}

// Parse extracts the entity kind ("track"/"playlist") and id from a Spotify URL
// or URI. It accepts:
//
//	https://open.spotify.com/track/{id}[?si=...]
//	https://open.spotify.com/playlist/{id}[?si=...]
//	spotify:track:{id}
//	spotify:playlist:{id}
//
// Locale-prefixed web URLs (open.spotify.com/intl-de/track/{id}) are handled.
func Parse(raw string) (kind, id string, ok bool) {
	raw = strings.TrimSpace(raw)

	// spotify:track:{id} / spotify:playlist:{id}
	if rest, found := strings.CutPrefix(raw, "spotify:"); found {
		parts := strings.Split(rest, ":")
		if len(parts) >= 2 {
			return classify(parts[0], parts[1])
		}
		return "", "", false
	}

	u, err := url.Parse(raw)
	if err != nil || !strings.Contains(u.Host, "open.spotify.com") {
		return "", "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Walk segments and take the first track/playlist pair, skipping any
	// locale prefix such as "intl-de".
	for i := 0; i+1 < len(segs); i++ {
		if k, id, ok := classify(segs[i], segs[i+1]); ok {
			return k, id, true
		}
	}
	return "", "", false
}

// classify validates an (entity, id) pair we support.
func classify(entity, id string) (kind, entityID string, ok bool) {
	switch entity {
	case "track", "playlist":
		if id != "" && idPattern.MatchString(id) {
			return entity, id, true
		}
	}
	return "", "", false
}

// Resolve fetches the track list for a Spotify track or playlist URL. The given
// http.Client controls timeout and transport; ctx is honoured for cancellation.
func Resolve(ctx context.Context, hc *http.Client, raw string) (*Source, error) {
	kind, id, ok := Parse(raw)
	if !ok {
		return nil, errors.New("not a recognised Spotify track or playlist URL")
	}
	switch kind {
	case "track":
		return resolveTrack(ctx, hc, id)
	case "playlist":
		return resolvePlaylist(ctx, hc, id)
	default:
		return nil, fmt.Errorf("unsupported Spotify entity %q", kind)
	}
}

// resolveTrack reads a single track's title via the official oEmbed endpoint.
// oEmbed returns only a combined title string (no structured artist).
func resolveTrack(ctx context.Context, hc *http.Client, id string) (*Source, error) {
	ref := "https://open.spotify.com/track/" + id
	u := "https://open.spotify.com/oembed?url=" + url.QueryEscape(ref)

	body, err := httpGet(ctx, hc, u, false)
	if err != nil {
		return nil, err
	}
	var oe struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(body, &oe); err != nil {
		return nil, fmt.Errorf("decode Spotify oEmbed: %w", err)
	}
	if oe.Title == "" {
		return nil, errors.New("spotify oEmbed returned no title")
	}
	return &Source{
		Kind:   "track",
		Name:   oe.Title,
		Tracks: []SourceTrack{{Title: oe.Title}},
	}, nil
}

// resolvePlaylist scrapes the embed page's __NEXT_DATA__ blob for the full
// track list. Unofficial and layout-dependent.
func resolvePlaylist(ctx context.Context, hc *http.Client, id string) (*Source, error) {
	u := "https://open.spotify.com/embed/playlist/" + id
	body, err := httpGet(ctx, hc, u, true)
	if err != nil {
		return nil, err
	}
	return parsePlaylistPage(body)
}

// nextDataRe captures the JSON inside the page's __NEXT_DATA__ script tag.
var nextDataRe = regexp.MustCompile(
	`(?s)<script id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

// embedPage models the slice of __NEXT_DATA__ we depend on. Every field is
// guarded so a layout change degrades to a clear error, not a panic.
type embedPage struct {
	Props struct {
		PageProps struct {
			State struct {
				Data struct {
					Entity struct {
						Name      string `json:"name"`
						Title     string `json:"title"`
						TrackList []struct {
							Title    string `json:"title"`
							Subtitle string `json:"subtitle"`
						} `json:"trackList"`
					} `json:"entity"`
				} `json:"data"`
			} `json:"state"`
		} `json:"pageProps"`
	} `json:"props"`
}

// parsePlaylistPage extracts the track list from an embed page's HTML. Exported
// indirectly via Resolve; split out so tests can feed saved fixtures.
func parsePlaylistPage(html []byte) (*Source, error) {
	const layoutErr = "could not parse Spotify playlist page — its layout may have changed"

	m := nextDataRe.FindSubmatch(html)
	if m == nil {
		return nil, errors.New(layoutErr)
	}
	var page embedPage
	if err := json.Unmarshal(m[1], &page); err != nil {
		return nil, fmt.Errorf("%s: %w", layoutErr, err)
	}

	ent := page.Props.PageProps.State.Data.Entity
	if len(ent.TrackList) == 0 {
		return nil, errors.New(layoutErr)
	}

	name := ent.Name
	if name == "" {
		name = ent.Title
	}
	tracks := make([]SourceTrack, 0, len(ent.TrackList))
	for _, t := range ent.TrackList {
		if t.Title == "" {
			continue
		}
		st := SourceTrack{Title: t.Title}
		// subtitle is a comma-separated artist string, e.g. "A, B".
		if t.Subtitle != "" {
			for a := range strings.SplitSeq(t.Subtitle, ",") {
				if a = strings.TrimSpace(a); a != "" {
					st.Artists = append(st.Artists, a)
				}
			}
		}
		tracks = append(tracks, st)
	}
	if len(tracks) == 0 {
		return nil, errors.New(layoutErr)
	}
	return &Source{Kind: "playlist", Name: name, Tracks: tracks}, nil
}

// httpGet issues a GET and returns the body, optionally setting a browser
// User-Agent (needed for the embed page to render server-side).
func httpGet(ctx context.Context, hc *http.Client, u string, browserUA bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	if browserUA {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spotify request failed: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}
