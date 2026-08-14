package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// artistViewActive reports whether the transient artist drill-down is showing.
func (m *Model) artistViewActive() bool { return m.showArtist }

// renderQueuePane renders the track list for the Queue / Favorites-Songs
// sections inside a titled panel. The Queue is split into a track list on the
// left and a cover panel on the right showing the hovered (cursor) track's art.
func (m *Model) renderQueuePane(t Theme, w, h int) string {
	coverW, showCover := m.queueCoverWidth(w)
	listW := w
	if showCover {
		listW = w - coverW
	}

	innerW := max(listW-2, 1)
	rows := make([]string, 0, len(m.tracks))
	for i := range m.tracks {
		tr := m.tracks[i]
		rows = append(rows, renderTrackRow(t, tr, rowOpts{
			selected:   m.focusMain && i == m.cursor,
			playing:    m.currentTrack != nil && m.currentTrack.ID == tr.ID && m.isPlaying,
			fav:        m.favorites[tr.ID],
			showIndex:  true,
			showArtist: true,
			index:      i + 1,
			width:      innerW,
			duration:   tr.Duration,
		}))
	}
	if len(rows) == 0 {
		rows = append(rows, t.RowDim.Render("Queue is empty. Search or open a mix to add tracks."))
	}
	listPanel := renderListPanel(t, m.queueHeader(t), m.focusMain, rows, m.cursor, listW, h)
	if !showCover {
		return listPanel
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, listPanel, m.renderQueueCover(t, coverW, h))
}

// minCoverPaneW and minCoverBodyH are the smallest pane width and body height
// that can hold a legible cover panel. Below either, the cover is hidden
// entirely rather than squashed into a sliver that overlaps the surrounding UI.
const (
	minCoverPaneW = 70
	minCoverBodyH = 12
)

// queueCoverWidth returns the width of the Queue's right-hand cover panel and
// whether there is room to show it (hidden on small terminals).
func (m *Model) queueCoverWidth(paneW int) (int, bool) {
	// Need a usable list plus a square-ish cover; require a comfortably wide
	// pane and enough rows that the image box is not reduced to its floor.
	if paneW < minCoverPaneW || m.bodyHeight() < minCoverBodyH {
		return 0, false
	}
	w := min(max(paneW/3, 26), 44)
	return w, true
}

