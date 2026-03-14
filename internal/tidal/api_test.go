package tidal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/Benehiko/tidalt/internal/tidal"
)

// roundTripFunc is a convenience type that lets a plain function satisfy
// http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newTestClient returns a *tidal.Client pre-loaded with a dummy session and a
// transport that delegates to the supplied httptest.Server.  The token is set
// far in the future so the oauth2 layer never attempts a refresh.
func newTestClient(srv *httptest.Server) *tidal.Client {
	c := tidal.NewClient()
	c.Session = &tidal.Session{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
		UserID:      42,
		CountryCode: "US",
	}
	// Rewrite every request to point at the test server instead of the real
	// Tidal API endpoints.
	c.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		req.URL.Scheme = "http"
		return srv.Client().Transport.RoundTrip(req)
	})
	// Give the oauth2 config a token endpoint on the test server so that any
	// token refresh attempt would also stay local (won't be triggered in
	// practice because the expiry is in the future).
	c.Oauth.Endpoint = oauth2.Endpoint{
		TokenURL: srv.URL + "/token",
	}
	return c
}

// respond writes JSON to the ResponseRecorder / hijacked response.
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- GetUser ---

func TestGetUser_OK(t *testing.T) {
	want := tidal.UserResponse{
		ID:          42,
		CountryCode: "US",
		Email:       "user@example.com",
		FullName:    "Test User",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/users/42") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		respond(w, 200, want)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Email != want.Email || got.FullName != want.FullName {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUser_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 401, map[string]string{"error": "unauthorized"})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetUser(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

// --- GetTrack ---

func TestGetTrack_OK(t *testing.T) {
	want := tidal.Track{ID: 123, Title: "Song Title"}
	want.Artist.Name = "Artist Name"
	want.Album.Title = "Album Title"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tracks/123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		respond(w, 200, want)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetTrack(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.Artist.Name != want.Artist.Name {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetTrack_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 404, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetTrack(context.Background(), "999")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// --- Search ---

func TestSearch_OK(t *testing.T) {
	payload := tidal.SearchResponse{}
	payload.Tracks.Items = []tidal.Track{
		{ID: 1, Title: "Alpha"},
		{ID: 2, Title: "Beta"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/search") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if q := r.URL.Query().Get("query"); q != "test query" {
			t.Errorf("unexpected query param: %q", q)
		}
		if r.URL.Query().Get("types") != "TRACKS" {
			t.Errorf("types param missing or wrong")
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).Search(context.Background(), "test query")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].Title != "Alpha" || tracks[1].Title != "Beta" {
		t.Errorf("unexpected tracks: %+v", tracks)
	}
}

func TestSearch_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, tidal.SearchResponse{})
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).Search(context.Background(), "nothing")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Errorf("expected empty slice, got %d tracks", len(tracks))
	}
}

// --- GetStreamURL ---

func TestGetStreamURL_FirstQualitySucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("audioquality")
		if q == "HI_RES_LOSSLESS" {
			respond(w, 200, tidal.StreamResponse{URLs: []string{"https://cdn.tidal.com/stream.flac"}})
			return
		}
		respond(w, 404, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	u, err := newTestClient(srv).GetStreamURL(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://cdn.tidal.com/stream.flac" {
		t.Errorf("unexpected URL: %s", u)
	}
}

func TestGetStreamURL_FallsBackThroughQualities(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("audioquality")
		seen = append(seen, q)
		if q == "LOSSLESS" {
			respond(w, 200, tidal.StreamResponse{URLs: []string{"https://cdn.tidal.com/lossless.flac"}})
			return
		}
		respond(w, 404, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	u, err := newTestClient(srv).GetStreamURL(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://cdn.tidal.com/lossless.flac" {
		t.Errorf("unexpected URL: %s", u)
	}
	if len(seen) < 2 || seen[0] != "HI_RES_LOSSLESS" || seen[1] != "LOSSLESS" {
		t.Errorf("unexpected quality ladder: %v", seen)
	}
}

func TestGetStreamURL_AllQualitiesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 404, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetStreamURL(context.Background(), 123)
	if err == nil {
		t.Fatal("expected error when all qualities fail")
	}
}

func TestGetStreamURL_EmptyURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, tidal.StreamResponse{URLs: []string{}})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetStreamURL(context.Background(), 123)
	if err == nil {
		t.Fatal("expected error for empty URLs in response")
	}
}

// --- GetFavorites ---

