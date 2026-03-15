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

// kittyRowSequences slices img into rows horizontal strips and returns a
// Kitty terminal graphics protocol escape sequence for each strip.  Each
// sequence renders its strip inline at the current cursor position, occupying
// cols terminal columns and exactly 1 terminal row (r=1).
//
// Using r=1 per sequence matches BubbleTea's line-based rendering: after a
// r=1 Kitty sequence the terminal cursor is at the end of the current line,
// and the trailing "\n" in the rendered frame advances exactly one row — the
// same as any other line.
//
// The image is scaled to 320×320 before slicing; each strip is a 320-pixel-
// wide sub-image of height (320 / rows), PNG-encoded and base64-chunked.
func kittyRowSequences(img image.Image, cols, rows int) []string {
	if img == nil || cols <= 0 || rows <= 0 {
		return nil
	}

	const targetPx = 320

	rowPx := targetPx / rows
	if rowPx < 1 {
		rowPx = 1
	}

	// Scale the full image to 320 × (rowPx*rows) so each slice is uniform.
	scaledH := rowPx * rows
	scaled := image.NewRGBA(image.Rect(0, 0, targetPx, scaledH))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Over, nil)

	seqs := make([]string, rows)
	for i := range rows {
		y0 := i * rowPx
		y1 := y0 + rowPx

		// Extract the strip as a sub-image and encode as PNG.
		strip := scaled.SubImage(image.Rect(0, y0, targetPx, y1))
		var buf bytes.Buffer
		if err := png.Encode(&buf, strip); err != nil {
			seqs[i] = strings.Repeat(" ", cols)
			continue
		}

		encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
		seqs[i] = kittyChunked(encoded, cols)
	}
	return seqs
}

// kittyChunked wraps base64-encoded image data in one or more Kitty APC
// sequences with c=cols, r=1.  Large payloads are split into kittyChunkSize
// chunks using the m= (more) flag.
func kittyChunked(encoded string, cols int) string {
	var sb strings.Builder
	for i := 0; i < len(encoded); i += kittyChunkSize {
		end := i + kittyChunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[i:end]
		more := 1
		if end >= len(encoded) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,c=%d,r=1,q=2,m=%d;%s\x1b\\", cols, more, chunk)
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return sb.String()
}
