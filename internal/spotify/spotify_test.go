package spotify

import (
	"os"
	"testing"
)

const kindPlaylist = "playlist"

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantKind string
		wantID   string
		wantOK   bool
	}{
		{"track url", "https://open.spotify.com/track/20jbSiX29FDX4oQxBXyUEi", "track", "20jbSiX29FDX4oQxBXyUEi", true},
		{"track url with si", "https://open.spotify.com/track/20jbSiX29FDX4oQxBXyUEi?si=abc123", "track", "20jbSiX29FDX4oQxBXyUEi", true},
		{"playlist url", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", kindPlaylist, "37i9dQZF1DXcBWIGoYBM5M", true},
		{"locale-prefixed", "https://open.spotify.com/intl-de/track/20jbSiX29FDX4oQxBXyUEi", "track", "20jbSiX29FDX4oQxBXyUEi", true},
		{"track uri", "spotify:track:20jbSiX29FDX4oQxBXyUEi", "track", "20jbSiX29FDX4oQxBXyUEi", true},
		{"playlist uri", "spotify:playlist:37i9dQZF1DXcBWIGoYBM5M", kindPlaylist, "37i9dQZF1DXcBWIGoYBM5M", true},
		{"leading/trailing space", "  https://open.spotify.com/track/abc123  ", "track", "abc123", true},
		{"album unsupported", "https://open.spotify.com/album/abc123", "", "", false},
		{"empty id", "https://open.spotify.com/track/", "", "", false},
		{"not spotify", "https://tidal.com/track/123", "", "", false},
		{"garbage", "not a url at all", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, id, ok := Parse(tc.in)
			if ok != tc.wantOK || kind != tc.wantKind || id != tc.wantID {
				t.Errorf("Parse(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, kind, id, ok, tc.wantKind, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestIsSpotifyURL(t *testing.T) {
	yes := []string{
		"https://open.spotify.com/track/abc",
		"spotify:playlist:abc",
		"  spotify:track:abc",
	}
	no := []string{"https://tidal.com/track/1", "hello", ""}
	for _, s := range yes {
		if !IsSpotifyURL(s) {
			t.Errorf("IsSpotifyURL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsSpotifyURL(s) {
			t.Errorf("IsSpotifyURL(%q) = true, want false", s)
		}
	}
}

func TestParsePlaylistPage(t *testing.T) {
	blob, err := os.ReadFile("testdata/playlist_embed.json")
	if err != nil {
		t.Fatal(err)
	}
	// Wrap the saved __NEXT_DATA__ JSON in a minimal page, as it appears live.
	html := []byte(`<html><body><script id="__NEXT_DATA__" type="application/json">` +
		string(blob) + `</script></body></html>`)

	src, err := parsePlaylistPage(html)
	if err != nil {
		t.Fatalf("parsePlaylistPage: %v", err)
	}
	if src.Kind != kindPlaylist {
		t.Errorf("Kind = %q, want playlist", src.Kind)
	}
	if src.Name != "Today’s Top Hits" {
		t.Errorf("Name = %q", src.Name)
	}
	if len(src.Tracks) != 3 {
		t.Fatalf("got %d tracks, want 3", len(src.Tracks))
	}
	if src.Tracks[0].Title != "hate that i made you love me" {
		t.Errorf("track[0].Title = %q", src.Tracks[0].Title)
	}
	if src.Tracks[0].Artist() != "Ariana Grande" {
		t.Errorf("track[0].Artist = %q", src.Tracks[0].Artist())
	}
}

func TestParsePlaylistPageErrors(t *testing.T) {
	cases := map[string][]byte{
		"no script tag":   []byte(`<html><body>nothing</body></html>`),
		"malformed json":  []byte(`<script id="__NEXT_DATA__" type="application/json">{not json</script>`),
		"empty tracklist": []byte(`<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"state":{"data":{"entity":{"name":"x","trackList":[]}}}}}}</script>`),
	}
	for name, html := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePlaylistPage(html); err == nil {
				t.Errorf("expected error for %s, got nil", name)
			}
		})
	}
}
