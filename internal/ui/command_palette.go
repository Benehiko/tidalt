package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteItem is one runnable entry in the command palette.
type paletteItem struct {
	icon  string
	label string
	hint  string
	group string // "ACTIONS" or "JUMP TO"
	run   func(m Model) (tea.Model, tea.Cmd)
}

// openCommandPalette raises the command-palette overlay with a fresh, focused
// fuzzy input.
func (m *Model) openCommandPalette() {
	ti := textinput.New()
	ti.Placeholder = "Run a command or jump to a section…"
	ti.Prompt = ""
	ti.Focus()
	m.paletteInput = ti
	m.paletteCursor = 0
	m.overlay = OverlayCommandPalette
}

// allPaletteItems is the full (unfiltered) command list. Actions act on the
// current queue/track; "Jump to" entries switch sections.
func allPaletteItems() []paletteItem {
	jump := func(sec Section) func(Model) (tea.Model, tea.Cmd) {
		return func(m Model) (tea.Model, tea.Cmd) {
			m.overlay = OverlayNone
			return m.selectSection(sec)
		}
	}
	items := []paletteItem{
		{icon: "⤓", label: "Save queue as playlist…", hint: "creates new", group: "ACTIONS", run: func(m Model) (tea.Model, tea.Cmd) {
			m.overlay = OverlayNone
			return m.beginSaveQueue()
		}},
		{icon: "＋", label: "Save queue to existing playlist…", group: "ACTIONS", run: func(m Model) (tea.Model, tea.Cmd) {
			m.overlay = OverlayNone
			return m.beginSaveToExisting()
		}},
	}
	for _, e := range sidebarEntries {
		if e.group != "" {
			continue
		}
		sec := e.section
		items = append(items, paletteItem{
			icon: e.icon, label: "Go to " + e.label, group: "JUMP TO", run: jump(sec),
		})
	}
	return items
}

// filterPaletteItems returns the items whose label fuzzily contains the query
// (case-insensitive subsequence match), preserving registry order.
func filterPaletteItems(query string) []paletteItem {
	all := allPaletteItems()
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return all
	}
	var out []paletteItem
	for _, it := range all {
		if fuzzyMatch(strings.ToLower(it.label), q) {
			out = append(out, it)
		}
	}
	return out
}

// fuzzyMatch reports whether all runes of needle appear in haystack in order.
func fuzzyMatch(haystack, needle string) bool {
	i := 0
	nr := []rune(needle)
	for _, r := range haystack {
		if i < len(nr) && r == nr[i] {
			i++
		}
	}
	return i == len(nr)
}

// updateCommandPalette handles typing, navigation, and selection in the palette.
func (m Model) updateCommandPalette(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := filterPaletteItems(m.paletteInput.Value())
	switch k.String() {
	case keyEsc:
		m.overlay = OverlayNone
		return m, nil
	case keyUp, "ctrl+k":
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		return m, nil
	case keyDown, "ctrl+j":
		if m.paletteCursor < len(items)-1 {
			m.paletteCursor++
		}
		return m, nil
	case keyEnter:
		if m.paletteCursor >= 0 && m.paletteCursor < len(items) {
			return items[m.paletteCursor].run(m)
		}
		m.overlay = OverlayNone
		return m, nil
	}
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(k)
	// Keep the cursor in range as the filtered list shrinks.
	if n := len(filterPaletteItems(m.paletteInput.Value())); m.paletteCursor >= n {
		m.paletteCursor = max(n-1, 0)
	}
	return m, cmd
}

// renderCommandPalette renders the command-palette popup: a titled box with the
// fuzzy input, then grouped, filtered results.
func (m *Model) renderCommandPalette(t Theme) string {
	w := min(max(m.width*2/3, 40), 72)
	innerW := w - 2

	items := filterPaletteItems(m.paletteInput.Value())

	prompt := t.KeyBarKey.Render("⌃P ")
	inputLine := " " + prompt + m.paletteInput.View()

	var rows []string
	rows = append(rows, inputLine, "")
	lastGroup := ""
	for i, it := range items {
		if it.group != lastGroup {
			rows = append(rows, t.CmdGroup.Render(it.group))
			lastGroup = it.group
		}
		plain := " " + it.icon + "  " + it.label
		if it.hint != "" {
			pad := max(innerW-lipgloss.Width(plain)-len([]rune(it.hint))-1, 1)
			plain += strings.Repeat(" ", pad) + it.hint
		}
		if i == m.paletteCursor {
			rows = append(rows, t.CmdItemSel.Width(innerW).Render(plain))
			continue
		}
		line := " " + t.RowDim.Render(it.icon) + "  " + it.label
		if it.hint != "" {
			hint := t.CmdHint.Render(it.hint)
			pad := max(innerW-lipgloss.Width(" "+it.icon+"  "+it.label)-lipgloss.Width(hint)-1, 1)
			line += strings.Repeat(" ", pad) + hint
		}
		rows = append(rows, line)
	}
	if len(items) == 0 {
		rows = append(rows, t.RowDim.Render(" No matching commands."))
	}

	h := min(len(rows)+2, m.height-2)
	body := strings.Join(rows, "\n")
	return renderPanel(t, "COMMAND", true, w, max(h, 5), body)
}

// beginSaveQueue saves the queue as a new playlist.
func (m Model) beginSaveQueue() (tea.Model, tea.Cmd) {
	return m.saveQueueAsNew()
}

// beginSaveToExisting opens the add-to-playlist picker, loading the user's
// playlists first if they aren't already cached.
func (m Model) beginSaveToExisting() (tea.Model, tea.Cmd) {
	if len(m.tracks) == 0 {
		m.errText = "Queue is empty — nothing to save"
		return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })
	}
	m.overlay = OverlayAddToPlaylist
	m.cursor = 0
	if len(m.playlists) == 0 {
		return m, func() tea.Msg {
			pls, err := m.client.GetUserPlaylists(m.ctx)
			if err != nil {
				return errMsg(err)
			}
			return playlistsMsg(pls)
		}
	}
	return m, nil
}
