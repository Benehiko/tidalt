package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Benehiko/tidalt/v4/internal/spotify"
	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// importStage tracks where the Spotify-import overlay is in its flow.
type importStage int

const (
	importStageInput   importStage = iota // entering / pasting the Spotify URL
	importStageLoading                    // resolving Spotify + matching on Tidal
	importStageReview                     // showing matched / "not available" rows
	importStageOutput                     // choosing create-playlist vs load-queue
)

// importRow pairs a Spotify source track with its best Tidal match (nil when no
// Tidal track was found — rendered as "not available").
type importRow struct {
	source spotify.SourceTrack
	match  *tidal.Track
}

// spotifyHTTPClient is the HTTP client used for Spotify oEmbed / embed-page
// requests. Separate from the Tidal OAuth client; modest timeout.
func spotifyHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// resolveSpotifyCmd fetches the Spotify source then matches each track on Tidal,
// emitting a single spotifyResolvedMsg with the resulting rows.
func (m *Model) resolveSpotifyCmd(rawURL string) tea.Cmd {
	client := m.client
	ctx := m.ctx
	return func() tea.Msg {
		src, err := spotify.Resolve(ctx, spotifyHTTPClient(), rawURL)
		if err != nil {
			return spotifyResolvedMsg{err: err}
		}
		rows := matchSpotifyTracks(ctx, client, src.Tracks)
		return spotifyResolvedMsg{src: src, rows: rows}
	}
}

// matchSpotifyTracks searches Tidal for each source track and returns a row per
// track, the match being the top search result (or nil). Runs sequentially to
// stay within Tidal's search rate limits.
func matchSpotifyTracks(ctx context.Context, client *tidal.Client, srcs []spotify.SourceTrack) []importRow {
	rows := make([]importRow, 0, len(srcs))
	for _, s := range srcs {
		row := importRow{source: s}
		query := strings.TrimSpace(s.Title + " " + s.Artist())
		if query != "" {
			if tracks, err := client.Search(ctx, query); err == nil && len(tracks) > 0 {
				t := tracks[0]
				row.match = &t
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// matchedIDs returns the Tidal track IDs of the matched rows, in order.
func matchedIDs(rows []importRow) []int {
	var ids []int
	for _, r := range rows {
		if r.match != nil {
			ids = append(ids, r.match.ID)
		}
	}
	return ids
}

// matchedTracks returns the matched Tidal tracks, in order.
func matchedTracks(rows []importRow) []tidal.Track {
	var out []tidal.Track
	for _, r := range rows {
		if r.match != nil {
			out = append(out, *r.match)
		}
	}
	return out
}

// createSpotifyPlaylistCmd creates a Tidal playlist named after the Spotify
// source and adds every matched track.
func (m *Model) createSpotifyPlaylistCmd(name string, ids []int) tea.Cmd {
	client := m.client
	ctx := m.ctx
	return func() tea.Msg {
		uuid, err := client.CreatePlaylist(ctx, name, "Imported from Spotify via tidalt")
		if err != nil {
			return errMsg(err)
		}
		if err := client.AddTracksToPlaylist(ctx, uuid, ids); err != nil {
			return errMsg(err)
		}
		return queueSavedMsg{uuid: uuid, name: name, count: len(ids)}
	}
}

// openImportSpotify raises the Spotify-import overlay at the URL-input stage with
// a fresh, focused input.
func (m *Model) openImportSpotify() {
	ti := textinput.New()
	ti.Placeholder = "Paste a Spotify track or playlist URL…"
	ti.Prompt = ""
	ti.Focus()
	m.importInput = ti
	m.importSource = nil
	m.importRows = nil
	m.importCursor = 0
	m.importError = ""
	m.importStage = importStageInput
	m.overlay = OverlayImportSpotify
}

// closeImportSpotify dismisses the overlay and resets its state.
func (m *Model) closeImportSpotify() {
	m.overlay = OverlayNone
	m.importSource = nil
	m.importRows = nil
	m.importError = ""
	m.importStage = importStageInput
}

// updateImportSpotify drives the import overlay's four stages.
func (m Model) updateImportSpotify(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.importStage {
	case importStageInput:
		return m.updateImportInput(k)
	case importStageLoading:
		if k.String() == keyEsc {
			m.closeImportSpotify()
		}
		return m, nil
	case importStageReview:
		return m.updateImportReview(k)
	case importStageOutput:
		return m.updateImportOutput(k)
	}
	return m, nil
}

// updateImportInput handles the URL-entry stage.
func (m Model) updateImportInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case keyEsc:
		m.closeImportSpotify()
		return m, nil
	case keyEnter:
		raw := strings.TrimSpace(m.importInput.Value())
		if !spotify.IsSpotifyURL(raw) {
			m.importError = "Not a Spotify track or playlist URL."
			return m, nil
		}
		if _, _, ok := spotify.Parse(raw); !ok {
			m.importError = "Unsupported Spotify URL — only track and playlist links work."
			return m, nil
		}
		m.importError = ""
		m.importStage = importStageLoading
		m.importInput.Blur()
		cmd := m.resolveSpotifyCmd(raw)
		return m, cmd
	}
	var cmd tea.Cmd
	m.importInput, cmd = m.importInput.Update(k)
	return m, cmd
}

// updateImportReview handles list navigation and advancing to the output choice.
func (m Model) updateImportReview(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case keyEsc:
		m.closeImportSpotify()
		return m, nil
	case keyUp, "k":
		if m.importCursor > 0 {
			m.importCursor--
		}
		return m, nil
	case keyDown, "j":
		if m.importCursor < len(m.importRows)-1 {
			m.importCursor++
		}
		return m, nil
	case keyEnter:
		if len(matchedIDs(m.importRows)) == 0 {
			m.importError = "No tracks matched on Tidal — nothing to import."
			return m, nil
		}
		m.importError = ""
		m.importCursor = 0
		m.importStage = importStageOutput
		return m, nil
	}
	return m, nil
}

// importOutputOptions are the two destinations offered after matching.
var importOutputOptions = []string{"Create Tidal playlist", "Load into queue"}

// updateImportOutput handles the create-playlist vs load-queue choice.
func (m Model) updateImportOutput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case keyEsc:
		m.importStage = importStageReview
		return m, nil
	case keyUp, "k":
		if m.importCursor > 0 {
			m.importCursor--
		}
		return m, nil
	case keyDown, "j":
		if m.importCursor < len(importOutputOptions)-1 {
			m.importCursor++
		}
		return m, nil
	case keyEnter:
		rows := m.importRows
		name := "Spotify import"
		if m.importSource != nil && m.importSource.Name != "" {
			name = m.importSource.Name
		}
		switch m.importCursor {
		case 0: // Create Tidal playlist
			cmd := m.createSpotifyPlaylistCmd(name, matchedIDs(rows))
			m.closeImportSpotify()
			return m, cmd
		case 1: // Load into queue
			cmd := m.playListIntoQueue(matchedTracks(rows), 0)
			m.closeImportSpotify()
			return m, cmd
		}
	}
	return m, nil
}

