package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Benehiko/tidalt/v4/internal/logger"
	"github.com/Benehiko/tidalt/v4/internal/mpris"
	"github.com/Benehiko/tidalt/v4/internal/player"
	"github.com/Benehiko/tidalt/v4/internal/spotify"
	"github.com/Benehiko/tidalt/v4/internal/store"
	"github.com/Benehiko/tidalt/v4/internal/tidal"
)

// ShuffleMode controls how the track list is shuffled.
type ShuffleMode int

const (
	ShuffleOff         ShuffleMode = iota // original order
	ShuffleRandom                         // random pick on each advance
	ShuffleFisherYates                    // pre-shuffled with Fisher-Yates
)

func (s ShuffleMode) String() string {
	switch s {
	case ShuffleRandom:
		return "Random"
	case ShuffleFisherYates:
		return "Shuffle"
	default:
		return "Off"
	}
}

// Overlay is the active modal layer over the main view. OverlayNone means the
// section pane (or sidebar) has focus.
type Overlay int

const (
	OverlayNone Overlay = iota
	OverlayCommandPalette
	OverlayActionSheet
	OverlayDeviceSelect
	OverlayAddToPlaylist
	OverlayImportSpotify
)

//nolint:recvcheck // tea.Model requires value-receiver Init/Update/View; helper methods mutate via pointer receiver
type Model struct {
	//nolint:containedctx // the long-lived TUI model holds the app context for command goroutines
	ctx    context.Context
	client *tidal.Client
	store  *store.SecretsStore
	player *player.Player

	// Navigation: which sidebar Section is active, the active modal Overlay,
	// whether focus is on the main pane (vs. the sidebar), and the sidebar nav
	// cursor. prevSection records where to return after the artist drill-down.
	section       Section
	overlay       Overlay
	focusMain     bool
	sidebarCursor int
	prevSection   Section

	// Action sheet overlay state.
	sheetTrack  *tidal.Track
	sheetCursor int

	// Command palette overlay state.
	paletteInput  textinput.Model
	paletteCursor int

	// Spotify-import overlay state: the URL input, the resolved source, the
	// matched/"not available" rows, the flow stage, and the list cursor.
	importInput  textinput.Model
	importSource *spotify.Source
	importRows   []importRow
	importStage  importStage
	importCursor int
	importError  string

	// Theme picker (Settings section) cursor; previewPalette (below) holds the
	// live-previewed scheme while the cursor moves.
	themeCursor int

	errText string // transient error shown in status bar; cleared after display

	// Data
	tracks []tidal.Track
	mixes  []tidal.Mix
	cursor int

	// Search — own list so results don't clobber My Music. searchResults holds
	// the grouped multi-category results; searchCursor indexes the flattened
	// list of selectable result rows (see searchRows).
	searchInput   textinput.Model
	searchTracks  []tidal.Track // legacy flat track list (still used by URL resolver path)
	searchResults tidal.SearchResults
	searchCursor  int
	searchLoading bool

	// Artist view — the selected artist's albums plus two synthetic quick-play
	// rows ("Play all tracks", "Top tracks"). Reached from the action sheet.
	artistID      int // 0 = none
	artistName    string
	artistAlbums  []tidal.Album
	artistCursor  int
	artistLoading bool
	showArtist    bool // true while the transient artist drill-down is open
	// Album drill-down within the artist view: when an album is opened its
	// tracks are listed here (artistAlbum != nil) before being loaded to queue.
	artistAlbum       *tidal.Album
	artistAlbumTracks []tidal.Track
	artistAlbumCursor int

	// Library data loaded on demand for the favorites/playlists sections.
	favSongs   []tidal.Track // the favorite-songs list (SecFavSongs)
	playlists  []tidal.Playlist
	favArtists []tidal.Artist
	favAlbums  []tidal.Album
	history    []tidal.Track

	// Playlists detail pane: which playlist is open and its tracks. The index
	// column uses m.cursor; detailCursor indexes the open playlist's tracks and
	// detailFocus toggles between the index (false) and detail (true) columns.
	openPlaylist *tidal.Playlist
	playlistName string
	detailTracks []tidal.Track
	detailCursor int
	detailFocus  bool

	// Hybrid queue/playlist model. queueSource describes the queue's origin
	// ("playlist:<name>", "radio", or ""); queuePlaylistUUID is the saved
	// playlist it was loaded from (if any); queueDirty marks unsaved edits.
	queueSource       string
	queuePlaylistUUID string
	queueDirty        bool
	// pendingQueueSource is applied to queueSource when the next tracksMsg
	// lands (e.g. set to "radio" before a GetTrackRadio request resolves).
	pendingQueueSource string

	// toast is a transient green confirmation flash, cleared after a delay.
	toast string

	// Terminal size
	width  int
	height int

	// Device selection
	devices       []player.DeviceInfo
	currentDevice string // hw device string, "" = auto-detect

	// Player UI
	currentTrack   *tidal.Track
	currentQuality tidal.Quality // granted stream quality tier for currentTrack
	// activeDevice is the ALSA device actually opened, which differs from
	// currentDevice when the plughw: fallback engaged. bitPerfect is false in
	// that case: ALSA's plug layer is resampling/remixing, so the quality
	// badge and device label must not claim untouched output.
	activeDevice string
	bitPerfect   bool
	volume       float64
	isPlaying    bool
	advancing    bool   // true while auto-advancing to next track; suppresses re-trigger
	skipGen      uint64 // monotonic counter; incremented on every doPlayTrack call
	progress     progress.Model
	currPos      float64
	duration     float64

	// Logo animation
	logoFrame int
	barFrame  int // advances on the faster bar tick

	// barHeights holds the current smoothed height (×10 for sub-integer motion)
	// and target height for each equaliser bar.
	barHeights [9]int // current height scaled ×10
	barTargets [9]int // target height scaled ×10
	// barsTicking is true while the fast (80ms) equaliser tick is scheduled. It
	// lapses when playback stops and is restarted by the 1s tick on resume, so
	// the UI doesn't re-render 12×/s while idle.
	barsTicking bool

	// MPRIS media key commands (nil in client mode)
	mprisCh <-chan mpris.Event

	// Favorited track IDs (populated from GetFavorites; toggled by "f")
	favorites map[int]bool

	// Shuffle
	shuffleMode   ShuffleMode
	tracksOrder   []tidal.Track // original order, saved when shuffle is enabled
	shufflePlayed []int         // indices already played (for ShuffleRandom deduplication)

	// openURL is a tidal:// or https://tidal.com/ URL passed at startup (e.g. from
	// "Open in desktop app"). Consumed once during Init.
	openURL string

	// clientMode is true when a parent tidalt instance is already running.
	// Playback commands are forwarded over D-Bus instead of driving the local player.
	clientMode  bool
	mprisClient *mpris.Client

	// localPlaylist is true when the client has loaded a playlist locally
	// (from a mix, radio, or URL) that has not yet been sent to the server.
	// While true, parentStateMsg will not overwrite m.tracks so the user can
	// browse the local list freely before committing it to the server.
	localPlaylist bool

	// mprisServer is non-nil in normal mode; used to push live state to clients.
	mprisServer *mpris.Server

	// playlistRestored is true when a playlist was loaded from the bbolt cache
	// on startup. Prevents favoritesLoadedMsg from overwriting the restored list.
	playlistRestored bool

	// restorePosition is the playback position (seconds) to seek to when the
	// next track starts. Set from the persisted last position on startup,
	// consumed once by nowPlayingMsg.
	restorePosition float64

	// Cover art panel.
	coverImage    image.Image // nil while loading or unavailable
	coverCacheKey string      // UUID of the currently displayed cover

	// kittySupported is set once at startup; when true the Now-Playing cover is
	// drawn with the Kitty graphics protocol (overlaid in View at absolute
	// coordinates), otherwise Unicode block art is used.
	kittySupported bool

	// kitty caches the expensive PNG-encode + tracks what was last emitted so
	// the image escape is only re-encoded/re-sent on a real change (new cover
	// or resize), not on every animation frame. Pointer-backed so the
	// value-receiver View can update it.
	kitty *kittyState

	// Theme / color scheme.
	// themeName is the registry key persisted to the store; palette is the
	// resolved Palette; theme holds the styles derived from it. previewPalette
	// is non-nil only while the theme picker is live-previewing a scheme that
	// has not yet been committed — View renders through it when set.
	themeName      string
	palette        Palette
	theme          Theme
	previewPalette *Palette
}

