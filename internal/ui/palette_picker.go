package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// updateSettings drives the theme picker: moving the cursor live-previews the
// whole UI; Enter commits and persists; Esc reverts to the committed theme.
func (m Model) updateSettings(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "h", keyLeft:
		m.cancelPreview()
		m.focusMain = false
		return m, nil
	case keyEsc:
		m.cancelPreview()
		m.focusMain = false
		return m, nil
	case keyUp, "k":
		if m.themeCursor > 0 {
			m.themeCursor--
			m.preview(m.themeCursor)
		}
		return m, nil
	case keyDown, "j":
		if m.themeCursor < len(paletteOrder)-1 {
			m.themeCursor++
			m.preview(m.themeCursor)
		}
		return m, nil
	case keyEnter:
		m.applyTheme(paletteOrder[m.themeCursor])
		return m, nil
	case "t":
		// Cycle within the picker too.
		m.themeCursor = (m.themeCursor + 1) % len(paletteOrder)
		m.preview(m.themeCursor)
		return m, nil
	}
	return m, nil
}

// preview sets the live-preview palette for the scheme at index i.
func (m *Model) preview(i int) {
	pal := resolvePalette(paletteOrder[i])
	m.previewPalette = &pal
}

// cancelPreview drops the live preview, reverting to the committed theme.
func (m *Model) cancelPreview() {
	m.previewPalette = nil
}

// enterSettings positions the picker cursor on the active theme and starts a
// preview so the highlighted row matches what's on screen.
func (m *Model) enterSettings() {
	m.themeCursor = 0
	for i, name := range paletteOrder {
		if name == m.themeName {
			m.themeCursor = i
			break
		}
	}
}

// swatch renders five small color blocks sampled from a palette.
func swatch(p Palette) string {
	cells := []lipgloss.TerminalColor{p.Bg2, p.Fg, p.Cyan, p.Purple, p.Amber}
	var sb strings.Builder
	for _, c := range cells {
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render("█"))
	}
	return sb.String()
}

// renderThemePicker renders the Settings theme picker pane. The active scheme is
// marked, the cursor row uses the cyan band, and a "live preview" hint shows.
func (m *Model) renderThemePicker(t Theme, w, h int) string {
	innerW := max(w-2, 1)
	rows := make([]string, 0, len(paletteOrder)+2)
	rows = append(rows,
		t.RowFaint.Render(" Move with j/k to preview · ↵ apply & save · t cycle · Esc cancel"),
		"",
	)

	for i, name := range paletteOrder {
		pal := resolvePalette(name)
		activeMark := " "
		if name == m.themeName {
			activeMark = "●"
		}
		label := paletteNames[name]
		// The swatch keeps its own colors; the rest of the row is themed.
		body := " " + swatch(pal) + " " + label
		if i == m.themeCursor {
			hint := "previewing"
			// Measure the plain text to right-align the hint, then render the
			// row content (swatch retains color) on the selection band color.
			plain := " " + activeMark + " ····· " + label
			pad := max(innerW-lipgloss.Width(plain)-len(hint)-1, 1)
			rowText := " " + activeMark + body + strings.Repeat(" ", pad) + hint
			rows = append(rows, lipgloss.NewStyle().Background(t.P.BgSel).Width(innerW).Render(rowText))
			continue
		}
		mark := t.RowFaint.Render(activeMark)
		if activeMark == "●" {
			mark = t.GreenT.Render(activeMark)
		}
		rows = append(rows, truncateStr(" "+mark+body, innerW))
	}

	return renderListPanel(t, "THEMES", m.focusMain, rows, m.themeCursor+2, w, h)
}
