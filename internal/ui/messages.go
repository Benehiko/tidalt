package ui

import (
	"image"
	"time"

	"github.com/Benehiko/tidalt/v4/internal/mpris"
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
	errMsg      error
	clearErrMsg struct{}
	// queueSavedMsg confirms the queue was saved as / appended to a playlist.
	queueSavedMsg struct {
		uuid  string
		name  string
		count int
	}
	clearToastMsg struct{}
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
