package ui

import (
	"fmt"
	"strings"
)

// artistViewActive reports whether the transient artist drill-down is showing.
func (m *Model) artistViewActive() bool { return m.showArtist }

// renderQueuePane renders the track list for the Queue / Favorites-Songs
// sections inside a titled panel.
func (m *Model) renderQueuePane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
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
	title := sectionTitle(m.section)
	if m.section == SecQueue {
		title = m.queueHeader(t)
	}
	return renderListPanel(t, title, m.focusMain, rows, m.cursor, w, h)
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
// quick-play rows followed by the artist's albums.
func (m *Model) renderArtistPane(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	var rows []string
	if m.artistLoading {
		rows = append(rows, t.RowDim.Render("Loading artist…"))
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

// useKittyCover reports whether the Now-Playing cover should be drawn with the
// Kitty graphics protocol (overlaid in View) instead of block art. Kitty is
// only safe when no overlay is covering the pane, since the popup would not
// hide a terminal-drawn image.
func (m *Model) useKittyCover() bool {
	return m.kittySupported && m.coverImage != nil && m.overlay == OverlayNone
}

// renderNowPlayingPane renders the dedicated Now-Playing section: a large cover
// above the track metadata and progress. The cover is either block art (drawn
// here, in the cell grid) or a reserved blank box that View() overlays with a
// Kitty image at absolute coordinates.
func (m *Model) renderNowPlayingPane(t Theme, w, h int) string {
	if m.currentTrack == nil {
		return renderPanel(t, "NOW PLAYING", m.focusMain, w, h,
			t.RowDim.Render("Nothing playing. Pick a track from the Queue."))
	}
	tr := m.currentTrack
	panelW, imgRows := m.coverPaneDims()

	var b strings.Builder
	if m.useKittyCover() {
		// Reserve a blank box; View() draws the real image on top of it.
		for range imgRows {
			b.WriteString(strings.Repeat(" ", panelW))
			b.WriteByte('\n')
		}
		b.WriteString(t.RowDim.Render(tr.Title) + "\n")
		b.WriteString(t.RowDim.Render(tr.Artist.Name) + "\n")
		b.WriteString(t.RowFaint.Render(tr.Album.Title) + "\n")
	} else {
		cover := coverPanelLines(m.coverImage, tr.Title, tr.Artist.Name, tr.Album.Title, panelW, imgRows+4)
		for _, ln := range cover {
			b.WriteString(ln)
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n")
	percent := 0.0
	if m.duration > 0 {
		percent = m.currPos / m.duration
	}
	b.WriteString(m.progress.ViewAs(percent))
	b.WriteString(t.RowDim.Render(fmt.Sprintf("  %s / %s", formatTime(m.currPos), formatTime(m.duration))))

	return renderPanel(t, "NOW PLAYING", m.focusMain, w, h, b.String())
}