// loadTheme resolves the persisted theme name (or the default) into the model's
// palette/theme fields. Called from the constructors.
func loadTheme(s *store.SecretsStore) (name string, pal Palette, th Theme) {
	name = defaultThemeName
	if saved, err := s.LoadTheme(); err == nil && saved != "" {
		name = saved
	}
	pal = resolvePalette(name)
	return name, pal, pal.Theme()
}

// activeTheme returns the theme View should render with: the live preview while
// the picker is open, otherwise the committed theme. In client mode the focus
// accent is overridden with a steel-blue tint so the instance reads as distinct.
func (m *Model) activeTheme() Theme {
	pal := m.palette
	if m.previewPalette != nil {
		pal = *m.previewPalette
	}
	if m.clientMode {
		return clientTint(pal).Theme()
	}
	if m.previewPalette != nil {
		return pal.Theme()
	}
	return m.theme
}

func InitialModel(ctx context.Context, client *tidal.Client, s *store.SecretsStore, srv *mpris.Server, openURL string) Model {
	ti := textinput.New()
	ti.Placeholder = "Search for a song..."
	ti.CharLimit = 156
	ti.Width = 30

	p := player.NewPlayer()

	vol := 50.0
	if v, err := s.LoadVolume(); err == nil {
		vol = v
	}
	_ = p.SetVolume(vol)

	currentDevice := ""
	if dev, err := s.LoadDevice(); err == nil {
		currentDevice = dev
		p.SetDevice(dev)
	}

	var mprisCh <-chan mpris.Event
	if srv != nil {
		mprisCh = srv.Commands
	}

	themeName, palette, theme := loadTheme(s)

	return Model{
		ctx:            ctx,
		client:         client,
		store:          s,
		player:         p,
		searchInput:    ti,
		section:        SecQueue,
		focusMain:      true,
		volume:         vol,
		currentDevice:  currentDevice,
		bitPerfect:     true,
		progress:       progressWithTheme(theme, 40),
		mprisCh:        mprisCh,
		favorites:      make(map[int]bool),
		openURL:        openURL,
		mprisServer:    srv,
		kittySupported: KittySupported(),
		themeName:      themeName,
		palette:        palette,
		theme:          theme,
	}
}

// ClientModel creates a TUI model that forwards all playback actions to an
// already-running tidalt instance via the provided mprisClient. The local
// player is not started. The UI is tinted to indicate client mode.
func ClientModel(ctx context.Context, client *tidal.Client, s *store.SecretsStore, mprisClient *mpris.Client, openURL string) Model {
	ti := textinput.New()
	ti.Placeholder = "Search for a song..."
	ti.CharLimit = 156
	ti.Width = 30

	p := player.NewPlayer()

	vol := 50.0
	if v, err := s.LoadVolume(); err == nil {
		vol = v
	}
	_ = p.SetVolume(vol)

	currentDevice := ""
	if dev, err := s.LoadDevice(); err == nil {
		currentDevice = dev
		p.SetDevice(dev)
	}

	themeName, palette, theme := loadTheme(s)

	return Model{
		ctx:            ctx,
		client:         client,
		store:          s,
		player:         p,
		searchInput:    ti,
		section:        SecQueue,
		focusMain:      true,
		volume:         vol,
		currentDevice:  currentDevice,
		bitPerfect:     true,
		progress:       progressWithTheme(clientTint(palette).Theme(), 40),
		favorites:      make(map[int]bool),
		openURL:        openURL,
		clientMode:     true,
		mprisClient:    mprisClient,
		kittySupported: KittySupported(),
		themeName:      themeName,
		palette:        palette,
		theme:          theme,
	}
}

// applyShuffle reorders m.tracks according to the current shuffle mode and
// resets the played-index history. Call whenever the mode changes or a new
// track list is loaded.
func (m *Model) applyShuffle() {
	switch m.shuffleMode {
	case ShuffleFisherYates:
		shuffled := make([]tidal.Track, len(m.tracksOrder))
		copy(shuffled, m.tracksOrder)
		rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		m.tracks = shuffled
	default:
		m.tracks = make([]tidal.Track, len(m.tracksOrder))
		copy(m.tracks, m.tracksOrder)
	}
	m.shufflePlayed = nil
}

