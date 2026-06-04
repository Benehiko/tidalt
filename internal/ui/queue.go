package ui

import "github.com/Benehiko/tidalt/v3/internal/tidal"

// enqueueEnd appends a track to the end of the live queue.
func (m *Model) enqueueEnd(t tidal.Track) {
	m.tracksOrder = append(m.tracksOrder, t)
	m.tracks = append(m.tracks, t)
	_ = m.store.SavePlaylist(m.tracks)
}

// enqueueNext inserts a track immediately after the current cursor position so
// it plays next.
func (m *Model) enqueueNext(t tidal.Track) {
	pos := min(m.cursor+1, len(m.tracks))
	m.tracks = insertTrack(m.tracks, pos, t)
	// Keep the unshuffled order in sync by appending (order is only meaningful
	// for re-shuffling; "play next" is a live-queue affordance).
	m.tracksOrder = append(m.tracksOrder, t)
	_ = m.store.SavePlaylist(m.tracks)
}

// insertTrack returns s with t inserted at index i.
func insertTrack(s []tidal.Track, i int, t tidal.Track) []tidal.Track {
	s = append(s, tidal.Track{})
	copy(s[i+1:], s[i:])
	s[i] = t
	return s
}
