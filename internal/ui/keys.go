package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Benehiko/tidalt/v3/internal/player"
	"github.com/Benehiko/tidalt/v3/internal/tidal"
)

// Frequently-compared key strings, hoisted to constants (goconst).
const (
	keyEsc   = "esc"
	keyUp    = "up"
	keyDown  = "down"
	keyEnter = "enter"
	keyLeft  = "left"
	keyRight = "right"
)

// handleKey is the top-level key dispatcher. Order of precedence:
//  1. global keys (quit, command palette, theme cycle, device overlay)
//  2. an active overlay (command palette / action sheet / device select)
//  3. a focused search input
//  4. sidebar navigation (when the sidebar holds focus)
//  5. the active section's main-pane handler
func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if cmd, done := m.handleGlobalKey(&m, k); done {
		return m, cmd
	}

	if m.overlay != OverlayNone {
		return m.updateOverlay(k)
	}

	// A focused search input consumes typing; global controls already handled.
	if m.searchInput.Focused() {
		switch k.String() {
		case keyEsc:
			m.searchInput.Blur()
			return m, nil
		case keyEnter, keyUp, keyDown:
			// fall through to section handler (search nav / submit)
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(k)
			return m, cmd
		}
	}

	if !m.focusMain {
		return m.updateSidebar(k)
	}
	return m.updateSection(k)
}

// handleGlobalKey handles keys that work regardless of section/overlay. It
// returns (cmd, true) when it consumed the key. It mutates through the pointer.
func (m *Model) handleGlobalKey(_ *Model, k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "ctrl+c":
		return m.quit(), true
	case "q":
		// In a focused text input, "q" is literal text — don't quit.
		if m.searchInput.Focused() || m.overlay == OverlayCommandPalette {
			return nil, false
		}
		return m.quit(), true

	case "ctrl+p", ":":
		if m.overlay == OverlayCommandPalette {
			return nil, true
		}
		if m.searchInput.Focused() {
			return nil, false
		}
		m.openCommandPalette()
		return nil, true

	case "t":
		if m.searchInput.Focused() || m.section == SecSettings {
			return nil, false // Settings owns "t"; input treats it as text
		}
		m.cycleTheme()
		return nil, true

	case "d":
		if m.searchInput.Focused() || m.overlay != OverlayNone || m.section == SecSearch {
			return nil, false
		}
		m.openDeviceSelect()
		return nil, true
	}
	return nil, false
}

func (m *Model) quit() tea.Cmd {
	if m.currentTrack != nil {
		_ = m.store.SaveLastPosition(m.currPos)
	}
	if m.player != nil {
		m.player.Close()
	}
	m.store.Close()
	return tea.Quit
}

// openDeviceSelect populates the device list and raises the device overlay.
func (m *Model) openDeviceSelect() {
	devs, err := player.ListDevices()
	if err != nil {
		m.errText = err.Error()
		return
	}
	m.devices = devs
	m.overlay = OverlayDeviceSelect
	m.cursor = 0
	for i, d := range devs {
		if d.HWName == m.currentDevice {
			m.cursor = i
			break
		}
	}
}

// cycleTheme advances to the next palette in paletteOrder and commits it.
func (m *Model) cycleTheme() {
	i := 0
	for j, name := range paletteOrder {
		if name == m.themeName {
			i = j
			break
		}
	}
	next := paletteOrder[(i+1)%len(paletteOrder)]
	m.applyTheme(next)
}

// applyTheme commits a palette by name and persists it.
func (m *Model) applyTheme(name string) {
	m.themeName = name
	m.palette = resolvePalette(name)
	m.theme = m.palette.Theme()
	m.previewPalette = nil
	m.rebuildProgress()
	_ = m.store.SaveTheme(name)
}

// rebuildProgress rebuilds the progress bar with the active theme's gradient at
// the current width.
func (m *Model) rebuildProgress() {
	t := m.activeTheme()
	barWidth := max(m.width-22, 10)
	m.progress = progressWithTheme(t, barWidth)
}

// updateSidebar handles navigation while the sidebar holds focus.
func (m Model) updateSidebar(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case keyUp, "k":
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
		}
	case keyDown, "j":
		if m.sidebarCursor < len(navSections)-1 {
			m.sidebarCursor++
		}
	case keyEnter, "l", keyRight, " ":
		return m.selectSection(navSections[m.sidebarCursor])
	case "/":
		return m.selectSection(SecSearch)
	}
	return m, nil
}