// prevIndex returns the index of the previously played track. In shuffle
// modes it pops from the play history stack; otherwise it returns cursor-1.
// Returns -1 if there is no previous track.
func (m *Model) prevIndex() int {
	if len(m.tracks) == 0 {
		return -1
	}
	if len(m.shufflePlayed) > 0 {
		prev := m.shufflePlayed[len(m.shufflePlayed)-1]
		m.shufflePlayed = m.shufflePlayed[:len(m.shufflePlayed)-1]
		if prev >= 0 && prev < len(m.tracks) {
			return prev
		}
	}
	prev := m.cursor - 1
	if prev >= 0 {
		return prev
	}
	return -1
}

// nextIndex returns the index of the next track to play given the current
// cursor and shuffle mode. Returns -1 if there is no next track.
func (m *Model) nextIndex() int {
	if len(m.tracks) == 0 {
		return -1
	}
	switch m.shuffleMode {
	case ShuffleRandom:
		// Pick a random index that has not been played yet.
		played := make(map[int]bool, len(m.shufflePlayed))
		for _, i := range m.shufflePlayed {
			played[i] = true
		}
		// Build candidate list.
		var candidates []int
		for i := range m.tracks {
			if !played[i] {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 0 {
			return -1
		}
		return candidates[rand.IntN(len(candidates))] //nolint:gosec // G404: playlist shuffle does not need crypto-grade randomness
	default:
		// ShuffleOff and ShuffleFisherYates both advance linearly through
		// the (possibly pre-shuffled) slice.
		next := m.cursor + 1
		if next < len(m.tracks) {
			return next
		}
		return -1
	}
}

// playTrackCmd returns a tea.Cmd that starts playback of track.
// In normal mode it streams via the local player and returns nowPlayingMsg.
// In client mode it resolves the stream URL and forwards it to the parent
// instance via MPRIS, then returns nil (no local playback state to track).
func (m *Model) playTrackCmd(track tidal.Track) tea.Cmd {
	return m.doPlayTrack(track, m.player.Play)
}

// playNextTrackCmd is like playTrackCmd but uses PlayNext to transition
// without closing the ALSA device, avoiding pops between playlist tracks.
func (m *Model) playNextTrackCmd(track tidal.Track) tea.Cmd {
	return m.doPlayTrack(track, m.player.PlayNext)
}

func (m *Model) doPlayTrack(track tidal.Track, playFn func(string) (<-chan struct{}, error)) tea.Cmd {
	if m.clientMode {
		mc := m.mprisClient
		if m.localPlaylist && len(m.tracks) > 0 {
			// Find the index of this track in the local playlist.
			idx := 0
			for i := range m.tracks {
				if m.tracks[i].ID == track.ID {
					idx = i
					break
				}
			}
			tracksJSON := mpris.MarshalTracks(m.tracks)
			m.localPlaylist = false
			return func() tea.Msg {
				if err := mc.SendPlaylist(tracksJSON, idx); err != nil {
					return errMsg(err)
				}
				return nil
			}
		}
		trackID := track.ID
		return func() tea.Msg {
			if err := mc.SendTrackID(trackID); err != nil {
				return errMsg(err)
			}
			return nil
		}
	}
	m.currentTrack = &track
	m.isPlaying = true
	m.skipGen++
	m.advancing = true // suppresses any stale trackDoneMsg until nowPlayingMsg resets it
	m.restorePosition = 0
	m.currPos = 0
	_ = m.store.SaveLastTrackID(track.ID)
	gen := m.skipGen
	ctx := m.ctx
	client := m.client
	return func() tea.Msg {
		// Fetch fresh track metadata in parallel with the stream URL so we
		// always have a cover UUID even when the cached entry predates cover support.
		type freshResult struct {
			track *tidal.Track
			err   error
		}
		freshCh := make(chan freshResult, 1)
		if track.Album.Cover == "" {
			go func() {
				t, err := client.GetTrack(ctx, strconv.Itoa(track.ID))
				freshCh <- freshResult{t, err}
			}()
		} else {
			freshCh <- freshResult{&track, nil}
		}

		info, err := client.GetStreamURL(ctx, track.ID)
		if err != nil {
			logger.L.Error("GetStreamURL failed", "trackID", track.ID, "err", err)
			return skipErrMsg{err: err, gen: gen}
		}
		logger.L.Info("stream resolved", "trackID", track.ID, "ext", info.Ext, "quality", info.Quality)
		done, err := playFn(info.URL)
		if err != nil {
			logger.L.Error("playFn failed", "trackID", track.ID, "err", err)
			return playbackFailedMsg{err: err, gen: gen}
		}
		logger.L.Debug("doPlayTrack: playFn returned, calling SetDuration", "trackID", track.ID, "trackDuration", track.Duration)
		if track.Duration > 0 {
			m.player.SetDuration(float64(track.Duration))
		}

		res := <-freshCh
		if res.err == nil && res.track != nil {
			return nowPlayingMsg{done: done, track: res.track, gen: gen, quality: info.Quality}
		}
		return nowPlayingMsg{done: done, gen: gen, quality: info.Quality}
	}
}

// tidalURLID extracts the last path segment (before any query string) from a
// URL string, which is the resource ID for Tidal API calls.
func tidalURLID(rawURL string) string {
	// Strip query string.
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	parts := strings.Split(strings.TrimRight(rawURL, "/"), "/")
	return parts[len(parts)-1]
}

// resolveQuery turns a search query into a list of tracks.
// It handles:
//   - tidal:// deep-link URLs (tidal://track/ID, tidal://album/ID, tidal://mix/ID)
//   - Tidal web URLs         (tidal.com/browse/track/ID, etc.)
//   - Plain text             (title, artist, album — Tidal search covers all three)
//
// For plain-text queries the store cache is checked first to avoid redundant
// API calls. Pass nil for s to skip caching.
func resolveQuery(ctx context.Context, client *tidal.Client, s *store.SecretsStore, query string) ([]tidal.Track, error) {
	// Normalise tidal:// deep links to a recognisable path so the checks below work.
	// tidal://track/12345  →  tidal.com/track/12345
	// tidal://album/67890  →  tidal.com/album/67890
	// tidal://mix/abcdef   →  tidal.com/mix/abcdef
	if after, ok := strings.CutPrefix(query, "tidal://"); ok {
		query = "tidal.com/" + after
	}

	if strings.Contains(query, "tidal.com/") && strings.Contains(query, "/track/") {
		track, err := client.GetTrack(ctx, tidalURLID(query))
		if err != nil {
			return nil, err
		}
		return []tidal.Track{*track}, nil
	}
	if strings.Contains(query, "tidal.com/") && strings.Contains(query, "/album/") {
		return client.GetAlbumTracks(ctx, tidalURLID(query))
	}
	if strings.Contains(query, "tidal.com/") && strings.Contains(query, "/mix/") {
		return client.GetMixTracks(ctx, tidalURLID(query))
	}

	// Spotify URL: resolve the source and match each track on Tidal, returning
	// the matched Tidal tracks. Unmatched tracks are dropped on this quick path
	// (the import overlay shows them as "not available").
	if spotify.IsSpotifyURL(query) {
		src, err := spotify.Resolve(ctx, spotifyHTTPClient(), query)
		if err != nil {
			return nil, err
		}
		rows := matchSpotifyTracks(ctx, client, src.Tracks)
		return matchedTracks(rows), nil
	}

	// Plain-text search — check cache first.
	if s != nil {
		var cached []tidal.Track
		if found, err := s.LoadSearchResults(query, &cached); err == nil && found {
			return cached, nil
		}
	}
	tracks, err := client.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	if s != nil {
		_ = s.CacheSearchResults(query, tracks)
	}
	return tracks, nil
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func barTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return barTickMsg(t)
	})
}

