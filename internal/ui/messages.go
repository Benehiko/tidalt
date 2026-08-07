package ui

import (
	"image"
	"time"

	"github.com/Benehiko/tidalt/v4/internal/mpris"
	"github.com/Benehiko/tidalt/v4/internal/spotify"
	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// tea.Msg types delivered to Model.Update. Grouped here so the message contract
// is in one place as the UI grows.
type (
	tracksMsg          []tidal.Track
	favoritesLoadedMsg []tidal.Track
	mixesMsg           []tidal.Mix
	searchResultsMsg   []tidal.Track
	searchGroupedMsg   tidal.SearchResults
	// Library section data.
	playlistsMsg  []tidal.Playlist
	favArtistsMsg []tidal.Artist
	favAlbumsMsg  []tidal.Album
	// playlistDetailMsg carries the tracks of an opened playlist for the
	// Playlists detail pane.
	playlistDetailMsg struct {
		uuid   string
		title  string
		tracks []tidal.Track
	}
	openURLTracksMsg  []tidal.Track // tracks resolved from a startup tidal:// URL
	cachedPlaylistMsg []tidal.Track // playlist restored from bbolt on startup
	historyLoadedMsg  []tidal.Track // recently-played restored from bbolt on startup
	// artistAlbumsMsg carries an artist's discography after the user opens the
	// artist view with "a".
	artistAlbumsMsg struct {
		artistID   int
		artistName string
		albums     []tidal.Album
	}
	// artistAlbumTracksMsg carries the tracks of an album opened inside the
	// artist drill-down (shown as a sub-list before loading into the queue).
	artistAlbumTracksMsg struct {
		album  tidal.Album
		tracks []tidal.Track
	}
	errMsg      error
	clearErrMsg struct{}
	// queueSavedMsg confirms the queue was saved as / appended to a playlist.
	queueSavedMsg struct {
		uuid  string
		name  string
		count int
	}
	clearToastMsg struct{}
	// spotifyResolvedMsg carries the result of resolving + Tidal-matching a
	// pasted Spotify URL. On success src is set and rows holds one entry per
	// source track (match nil ⇒ "not available"); on failure err is set.
	spotifyResolvedMsg struct {
		src  *spotify.Source
		rows []importRow
		err  error
	}
	tickMsg       time.Time
	barTickMsg    time.Time
	nowPlayingMsg struct {
		done  <-chan struct{}
		track *tidal.Track // refreshed track metadata (may be nil)
		gen   uint64       // skip generation that spawned this command
	}
	trackDoneMsg struct {
		gen uint64
	}
	// skipErrMsg is returned when a track cannot be streamed (e.g. no FLAC
	// available). It shows a transient error and auto-advances the queue.
	skipErrMsg struct {
		err error
		gen uint64
	}
	// playbackFailedMsg is returned when the player refuses to start playback
	// (e.g. the ALSA device could not be claimed, or the previous playback
	// loop is still shutting down). doPlayTrack marks the track as playing
	// synchronously, before the async body ever calls into the player, so this
	// must roll that state back — otherwise the header shows the track as
	// Playing with a ticking progress bar while nothing plays, and no
	// trackDoneMsg ever arrives to advance the queue.
	//
	// Unlike skipErrMsg this deliberately does not auto-advance: the failure is
	// with the audio device rather than the track, so the next track would fail
	// the same way and walking the queue would just spam the device.
	playbackFailedMsg struct {
		err error
		gen uint64
	}
	// playerPausedMsg reports that the player forced itself back into the
	// paused state (a failed ALSA reacquire on resume). The UI drives
	// play/pause optimistically, so it must resync or every later press does
	// the opposite of its label.
	playerPausedMsg struct {
		err error
	}
	mprisMsg    mpris.Event
	favoriteMsg struct {
		trackID int
		added   bool
	}
	// parentStateMsg carries the live state polled from the parent instance.
	parentStateMsg mpris.PlayerState
	// coverLoadedMsg delivers a fetched album cover image.
	coverLoadedMsg struct {
		key string
		img image.Image
	}
	// playPlaylistMsg asks the server model to replace its queue and start
	// playing from the given index. Produced by CmdPlayPlaylist handling.
	playPlaylistMsg struct {
		tracks     []tidal.Track
		startIndex int
	}
	// playNextMsg is sent after a short delay when auto-advancing past a track
	// that failed to stream, to avoid hammering the API.
	playNextMsg struct {
		track tidal.Track
		gen   uint64
	}
)