// selectSection switches to a section, moves focus to the main pane, and fires
// any data-load command the section needs.
func (m Model) selectSection(sec Section) (tea.Model, tea.Cmd) {
	m.section = sec
	m.showArtist = false
	m.focusMain = true
	m.sidebarCursor = navIndexOf(sec)
	m.cursor = 0
	if sec == SecPlaylists {
		m.detailFocus = false
	}
	if sec == SecSettings {
		m.enterSettings()
	}

	switch sec {
	case SecSearch:
		m.searchInput.Focus()
	default:
		m.searchInput.Blur()
	}
	cmd := m.loadSection(sec)
	return m, cmd
}

// loadSection returns the command to (re)load a section's data, or nil when the
// section reuses already-loaded data. Favorites/playlists loaders arrive in
// later steps; for now only the always-available sections are wired.
func (m *Model) loadSection(sec Section) tea.Cmd {
	switch sec {
	case SecMixes:
		if len(m.mixes) > 0 {
			return nil
		}
		return func() tea.Msg {
			mixes, err := m.client.GetMixes(m.ctx)
			if err != nil {
				return errMsg(err)
			}
			return mixesMsg(mixes)
		}
	case SecPlaylists:
		return func() tea.Msg {
			pls, err := m.client.GetUserPlaylists(m.ctx)
			if err != nil {
				return errMsg(err)
			}
			return playlistsMsg(pls)
		}
	case SecFavArtists:
		return func() tea.Msg {
			artists, err := m.client.GetFavoriteArtists(m.ctx, 200)
			if err != nil {
				return errMsg(err)
			}
			return favArtistsMsg(artists)
		}
	case SecFavAlbums:
		return func() tea.Msg {
			albums, err := m.client.GetFavoriteAlbums(m.ctx, 200)
			if err != nil {
				return errMsg(err)
			}
			return favAlbumsMsg(albums)
		}
	default:
		// SecHistory uses the in-memory m.history; others reuse loaded data.
		return nil
	}
}

// updateSection routes keys to the active section's handler.
func (m Model) updateSection(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc backs out of the artist drill-down, else returns focus to the sidebar.
	if k.String() == keyEsc {
		switch {
		case m.showArtist:
			m.showArtist = false
		case m.section == SecPlaylists && m.detailFocus:
			m.detailFocus = false
		default:
			m.focusMain = false
		}
		return m, nil
	}

	if m.showArtist {
		return m.updateArtist(k)
	}

	switch m.section {
	case SecSearch:
		return m.updateSearchKeys(k)
	case SecPlaylists:
		return m.updatePlaylists(k)
	case SecFavArtists:
		return m.updateFavArtists(k)
	case SecFavAlbums:
		return m.updateFavAlbums(k)
	case SecHistory:
		return m.updateHistory(k)
	case SecSettings:
		return m.updateSettings(k)
	default:
		// Queue, Favorites songs, Now Playing, Mixes.
		if k.String() == "h" || (k.String() == keyLeft && m.currentTrack == nil) {
			m.focusMain = false
			return m, nil
		}
		return m.updateListKeys(k)
	}
}

// updateListKeys handles the common track-list sections (Queue, Favorites songs,
// Now Playing): cursor movement, playback, and track actions.
func (m Model) updateListKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case keyUp, "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case keyDown, "j":
		maxIdx := m.currentListLen()
		if m.cursor < maxIdx-1 {
			m.cursor++
		}
		return m, nil
	case keyEnter:
		if m.section == SecMixes && len(m.mixes) > 0 {
			mix := m.mixes[m.cursor]
			return m, func() tea.Msg {
				tracks, err := m.client.GetMixTracks(m.ctx, mix.ID)
				if err != nil {
					return errMsg(err)
				}
				return tracksMsg(tracks)
			}
		}
		if t := m.selectedTrack(); t != nil {
			_ = m.store.CacheTrack(t.ID, *t)
			cmd := m.playTrackCmd(*t)
			return m, cmd
		}
		return m, nil
	}
	return m.commonKeys(k)
}