// ensureBarsTicking returns the command to (re)start the fast equaliser tick if
// it has lapsed, marking it running. Returns nil when it is already scheduled.
func (m *Model) ensureBarsTicking() []tea.Cmd {
	if m.barsTicking {
		return nil
	}
	m.barsTicking = true
	return []tea.Cmd{barTickCmd()}
}

// waitForTrackDone returns a command that blocks until the given done channel
// is closed (i.e. the track finished naturally), then sends a trackDoneMsg.
// Callers should pass the channel returned by player.Play() directly so there
// is no race between stop() clearing the old channel and Play() setting a new one.
func waitForTrackDone(done <-chan struct{}, gen uint64) tea.Cmd {
	return func() tea.Msg {
		<-done
		return trackDoneMsg{gen: gen}
	}
}

// watchPlayerPaused returns a command that blocks until the player reports it
// forced itself back into the paused state, then sends a playerPausedMsg. The
// handler re-registers it so the stream continues for the session's lifetime.
// In client mode there is no local player to watch.
func (m Model) watchPlayerPaused() tea.Cmd {
	if m.player == nil {
		return nil
	}
	ch := m.player.PausedEvents()
	return func() tea.Msg {
		err, ok := <-ch
		if !ok {
			return nil
		}
		return playerPausedMsg{err: err}
	}
}

