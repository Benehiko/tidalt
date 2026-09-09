package tidal_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/Benehiko/tidalt/v4/internal/tidal"
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
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
		if !strings.Contains(r.URL.Query().Get("types"), "TRACKS") {
			t.Errorf("types param missing TRACKS: %q", r.URL.Query().Get("types"))
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

	info, err := newTestClient(srv).GetStreamURL(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "https://cdn.tidal.com/stream.flac" {
		t.Errorf("unexpected URL: %s", info.URL)
	}
	if info.Ext != "flac" {
		t.Errorf("unexpected ext: %s", info.Ext)
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

	info, err := newTestClient(srv).GetStreamURL(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "https://cdn.tidal.com/lossless.flac" {
		t.Errorf("unexpected URL: %s", info.URL)
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

// mixPage builds a v1 "pages/my_collection_my_mixes" payload from the given
// mix entries.
func mixPage(items ...map[string]any) map[string]any {
	return map[string]any{
		"rows": []map[string]any{{
			"modules": []map[string]any{{
				"type": "MIX_LIST",
				"pagedList": map[string]any{
					"totalNumberOfItems": len(items),
					"items":              items,
				},
			}},
		}},
	}
}

func TestGetMixes_OK(t *testing.T) {
	payload := mixPage(
		map[string]any{
			"id":       "mix1",
			"title":    "Daily Mix",
			"subTitle": "Your daily picks",
			"mixType":  "DAILY_MIX",
		},
		map[string]any{
			"id":       "mix2",
			"title":    "Chill Mix",
			"subTitle": "Relaxing vibes",
			"mixType":  "DISCOVERY_MIX",
		},
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pages/my_collection_my_mixes") {
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
	if mixes[0].ID != "mix1" {
		t.Errorf("unexpected mix ID: %q", mixes[0].ID)
	}
	if mixes[0].SubTitle != "Your daily picks" {
		t.Errorf("unexpected subtitle: %q", mixes[0].SubTitle)
	}
}

func TestGetMixes_SkipsVideoMixes(t *testing.T) {
	// Video mixes serve video items, which the player cannot decode, so they
	// must not appear in the list.
	payload := mixPage(
		map[string]any{"id": "mix1", "title": "My Mix 1", "mixType": "DAILY_MIX"},
		map[string]any{"id": "vid1", "title": "My Video Mix 1", "mixType": "VIDEO_DAILY_MIX"},
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, payload)
	}))
	defer srv.Close()

	mixes, err := newTestClient(srv).GetMixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mixes) != 1 {
		t.Fatalf("expected 1 mix (video mix skipped), got %d", len(mixes))
	}
	if mixes[0].ID != "mix1" {
		t.Errorf("unexpected mix: %+v", mixes[0])
	}
}

func TestGetMixes_SkipsDuplicatesAndBlankIDs(t *testing.T) {
	// A page can repeat the same mix across modules; entries without an ID are
	// unusable.
	payload := mixPage(
		map[string]any{"id": "mix1", "title": "My Mix 1", "mixType": "DAILY_MIX"},
		map[string]any{"id": "mix1", "title": "My Mix 1", "mixType": "DAILY_MIX"},
		map[string]any{"id": "", "title": "Nameless", "mixType": "DAILY_MIX"},
	)

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
}

func TestGetMixes_NullDescription(t *testing.T) {
	// Tidal sends "description": null for every mix; it must decode to "".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":[{"modules":[{"pagedList":{"items":[
			{"id":"mix1","title":"My Mix 1","subTitle":"a, b and more","description":null,"mixType":"DAILY_MIX"}
		]}}]}]}`))
	}))
	defer srv.Close()

	mixes, err := newTestClient(srv).GetMixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mixes) != 1 {
		t.Fatalf("expected 1 mix, got %d", len(mixes))
	}
	if mixes[0].Description != "" {
		t.Errorf("expected empty description, got %q", mixes[0].Description)
	}
}

func TestGetMixes_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 404, map[string]any{"userMessage": "Resource not found"})
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).GetMixes(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- GetMixTracks ---

// mixItems builds a v1 /mixes/{id}/items payload from typed entries.
func mixItems(entries ...map[string]any) map[string]any {
	return map[string]any{
		"totalNumberOfItems": len(entries),
		"items":              entries,
	}
}

func TestGetMixTracks_OK(t *testing.T) {
	// The v1 mix items endpoint returns fully populated tracks in one request.
	track1 := tidal.Track{ID: 101, Title: "Big Song"}
	track1.Artist.Name = "The Band"
	track1.Album.Title = "The Album"

	track2 := tidal.Track{ID: 202, Title: "Small Song"}
	track2.Artist.Name = "Other Artist"
	track2.Album.Title = "Other Album"

	payload := mixItems(
		map[string]any{"item": track1, "type": "track"},
		map[string]any{"item": track2, "type": "track"},
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/mixes/mix1/items") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).GetMixTracks(context.Background(), "mix1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	// Mix order must be preserved: 101 first, 202 second.
	if tracks[0].ID != 101 || tracks[0].Title != "Big Song" {
		t.Errorf("unexpected track[0]: %+v", tracks[0])
	}
	if tracks[0].Artist.Name != "The Band" {
		t.Errorf("unexpected artist: %q", tracks[0].Artist.Name)
	}
	if tracks[0].Album.Title != "The Album" {
		t.Errorf("unexpected album: %q", tracks[0].Album.Title)
	}
	if tracks[1].ID != 202 || tracks[1].Title != "Small Song" {
		t.Errorf("unexpected track[1]: %+v", tracks[1])
	}
}

func TestGetMixTracks_NormalizesArtist(t *testing.T) {
	// Some payloads carry only the plural "artists" array.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"type":"track","item":{
			"id":101,"title":"Song","artists":[{"id":7,"name":"Plural Only"}]
		}}]}`))
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).GetMixTracks(context.Background(), "mix1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Artist.Name != "Plural Only" || tracks[0].Artist.ID != 7 {
		t.Errorf("artist not normalized: %+v", tracks[0].Artist)
	}
}

func TestGetMixTracks_SkipsNonTrackItems(t *testing.T) {
	// Video items cannot be decoded by the player and must be dropped.
	payload := mixItems(
		map[string]any{"item": tidal.Track{ID: 101, Title: "Song"}, "type": "track"},
		map[string]any{"item": tidal.Track{ID: 555, Title: "Clip"}, "type": "video"},
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, payload)
	}))
	defer srv.Close()

	tracks, err := newTestClient(srv).GetMixTracks(context.Background(), "mix1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track (video skipped), got %d", len(tracks))
	}
	if tracks[0].ID != 101 {
		t.Errorf("unexpected track: %+v", tracks[0])
	}
}

func TestGetMixTracks_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 404, map[string]any{"userMessage": "Resource not found"})
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMixTracks(context.Background(), "gone")
	if !errors.Is(err, tidal.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