// commonKeys handles keys shared by every main-pane context (playback transport,
// volume, shuffle, track actions). Returns the model unchanged for unknown keys.
func (m Model) commonKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case " ":
		return m.togglePlay()
	case keyLeft:
		if !m.clientMode && m.player != nil && m.currentTrack != nil {
			if err := m.player.Seek(m.currPos - 10); err != nil {
				m.errText = err.Error()
			}
		}
	case keyRight:
		if !m.clientMode && m.player != nil && m.currentTrack != nil {
			if err := m.player.Seek(m.currPos + 10); err != nil {
				m.errText = err.Error()
			}
		}
	case "9":
		m.setVolume(m.volume - 5)
	case "0":
		m.setVolume(m.volume + 5)
	case "s":
		m.cycleShuffle()
	case "S":
		return m.saveQueueAsNew()
	case ">", ".":
		return m.skipNext()
	case "<", ",":
		return m.skipPrev()
	case "o":
		if t := m.selectedTrack(); t != nil {
			m.openActionSheet(*t)
		}
	case "r":
		if t := m.selectedTrack(); t != nil {
			cmd := m.radioFrom(*t)
			return m, cmd
		}
	case "f":
		if t := m.selectedTrack(); t != nil {
			cmd := m.toggleFavorite(*t)
			return m, cmd
		}
	case "a":
		return m.openArtistFor(m.selectedTrack())
	case "c":
		return m.copyLink()
	}
	return m, nil
}

// --- shared action helpers ---

func (m *Model) setVolume(v float64) {
	if m.clientMode {
		return
	}
	m.volume = max(min(v, 100), 0)
	_ = m.player.SetVolume(m.volume)
	_ = m.store.SaveVolume(m.volume)
}

func (m *Model) cycleShuffle() {
	switch m.shuffleMode {
	case ShuffleOff:
		m.shuffleMode = ShuffleFisherYates
	case ShuffleFisherYates:
		m.shuffleMode = ShuffleRandom
	default:
		m.shuffleMode = ShuffleOff
	}
	m.applyShuffle()
	m.cursor = 0
}

func (m Model) togglePlay() (tea.Model, tea.Cmd) {
	if m.clientMode {
		mc := m.mprisClient
		return m, func() tea.Msg {
			if err := mc.SendPlayPause(); err != nil {
				return errMsg(err)
			}
			return nil
		}
	}
	if m.currentTrack == nil {
		if t := m.selectedTrack(); t != nil {
			_ = m.store.CacheTrack(t.ID, *t)
			cmd := m.playTrackCmd(*t)
			return m, cmd
		}
		return m, nil
	}
	_ = m.player.Pause()
	m.isPlaying = !m.isPlaying
	m.pushState()
	return m, nil
}

func (m Model) skipNext() (tea.Model, tea.Cmd) {
	if len(m.tracks) == 0 {
		return m, nil
	}
	m.shufflePlayed = append(m.shufflePlayed, m.cursor)
	next := m.nextIndex()
	if next < 0 {
		return m, nil
	}
	m.advancing = true
	m.cursor = next
	track := m.tracks[next]
	m.currPos = 0
	m.duration = 0
	_ = m.store.CacheTrack(track.ID, track)
	cmd := m.playNextTrackCmd(track)
	return m, cmd
}

func (m Model) skipPrev() (tea.Model, tea.Cmd) {
	if len(m.tracks) == 0 {
		return m, nil
	}
	prev := m.prevIndex()
	if prev < 0 {
		return m, nil
	}
	m.advancing = false
	m.cursor = prev
	track := m.tracks[prev]
	m.currPos = 0
	m.duration = 0
	_ = m.store.CacheTrack(track.ID, track)
	cmd := m.playTrackCmd(track)
	return m, cmd
}

func (m *Model) radioFrom(t tidal.Track) tea.Cmd {
	id := t.ID
	m.pendingQueueSource = "radio" // tracksMsg marks the resulting queue unsaved
	return func() tea.Msg {
		tracks, err := m.client.GetTrackRadio(m.ctx, id)
		if err != nil {
			return errMsg(err)
		}
		return tracksMsg(tracks)
	}
}

func (m *Model) toggleFavorite(t tidal.Track) tea.Cmd {
	id := t.ID
	isFav := m.favorites[id]
	return func() tea.Msg {
		var err error
		if isFav {
			err = m.client.RemoveFavorite(m.ctx, id)
		} else {
			err = m.client.AddFavorite(m.ctx, id)
		}
		if err != nil {
			return errMsg(err)
		}
		return favoriteMsg{trackID: id, added: !isFav}
	}
}