// listenMPRIS returns a command that blocks until the next MPRIS event
// arrives, then re-registers itself so the stream continues.
func listenMPRIS(ch <-chan mpris.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			// Channel closed — MPRIS server shut down; stop listening.
			return nil
		}
		return mprisMsg(ev)
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		// Restore persisted playlist before the API call for favorites completes.
		func() tea.Msg {
			if m.store == nil {
				return nil
			}
			var cached []tidal.Track
			if err := m.store.LoadPlaylist(&cached); err == nil && len(cached) > 0 {
				return cachedPlaylistMsg(cached)
			}
			return nil
		},
		// Restore the recently-played history persisted last session.
		func() tea.Msg {
			if m.store == nil {
				return nil
			}
			var hist []tidal.Track
			if err := m.store.LoadHistory(&hist); err == nil && len(hist) > 0 {
				return historyLoadedMsg(hist)
			}
			return nil
		},
		func() tea.Msg {
			tracks, err := m.client.GetFavorites(m.ctx, 50)
			if err != nil {
				return errMsg(err)
			}
			return favoritesLoadedMsg(tracks)
		},
		func() tea.Msg {
			mixes, err := m.client.GetMixes(m.ctx)
			if err != nil {
				return errMsg(err)
			}
			return mixesMsg(mixes)
		},
		// Prefetch the library sections so the sidebar counts are correct at
		// launch instead of showing 0 until the section is first opened.
		func() tea.Msg {
			pls, err := m.client.GetUserPlaylists(m.ctx)
			if err != nil {
				return errMsg(err)
			}
			return playlistsMsg(pls)
		},
		func() tea.Msg {
			artists, err := m.client.GetFavoriteArtists(m.ctx, 200)
			if err != nil {
				return errMsg(err)
			}
			return favArtistsMsg(artists)
		},
		func() tea.Msg {
			albums, err := m.client.GetFavoriteAlbums(m.ctx, 200)
			if err != nil {
				return errMsg(err)
			}
			return favAlbumsMsg(albums)
		},
		m.waitForContextCancel(),
		tickCmd(),
		// The fast equaliser tick is started on demand by the 1s tick / playback
		// start (ensureBarsTicking) so an idle UI doesn't re-render 12×/s.
	}
	if !m.clientMode {
		cmds = append(cmds, listenMPRIS(m.mprisCh))
		if c := m.watchPlayerPaused(); c != nil {
			cmds = append(cmds, c)
		}
	} else {
		cmds = append(cmds, pollParentState(m.mprisClient))
	}
	if m.openURL != "" {
		u := m.openURL
		cmds = append(cmds, func() tea.Msg {
			tracks, err := resolveQuery(m.ctx, m.client, m.store, u)
			if err != nil {
				return errMsg(err)
			}
			return openURLTracksMsg(tracks)
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) waitForContextCancel() tea.Cmd {
	return func() tea.Msg {
		<-m.ctx.Done()
		if m.player != nil {
			m.player.Close()
		}
		if m.store != nil {
			m.store.Close()
		}
		return tea.Quit()
	}
}

// pushState publishes the current track and playlist to the MPRIS server so
// client instances can read it. Called whenever playback state changes.
func (m *Model) pushState() {
	if m.mprisServer == nil {
		return
	}
	m.mprisServer.SetState(mpris.PlayerState{
		CurrentTrackJSON: mpris.MarshalTracks(m.currentTrack),
		PlaylistJSON:     mpris.MarshalTracks(m.tracks),
		Position:         m.currPos,
		Duration:         m.duration,
		Volume:           m.volume,
		Device:           m.currentDevice,
		ShuffleMode:      m.shuffleMode.String(),
		Quality:          string(m.currentQuality),
		ActiveDevice:     m.activeDevice,
		BitPerfect:       m.bitPerfect,
	}, m.isPlaying)
}

// pollParentState returns a tea.Cmd that fetches the parent's state once and
// delivers it as a parentStateMsg. Used by the client TUI on each tick.
func pollParentState(mc *mpris.Client) tea.Cmd {
	return func() tea.Msg {
		ps, err := mc.GetState()
		if err != nil {
			// Parent may have gone away; deliver an empty state rather than an error.
			return parentStateMsg{}
		}
		return parentStateMsg(ps)
	}
}

// fetchCoverCmd downloads the album cover for the given track and delivers a
// coverLoadedMsg. It is a no-op when cover is empty.
func fetchCoverCmd(cover string) tea.Cmd {
	if cover == "" {
		return nil
	}
	return func() tea.Msg {
		img, err := fetchCoverImage(tidal.CoverURL(cover, "640x640"))
		if err != nil {
			return errMsg(err)
		}
		return coverLoadedMsg{key: cover, img: img}
	}
}

// maybeUpdateCover checks whether the cover for t differs from the currently
// cached key. If so it clears the cached image and returns a fetch command.
// If the key is the same (fetch already in-flight or image already loaded),
// it does nothing. Call this whenever m.currentTrack changes.
func (m *Model) maybeUpdateCover(t *tidal.Track) tea.Cmd {
	if t == nil {
		return nil
	}
	cover := t.Album.Cover
	if cover == "" || cover == m.coverCacheKey {
		return nil
	}
	m.coverImage = nil
	m.coverCacheKey = cover
	return fetchCoverCmd(cover)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildProgress()

	case nowPlayingMsg:
		if msg.gen != m.skipGen {
			return m, nil // stale — a newer doPlayTrack superseded this one
		}
		m.advancing = false
		m.currentQuality = msg.quality
		// Update currentTrack with refreshed metadata (includes cover UUID).
		if msg.track != nil {
			m.currentTrack = msg.track
			_ = m.store.CacheTrack(msg.track.ID, *msg.track)
		}
		// Reset position and seed duration from track metadata so the progress
		// bar is correct before the first tick fires.
		if m.restorePosition == 0 {
			m.currPos = 0
		}
		if m.currentTrack != nil && m.currentTrack.Duration > 0 {
			m.duration = float64(m.currentTrack.Duration)
		}
		logger.L.Debug("nowPlayingMsg: seeded progress",
			"trackID", func() int {
				if m.currentTrack != nil {
					return m.currentTrack.ID
				}
				return 0
			}(),
			"currPos", m.currPos,
			"duration", m.duration,
			"restorePosition", m.restorePosition,
		)
		m.pushState()
		if m.currentTrack != nil {
			m.recordHistory(*m.currentTrack)
		}
		if m.restorePosition > 0 {
			pos := m.restorePosition
			m.restorePosition = 0
			_ = m.player.Seek(pos)
		}
		bars := m.ensureBarsTicking()
		cmds := make([]tea.Cmd, 0, 2+len(bars))
		cmds = append(cmds, waitForTrackDone(msg.done, msg.gen), m.maybeUpdateCover(m.coverTrack()))
		cmds = append(cmds, bars...)
		return m, tea.Batch(cmds...)

	case coverLoadedMsg:
		if msg.key == m.coverCacheKey {
			m.coverImage = msg.img
		}

	case barTickMsg:
		m.barFrame++
		updateBars(m.barFrame, &m.barHeights, &m.barTargets, m.isPlaying)
		// The fast equaliser animation only needs to run while playing — when
		// stopped the bars are flat and re-rendering 12×/s is wasted work. Drop
		// to the 1s logo tick when idle; the 1s tick restarts this one when
		// playback resumes.
		if m.isPlaying {
			return m, barTickCmd()
		}
		m.barsTicking = false
		return m, nil

	case tickMsg:
		m.logoFrame++
		if m.isPlaying && !m.clientMode {
			m.currPos, _ = m.player.GetPosition()
			m.duration, _ = m.player.GetDuration()
			// Track the device actually opened: the plughw: fallback can
			// engage mid-session (e.g. on resume), and the badge/device
			// label must stop claiming bit-perfect output when it does.
			m.activeDevice, m.bitPerfect = m.player.AudioPath()
			logger.L.Debug("tick: progress state",
				"currPos", m.currPos,
				"duration", m.duration,
				"isPlaying", m.isPlaying,
			)
			_ = m.store.SaveLastPosition(m.currPos)
			m.pushState()
		}
		cmds := []tea.Cmd{tickCmd()}
		// Restart the fast equaliser tick if playback resumed while it was idle
		// (covers client-mode playback and MPRIS-driven resume).
		if m.isPlaying {
			cmds = append(cmds, m.ensureBarsTicking()...)
		}
		if m.clientMode {
			cmds = append(cmds, pollParentState(m.mprisClient))
		}
		return m, tea.Batch(cmds...)

	case parentStateMsg:
		ps := mpris.PlayerState(msg)
		// Update current track from parent.
		var coverCmd tea.Cmd
		if ps.CurrentTrackJSON != "" {
			var t tidal.Track
			if err := json.Unmarshal([]byte(ps.CurrentTrackJSON), &t); err == nil {
				m.currentTrack = &t
				coverCmd = m.maybeUpdateCover(m.coverTrack())
			}
		} else {
			m.currentTrack = nil
		}
		// Update playlist ("My Music") from parent if non-empty.
		// Skip when the user has loaded a local playlist they haven't sent yet.
		if !m.localPlaylist && ps.PlaylistJSON != "" && ps.PlaylistJSON != "null" {
			var tracks []tidal.Track
			if err := json.Unmarshal([]byte(ps.PlaylistJSON), &tracks); err == nil && len(tracks) > 0 {
				// Only replace the list when it actually changed to avoid
				// clobbering the cursor position on every tick.
				if len(tracks) != len(m.tracks) || (len(tracks) > 0 && tracks[0].ID != m.tracks[0].ID) {
					m.tracksOrder = tracks
					m.applyShuffle()
					// Don't yank the user out of a view they're actively browsing
					// (search results, artist view, device select). The updated
					// playlist is still applied underneath, so it's there when they
					// Tab back to Browse.
				}
			}
		}
		m.isPlaying = ps.PlaybackStatus == "Playing"
		m.currPos = ps.Position
		m.duration = ps.Duration
		if ps.Volume > 0 {
			m.volume = ps.Volume
		}
		if ps.Device != "" {
			m.currentDevice = ps.Device
		}
		// Mirror the parent's audio path so the client's badge and device
		// label describe the stream the parent is actually rendering.
		m.currentQuality = tidal.Quality(ps.Quality)
		m.activeDevice = ps.ActiveDevice
		m.bitPerfect = ps.BitPerfect
		if ps.ShuffleMode != "" {
			switch ps.ShuffleMode {
			case "Random":
				m.shuffleMode = ShuffleRandom
			case "Shuffle":
				m.shuffleMode = ShuffleFisherYates
			default:
				m.shuffleMode = ShuffleOff
			}
		}
		if coverCmd != nil {
			return m, coverCmd
		}

	case skipErrMsg:
		if msg.gen != m.skipGen {
			break
		}
		m.errText = msg.err.Error()
		m.advancing = false
		m.shufflePlayed = append(m.shufflePlayed, m.cursor)
		next := m.nextIndex()
		if next >= 0 {
			m.advancing = true
			m.cursor = next
			track := m.tracks[next]
			m.currPos = 0
			m.duration = 0
			_ = m.store.CacheTrack(track.ID, track)
			// Delay before retrying to avoid hammering the API when many
			// consecutive tracks fail (e.g. during rate-limiting).
			return m, tea.Batch(
				tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return playNextMsg{track: track, gen: m.skipGen}
				}),
				tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} }),
			)
		}
		m.isPlaying = false
		m.currentQuality = ""
		m.pushState()
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })

	case playbackFailedMsg:
		if msg.gen != m.skipGen {
			break // stale — a newer skip superseded this attempt
		}
		// Roll back the optimistic state doPlayTrack set before calling the
		// player, so the UI stops claiming a track is playing.
		m.errText = msg.err.Error()
		m.advancing = false
		m.isPlaying = false
		m.currentTrack = nil
		m.currPos = 0
		m.duration = 0
		m.pushState()
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })

	case playerPausedMsg:
		// The player paused itself; adopt its state rather than our own guess.
		m.isPlaying = false
		if msg.err != nil {
			m.errText = msg.err.Error()
		}
		m.pushState()
		return m, tea.Batch(
			m.watchPlayerPaused(),
			tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} }),
		)

	case playNextMsg:
		if msg.gen != m.skipGen {
			break // stale — a newer skip superseded this delayed advance
		}
		cmd := m.playNextTrackCmd(msg.track)
		return m, cmd

	case trackDoneMsg:
		if msg.gen != m.skipGen {
			break // stale — from a track that was already skipped past
		}
		if !m.advancing {
			m.shufflePlayed = append(m.shufflePlayed, m.cursor)
			next := m.nextIndex()
			if next >= 0 {
				m.advancing = true
				m.cursor = next
				track := m.tracks[next]
				m.currPos = 0
				m.duration = 0
				_ = m.store.CacheTrack(track.ID, track)
				cmd := m.playNextTrackCmd(track)
				return m, cmd
			}
			m.isPlaying = false
			// The queue is exhausted; drop the granted tier so the badge
			// doesn't linger describing the track that just finished.
			m.currentQuality = ""
			m.pushState()
		}

	case cachedPlaylistMsg:
		if len(msg) > 0 {
			m.tracksOrder = msg
			m.applyShuffle()
			m.playlistRestored = true
			if lastID, err := m.store.LoadLastTrackID(); err == nil && lastID != 0 {
				for i := range m.tracks {
					if m.tracks[i].ID == lastID {
						m.cursor = i
						break
					}
				}
			}
			if lastPos, err := m.store.LoadLastPosition(); err == nil && lastPos > 0 {
				m.restorePosition = lastPos
				m.currPos = lastPos
			}
		}

	case historyLoadedMsg:
		if len(m.history) == 0 {
			m.history = []tidal.Track(msg)
		}

	case favoritesLoadedMsg:
		tracks := []tidal.Track(msg)
		// Keep the favorite-songs list (for the Songs section) and the lookup
		// map (for the ♥ indicators) in sync.
		m.favSongs = tracks
		for i := range tracks {
			m.favorites[tracks[i].ID] = true
		}
		// Only replace the track list with favorites when no playlist was
		// restored from cache — otherwise we'd clobber the user's last session.
		if !m.playlistRestored {
			m.tracksOrder = tracks
			m.shuffleMode = ShuffleOff
			m.applyShuffle()
			m.section = SecQueue
			m.cursor = 0
			_ = m.store.SavePlaylist(m.tracks)
		}
		m.pushState()

	case searchResultsMsg:
		m.searchTracks = msg
		m.searchCursor = 0
		m.searchLoading = false
		m.searchInput.Blur()

	case searchGroupedMsg:
		m.searchResults = tidal.SearchResults(msg)
		m.searchCursor = 0
		m.searchLoading = false
		m.searchInput.Blur()

	case tracksMsg:
		m.tracksOrder = msg
		m.shuffleMode = ShuffleOff
		m.applyShuffle()
		// Record the queue's origin: a pending source (e.g. "radio") if one was
		// set before the request, otherwise this is an ad-hoc load with no
		// saved-playlist backing.
		m.queueSource = m.pendingQueueSource
		m.pendingQueueSource = ""
		m.queuePlaylistUUID = ""
		m.queueDirty = false
		// Don't yank focus away if the user is in the Search section.
		if m.section != SecSearch {
			m.section = SecQueue
			m.showArtist = false
			m.focusMain = true
			m.searchInput.Blur()
			m.cursor = 0
		}
		// In client mode, mark this as a local playlist so parentStateMsg
		// doesn't overwrite it before the user commits it to the server.
		if m.clientMode {
			m.localPlaylist = true
		}
		_ = m.store.SavePlaylist(m.tracks)
		m.pushState()

	case favoriteMsg:
		if msg.added {
			m.favorites[msg.trackID] = true
		} else {
			delete(m.favorites, msg.trackID)
		}

	case openURLTracksMsg:
		if len(msg) == 0 {
			break
		}
		m.tracksOrder = msg
		m.shuffleMode = ShuffleOff
		m.applyShuffle()
		m.section = SecQueue
		m.showArtist = false
		m.focusMain = true
		m.cursor = 0
		_ = m.store.SavePlaylist(m.tracks)
		// Auto-play the first track.
		track := m.tracks[0]
		_ = m.store.CacheTrack(track.ID, track)
		cmd := m.playTrackCmd(track)
		return m, cmd

	case mixesMsg:
		m.mixes = msg

	case playlistsMsg:
		m.playlists = msg

	case favArtistsMsg:
		m.favArtists = msg

	case favAlbumsMsg:
		m.favAlbums = msg

	case playlistDetailMsg:
		m.detailTracks = msg.tracks
		m.playlistName = msg.title
		m.detailCursor = 0
		m.detailFocus = true

	case artistAlbumsMsg:
		m.artistID = msg.artistID
		m.artistName = msg.artistName
		m.artistAlbums = msg.albums
		m.artistCursor = 0
		m.artistLoading = false
		m.artistAlbum = nil
		m.artistAlbumTracks = nil
		m.showArtist = true
		m.focusMain = true

	case artistAlbumTracksMsg:
		album := msg.album
		m.artistAlbum = &album
		m.artistAlbumTracks = msg.tracks
		m.artistAlbumCursor = 0
		m.artistLoading = false

	case errMsg:
		m.errText = msg.Error()
		// Clear the artist-view spinner if a fetch failed while loading it,
		// otherwise it would hang on "Loading artist...".
		m.artistLoading = false
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })

	case clearErrMsg:
		m.errText = ""

	case queueSavedMsg:
		m.queuePlaylistUUID = msg.uuid
		m.queueSource = "playlist:" + msg.name
		m.queueDirty = false
		m.toast = fmt.Sprintf("✓ Saved %q — %d tracks", msg.name, msg.count)
		return m, toastClearCmd()

	case spotifyResolvedMsg:
		if m.overlay != OverlayImportSpotify {
			return m, nil // overlay was dismissed while resolving
		}
		if msg.err != nil {
			m.importStage = importStageInput
			m.importInput.Focus()
			m.importError = msg.err.Error()
			return m, nil
		}
		m.importSource = msg.src
		m.importRows = msg.rows
		m.importCursor = 0
		m.importError = ""
		m.importStage = importStageReview
		return m, nil

	case clearToastMsg:
		m.toast = ""

	case playPlaylistMsg:
		m.tracksOrder = msg.tracks
		m.shuffleMode = ShuffleOff
		m.applyShuffle()
		m.section = SecQueue
		m.showArtist = false
		m.focusMain = true
		m.searchInput.Blur()
		idx := msg.startIndex
		if idx < 0 || idx >= len(m.tracks) {
			idx = 0
		}
		m.cursor = idx
		_ = m.store.SavePlaylist(m.tracks)
		track := m.tracks[idx]
		_ = m.store.CacheTrack(track.ID, track)
		cmd := m.playTrackCmd(track)
		return m, cmd

	case mprisMsg:
		ev := mpris.Event(msg)
		switch ev.Cmd {
		case mpris.CmdPlayPause:
			// If nothing is playing (restored session), start the cursor track.
			if m.currentTrack == nil && len(m.tracks) > 0 {
				track := m.tracks[m.cursor]
				_ = m.store.CacheTrack(track.ID, track)
				return m, tea.Batch(m.playTrackCmd(track), listenMPRIS(m.mprisCh))
			}
			_ = m.player.Pause()
			// Read back the real state — see togglePlay in keys.go.
			m.isPlaying = !m.player.IsPaused()
		case mpris.CmdNext:
			m.shufflePlayed = append(m.shufflePlayed, m.cursor)
			next := m.nextIndex()
			if next >= 0 {
				m.advancing = true
				m.cursor = next
				track := m.tracks[next]
				m.currPos = 0
				m.duration = 0
				_ = m.store.CacheTrack(track.ID, track)
				return m, tea.Batch(m.playNextTrackCmd(track), listenMPRIS(m.mprisCh))
			}
		case mpris.CmdPrevious:
			prev := m.prevIndex()
			if prev >= 0 {
				m.advancing = false
				m.cursor = prev
				track := m.tracks[prev]
				m.currPos = 0
				m.duration = 0
				_ = m.store.CacheTrack(track.ID, track)
				return m, tea.Batch(m.playTrackCmd(track), listenMPRIS(m.mprisCh))
			}
		case mpris.CmdPlayTrackID:
			trackID := ev.TrackID
			return m, tea.Batch(
				func() tea.Msg {
					track, err := m.client.GetTrack(m.ctx, strconv.Itoa(trackID))
					if err != nil {
						return errMsg(err)
					}
					return openURLTracksMsg([]tidal.Track{*track})
				},
				listenMPRIS(m.mprisCh),
			)
		case mpris.CmdOpenURL:
			u := ev.URL
			return m, tea.Batch(
				func() tea.Msg {
					tracks, err := resolveQuery(m.ctx, m.client, m.store, u)
					if err != nil {
						return errMsg(err)
					}
					return openURLTracksMsg(tracks)
				},
				listenMPRIS(m.mprisCh),
			)
		case mpris.CmdPlayPlaylist:
			playlistJSON := ev.PlaylistJSON
			startIdx := ev.PlaylistStartIndex
			return m, tea.Batch(
				func() tea.Msg {
					var tracks []tidal.Track
					if err := json.Unmarshal([]byte(playlistJSON), &tracks); err != nil || len(tracks) == 0 {
						return errMsg(fmt.Errorf("invalid playlist from client: %w", err))
					}
					return playPlaylistMsg{tracks: tracks, startIndex: startIdx}
				},
				listenMPRIS(m.mprisCh),
			)
		case mpris.CmdSetDevice:
			m.currentDevice = ev.Device
			m.player.SetDevice(ev.Device)
			_ = m.store.SaveDevice(ev.Device)
		}
		return m, listenMPRIS(m.mprisCh)
	}

	if m.section == SecSearch {
		m.searchInput, cmd = m.searchInput.Update(msg)
	}

	return m, cmd
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	t := m.activeTheme()

	sidebarW, mainW := m.layoutDims()
	bodyH := m.bodyHeight()

	main := m.renderMain(t, mainW, bodyH)

	var body string
	if sidebarW == 0 {
		body = main
	} else {
		sidebar := m.renderSidebar(t, sidebarW, bodyH)
		gap := strings.Repeat(" ", zoneGap)
		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, gap, main)
	}

	parts := []string{body}
	switch {
	case m.toast != "":
		parts = append(parts, t.GreenT.Render(" "+truncateStr(m.toast, m.width-2)))
	case m.errText != "":
		parts = append(parts, t.Err.Render(" ! "+truncateStr(m.errText, m.width-3)))
	}
	parts = append(parts,
		m.renderNowPlayingBar(t, m.width),
		m.footerKeyBar(t, m.width),
	)

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if m.overlay != OverlayNone {
		view = m.renderOverlay(t, view)
	}

	// Manage the Kitty cover image (memoized; only emits on a real change).
	if m.kittySupported {
		view += m.kittyFrame()
	}
	return view
}