func TestGetFavorites_OK(t *testing.T) {
	payload := tidal.FavoritesResponse{
		Items: []struct {
			Item tidal.Track `json:"item"`
		}{
			{Item: tidal.Track{ID: 10, Title: "Fav One"}},
			{Item: tidal.Track{ID: 20, Title: "Fav Two"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/favorites/tracks") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("order") != "DATE" {
			t.Errorf("order param missing or wrong")
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).GetFavorites(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].ID != 10 || tracks[1].ID != 20 {
		t.Errorf("unexpected tracks: %+v", tracks)
	}
}

// --- GetMixes ---

func TestGetMixes_OK(t *testing.T) {
	// Build a v2 JSON:API response: two mix references + their playlist attributes
	// in included.
	type playlistObj struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"attributes"`
	}

	pl1 := playlistObj{ID: "mix1", Type: "playlists"}
	pl1.Attributes.Name = "Daily Mix"
	pl1.Attributes.Description = "Your daily picks"

	pl2 := playlistObj{ID: "mix2", Type: "playlists"}
	pl2.Attributes.Name = "Chill Mix"
	pl2.Attributes.Description = "Relaxing vibes"

	inc1, _ := json.Marshal(pl1)
	inc2, _ := json.Marshal(pl2)

	payload := map[string]any{
		"data": []map[string]string{
			{"id": "mix1", "type": "playlists"},
			{"id": "mix2", "type": "playlists"},
		},
		"included": []json.RawMessage{inc1, inc2},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/userRecommendations/me/relationships/myMixes") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	mixes, err := newTestClient(srv).GetMixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mixes) != 2 {
		t.Fatalf("expected 2 mixes, got %d", len(mixes))
	}
	if mixes[0].Title != "Daily Mix" || mixes[1].Title != "Chill Mix" {
		t.Errorf("unexpected mix titles: %+v", mixes)
	}
	if mixes[0].SubTitle != "Your daily picks" {
		t.Errorf("unexpected subtitle: %q", mixes[0].SubTitle)
	}
}

func TestGetMixes_MissingIncluded(t *testing.T) {
	// data references mix IDs that have no matching included entry —
	// the title should fall back to the raw ID.
	payload := map[string]any{
		"data": []map[string]string{
			{"id": "orphan-mix", "type": "playlists"},
		},
		"included": []json.RawMessage{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, payload)
	}))
	defer srv.Close()

	mixes, err := newTestClient(srv).GetMixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mixes) != 1 {
		t.Fatalf("expected 1 mix, got %d", len(mixes))
	}
	if mixes[0].Title != "orphan-mix" {
		t.Errorf("expected fallback to ID, got %q", mixes[0].Title)
	}
}

// --- GetMixTracks ---

func TestGetMixTracks_OK(t *testing.T) {
	type artistAttr struct {
		Name string `json:"name"`
	}
	type artistObj struct {
		ID         string     `json:"id"`
		Type       string     `json:"type"`
		Attributes artistAttr `json:"attributes"`
	}
	type trackAttr struct {
		Title string `json:"title"`
	}
	type trackRel struct {
		Artists struct {
			Data []map[string]string `json:"data"`
		} `json:"artists"`
	}
	type trackObj struct {
		ID            string    `json:"id"`
		Type          string    `json:"type"`
		Attributes    trackAttr `json:"attributes"`
		Relationships trackRel  `json:"relationships"`
	}

	artist := artistObj{ID: "a1", Type: "artists", Attributes: artistAttr{Name: "The Band"}}
	track := trackObj{ID: "101", Type: "tracks", Attributes: trackAttr{Title: "Big Song"}}
	track.Relationships.Artists.Data = []map[string]string{{"id": "a1", "type": "artists"}}

	incArtist, _ := json.Marshal(artist)
	incTrack, _ := json.Marshal(track)

	payload := map[string]any{
		"data": []map[string]string{
			{"id": "101", "type": "tracks"},
		},
		"included": []json.RawMessage{incArtist, incTrack},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/playlists/mix1/relationships/items") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).GetMixTracks(context.Background(), "mix1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].ID != 101 {
		t.Errorf("unexpected track ID: %d", tracks[0].ID)
	}
	if tracks[0].Title != "Big Song" {
		t.Errorf("unexpected title: %q", tracks[0].Title)
	}
	if tracks[0].Artist.Name != "The Band" {
		t.Errorf("unexpected artist: %q", tracks[0].Artist.Name)
	}
}

func TestGetMixTracks_SkipsNonTrackRefs(t *testing.T) {
	// data contains a "videos" ref that should be silently skipped.
	payload := map[string]any{
		"data": []map[string]string{
			{"id": "v1", "type": "videos"},
		},
		"included": []json.RawMessage{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, payload)
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).GetMixTracks(context.Background(), "mix1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Errorf("expected 0 tracks, got %d", len(tracks))
	}
}
