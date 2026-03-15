package ui

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"net/http"
	"strings"
)

// fetchCoverImage downloads the image at url and decodes it.
func fetchCoverImage(url string) (image.Image, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("cover http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cover fetch: status %d", resp.StatusCode)
	}
	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cover decode (format=%s): %w", format, err)
	}
	return img, nil
}

// renderBlockArt renders img using Unicode quadrant-block characters, giving
// 2×2 pixel resolution per terminal cell. Each cell samples four image pixels
// (TL, TR, BL, BR), clusters them into two colours via one-step k-means, and
// picks the quadrant glyph that best represents the fg/bg split.
func renderBlockArt(img image.Image, cols, rows int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	bounds := img.Bounds()
	ox, oy := bounds.Min.X, bounds.Min.Y
	imgW := bounds.Max.X - ox
	imgH := bounds.Max.Y - oy
	pixCols := cols * 2
	pixRows := rows * 2

	var sb strings.Builder
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			pxL := col * 2 * imgW / pixCols
			pxR := (col*2 + 1) * imgW / pixCols
			pyT := row * 2 * imgH / pixRows
			pyB := (row*2 + 1) * imgH / pixRows

			tl := rgbaOf(img.At(ox+pxL, oy+pyT))
			tr := rgbaOf(img.At(ox+pxR, oy+pyT))
			bl := rgbaOf(img.At(ox+pxL, oy+pyB))
			br := rgbaOf(img.At(ox+pxR, oy+pyB))

			fg, bg, bits := quadrantColors(tl, tr, bl, br)
			fmt.Fprintf(&sb,
				"\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%c",
				fg.R, fg.G, fg.B,
				bg.R, bg.G, bg.B,
				quadrantGlyph(bits),
			)
		}
		sb.WriteString("\x1b[0m\n")
	}
	return sb.String()
}

// quadrantColors assigns TL/TR/BL/BR to fg or bg via one-step k-means.
// Returns fg colour, bg colour, and a 4-bit mask (bit3=TL,2=TR,1=BL,0=BR) for fg pixels.
func quadrantColors(tl, tr, bl, br color.RGBA) (fg, bg color.RGBA, bits uint8) {
	c0 := avgRGBA(tl, br)
	c1 := avgRGBA(tr, bl)
	quads := [4]color.RGBA{tl, tr, bl, br}
	var mask uint8
	for i, q := range quads {
		if colorDist(q, c1) < colorDist(q, c0) {
			mask |= 1 << (3 - i)
		}
	}
	var fgSum, bgSum [3]int
	var fgN, bgN int
	for i, q := range quads {
		if mask&(1<<(3-i)) != 0 {
			fgSum[0] += int(q.R)
			fgSum[1] += int(q.G)
			fgSum[2] += int(q.B)
			fgN++
		} else {
			bgSum[0] += int(q.R)
			bgSum[1] += int(q.G)
			bgSum[2] += int(q.B)
			bgN++
		}
	}
	if fgN > 0 {
		fg = color.RGBA{R: uint8(fgSum[0] / fgN), G: uint8(fgSum[1] / fgN), B: uint8(fgSum[2] / fgN)}
	} else {
		fg = c1
	}
	if bgN > 0 {
		bg = color.RGBA{R: uint8(bgSum[0] / bgN), G: uint8(bgSum[1] / bgN), B: uint8(bgSum[2] / bgN)}
	} else {
		bg = c0
	}
	return fg, bg, mask
}

// quadrantGlyph maps a 4-bit mask (TL=3,TR=2,BL=1,BR=0) to a Unicode block glyph.
func quadrantGlyph(bits uint8) rune {
	switch bits {
	case 0b0000:
		return ' '
	case 0b1000:
		return '▘'
	case 0b0100:
		return '▝'
	case 0b0010:
		return '▖'
	case 0b0001:
		return '▗'
	case 0b1100:
		return '▀'
	case 0b0011:
		return '▄'
	case 0b1010:
		return '▌'
	case 0b0101:
		return '▐'
	case 0b1001:
		return '▚'
	case 0b0110:
		return '▞'
	case 0b1110:
		return '▛'
	case 0b1101:
		return '▜'
	case 0b1011:
		return '▙'
	case 0b0111:
		return '▟'
	default: // 0b1111
		return '█'
	}
}

func avgRGBA(a, b color.RGBA) color.RGBA {
	return color.RGBA{
		R: uint8((int(a.R) + int(b.R)) / 2),
		G: uint8((int(a.G) + int(b.G)) / 2),
		B: uint8((int(a.B) + int(b.B)) / 2),
	}
}

func colorDist(a, b color.RGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

func rgbaOf(c color.Color) color.RGBA {
	r, g, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
}

// coverPanelLines returns h lines of text for the right-side panel showing
// the album art and track metadata.  w is the panel width in terminal columns.
//
// If kittyRows is non-nil (Kitty terminal graphics protocol supported and
// sequences pre-generated), each art row is rendered as a Kitty inline image
// strip — one entry from kittyRows per line.  The terminal renders each strip
// at the correct cursor position; cells are otherwise left transparent (reset
// attributes, no explicit background) so the image shows through.
//
// If kittyRows is nil, the existing Unicode block-art renderer is used as a
// fallback, which works in any colour terminal.
func coverPanelLines(img image.Image, title, artist, album string, w, h int, kittyRows []string) []string {
	lines := make([]string, h)
	if w <= 0 || h <= 0 {
		return lines
	}

	// Reserve 1 blank + 3 text lines below the image; clamp so imgRows never
	// exceeds h (which can be as small as 1 after a terminal resize).
	imgRows := h - 4
	if imgRows < 2 {
		imgRows = 2
	}
	if imgRows > h {
		imgRows = h
	}

	if len(kittyRows) > 0 {
		// Kitty path: use pre-generated per-row sequences for the art area.
		// Rows beyond the available sequences fall back to transparent spaces.
		transparent := "\x1b[0m" + strings.Repeat(" ", w)
		for i := 0; i < imgRows && i < h; i++ {
			if i < len(kittyRows) {
				lines[i] = kittyRows[i]
			} else {
				lines[i] = transparent
			}
		}
	} else if img != nil {
		// Block-art fallback.
		artLines := strings.Split(strings.TrimRight(renderBlockArt(img, w, imgRows), "\n"), "\n")
		for i := 0; i < imgRows && i < len(artLines) && i < h; i++ {
			lines[i] = artLines[i]
		}
	} else {
		placeholder := strings.Repeat("░", w)
		for i := 0; i < imgRows && i < h; i++ {
			lines[i] = placeholder
		}
	}

	// Text lines below the image — all guarded by h.
	base := imgRows + 1 // +1 blank gap
	if base < h {
		lines[base] = truncateStr(title, w)
	}
	if base+1 < h {
		lines[base+1] = truncateStr(artist, w)
	}
	if base+2 < h {
		lines[base+2] = truncateStr(album, w)
	}

	return lines
}

// truncateStr shortens s to at most n runes, adding "…" if truncated.
func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	if n < 4 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