// kittyCoverOverlay returns the Kitty escape that draws the cover image over the
// blank box reserved in the Now-Playing pane. Coordinates are 1-indexed screen
// cells derived from the layout: the main pane begins after the sidebar + gap,
// the panel border adds one column/row, and the cover box is the pane's first
// inner rows.
// kittyFrame returns the Kitty escape to append to this frame and updates the
// memoized draw state. The expensive PNG encode runs only when the cover or box
// geometry changes; the draw escape is emitted only when the on-screen image
// must change (cover appears, changes, or must be cleared), so animation frames
// that don't touch the cover send nothing — keeping navigation snappy.
func (m *Model) kittyFrame() string {
	if m.kitty == nil {
		m.kitty = &kittyState{}
	}
	ks := m.kitty

	col, row, panelW, imgRows, ok := m.coverBoxRect()
	if !ok || !m.useKittyCover() {
		// Cover should not be shown: clear it once, then stay quiet.
		if ks.drawnKey != "" {
			ks.drawnKey = ""
			return kittyClearAll()
		}
		return ""
	}

	key := fmt.Sprintf("%s@%dx%d+%d,%d", m.coverCacheKey, panelW, imgRows, col, row)
	if ks.drawnKey == key {
		return "" // already on screen unchanged — emit nothing
	}
	if ks.encodeKey != key {
		ks.escape = kittyImageAt(m.coverImage, col, row, panelW, imgRows)
		ks.encodeKey = key
	}
	ks.drawnKey = key
	return ks.escape
}