// renderQueueCover renders the cover panel for the track under the cursor. The
// crisp Kitty image (when supported) is written to the TTY separately; this
// draws the box
// (block art / placeholder) and the track metadata.
func (m *Model) renderQueueCover(t Theme, w, h int) string {
	tr := m.hoveredTrack()
	if tr == nil {
		return renderPanel(t, "", false, w, h, t.RowDim.Render("No track selected."))
	}
	panelW, imgRows := m.queueCoverDims(w, h)

	var b strings.Builder
	if m.useKittyCover() {
		for range imgRows {
			b.WriteString(strings.Repeat(" ", panelW))
			b.WriteByte('\n')
		}
	} else {
		cover := coverPanelLines(m.coverImage, "", "", "", panelW, imgRows)
		for _, ln := range cover {
			b.WriteString(ln)
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n")
	b.WriteString(t.Row.Render(truncateStr(tr.Title, panelW)) + "\n")
	b.WriteString(t.RowDim.Render(truncateStr(tr.Artist.Name, panelW)) + "\n")
	b.WriteString(t.RowFaint.Render(truncateStr(tr.Album.Title, panelW)))

	return renderPanel(t, "", false, w, h, b.String())
}

// queueCoverDims returns the cover image's cell width and row count inside the
// Queue's cover panel of outer size w×h.
func (m *Model) queueCoverDims(w, h int) (panelW, imgRows int) {
	panelW = max(w-2, 1)
	innerH := max(h-2, 2)
	imgRows = min(max(innerH-4, 2), innerH)
	return panelW, imgRows
}

// hoveredTrack is the track the Queue cover should show: the one under the
// cursor, falling back to the currently-playing track.
func (m *Model) hoveredTrack() *tidal.Track {
	if m.cursor >= 0 && m.cursor < len(m.tracks) {
		return &m.tracks[m.cursor]
	}
	return m.currentTrack
}

// coverTrack is the track whose cover should currently be displayed: the
// hovered Queue row, otherwise the playing track.
func (m *Model) coverTrack() *tidal.Track {
	if m.section == SecQueue {
		return m.hoveredTrack()
	}
	return m.currentTrack
}

// syncQueueCover fetches the cover for the track under the Queue cursor when it
// differs from the one displayed. A no-op off the Queue or when the cover is
// unchanged (maybeUpdateCover dedupes by UUID).
func (m *Model) syncQueueCover() tea.Cmd {
	if m.section != SecQueue {
		return nil
	}
	return m.maybeUpdateCover(m.coverTrack())
}

// renderMixesPane renders the Daily Mixes list.
func (m *Model) renderMixesPane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	rows := make([]string, 0, len(m.mixes))
	for i, mix := range m.mixes {
		cur := "  "
		nameStyle := t.Row
		if m.focusMain && i == m.cursor {
			cur = t.RowPlaying.Render("› ")
			nameStyle = t.RowPlaying
		}
		line := cur + nameStyle.Render(mix.Title)
		if mix.SubTitle != "" {
			line += t.RowDim.Render(" · " + mix.SubTitle)
		}
		line = truncateStr(line, innerW)
		if m.focusMain && i == m.cursor {
			rows = append(rows, t.RowSel.Width(innerW).Render(stripANSI(line)))
		} else {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		rows = append(rows, t.RowDim.Render("No mixes loaded yet."))
	}
	return renderListPanel(t, "DAILY MIXES", m.focusMain, rows, m.cursor, w, h)
}

// renderArtistPane renders the transient artist drill-down: two synthetic
// quick-play rows followed by the artist's albums. When an album is open it
// shows that album's track list instead.
func (m *Model) renderArtistPane(t Theme, w, h int) string {
	if m.artistAlbum != nil {
		return m.renderArtistAlbumPane(t, w, h)
	}
	innerW := max(w-2, 1)
	var rows []string
	if m.artistLoading {
		rows = append(rows, t.RowDim.Render("Loading album…"))
	} else {
		total := len(m.artistAlbums) + 2
		for i := range total {
			var label string
			switch i {
			case 0:
				label = "▶ Play all tracks"
			case 1:
				label = "★ Top tracks"
			default:
				a := m.artistAlbums[i-2]
				year := ""
				if len(a.ReleaseDate) >= 4 {
					year = " (" + a.ReleaseDate[:4] + ")"
				}
				label = fmt.Sprintf("%s%s — %d tracks", a.Title, year, a.NumberOfTracks)
			}
			cur := "  "
			style := t.Row
			if i == m.artistCursor {
				cur = t.RowPlaying.Render("› ")
				style = t.RowPlaying
			}
			line := truncateStr(cur+style.Render(label), innerW)
			if i == m.artistCursor {
				rows = append(rows, t.RowSel.Width(innerW).Render(stripANSI(line)))
			} else {
				rows = append(rows, line)
			}
		}
	}
	title := "ARTIST · " + strings.ToUpper(m.artistName)
	return renderListPanel(t, title, true, rows, m.artistCursor, w, h)
}

// renderArtistAlbumPane lists the tracks of an album opened inside the artist
// drill-down.
func (m *Model) renderArtistAlbumPane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	rows := make([]string, 0, len(m.artistAlbumTracks))
	for i := range m.artistAlbumTracks {
		tr := m.artistAlbumTracks[i]
		rows = append(rows, renderTrackRow(t, tr, rowOpts{
			selected:  i == m.artistAlbumCursor,
			playing:   m.currentTrack != nil && m.currentTrack.ID == tr.ID && m.isPlaying,
			fav:       m.favorites[tr.ID],
			showIndex: true,
			index:     i + 1,
			width:     innerW,
			duration:  tr.Duration,
		}))
	}
	if len(rows) == 0 {
		rows = append(rows, t.RowDim.Render("No tracks."))
	}
	title := "ALBUM · " + strings.ToUpper(m.artistAlbum.Title)
	return renderListPanel(t, title, true, rows, m.artistAlbumCursor, w, h)
}

// useKittyCover reports whether the Now-Playing cover should be drawn with the
// Kitty graphics protocol (written straight to the TTY) instead of block art. Kitty is
// only safe when no overlay is covering the pane, since the popup would not
// hide a terminal-drawn image.
func (m *Model) useKittyCover() bool {
	return m.kittySupported && m.coverImage != nil && m.overlay == OverlayNone
}
