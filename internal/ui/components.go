package ui

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// logoLines is a 5-row ASCII art representation of "tidalt".
var logoLines = [5]string{
	` ████████╗██╗██████╗  █████╗ ██╗  ████████╗`,
	`    ██╔══╝██║██╔══██╗██╔══██╗██║     ██╔══╝`,
	`    ██║   ██║██║  ██║███████║██║     ██║   `,
	`    ██║   ██║██║  ██║██╔══██║██║     ██║   `,
	`    ██║   ██║██████╔╝██║  ██║███████╗██║   `,
}

// compactLogo is the small wordmark used in the sidebar header (scale 0).
var compactLogo = [2]string{
	`▀█▀ █ █▀▄ ▄▀▄ █   ▀█▀`,
	` █  █ █▄▀ █▀█ █▄▄  █ `,
}

const (
	numBars    = 9
	numRows    = 5
	barScale   = 10                            // fixed-point scale for smooth motion
	barMax     = int(numRows * 0.8 * barScale) // 80% height ceiling, scaled
	barMin     = 1                             // near-zero minimum, scaled
	barStep    = 2                             // smoothing step per tick (scaled units)
	retargetIn = 4                             // re-randomise target every N ticks
)

// updateBars advances the bar animation state by one tick. It smooths current
// heights toward their targets and periodically picks new random targets.
func updateBars(frame int, heights, targets *[numBars]int, isPlaying bool) {
	if !isPlaying {
		for b := range heights {
			heights[b] = barMin
			targets[b] = barMin
		}
		return
	}

	// Re-randomise targets on a staggered schedule so bars don't all move together.
	for b := range targets {
		if (frame+b*3)%retargetIn == 0 {
			targets[b] = barMin + rand.IntN(barMax-barMin+1) //nolint:gosec // G404: equaliser bar animation does not need crypto-grade randomness
		}
	}

	// Smooth each bar toward its target.
	for b := range heights {
		diff := targets[b] - heights[b]
		switch {
		case diff > barStep:
			heights[b] += barStep
		case diff < -barStep:
			heights[b] -= barStep
		default:
			heights[b] = targets[b]
		}
	}
}

// gradColors returns the four-stop logo/eq gradient as a cycling slice, falling
// back gracefully if any stop is not a plain color.
func gradColors(t Theme) []lipgloss.TerminalColor {
	return []lipgloss.TerminalColor{t.P.Grad1, t.P.Grad2, t.P.Grad3, t.P.Grad4}
}

// musicBars renders the equaliser bar rows using pre-computed heights, colored
// from the theme's gradient stops.
func musicBars(frame int, heights [numBars]int, t Theme, isPlaying bool) [numRows]string {
	dimStyle := lipgloss.NewStyle().Foreground(t.P.FgFaint)
	palette := gradColors(t)

	var rows [numRows]string
	for row := range numRows {
		var sb strings.Builder
		sb.WriteString("  ") // gap between logo and bars
		for b := range numBars {
			h := (heights[b] + barScale - 1) / barScale // ceil-divide back to rows
			switch {
			case !isPlaying:
				sb.WriteString(dimStyle.Render("▁"))
			case row >= numRows-h:
				idx := (frame + b) % len(palette)
				sb.WriteString(lipgloss.NewStyle().Foreground(palette[idx]).Render("█"))
			default:
				sb.WriteRune(' ')
			}
			sb.WriteRune(' ') // gap between bars
		}
		rows[row] = sb.String()
	}
	return rows
}

// renderLogo returns the animated logo string. scale 0 renders the compact
// 2-row sidebar wordmark; scale >= 1 renders the full 5-row ASCII art with EQ
// bars. The wave color cycles the theme's gradient stops.
func renderLogo(frame, barFrame int, barHeights [numBars]int, t Theme, scale int, isPlaying bool) string {
	palette := gradColors(t)
	period := len(palette)

	if scale == 0 {
		// Compact sidebar wordmark — color each glyph along the gradient.
		var sb strings.Builder
		for _, row := range compactLogo {
			runes := []rune(row)
			width := len(runes)
			for col, r := range runes {
				if r == ' ' {
					sb.WriteRune(' ')
					continue
				}
				idx := (col*period/width + frame) % period
				if idx < 0 {
					idx += period
				}
				sb.WriteString(lipgloss.NewStyle().Foreground(palette[idx]).Render(string(r)))
			}
			sb.WriteByte('\n')
		}
		return sb.String()
	}

	width := len([]rune(logoLines[0]))
	bars := musicBars(barFrame, barHeights, t, isPlaying)

	var sb strings.Builder
	for rowIdx, row := range logoLines {
		runes := []rune(row)
		for col, r := range runes {
			if r == ' ' || r == '╗' || r == '╔' || r == '╝' || r == '╚' || r == '═' || r == '║' || r == '╠' || r == '╣' || r == '╦' || r == '╩' || r == '╬' {
				// Keep box-drawing and spaces uncoloured to preserve shape.
				sb.WriteRune(r)
				continue
			}
			idx := (col*period/width + frame) % period
			if idx < 0 {
				idx += period
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(palette[idx]).Render(string(r)))
		}
		sb.WriteString(bars[rowIdx])
		sb.WriteByte('\n')
	}
	return sb.String()
}

// rowOpts controls how renderTrackRow draws a single track line.
type rowOpts struct {
	selected   bool
	playing    bool
	fav        bool
	showIndex  bool
	showArtist bool
	index      int
	width      int
	duration   int // seconds; 0 hides the duration column
}

