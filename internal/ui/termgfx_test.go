package ui

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

// kittyCoverModel returns a Queue-section model wired to an in-memory writer,
// standing in for the TTY that graphics escapes are written to.
func kittyCoverModel(t *testing.T) (Model, *bytes.Buffer) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{200, 120, 60, 255})
		}
	}
	buf := &bytes.Buffer{}
	m := newSmokeModel()
	m.width, m.height = 120, 40
	m.section = SecQueue
	m.cursor = 1
	m.coverImage = img
	m.coverCacheKey = "cover-a"
	m.kittySupported = true
	m.ttyOut = buf
	return m, buf
}

const kittyDraw = "\x1b_Ga=T"

// TestKittyCoverDrawsWithoutViewChange is the core regression: the image must
// reach the terminal on its own, without relying on the View string. Escapes
// used to be appended to the frame, where BubbleTea's renderer truncated them
// to the terminal width and skipped lines unchanged since the previous frame —
// so the cover only appeared once an unrelated keypress altered that line.
func TestKittyCoverDrawsWithoutViewChange(t *testing.T) {
	m, buf := kittyCoverModel(t)

	m.syncKittyCover()
	if !strings.Contains(buf.String(), kittyDraw) {
		t.Fatalf("no draw escape written to the TTY on first sync")
	}
	if strings.Contains(m.View(), kittyDraw) {
		t.Errorf("graphics escape leaked into the View string; the renderer will mangle it")
	}

	// A second sync with nothing changed must stay silent.
	buf.Reset()
	m.syncKittyCover()
	if buf.Len() != 0 {
		t.Errorf("idle sync wrote %d bytes, want 0", buf.Len())
	}
}

// TestKittyCoverResizeClearsOldPlacement covers the reported bug: after a
// resize moves the cover box, the previous placement must be deleted. Kitty
// images live outside the cell grid, so without an explicit delete the old copy
// stays on screen — the user saw two and then three stacked covers.
func TestKittyCoverResizeClearsOldPlacement(t *testing.T) {
	m, buf := kittyCoverModel(t)
	m.syncKittyCover()

	buf.Reset()
	m.width, m.height = 100, 32
	m.kitty.stale = true // set by the WindowSizeMsg handler
	m.syncKittyCover()

	out := buf.String()
	if !strings.Contains(out, kittyClearAll()) {
		t.Errorf("resize did not delete the previous placement")
	}
	if !strings.Contains(out, kittyDraw) {
		t.Errorf("resize did not redraw the cover")
	}
	if strings.Index(out, kittyClearAll()) > strings.Index(out, kittyDraw) {
		t.Errorf("delete must precede the redraw, otherwise it erases the new image")
	}
}

// TestKittyCoverHiddenWhenTerminalTooSmall checks that shrinking past the
// legibility thresholds erases the image rather than leaving it stranded over
// the rest of the UI.
func TestKittyCoverHiddenWhenTerminalTooSmall(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"too narrow", 50, 40},
		{"too short", 120, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, buf := kittyCoverModel(t)
			m.syncKittyCover()

			buf.Reset()
			m.width, m.height = tc.w, tc.h
			m.syncKittyCover()

			out := buf.String()
			if !strings.Contains(out, kittyClearAll()) {
				t.Errorf("shrinking to %dx%d did not clear the cover", tc.w, tc.h)
			}
			if strings.Contains(out, kittyDraw) {
				t.Errorf("cover was redrawn at %dx%d despite being too small", tc.w, tc.h)
			}

			// Still hidden, and silent, on the next sync.
			buf.Reset()
			m.syncKittyCover()
			if buf.Len() != 0 {
				t.Errorf("hidden cover wrote %d bytes on idle sync, want 0", buf.Len())
			}
		})
	}
}

// TestKittyCoverDisabledWritesNothing guards the daemon path, which runs the
// same model with no renderer and no TTY.
func TestKittyCoverDisabledWritesNothing(t *testing.T) {
	m, buf := kittyCoverModel(t)
	m = m.WithoutGraphics()
	m.ttyOut = buf // prove kittySupported alone is enough to keep it silent

	m.syncKittyCover()
	if buf.Len() != 0 {
		t.Errorf("WithoutGraphics model wrote %d bytes, want 0", buf.Len())
	}
}