// coverBoxRect returns the 1-indexed screen position and cell size of the cover
// image box for the active section, and whether a cover box is shown. The Queue
// places it on the right of the track list.
func (m *Model) coverBoxRect() (col, row, panelW, imgRows int, ok bool) {
	if m.section != SecQueue {
		return 0, 0, 0, 0, false
	}
	sidebarW, mainW := m.layoutDims()
	coverW, show := m.queueCoverWidth(mainW)
	if !show {
		return 0, 0, 0, 0, false
	}
	mainLeft := 0
	if sidebarW > 0 {
		mainLeft = sidebarW + zoneGap
	}
	listW := mainW - coverW
	pw, ir := m.queueCoverDims(coverW, m.bodyHeight())
	col = mainLeft + listW + 1 /* cover panel left border */ + 1 /* 1-indexed */
	row = 1 /* panel top border */ + 1                           /* 1-indexed */
	return col, row, pw, ir, true
}

// footerKeyBar returns the context-sensitive key hint bar for the current
// section.
func (m *Model) footerKeyBar(t Theme, w int) string {
	base := [][2]string{
		{"j/k", "Move"},
		{"h/l", "Pane"},
		{"↵", "Play"},
		{"Space", "Pause"},
		{"o", "Actions"},
		{":", "Command"},
		{"/", "Search"},
		{"q", "Quit"},
	}
	switch m.section {
	case SecQueue:
		base = [][2]string{
			{"j/k", "Move"},
			{"↵", "Play"},
			{"x", "Remove"},
			{"C", "Clear"},
			{"S", "Save"},
			{"o", "Actions"},
			{":", "Command"},
			{"q", "Quit"},
		}
	case SecSearch:
		base = [][2]string{
			{"↵", "Search/Play"},
			{"j/k", "Move"},
			{"o", "Actions"},
			{"f", "Fav"},
			{"a", "Artist"},
			{":", "Command"},
			{"q", "Quit"},
		}
	case SecSettings:
		base = [][2]string{
			{"j/k", "Preview"}, {"↵", "Apply"}, {"t", "Cycle"}, {"Esc", "Cancel"}, {"q", "Quit"},
		}
	default:
	}
	return renderKeyBar(t, base, w)
}