// renderTrackRow renders one track line: cursor glyph, optional index, title,
// optional artist, favorite heart, and right-aligned duration — all themed and
// truncated to o.width.
func renderTrackRow(t Theme, tr tidal.Track, o rowOpts) string {
	cur := " "
	curStyle := t.RowFaint
	if o.playing {
		cur = "♪"
		curStyle = t.RowPlaying
	} else if o.selected {
		cur = "›"
		curStyle = t.RowPlaying
	}

	var idx string
	if o.showIndex {
		idx = t.RowFaint.Render(fmt.Sprintf("%2d ", o.index))
	}

	title := tr.Title
	titleStyle := t.Row
	if o.playing {
		titleStyle = t.RowPlaying
	}

	fav := "  "
	if o.fav {
		fav = " " + t.Fav.Render("♥")
	}

	var dur string
	if o.duration > 0 {
		dur = " " + t.RowDim.Render(formatTime(float64(o.duration)))
	}

	// Fixed-width left gutter (leading space + cursor + space + index) and the
	// right-aligned columns (fav + duration). The title/artist "middle" is
	// truncated to whatever space remains so the duration always survives.
	prefix := " " + curStyle.Render(cur) + " " + idx
	fixed := lipgloss.Width(prefix) + lipgloss.Width(fav) + lipgloss.Width(dur)
	midRoom := max(o.width-fixed, 1)

	mid := titleStyle.Render(title)
	if o.showArtist && tr.Artist.Name != "" {
		mid += t.RowFaint.Render(" — ") + t.RowDim.Render(tr.Artist.Name)
	}
	mid = truncateStr(mid, midRoom)

	// Pad between the middle and the right-aligned fav+duration columns.
	pad := max(o.width-lipgloss.Width(prefix)-lipgloss.Width(mid)-lipgloss.Width(fav)-lipgloss.Width(dur), 0)
	line := prefix + mid + fav + strings.Repeat(" ", pad) + dur

	if o.selected {
		// Nested fg colors would reset the selection background mid-line, so
		// the band is rendered over the plain text with a uniform foreground.
		return t.RowSel.Width(o.width).Render(stripANSI(line))
	}
	return line
}

// renderKeyBar renders the footer hint bar: each item is [key, label]; keys are
// cyan, labels dim, separated by faint pipes. Truncated to width w.
func renderKeyBar(t Theme, items [][2]string, w int) string {
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString(t.KeyBarSep.Render(" │ "))
		}
		sb.WriteString(t.KeyBarKey.Render(it[0]))
		sb.WriteString(" ")
		sb.WriteString(t.KeyBarLabel.Render(it[1]))
	}
	return truncateStr(" "+sb.String(), w)
}

// renderNowPlayingBar renders the persistent bottom now-playing bar: a small
// mini-EQ (when playing), the cyan track title, dim artist, and a thin progress
// readout. Returns a multi-line block sized to width w.
func (m *Model) renderNowPlayingBar(t Theme, w int) string {
	inner := max(w-2, 10)

	if m.currentTrack == nil {
		empty := t.RowDim.Render("  Nothing playing")
		body := empty + strings.Repeat(" ", max(inner-lipgloss.Width(empty), 0))
		return renderPanel(t, "", false, w, 3, body)
	}

	eq := miniEQ(t, m.barHeights, m.isPlaying)
	// Right-aligned status (volume / device / shuffle) on the title row.
	status := m.nowBarStatus(t)
	titleRoom := max(inner-lipgloss.Width(eq)-1-lipgloss.Width(status)-1, 1)
	title := t.RowPlaying.Render(truncateStr(m.currentTrack.Title, titleRoom))
	left := eq + " " + title
	pad := max(inner-lipgloss.Width(left)-lipgloss.Width(status), 0)
	head := left + strings.Repeat(" ", pad) + status
	artist := t.RowDim.Render(truncateStr(m.currentTrack.Artist.Name, inner))

	percent := 0.0
	if m.duration > 0 {
		percent = m.currPos / m.duration
	}
	bar := m.progress.ViewAs(percent)
	timeStr := t.RowDim.Render(fmt.Sprintf(" %s / %s", formatTime(m.currPos), formatTime(m.duration)))

	body := strings.Join([]string{head, artist, bar + timeStr}, "\n")
	return renderPanel(t, "", false, w, 5, body)
}

// nowBarStatus renders the compact volume / device / shuffle readout shown at
// the right of the now-playing bar.
func (m *Model) nowBarStatus(t Theme) string {
	vol := fmt.Sprintf("vol %.0f%%", m.volume)
	parts := []string{vol}
	if m.shuffleMode != ShuffleOff {
		parts = append(parts, "shuffle "+m.shuffleMode.String())
	}
	dev := m.currentDevice
	if dev == "" {
		dev = "auto"
	}
	parts = append(parts, dev)
	return t.RowDim.Render(strings.Join(parts, "  ·  "))
}

// miniEQ renders a tiny 4-bar equalizer indicator from the live bar heights.
func miniEQ(t Theme, heights [numBars]int, isPlaying bool) string {
	if !isPlaying {
		return t.RowFaint.Render("▪")
	}
	levels := []rune("▁▂▃▄▅▆▇█")
	var sb strings.Builder
	style := lipgloss.NewStyle().Foreground(t.P.Cyan)
	for i := range 4 {
		h := heights[i*2] // sample a few bars
		l := h * (len(levels) - 1) / max(barMax, 1)
		l = max(min(l, len(levels)-1), 0)
		sb.WriteRune(levels[l])
	}
	return style.Render(sb.String())
}
