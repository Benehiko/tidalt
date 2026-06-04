package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/Benehiko/tidalt/v3/internal/store"
	"github.com/Benehiko/tidalt/v3/internal/tidal"
)

const srcRadio = "radio"

// newSmokeModel builds a Model without the store/client/player dependencies so
// the render path can be exercised in isolation.
func newSmokeModel() Model {
	pal := paletteTidalt
	ti := textinput.New()
	tracks := []tidal.Track{
		{ID: 1, Title: "May These Noises", Artist: tidal.Artist{ID: 9, Name: "Pierce The Veil"}, Duration: 78},
		{ID: 2, Title: "Hell Above", Artist: tidal.Artist{ID: 9, Name: "Pierce The Veil"}, Duration: 212},
		{ID: 3, Title: "King For A Day", Artist: tidal.Artist{ID: 9, Name: "Pierce The Veil"}, Duration: 230},
	}
	return Model{
		store:       &store.SecretsStore{}, // nil db => Save* methods no-op
		searchInput: ti,
		section:     SecQueue,
		focusMain:   true,
		volume:      80,
		themeName:   "tidalt",
		palette:     pal,
		theme:       pal.Theme(),
		progress:    progressWithTheme(pal.Theme(), 40),
		favorites:   map[int]bool{2: true},
		tracks:      tracks,
		tracksOrder: tracks,
		mixes: []tidal.Mix{
			{ID: "m1", Title: "Daily Mix 1", SubTitle: "Pierce The Veil, …"},
		},
		barHeights: [9]int{10, 20, 15, 25, 12, 30, 8, 22, 18},
	}
}

// TestViewRendersAllSectionsAndSizes asserts View() never panics across every
// section, overlay, and a range of terminal sizes (including degenerate ones).
func TestViewRendersAllSectionsAndSizes(t *testing.T) {
	sections := []Section{
		SecNowPlaying, SecQueue, SecPlaylists, SecFavSongs, SecFavArtists,
		SecFavAlbums, SecHistory, SecMixes, SecSearch, SecSettings,
	}
	overlays := []Overlay{OverlayNone, OverlayDeviceSelect, OverlayCommandPalette, OverlayActionSheet}
	sizes := [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 12}, {30, 10}, {20, 6}, {1, 1}, {0, 0}}

	for _, sec := range sections {
		for _, ov := range overlays {
			for _, sz := range sizes {
				m := newSmokeModel()
				m.section = sec
				m.overlay = ov
				m.width, m.height = sz[0], sz[1]
				if ov == OverlayActionSheet && len(m.tracks) > 0 {
					tr := m.tracks[0]
					m.sheetTrack = &tr
				}
				if ov == OverlayCommandPalette {
					m.openCommandPalette()
				}
				// Should not panic.
				out := m.View()
				_ = out
			}
		}
	}
}

// TestViewSidebarFocus exercises both focus states and the artist drill-down.
func TestViewSidebarFocus(t *testing.T) {
	m := newSmokeModel()
	m.width, m.height = 100, 30

	m.focusMain = false
	_ = m.View()

	m.focusMain = true
	m.showArtist = true
	m.artistName = "Pierce The Veil"
	m.artistAlbums = []tidal.Album{{ID: 1, Title: "Collide With The Sky", ReleaseDate: "2012-07-17", NumberOfTracks: 13}}
	if !strings.Contains(stripANSI(m.View()), "Collide With The Sky") {
		t.Errorf("artist pane should list the album title")
	}
}

// TestActionSheetRenders confirms the action sheet lists its actions over the
// queue without panic, at several cursor positions.
func TestActionSheetRenders(t *testing.T) {
	for _, cur := range []int{0, 4, 8} {
		m := newSmokeModel()
		m.width, m.height = 96, 26
		tr := m.tracks[1]
		m.sheetTrack = &tr
		m.sheetCursor = cur
		m.overlay = OverlayActionSheet
		out := stripANSI(m.View())
		for _, want := range []string{"Play now", "Add to queue", "Start radio", "Copy Tidal link"} {
			if !strings.Contains(out, want) {
				t.Errorf("cursor %d: action sheet missing %q", cur, want)
			}
		}
	}
}

