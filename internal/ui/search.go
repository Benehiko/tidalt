package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// searchRowKind identifies what a flattened search result row points at.
type searchRowKind int

const (
	rowTrack searchRowKind = iota
	rowArtist
	rowAlbum
)

// searchRow is one selectable row in the flattened grouped-results list.
type searchRow struct {
	kind searchRowKind
	idx  int // index into the matching slice in searchResults
}

// searchRows flattens the grouped results into the navigable order shown on
// screen: Songs, then Artists, then Albums. (The "Top result" is the first
// track and is rendered specially but maps to the same row.)
func (m *Model) searchRows() []searchRow {
	rows := make([]searchRow, 0, len(m.searchResults.Tracks)+len(m.searchResults.Artists)+len(m.searchResults.Albums))
	for i := range m.searchResults.Tracks {
		rows = append(rows, searchRow{rowTrack, i})
	}
	for i := range m.searchResults.Artists {
		rows = append(rows, searchRow{rowArtist, i})
	}
	for i := range m.searchResults.Albums {
		rows = append(rows, searchRow{rowAlbum, i})
	}
	return rows
}

// selectedSearchRow returns the flattened row under the cursor, or (zero,false).
func (m *Model) selectedSearchRow() (searchRow, bool) {
	rows := m.searchRows()
	if m.searchCursor < 0 || m.searchCursor >= len(rows) {
		return searchRow{}, false
	}
	return rows[m.searchCursor], true
}

// searchSubmit fires the multi-category search for the current input.
func (m *Model) searchSubmit() tea.Cmd {
	query := strings.TrimSpace(m.searchInput.Value())
	if query == "" {
		return nil
	}
	m.searchLoading = true
	m.searchResults = tidal.SearchResults{}
	m.searchCursor = 0
	return func() tea.Msg {
		res, err := m.client.SearchAll(m.ctx, query)
		if err != nil {
			return errMsg(err)
		}
		return searchGroupedMsg(*res)
	}
}

// updateSearchKeys handles the Search section: the input plus grouped-result
// navigation and per-row actions.
func (m Model) updateSearchKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.searchRows()
	switch k.String() {
	case keyEnter:
		if m.searchInput.Focused() {
			cmd := m.searchSubmit()
			if cmd != nil {
				m.searchInput.Blur()
			}
			return m, cmd
		}
		return m.activateSearchRow()
	case keyUp, "k":
		switch {
		case m.searchInput.Focused():
		case m.searchCursor > 0:
			m.searchCursor--
		default:
			m.searchInput.Focus()
		}
		return m, nil
	case keyDown, "j":
		if m.searchInput.Focused() {
			m.searchInput.Blur()
		} else if m.searchCursor < len(rows)-1 {
			m.searchCursor++
		}
		return m, nil
	case "h":
		if !m.searchInput.Focused() {
			m.focusMain = false
			return m, nil
		}
	}
	if m.searchInput.Focused() {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(k)
		return m, cmd
	}
	// Track-level shortcuts (o/f/r/a/space/…) when a track row is selected.
	if row, ok := m.selectedSearchRow(); ok && row.kind == rowTrack {
		return m.commonKeys(k)
	}
	return m, nil
}

// activateSearchRow performs the default action for the selected result:
// play a track, drill into an artist, or load an album into the queue.
func (m Model) activateSearchRow() (tea.Model, tea.Cmd) {
	row, ok := m.selectedSearchRow()
	if !ok {
		return m, nil
	}
	switch row.kind {
	case rowTrack:
		track := m.searchResults.Tracks[row.idx]
		_ = m.store.CacheTrack(track.ID, track)
		cmd := m.playTrackCmd(track)
		return m, cmd
	case rowArtist:
		a := m.searchResults.Artists[row.idx]
		return m.openArtistByID(a.ID, a.Name)
	case rowAlbum:
		cmd := m.openAlbum(m.searchResults.Albums[row.idx].ID)
		return m, cmd
	}
	return m, nil
}

// selectedTrackForSearch returns the track under the cursor when a track row is
// selected (used by the action sheet / favorite / radio shortcuts).
func (m *Model) selectedTrackForSearch() *tidal.Track {
	if row, ok := m.selectedSearchRow(); ok && row.kind == rowTrack {
		t := m.searchResults.Tracks[row.idx]
		return &t
	}
	return nil
}

// renderSearchPane renders the search input above grouped results
// (Top result / Songs / Artists / Albums).
func (m *Model) renderSearchPane(t Theme, w, h int) string {
	innerW := max(w-4, 1)
	prompt := t.KeyBarKey.Render("/ ")
	input := " " + prompt + m.searchInput.View()

	var rows []string
	addGroup := func(label string) { rows = append(rows, t.CmdGroup.Render(label)) }

	res := m.searchResults
	flat := m.searchRows()
	cursorAt := func(kind searchRowKind, idx int) bool {
		if m.searchInput.Focused() || m.searchCursor < 0 || m.searchCursor >= len(flat) {
			return false
		}
		r := flat[m.searchCursor]
		return r.kind == kind && r.idx == idx
	}

	switch {
	case m.searchLoading:
		rows = append(rows, t.RowDim.Render("Searching…"))
	case len(flat) == 0 && m.searchInput.Value() != "":
		rows = append(rows, t.RowDim.Render("No results."))
	default:
		if len(res.Tracks) > 0 {
			addGroup("SONGS")
			for i := range res.Tracks {
				rows = append(rows, renderTrackRow(t, res.Tracks[i], rowOpts{
					selected:   cursorAt(rowTrack, i),
					fav:        m.favorites[res.Tracks[i].ID],
					showArtist: true,
					width:      innerW,
					duration:   res.Tracks[i].Duration,
				}))
			}
		}
		if len(res.Artists) > 0 {
			addGroup("ARTISTS")
			for i := range res.Artists {
				rows = append(rows, searchSimpleRow(t, "◎", res.Artists[i].Name, "", innerW, cursorAt(rowArtist, i)))
			}
		}
		if len(res.Albums) > 0 {
			addGroup("ALBUMS")
			for i := range res.Albums {
				a := res.Albums[i]
				meta := strconv.Itoa(a.NumberOfTracks) + " tracks"
				if len(a.ReleaseDate) >= 4 {
					meta = a.ReleaseDate[:4] + " · " + meta
				}
				rows = append(rows, searchSimpleRow(t, "⊞", a.Title, meta, innerW, cursorAt(rowAlbum, i)))
			}
		}
	}

	listH := max(h-2, 1)
	panel := renderListPanel(t, "SEARCH", m.focusMain, rows, m.searchCursor, w, listH)
	return joinSearchInput(input, panel)
}

// searchSimpleRow renders a non-track result (artist/album) row.
func searchSimpleRow(t Theme, icon, title, meta string, w int, selected bool) string {
	cur := "  "
	style := t.Row
	if selected {
		cur = t.RowPlaying.Render("› ")
		style = t.RowPlaying
	}
	line := cur + t.RowDim.Render(icon+" ") + style.Render(title)
	if meta != "" {
		line += t.RowFaint.Render("  " + meta)
	}
	line = truncateStr(line, w)
	if selected {
		return t.RowSel.Width(w).Render(stripANSI(line))
	}
	return line
}

// joinSearchInput stacks the input line, a blank, and the results panel.
func joinSearchInput(input, panel string) string {
	return fmt.Sprintf("%s\n\n%s", input, panel)
}
