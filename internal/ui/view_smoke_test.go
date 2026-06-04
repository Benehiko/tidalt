package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/Benehiko/tidalt/v3/internal/tidal"
)

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
