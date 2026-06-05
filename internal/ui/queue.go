package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// enqueueEnd appends a track to the end of the live queue and marks it edited.
func (m *Model) enqueueEnd(t tidal.Track) {
	m.tracksOrder = append(m.tracksOrder, t)
	m.tracks = append(m.tracks, t)
	m.queueDirty = true
	_ = m.store.SavePlaylist(m.tracks)
}

// enqueueNext inserts a track immediately after the current cursor position so
// it plays next, marking the queue edited.
func (m *Model) enqueueNext(t tidal.Track) {
	pos := min(m.cursor+1, len(m.tracks))
	m.tracks = insertTrack(m.tracks, pos, t)
	m.tracksOrder = append(m.tracksOrder, t)
	m.queueDirty = true
	_ = m.store.SavePlaylist(m.tracks)
}

// playListIntoQueue loads a list of tracks into the live queue (as an ad-hoc,
// unsaved queue) and plays from index i. Used by sections that play through a
// list — Favorite Songs, Recently Played, etc.
func (m *Model) playListIntoQueue(list []tidal.Track, i int) tea.Cmd {
	if i < 0 || i >= len(list) {
		return nil
	}
	m.tracksOrder = append([]tidal.Track(nil), list...)
	m.shuffleMode = ShuffleOff
	m.applyShuffle()
	m.queueSource = ""
	m.queuePlaylistUUID = ""
	m.queueDirty = false
	m.cursor = i
	_ = m.store.SavePlaylist(m.tracks)
	track := m.tracks[i]
	_ = m.store.CacheTrack(track.ID, track)
	return m.playTrackCmd(track)
}

// clearQueue empties the live queue. Playback of the current track continues
// (already buffered) but auto-advance has nothing to follow.
func (m *Model) clearQueue() {
	m.tracks = nil
	m.tracksOrder = nil
	m.shufflePlayed = nil
	m.cursor = 0
	m.queueSource = ""
	m.queuePlaylistUUID = ""
	m.queueDirty = false
	_ = m.store.SavePlaylist(m.tracks)
}

// removeFromQueue drops the track at index i from the live queue, marks the
// queue edited, and keeps the cursor in range. The currently-playing audio is
// unaffected (it is already buffered); only the queue list changes.
func (m *Model) removeFromQueue(i int) {
	if i < 0 || i >= len(m.tracks) {
		return
	}
	removed := m.tracks[i]
	m.tracks = append(m.tracks[:i], m.tracks[i+1:]...)

	// Mirror the removal in the unshuffled order so a later re-shuffle is
	// consistent (remove the first matching ID).
	for j := range m.tracksOrder {
		if m.tracksOrder[j].ID == removed.ID {
			m.tracksOrder = append(m.tracksOrder[:j], m.tracksOrder[j+1:]...)
			break
		}
	}

	if m.cursor >= len(m.tracks) {
		m.cursor = max(len(m.tracks)-1, 0)
	}
	m.queueDirty = true
	_ = m.store.SavePlaylist(m.tracks)
}

// loadQueueFromPlaylist replaces the live queue with a playlist's tracks and
// records its origin so the hybrid header can show the synced/edited state.
func (m *Model) loadQueueFromPlaylist(tracks []tidal.Track, pl tidal.Playlist) {
	m.tracksOrder = tracks
	m.shuffleMode = ShuffleOff
	m.applyShuffle()
	m.queueSource = "playlist:" + pl.Title
	m.queuePlaylistUUID = pl.UUID
	m.queueDirty = false
	_ = m.store.SavePlaylist(m.tracks)
}

// queueHeader returns the queue panel's title and an optional colored status
// suffix reflecting the hybrid model's state.
func (m *Model) queueHeader(t Theme) string {
	name, isPlaylist := strings.CutPrefix(m.queueSource, "playlist:")
	switch {
	case isPlaylist && m.queueDirty:
		return "QUEUE · " + name + " " + t.Amber.Render("· edited — S save")
	case isPlaylist:
		return "QUEUE · " + name + " " + t.GreenT.Render("· synced")
	case m.queueSource == "radio":
		return "QUEUE " + t.Amber.Render("· radio · unsaved — S save")
	default:
		return "QUEUE"
	}
}

// beginSaveQueue saves the current queue as a brand-new playlist. The name is
// derived from the source for now (a name prompt can come later); the command
// creates the playlist and appends every queue track.
func (m Model) saveQueueAsNew() (tea.Model, tea.Cmd) {
	if len(m.tracks) == 0 {
		m.errText = "Queue is empty — nothing to save"
		return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })
	}
	name := m.suggestedQueueName()
	cmd := m.saveQueueCmd(name)
	return m, cmd
}

// suggestedQueueName proposes a playlist name from the queue's origin or the
// currently playing track.
func (m *Model) suggestedQueueName() string {
	if name, ok := strings.CutPrefix(m.queueSource, "playlist:"); ok && name != "" {
		return name
	}
	if m.currentTrack != nil {
		return m.currentTrack.Title + " radio"
	}
	if len(m.tracks) > 0 {
		return m.tracks[0].Title + " mix"
	}
	return "tidalt queue"
}

// saveQueueCmd creates a new playlist named `name` and adds every queue track.
func (m *Model) saveQueueCmd(name string) tea.Cmd {
	tracks := append([]tidal.Track(nil), m.tracks...)
	client := m.client
	ctx := m.ctx
	return func() tea.Msg {
		uuid, err := client.CreatePlaylist(ctx, name, "Saved from tidalt")
		if err != nil {
			return errMsg(err)
		}
		ids := make([]int, len(tracks))
		for i := range tracks {
			ids[i] = tracks[i].ID
		}
		if err := client.AddTracksToPlaylist(ctx, uuid, ids); err != nil {
			return errMsg(err)
		}
		return queueSavedMsg{uuid: uuid, name: name, count: len(ids)}
	}
}

// saveQueueToExisting appends the queue to an already-saved playlist.
func (m *Model) saveQueueToExistingCmd(uuid, name string) tea.Cmd {
	tracks := append([]tidal.Track(nil), m.tracks...)
	client := m.client
	ctx := m.ctx
	return func() tea.Msg {
		ids := make([]int, len(tracks))
		for i := range tracks {
			ids[i] = tracks[i].ID
		}
		if err := client.AddTracksToPlaylist(ctx, uuid, ids); err != nil {
			return errMsg(err)
		}
		return queueSavedMsg{uuid: uuid, name: name, count: len(ids)}
	}
}

// toastCmd flashes a green confirmation and schedules its clearing.
func toastClearCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return clearToastMsg{} })
}

// insertTrack returns s with t inserted at index i.
func insertTrack(s []tidal.Track, i int, t tidal.Track) []tidal.Track {
	s = append(s, tidal.Track{})
	copy(s[i+1:], s[i:])
	s[i] = t
	return s
}
