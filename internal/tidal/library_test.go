package tidal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- SearchAll ---

func TestSearchAll_GroupsResults(t *testing.T) {
	payload := map[string]any{
		"tracks": map[string]any{"items": []map[string]any{
			{"id": 1, "title": "Song A", "artists": []map[string]any{{"id": 9, "name": "Artist X"}}},
		}},
		"artists":   map[string]any{"items": []map[string]any{{"id": 9, "name": "Artist X"}}},
		"albums":    map[string]any{"items": []map[string]any{{"id": 5, "title": "Album Y"}}},
		"playlists": map[string]any{"items": []map[string]any{{"uuid": "abc", "title": "Playlist Z"}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("types"), "ARTISTS") {
			t.Errorf("expected multi-type search, got types=%q", r.URL.Query().Get("types"))
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	res, err := newTestClient(srv).SearchAll(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 1 || len(res.Artists) != 1 || len(res.Albums) != 1 || len(res.Playlists) != 1 {
		t.Fatalf("unexpected group sizes: %+v", res)
	}
	// Track artist should be normalized from the plural array.
	if res.Tracks[0].Artist.Name != "Artist X" {
		t.Errorf("track artist not normalized: %+v", res.Tracks[0].Artist)
	}
	if res.Albums[0].Title != "Album Y" || res.Playlists[0].UUID != "abc" {
		t.Errorf("unexpected album/playlist decode: %+v %+v", res.Albums[0], res.Playlists[0])
	}
}

// --- Favorite artists / albums ---

func TestGetFavoriteAlbums_OK(t *testing.T) {
	payload := map[string]any{"items": []map[string]any{
		{"item": map[string]any{"id": 5, "title": "Album Y", "numberOfTracks": 11}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/users/42/favorites/albums") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		respond(w, 200, payload)
	}))
	defer srv.Close()

	albums, err := newTestClient(srv).GetFavoriteAlbums(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].Title != "Album Y" || albums[0].NumberOfTracks != 11 {
		t.Errorf("unexpected albums: %+v", albums)
	}
}

func TestAddFavoriteAlbum_SendsAlbumID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil { //nolint:gosec // G120: in-process test server, trusted small request bodies
			t.Fatal(err)
		}
		if r.PostForm.Get("albumId") != "5" {
			t.Errorf("expected albumId=5, got %q", r.PostForm.Get("albumId"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if err := newTestClient(srv).AddFavoriteAlbum(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
}

// --- Playlists ---

func TestGetUserPlaylists_OK(t *testing.T) {
	payload := map[string]any{"items": []map[string]any{
		{"uuid": "p1", "title": "Late Night", "numberOfTracks": 23},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, payload)
	}))
	defer srv.Close()

	pls, err := newTestClient(srv).GetUserPlaylists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pls) != 1 || pls[0].UUID != "p1" || pls[0].NumberOfTracks != 23 {
		t.Errorf("unexpected playlists: %+v", pls)
	}
}

func TestCreatePlaylist_ReturnsUUID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil { //nolint:gosec // G120: in-process test server, trusted small request bodies
			t.Fatal(err)
		}
		if r.PostForm.Get("title") != "My Mix" {
			t.Errorf("expected title=My Mix, got %q", r.PostForm.Get("title"))
		}
		respond(w, http.StatusCreated, map[string]string{"uuid": "new-uuid"})
	}))
	defer srv.Close()

	uuid, err := newTestClient(srv).CreatePlaylist(context.Background(), "My Mix", "")
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "new-uuid" {
		t.Errorf("expected uuid=new-uuid, got %q", uuid)
	}
}

// TestAddTracksToPlaylist_ETagFlow asserts the method first GETs the playlist
// to read its ETag, then POSTs the track IDs with an If-None-Match header.
func TestAddTracksToPlaylist_ETagFlow(t *testing.T) {
	var gotGet, gotPost bool
	var sentIfNoneMatch, sentTrackIDs string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gotGet = true
			w.Header().Set("ETag", `"etag-123"`)
			respond(w, 200, map[string]any{"uuid": "p1"})
		case http.MethodPost:
			gotPost = true
			sentIfNoneMatch = r.Header.Get("If-None-Match")
			if err := r.ParseForm(); err != nil { //nolint:gosec // G120: in-process test server, trusted small request bodies
				t.Fatal(err)
			}
			sentTrackIDs = r.PostForm.Get("trackIds")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	err := newTestClient(srv).AddTracksToPlaylist(context.Background(), "p1", []int{10, 20, 30})
	if err != nil {
		t.Fatal(err)
	}
	if !gotGet || !gotPost {
		t.Fatalf("expected GET then POST (get=%v post=%v)", gotGet, gotPost)
	}
	if sentIfNoneMatch != `"etag-123"` {
		t.Errorf("expected If-None-Match etag, got %q", sentIfNoneMatch)
	}
	if sentTrackIDs != "10,20,30" {
		t.Errorf("expected trackIds=10,20,30, got %q", sentTrackIDs)
	}
}

// Sanity: SearchAll surfaces a non-200 as an error.
func TestSearchAll_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 500, map[string]string{"userMessage": "boom"})
	}))
	defer srv.Close()
	if _, err := newTestClient(srv).SearchAll(context.Background(), "x"); err == nil {
		t.Fatal("expected error on 500")
	}
}
