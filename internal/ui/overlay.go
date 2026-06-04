package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Benehiko/tidalt/v3/internal/tidal"
)

// openCommandPalette raises the command-palette overlay. The palette UI and
// fuzzy matching are wired in a later step; here it just opens the layer.
func (m *Model) openCommandPalette() {
	m.overlay = OverlayCommandPalette
}

// openActionSheet raises the contextual action sheet for a track. The sheet UI
// is wired in a later step.
func (m *Model) openActionSheet(t tidal.Track) {
	m.sheetTrack = &t
	m.sheetCursor = 0
	m.overlay = OverlayActionSheet
}

// updateOverlay routes keys while a modal overlay is active.
func (m Model) updateOverlay(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case OverlayDeviceSelect:
		return m.updateDeviceSelect(k)
	default:
		// Palette / action sheet handlers land in later steps; until then any
		// key (notably Esc) just dismisses the overlay.
		if k.String() == "esc" {
			m.overlay = OverlayNone
		}
		return m, nil
	}
}

// updateDeviceSelect handles the device-picker overlay.
func (m Model) updateDeviceSelect(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.overlay = OverlayNone
		m.cursor = 0
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.devices)-1 {
			m.cursor++
		}
	case keyEnter:
		if len(m.devices) == 0 {
			m.overlay = OverlayNone
			return m, nil
		}
		chosen := m.devices[m.cursor]
		m.currentDevice = chosen.HWName
		m.overlay = OverlayNone
		m.cursor = 0
		if m.clientMode {
			mc := m.mprisClient
			return m, func() tea.Msg {
				if err := mc.SendDevice(chosen.HWName); err != nil {
					return errMsg(err)
				}
				return nil
			}
		}
		m.player.SetDevice(chosen.HWName)
		_ = m.store.SaveDevice(chosen.HWName)
	}
	return m, nil
}

// renderDeviceSelect renders the device-picker popup.
func (m *Model) renderDeviceSelect(t Theme) string {
	w := min(max(m.width-12, 30), 70)
	var rows []string
	if len(m.devices) == 0 {
		rows = append(rows, t.RowDim.Render("No playback devices found."))
	} else {
		for i, d := range m.devices {
			cur := "  "
			if m.cursor == i {
				cur = t.RowPlaying.Render("› ")
			}
			check := ""
			if d.HWName == m.currentDevice {
				check = t.GreenT.Render(" ✓")
			}
			line := fmt.Sprintf("%s%s  %s%s", cur, d.HWName, t.RowDim.Render(d.LongName), check)
			if m.cursor == i {
				rows = append(rows, t.RowSel.Width(w-2).Render(stripANSI(line)))
			} else {
				rows = append(rows, line)
			}
		}
	}
	h := min(len(rows)+2, m.height-4)
	body := strings.Join(rows, "\n")
	return renderPanel(t, "SELECT DEVICE", true, w, max(h, 4), body)
}
