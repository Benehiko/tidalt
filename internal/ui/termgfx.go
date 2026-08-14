package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"

	"golang.org/x/image/draw"
)

// KittySupported reports whether the running terminal supports the Kitty
// terminal graphics protocol. Ghostty, Kitty, and WezTerm all qualify.
func KittySupported() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	return os.Getenv("KITTY_WINDOW_ID") != ""
}

// kittyChunkSize is the maximum base64 payload length per Kitty APC chunk.
const kittyChunkSize = 4096

// kittyState memoizes the encoded cover image and tracks what is currently
// drawn on screen, so View only re-encodes/re-transmits on a real change.
type kittyState struct {
	encodeKey string // cover UUID + box geometry the cached escape was built for
	escape    string // cached draw escape for encodeKey
	drawnKey  string // the encodeKey currently displayed (""=nothing/cleared)

	// stale marks that whatever is on screen can no longer be trusted — set on
	// resize, where the terminal may keep placements at their old coordinates.
	// The next frame clears and redraws unconditionally.
	stale bool
}

// kittyClearAll returns the Kitty escape that deletes every image placement on
// screen. Kitty images live outside the terminal's cell grid, so redrawing the
// text frame does not erase them — this must be emitted on any frame that does
// not (re)draw the cover, or a stale image lingers.
func kittyClearAll() string {
	return "\x1b_Ga=d,d=A\x1b\\"
}

// kittyImageAt returns a Kitty escape that draws img occupying exactly cols×rows
// terminal cells, positioned at the absolute 1-indexed screen coordinates
// (row, col). The cursor is saved and restored around the draw so it can be
// appended to a fully-rendered frame without disturbing the layout underneath.
//
// Unlike kittyRowSequences this transmits the whole image in one placement
// (c=cols, r=rows), which is both crisper and avoids the per-row seams.
func kittyImageAt(img image.Image, col, row, cols, rows int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	// Scale to a generous pixel size for sharpness; the terminal maps it onto
	// the cols×rows cell box regardless of pixel dimensions.
	const targetPx = 640
	scaled := image.NewRGBA(image.Rect(0, 0, targetPx, targetPx))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return ""
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	var sb strings.Builder
	// Save cursor, move to the box's top-left, draw, restore cursor.
	fmt.Fprintf(&sb, "\x1b7\x1b[%d;%dH", row, col)
	for i := 0; i < len(encoded); i += kittyChunkSize {
		end := min(i+kittyChunkSize, len(encoded))
		chunk := encoded[i:end]
		more := 1
		if end >= len(encoded) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,c=%d,r=%d,q=2,m=%d;%s\x1b\\", cols, rows, more, chunk)
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	sb.WriteString("\x1b8") // restore cursor
	return sb.String()
}