// TestCommandPaletteFilterAndJump verifies fuzzy filtering and a "jump to"
// command switching sections.
func TestCommandPaletteFilterAndJump(t *testing.T) {
	m := newSmokeModel()
	m.width, m.height = 96, 26
	m.openCommandPalette()
	m.paletteInput.SetValue("mixes")
	items := filterPaletteItems(m.paletteInput.Value())
	if len(items) == 0 {
		t.Fatalf("expected at least one match for %q", "mixes")
	}
	out := stripANSI(m.renderCommandPalette(m.theme))
	if !strings.Contains(out, "Daily Mixes") {
		t.Errorf("palette should show the Daily Mixes jump entry, got:\n%s", out)
	}

	// Selecting the first match should switch sections and close the overlay.
	res, _ := items[0].run(m)
	nm, ok := res.(Model)
	if !ok {
		t.Fatalf("palette run should return a Model, got %T", res)
	}
	if nm.overlay != OverlayNone {
		t.Errorf("running a palette item should close the overlay")
	}
	if nm.section != SecMixes {
		t.Errorf("jump-to-mixes should select SecMixes, got %v", nm.section)
	}
}

// TestLibrarySectionsRender populates the favorites/playlists/history sections
// and renders each, asserting their content appears.
func TestLibrarySectionsRender(t *testing.T) {
	m := newSmokeModel()
	m.width, m.height = 104, 28
	m.playlists = []tidal.Playlist{{UUID: "p1", Title: "Late Night Drive", NumberOfTracks: 23}}
	m.openPlaylist = &m.playlists[0]
	m.playlistName = "Late Night Drive"
	m.detailTracks = m.tracks
	m.favArtists = []tidal.Artist{{ID: 9, Name: "Pierce The Veil"}}
	m.favAlbums = []tidal.Album{{ID: 5, Title: "Collide With The Sky", ReleaseDate: "2012-01-01", NumberOfTracks: 13}}
	m.history = m.tracks

	cases := []struct {
		sec  Section
		want string
	}{
		{SecPlaylists, "Late Night Drive"},
		{SecFavArtists, "Pierce The Veil"},
		{SecFavAlbums, "Collide With The Sky"},
		{SecHistory, "May These Noises"},
	}
	for _, c := range cases {
		m.section = c.sec
		m.sidebarCursor = navIndexOf(c.sec)
		out := stripANSI(m.View())
		if !strings.Contains(out, c.want) {
			t.Errorf("section %v should contain %q", c.sec, c.want)
		}
	}
}

// TestQueueHybridStates checks the queue header reflects synced/edited/unsaved
// origins and that an enqueue marks the queue dirty.
func TestQueueHybridStates(t *testing.T) {
	m := newSmokeModel()
	th := m.theme

	m.queueSource = "playlist:Late Night"
	m.queuePlaylistUUID = "p1"
	m.queueDirty = false
	if got := stripANSI(m.queueHeader(th)); !strings.Contains(got, "synced") {
		t.Errorf("synced header: %q", got)
	}

	m.enqueueEnd(m.tracks[0])
	if !m.queueDirty {
		t.Errorf("enqueue should mark the queue dirty")
	}
	if got := stripANSI(m.queueHeader(th)); !strings.Contains(got, "edited") {
		t.Errorf("edited header: %q", got)
	}

	m.queueSource = srcRadio
	m.queuePlaylistUUID = ""
	m.queueDirty = false
	if got := stripANSI(m.queueHeader(th)); !strings.Contains(got, "unsaved") {
		t.Errorf("radio header: %q", got)
	}
}

// TestQueueSavedMsg confirms a save confirmation flips origin to a synced
// playlist and raises the toast.
func TestQueueSavedMsg(t *testing.T) {
	m := newSmokeModel()
	m.queueSource = srcRadio
	updated, _ := m.Update(queueSavedMsg{uuid: "new", name: "My Mix", count: 3})
	nm, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update should return a Model")
	}
	if nm.queuePlaylistUUID != "new" || nm.queueDirty || nm.queueSource != "playlist:My Mix" {
		t.Errorf("unexpected post-save state: src=%q uuid=%q dirty=%v", nm.queueSource, nm.queuePlaylistUUID, nm.queueDirty)
	}
	if !strings.Contains(nm.toast, "My Mix") {
		t.Errorf("expected save toast, got %q", nm.toast)
	}
}

// TestClientTintRenders confirms client mode renders without panic and the
// theme tint applies.
func TestClientTintRenders(t *testing.T) {
	m := newSmokeModel()
	m.clientMode = true
	m.width, m.height = 90, 28
	out := m.View()
	if !strings.Contains(stripANSI(out), "CLIENT") {
		t.Errorf("client mode should show the CLIENT badge")
	}
}