// renderImportSpotify renders the import overlay for the current stage.
func (m *Model) renderImportSpotify(t Theme) string {
	w := min(max(m.width*2/3, 44), 76)
	innerW := w - 2

	switch m.importStage {
	case importStageInput:
		rows := []string{
			" " + t.KeyBarKey.Render("♫ ") + m.importInput.View(),
			"",
			t.RowDim.Render(" Track URLs resolve via Spotify oEmbed; playlist URLs are read"),
			t.RowDim.Render(" from the public embed page. No Spotify login required."),
		}
		if m.importError != "" {
			rows = append(rows, "", t.Err.Render(" "+m.importError))
		}
		body := strings.Join(rows, "\n")
		return renderPanel(t, "IMPORT FROM SPOTIFY", true, w, len(rows)+2, body)

	case importStageLoading:
		body := t.RowDim.Render(" Resolving Spotify and matching on Tidal…")
		return renderPanel(t, "IMPORT FROM SPOTIFY", true, w, 3, body)

	case importStageReview:
		return m.renderImportReview(t, w, innerW)

	case importStageOutput:
		return m.renderImportOutput(t, w, innerW)
	}
	return ""
}

// renderImportReview lists every source track with its Tidal match or a dim
// "not available" tag.
func (m *Model) renderImportReview(t Theme, w, innerW int) string {
	matched := len(matchedIDs(m.importRows))
	header := fmt.Sprintf(" %d/%d matched on Tidal", matched, len(m.importRows))

	var rows []string
	rows = append(rows, t.RowDim.Render(header), "")

	// Window the list so it never overflows the popup height.
	maxRows := max(min(m.height-10, 18), 4)
	start := 0
	if m.importCursor >= maxRows {
		start = m.importCursor - maxRows + 1
	}
	end := min(start+maxRows, len(m.importRows))
	for i := start; i < end; i++ {
		r := m.importRows[i]
		title := r.source.Title
		if a := r.source.Artist(); a != "" {
			title += t.RowFaint.Render(" — " + a)
		}
		var line string
		if r.match == nil {
			line = " ✕  " + title + "  " + t.RowFaint.Render("not available")
		} else {
			line = " ✓  " + title
		}
		if i == m.importCursor {
			rows = append(rows, t.RowSel.Width(innerW).Render(stripANSI(line)))
		} else {
			rows = append(rows, truncateStr(line, innerW))
		}
	}
	rows = append(rows, "", t.CmdHint.Render(" enter · choose destination   esc · cancel"))
	h := min(len(rows)+2, m.height-2)
	return renderPanel(t, "REVIEW IMPORT", true, w, max(h, 5), strings.Join(rows, "\n"))
}

// renderImportOutput renders the two-row destination chooser.
func (m *Model) renderImportOutput(t Theme, w, innerW int) string {
	matched := len(matchedIDs(m.importRows))
	var rows []string
	rows = append(rows, t.RowDim.Render(fmt.Sprintf(" %d tracks will be imported", matched)), "")
	for i, opt := range importOutputOptions {
		line := "   " + opt
		if i == m.importCursor {
			rows = append(rows, t.CmdItemSel.Width(innerW).Render(" › "+opt))
		} else {
			rows = append(rows, truncateStr(line, innerW))
		}
	}
	h := len(rows) + 2
	return renderPanel(t, "IMPORT DESTINATION", true, w, h, strings.Join(rows, "\n"))
}
