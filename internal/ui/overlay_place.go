package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// PlaceOverlay composites the fg block on top of the bg block at cell
// coordinates (x, y), returning the merged multi-line string.
//
// lipgloss v1.x has no native layering/compositing — Place* only positions a
// block within a blank box. This helper does the missing piece: it walks each
// row of bg, and for the rows the overlay covers it slices the bg line around
// the fg's horizontal footprint (ANSI-aware, so escape sequences and wide runes
// are measured by display width, never byte length) and stitches the fg line
// into the gap. Rows outside the overlay are passed through unchanged.
//
// It is intentionally width-aware via the x/ansi helpers (the same ones lipgloss
// itself uses) so styled, colored, and double-width content composites cleanly.
func PlaceOverlay(x, y int, fg, bg string) string {
	fgLines := strings.Split(fg, "\n")
	bgLines := strings.Split(bg, "\n")
	fgWidth := lipgloss.Width(fg)

	var b strings.Builder
	for i, bgLine := range bgLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fgIdx := i - y
		if fgIdx < 0 || fgIdx >= len(fgLines) {
			b.WriteString(bgLine)
			continue
		}
		fgLine := fgLines[fgIdx]

		// Left slice of the bg line, up to column x.
		left := ansi.Truncate(bgLine, x, "")
		// Pad the left slice if the bg line was shorter than x.
		if leftW := ansi.StringWidth(left); leftW < x {
			left += strings.Repeat(" ", x-leftW)
		}

		// Right slice: everything on the bg line past (x + fgWidth).
		right := truncateLeftANSI(bgLine, x+fgWidth)

		b.WriteString(left)
		b.WriteString(fgLine)
		b.WriteString(right)
	}
	return b.String()
}

// truncateLeftANSI returns the portion of s after the first n display columns,
// discarding any styling that applied to the removed prefix. Used to recover the
// right-hand remainder of a bg line after the overlay's footprint.
func truncateLeftANSI(s string, n int) string {
	w := ansi.StringWidth(s)
	if w <= n {
		return ""
	}
	// ansi.TruncateLeft keeps the right portion after n columns.
	return ansi.TruncateLeft(s, n, "")
}

// dim re-renders plain (already-rendered) content through a faint style so it
// reads as background behind an overlay. Because the input may already contain
// ANSI styling, we strip it first and re-color uniformly — this is a deliberate
// trade-off: the dimmed backdrop loses its original colors but reads clearly as
// inactive, which is the intent.
func dim(t Theme, s string) string {
	faint := lipgloss.NewStyle().Foreground(t.P.FgFaint)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = faint.Render(stripANSI(ln))
	}
	return strings.Join(lines, "\n")
}
