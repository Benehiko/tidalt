package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Benehiko/tidalt/v3/internal/tidal"
)

// recordHistory prepends a track to the Recently Played list (de-duplicated,
// most-recent first, capped).
func (m *Model) recordHistory(t tidal.Track) {
	const maxHistory = 100
	out := make([]tidal.Track, 0, len(m.history)+1)
	out = append(out, t)
	for i := range m.history {
		if m.history[i].ID == t.ID {
			continue
		}
		out = append(out, m.history[i])
		if len(out) >= maxHistory {
			break
		}
	}
	m.history = out
	_ = m.store.SaveHistory(m.history)
}

// --- Playlists (index + detail) ---

func (m Model) updatePlaylists(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.detailFocus {
		return m.updatePlaylistDetail(k)
	}
	switch k.String() {
	case "h", keyLeft:
		m.focusMain = false
		return m, nil
	case keyUp, "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case keyDown, "j":
		if m.cursor < len(m.playlists)-1 {
			m.cursor++
		}
	case keyEnter, "l", keyRight:
		if m.cursor >= 0 && m.cursor < len(m.playlists) {
			pl := m.playlists[m.cursor]
			m.openPlaylist = &pl
			cmd := m.loadPlaylistDetail(pl)
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) updatePlaylistDetail(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "h", keyLeft:
		m.detailFocus = false
		return m, nil
	case keyUp, "k":
		if m.detailCursor > 0 {
			m.detailCursor--
		}
		return m, nil
	case keyDown, "j":
		if m.detailCursor < len(m.detailTracks)-1 {
			m.detailCursor++
		}
		return m, nil
	case keyEnter:
		// Load the whole playlist into the queue, tracking its origin, and play
		// from the selected track.
		return m.playPlaylistFrom(m.detailCursor)
	}
	return m.commonKeys(k)
}

func (m *Model) loadPlaylistDetail(pl tidal.Playlist) tea.Cmd {
	uuid := pl.UUID
	title := pl.Title
	return func() tea.Msg {
		tracks, err := m.client.GetPlaylistTracks(m.ctx, uuid)
		if err != nil {
			return errMsg(err)
		}
		return playlistDetailMsg{uuid: uuid, title: title, tracks: tracks}
	}
}

// playPlaylistFrom loads the open playlist's tracks into the queue (tracking
// its origin for the hybrid model) and plays from index i.
func (m Model) playPlaylistFrom(i int) (tea.Model, tea.Cmd) {
	if len(m.detailTracks) == 0 || m.openPlaylist == nil {
		return m, nil
	}
	m.loadQueueFromPlaylist(m.detailTracks, *m.openPlaylist)
	if i < 0 || i >= len(m.tracks) {
		i = 0
	}
	m.cursor = i
	m.section = SecQueue
	m.sidebarCursor = navIndexOf(SecQueue)
	track := m.tracks[i]
	_ = m.store.CacheTrack(track.ID, track)
	cmd := m.playTrackCmd(track)
	return m, cmd
}

// --- Favorite artists ---

func (m Model) updateFavArtists(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "h", keyLeft:
		m.focusMain = false
	case keyUp, "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case keyDown, "j":
		if m.cursor < len(m.favArtists)-1 {
			m.cursor++
		}
	case keyEnter:
		if m.cursor >= 0 && m.cursor < len(m.favArtists) {
			a := m.favArtists[m.cursor]
			return m.openArtistByID(a.ID, a.Name)
		}
	}
	return m, nil
}

// openArtistByID opens the artist drill-down for an explicit artist.
func (m Model) openArtistByID(id int, name string) (tea.Model, tea.Cmd) {
	if id == 0 {
		return m, nil
	}
	m.prevSection = m.section
	m.artistLoading = true
	m.showArtist = true
	return m, func() tea.Msg {
		albums, err := m.client.GetArtistAlbums(m.ctx, id)
		if err != nil {
			return errMsg(err)
		}
		return artistAlbumsMsg{artistID: id, artistName: name, albums: albums}
	}
}

// --- Favorite albums ---

func (m Model) updateFavAlbums(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "h", keyLeft:
		m.focusMain = false
	case keyUp, "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case keyDown, "j":
		if m.cursor < len(m.favAlbums)-1 {
			m.cursor++
		}
	case keyEnter:
		if m.cursor >= 0 && m.cursor < len(m.favAlbums) {
			cmd := m.openAlbum(m.favAlbums[m.cursor].ID)
			return m, cmd
		}
	}
	return m, nil
}

// --- History ---

func (m Model) updateHistory(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "h", keyLeft:
		m.focusMain = false
		return m, nil
	case keyUp, "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case keyDown, "j":
		if m.cursor < len(m.history)-1 {
			m.cursor++
		}
		return m, nil
	case keyEnter:
		if m.cursor >= 0 && m.cursor < len(m.history) {
			track := m.history[m.cursor]
			_ = m.store.CacheTrack(track.ID, track)
			cmd := m.playTrackCmd(track)
			return m, cmd
		}
		return m, nil
	}
	return m.commonKeys(k)
}