// renderOverlay composites the active overlay popup over a dimmed base view.
// Step 3 wires only the device-select overlay; later steps add the others.
func (m *Model) renderOverlay(t Theme, base string) string {
	var popup string
	anchorCentered := true
	switch m.overlay {
	case OverlayDeviceSelect:
		popup = m.renderDeviceSelect(t)
	case OverlayCommandPalette:
		popup = m.renderCommandPalette(t)
	case OverlayAddToPlaylist:
		popup = m.renderAddToPlaylist(t)
	case OverlayImportSpotify:
		popup = m.renderImportSpotify(t)
	case OverlayActionSheet:
		popup = m.renderActionSheet(t)
		anchorCentered = false
	default:
		return base
	}
	if popup == "" {
		return base
	}
	pw := lipgloss.Width(popup)
	ph := strings.Count(popup, "\n") + 1

	x := max((m.width-pw)/2, 0)
	y := max((m.height-ph)/2, 0)
	if !anchorCentered {
		// Anchor the action sheet near the selected row: just right of the
		// sidebar, vertically tracking the cursor but clamped on-screen.
		sidebarW, _ := m.layoutDims()
		x = min(sidebarW+4, max(m.width-pw-1, 0))
		y = min(max(m.cursor+2, 1), max(m.height-ph-2, 1))
	}
	return PlaceOverlay(x, y, popup, dim(t, base))
}

// renderMain renders the main content pane for the current section, sized to
// exactly w columns by h rows.
func (m *Model) renderMain(t Theme, w, h int) string {
	// The transient artist drill-down overlays whatever section is active.
	if m.artistViewActive() {
		return m.renderArtistPane(t, w, h)
	}
	switch m.section {
	case SecMixes:
		return m.renderMixesPane(t, w, h)
	case SecSearch:
		return m.renderSearchPane(t, w, h)
	case SecQueue:
		return m.renderQueuePane(t, w, h)
	case SecFavSongs:
		return m.renderFavSongsPane(t, w, h)
	case SecPlaylists:
		return m.renderPlaylistsPane(t, w, h)
	case SecFavArtists:
		return m.renderFavArtistsPane(t, w, h)
	case SecFavAlbums:
		return m.renderFavAlbumsPane(t, w, h)
	case SecHistory:
		return m.renderHistoryPane(t, w, h)
	case SecSettings:
		return m.renderThemePicker(t, w, h)
	default:
		return renderPanel(t, sectionTitle(m.section), m.focusMain, w, h,
			t.RowDim.Render("Coming soon."))
	}
}