func (m Model) copyLink() (tea.Model, tea.Cmd) {
	if t := m.selectedTrack(); t != nil {
		return m.copyTrackLink(*t)
	}
	return m, nil
}

// copyTrackLink copies a track's Tidal URL to the clipboard and flashes a
// confirmation in the status line.
func (m Model) copyTrackLink(t tidal.Track) (tea.Model, tea.Cmd) {
	link := fmt.Sprintf("https://tidal.com/track/%d", t.ID)
	if err := clipboard.WriteAll(link); err != nil {
		m.errText = err.Error()
		return m, nil
	}
	m.errText = "Copied link to clipboard"
	return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })
}

// openAlbum loads an album's tracks into the queue.
func (m *Model) openAlbum(albumID int) tea.Cmd {
	if albumID == 0 {
		return nil
	}
	id := strconv.Itoa(albumID)
	return func() tea.Msg {
		tracks, err := m.client.GetAlbumTracks(m.ctx, id)
		if err != nil {
			return errMsg(err)
		}
		return tracksMsg(tracks)
	}
}

// openArtistFor opens the transient artist drill-down for a track's artist.
func (m Model) openArtistFor(t *tidal.Track) (tea.Model, tea.Cmd) {
	if t == nil {
		t = m.currentTrack
	}
	if t == nil || t.Artist.ID == 0 {
		return m, nil
	}
	artistID := t.Artist.ID
	artistName := t.Artist.Name
	m.prevSection = m.section
	m.artistLoading = true
	m.showArtist = true
	return m, func() tea.Msg {
		albums, err := m.client.GetArtistAlbums(m.ctx, artistID)
		if err != nil {
			return errMsg(err)
		}
		return artistAlbumsMsg{artistID: artistID, artistName: artistName, albums: albums}
	}
}

// updateArtist handles the transient artist drill-down sub-view.
func (m Model) updateArtist(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case keyUp, "k":
		if m.artistCursor > 0 {
			m.artistCursor--
		}
		return m, nil
	case keyDown, "j":
		if m.artistCursor < len(m.artistAlbums)+2-1 {
			m.artistCursor++
		}
		return m, nil
	case keyEnter:
		artistID := m.artistID
		switch m.artistCursor {
		case 0: // ▶ Play all tracks
			m.artistLoading = true
			return m, func() tea.Msg {
				tracks, err := m.client.GetArtistAllTracks(m.ctx, artistID)
				if err != nil {
					return errMsg(err)
				}
				return tracksMsg(tracks)
			}
		case 1: // ★ Top tracks
			return m, func() tea.Msg {
				tracks, err := m.client.GetArtistTopTracks(m.ctx, artistID, 100)
				if err != nil {
					return errMsg(err)
				}
				return tracksMsg(tracks)
			}
		default:
			if idx := m.artistCursor - 2; idx >= 0 && idx < len(m.artistAlbums) {
				albumID := strconv.Itoa(m.artistAlbums[idx].ID)
				return m, func() tea.Msg {
					tracks, err := m.client.GetAlbumTracks(m.ctx, albumID)
					if err != nil {
						return errMsg(err)
					}
					return tracksMsg(tracks)
				}
			}
		}
		return m, nil
	}
	return m.commonKeys(k)
}

// --- selection helpers ---

// selectedTrack returns the track under the cursor in the active context, or
// the current track as a fallback. Returns nil when nothing is selectable.
func (m *Model) selectedTrack() *tidal.Track {
	switch {
	case m.section == SecSearch:
		return m.selectedTrackForSearch()
	case m.section == SecHistory && len(m.history) > 0 && m.cursor < len(m.history):
		t := m.history[m.cursor]
		return &t
	case m.section == SecPlaylists && m.detailFocus && len(m.detailTracks) > 0 && m.detailCursor < len(m.detailTracks):
		t := m.detailTracks[m.detailCursor]
		return &t
	case len(m.tracks) > 0 && m.cursor >= 0 && m.cursor < len(m.tracks):
		t := m.tracks[m.cursor]
		return &t
	case m.currentTrack != nil:
		return m.currentTrack
	default:
		return nil
	}
}

// currentListLen returns the length of the list the main cursor indexes for the
// active section.
func (m *Model) currentListLen() int {
	switch m.section {
	case SecMixes:
		return len(m.mixes)
	default:
		return len(m.tracks)
	}
}