// --- Render panes ---

// renderPlaylistsPane draws the playlist index (left) and the open playlist's
// detail (right) side by side.
func (m *Model) renderPlaylistsPane(t Theme, w, h int) string {
	idxW := min(max(w/3, 22), 30)
	detailW := w - idxW

	idxRows := make([]string, 0, len(m.playlists))
	for i := range m.playlists {
		pl := m.playlists[i]
		sub := fmt.Sprintf("%d tracks", pl.NumberOfTracks)
		cur := "  "
		nameStyle := t.Row
		if !m.detailFocus && i == m.cursor {
			cur = t.RowPlaying.Render("› ")
			nameStyle = t.RowPlaying
		}
		name := truncateStr(cur+nameStyle.Render(pl.Title), idxW-2)
		if !m.detailFocus && i == m.cursor {
			idxRows = append(idxRows, t.RowSel.Width(idxW-2).Render(stripANSI(name)))
		} else {
			idxRows = append(idxRows, name)
		}
		idxRows = append(idxRows, t.RowFaint.Render("    "+sub))
	}
	if len(idxRows) == 0 {
		idxRows = append(idxRows, t.RowDim.Render("No playlists."))
	}
	indexPanel := renderListPanel(t, "PLAYLISTS", !m.detailFocus, idxRows, m.cursor*2, idxW, h)

	var detailRows []string
	for i := range m.detailTracks {
		tr := m.detailTracks[i]
		detailRows = append(detailRows, renderTrackRow(t, tr, rowOpts{
			selected:  m.detailFocus && i == m.detailCursor,
			playing:   m.currentTrack != nil && m.currentTrack.ID == tr.ID && m.isPlaying,
			fav:       m.favorites[tr.ID],
			showIndex: true,
			index:     i + 1,
			width:     detailW - 2,
			duration:  tr.Duration,
		}))
	}
	title := "SELECT A PLAYLIST"
	if m.openPlaylist != nil {
		title = strings.ToUpper(m.playlistName)
	}
	if len(detailRows) == 0 {
		detailRows = append(detailRows, t.RowDim.Render("Press → to open a playlist."))
	}
	detailPanel := renderListPanel(t, title, m.detailFocus, detailRows, m.detailCursor, detailW, h)

	return lipgloss.JoinHorizontal(lipgloss.Top, indexPanel, detailPanel)
}

// renderFavArtistsPane lists favorited artists.
func (m *Model) renderFavArtistsPane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	rows := make([]string, 0, len(m.favArtists))
	for i := range m.favArtists {
		a := m.favArtists[i]
		cur := "  "
		style := t.Row
		if m.focusMain && i == m.cursor {
			cur = t.RowPlaying.Render("› ")
			style = t.RowPlaying
		}
		line := truncateStr(cur+t.RowDim.Render("◎ ")+style.Render(a.Name), innerW)
		if m.focusMain && i == m.cursor {
			rows = append(rows, t.RowSel.Width(innerW).Render(stripANSI(line)))
		} else {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		rows = append(rows, t.RowDim.Render("No favorite artists."))
	}
	return renderListPanel(t, "ARTISTS", m.focusMain, rows, m.cursor, w, h)
}

// renderFavAlbumsPane lists favorited albums.
func (m *Model) renderFavAlbumsPane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	rows := make([]string, 0, len(m.favAlbums))
	for i := range m.favAlbums {
		a := m.favAlbums[i]
		year := ""
		if len(a.ReleaseDate) >= 4 {
			year = " (" + a.ReleaseDate[:4] + ")"
		}
		cur := "  "
		style := t.Row
		if m.focusMain && i == m.cursor {
			cur = t.RowPlaying.Render("› ")
			style = t.RowPlaying
		}
		meta := t.RowFaint.Render(year + " · " + strconv.Itoa(a.NumberOfTracks) + " tracks")
		line := truncateStr(cur+t.RowDim.Render("⊞ ")+style.Render(a.Title)+meta, innerW)
		if m.focusMain && i == m.cursor {
			rows = append(rows, t.RowSel.Width(innerW).Render(stripANSI(line)))
		} else {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		rows = append(rows, t.RowDim.Render("No favorite albums."))
	}
	return renderListPanel(t, "ALBUMS", m.focusMain, rows, m.cursor, w, h)
}

// renderHistoryPane lists recently played tracks.
func (m *Model) renderHistoryPane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	rows := make([]string, 0, len(m.history))
	for i := range m.history {
		tr := m.history[i]
		rows = append(rows, renderTrackRow(t, tr, rowOpts{
			selected:   m.focusMain && i == m.cursor,
			playing:    m.currentTrack != nil && m.currentTrack.ID == tr.ID && m.isPlaying,
			fav:        m.favorites[tr.ID],
			showArtist: true,
			width:      innerW,
			duration:   tr.Duration,
		}))
	}
	if len(rows) == 0 {
		rows = append(rows, t.RowDim.Render("Nothing played yet this session."))
	}
	return renderListPanel(t, "RECENTLY PLAYED", m.focusMain, rows, m.cursor, w, h)
}
