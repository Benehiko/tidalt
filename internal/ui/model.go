package ui

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Benehiko/tidalt/internal/mpris"
	"github.com/Benehiko/tidalt/internal/player"
	"github.com/Benehiko/tidalt/internal/store"
	"github.com/Benehiko/tidalt/internal/tidal"
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

type State int

const (
	StateBrowse State = iota
	StateMixes
	StateSearch
	StateDeviceSelect
)

type Model struct {
	ctx    context.Context
	client *tidal.Client
	store  *store.SecretsStore
	player *player.Player
	state  State

	errText string // transient error shown in status bar; cleared after display

	// Data
	tracks []tidal.Track
	mixes  []tidal.Mix
	cursor int

	// Search — own list so results don't clobber My Music
	searchInput   textinput.Model
	searchTracks  []tidal.Track
	searchCursor  int
	searchLoading bool

	// Terminal size
	width  int
	height int

	// Device selection
	devices       []player.DeviceInfo
	currentDevice string // hw device string, "" = auto-detect
	prevState     State  // state to return to after device selection

	// Player UI
	currentTrack *tidal.Track
	volume       float64
	isPlaying    bool
	advancing    bool // true while auto-advancing to next track; suppresses re-trigger
	progress     progress.Model
	currPos      float64
	duration     float64

	// Logo animation
	logoFrame int

	// MPRIS media key commands
	mprisCh <-chan mpris.Cmd

	// Favorited track IDs (populated from GetFavorites; toggled by "f")
	favorites map[int]bool

	// Shuffle
	shuffleMode   ShuffleMode
	tracksOrder   []tidal.Track // original order, saved when shuffle is enabled
	shufflePlayed []int         // indices already played (for ShuffleRandom deduplication)
}

func InitialModel(ctx context.Context, client *tidal.Client, s *store.SecretsStore, mprisCh <-chan mpris.Cmd) Model {
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

	return Model{
		ctx:           ctx,
		client:        client,
		store:         s,
		player:        p,
		searchInput:   ti,
		state:         StateBrowse,
		volume:        vol,
		currentDevice: currentDevice,
		progress:      progress.New(progress.WithDefaultGradient()),
		mprisCh:       mprisCh,
		favorites:     make(map[int]bool),
	}
}

// Messages
type (
	tracksMsg          []tidal.Track
	favoritesLoadedMsg []tidal.Track
	mixesMsg           []tidal.Mix
	searchResultsMsg   []tidal.Track
	errMsg             error
	clearErrMsg        struct{}
	tickMsg            time.Time
	nowPlayingMsg      struct{ done <-chan struct{} }
	trackDoneMsg       struct{}
	mprisMsg           mpris.Cmd
	favoriteMsg        struct {
		trackID int
		added   bool
	}
)

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
		return candidates[rand.IntN(len(candidates))]
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

// resolveQuery turns a search query into a list of tracks.
// It handles:
//   - Tidal track URLs  (tidal.com/track/ID)
//   - Tidal album URLs  (tidal.com/album/ID)
//   - Plain text        (title, artist, album — Tidal search covers all three)
func resolveQuery(ctx context.Context, client *tidal.Client, query string) ([]tidal.Track, error) {
	if strings.Contains(query, "tidal.com/track/") {
		parts := strings.Split(query, "/")
		trackID := strings.Split(parts[len(parts)-1], "?")[0]
		track, err := client.GetTrack(ctx, trackID)
		if err != nil {
			return nil, err
		}
		return []tidal.Track{*track}, nil
	}
	if strings.Contains(query, "tidal.com/album/") {
		parts := strings.Split(query, "/")
		albumID := strings.Split(parts[len(parts)-1], "?")[0]
		return client.GetAlbumTracks(ctx, albumID)
	}
	return client.Search(ctx, query)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// waitForTrackDone returns a command that blocks until the given done channel
// is closed (i.e. the track finished naturally), then sends a trackDoneMsg.
// Callers should pass the channel returned by player.Play() directly so there
// is no race between stop() clearing the old channel and Play() setting a new one.
func waitForTrackDone(done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-done
		return trackDoneMsg{}
	}
}

// listenMPRIS returns a command that blocks until the next MPRIS media-key
// command arrives, then re-registers itself so the stream continues.
func listenMPRIS(ch <-chan mpris.Cmd) tea.Cmd {
	return func() tea.Msg {
		cmd, ok := <-ch
		if !ok {
			// Channel closed — MPRIS server shut down; stop listening.
			return nil
		}
		return mprisMsg(cmd)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
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
		m.waitForContextCancel(),
		tickCmd(),
		listenMPRIS(m.mprisCh),
	)
}

func (m Model) waitForContextCancel() tea.Cmd {
	return func() tea.Msg {
		<-m.ctx.Done()
		m.player.Close()
		m.store.Close()
		return tea.Quit()
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// When the search input is focused, let it consume all keypresses except
		// the global controls (quit, tab, esc) and enter (which we handle to
		// trigger search or play).
		if m.searchInput.Focused() {
			switch msg.String() {
			case "ctrl+c", "q", "tab", "esc", "enter":
				// fall through to the main switch below
			default:
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.player.Close()
			m.store.Close()
			return m, tea.Quit

		case "esc":
			if m.state == StateDeviceSelect {
				m.state = m.prevState
				m.cursor = 0
			}

		case "d":
			if m.state != StateSearch && m.state != StateDeviceSelect {
				devs, err := player.ListDevices()
				if err != nil {
					m.errText = err.Error()
					break
				}
				m.devices = devs
				m.prevState = m.state
				m.state = StateDeviceSelect
				// Position cursor on the currently selected device.
				m.cursor = 0
				for i, d := range devs {
					if d.HWName == m.currentDevice {
						m.cursor = i
						break
					}
				}
			}

		case "tab":
			switch m.state {
			case StateBrowse:
				m.state = StateMixes
			case StateMixes:
				m.state = StateSearch
				m.searchInput.Focus()
			default:
				m.state = StateBrowse
				m.searchInput.Blur()
			}
			m.cursor = 0

		case "enter":
			if m.state == StateDeviceSelect && len(m.devices) > 0 {
				chosen := m.devices[m.cursor]
				m.currentDevice = chosen.HWName
				m.player.SetDevice(chosen.HWName)
				_ = m.store.SaveDevice(chosen.HWName)
				m.state = m.prevState
				m.cursor = 0
				break
			}
			if m.state == StateSearch && m.searchInput.Focused() {
				query := strings.TrimSpace(m.searchInput.Value())
				if query == "" {
					break
				}
				m.searchLoading = true
				m.searchTracks = nil
				m.searchCursor = 0
				return m, func() tea.Msg {
					tracks, err := resolveQuery(m.ctx, m.client, query)
					if err != nil {
						return errMsg(err)
					}
					return searchResultsMsg(tracks)
				}
			}
			// Enter on a search result — play it
			if m.state == StateSearch && len(m.searchTracks) > 0 {
				track := m.searchTracks[m.searchCursor]
				m.currentTrack = &track
				m.isPlaying = true
				m.advancing = false
				_ = m.store.CacheTrack(track.ID, track)
				play := func() tea.Msg {
					url, err := m.client.GetStreamURL(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					done, err := m.player.Play(url)
					if err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{done: done}
				}
				return m, play
			}
			if m.state == StateMixes && len(m.mixes) > 0 {
				mix := m.mixes[m.cursor]
				return m, func() tea.Msg {
					tracks, err := m.client.GetMixTracks(m.ctx, mix.ID)
					if err != nil {
						return errMsg(err)
					}
					return tracksMsg(tracks)
				}
			}
			if len(m.tracks) > 0 {
				track := m.tracks[m.cursor]
				m.currentTrack = &track
				m.isPlaying = true
				m.advancing = false
				// Cache track metadata
				_ = m.store.CacheTrack(track.ID, track)

				play := func() tea.Msg {
					url, err := m.client.GetStreamURL(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					done, err := m.player.Play(url)
					if err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{done: done}
				}
				return m, play
			}

		case "up", "k":
			if m.state == StateSearch {
				if m.searchCursor > 0 {
					m.searchCursor--
				} else if !m.searchInput.Focused() {
					// At top of results — move focus back to the search input.
					m.searchInput.Focus()
				}
			} else if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.state == StateSearch {
				if m.searchInput.Focused() {
					// Move focus from input to the first result.
					m.searchInput.Blur()
				} else if m.searchCursor < len(m.searchTracks)-1 {
					m.searchCursor++
				}
			} else {
				max := len(m.tracks)
				switch m.state {
				case StateMixes:
					max = len(m.mixes)
				case StateDeviceSelect:
					max = len(m.devices)
				}
				if m.cursor < max-1 {
					m.cursor++
				}
			}

		case " ":
			_ = m.player.Pause()
			m.isPlaying = !m.isPlaying

		case "9":
			m.volume -= 5
			if m.volume < 0 {
				m.volume = 0
			}
			_ = m.player.SetVolume(m.volume)
			_ = m.store.SaveVolume(m.volume)
		case "0":
			m.volume += 5
			if m.volume > 100 {
				m.volume = 100
			}
			_ = m.player.SetVolume(m.volume)
			_ = m.store.SaveVolume(m.volume)

		case "s":
			if m.state != StateDeviceSelect {
				// Cycle: Off → Fisher-Yates → Random → Off
				switch m.shuffleMode {
				case ShuffleOff:
					m.shuffleMode = ShuffleFisherYates
				case ShuffleFisherYates:
					m.shuffleMode = ShuffleRandom
				default:
					m.shuffleMode = ShuffleOff
				}
				m.applyShuffle()
				m.cursor = 0
			}

		case "r":
			if m.state == StateSearch && len(m.searchTracks) > 0 {
				track := m.searchTracks[m.searchCursor]
				m.searchLoading = true
				return m, func() tea.Msg {
					tracks, err := m.client.GetTrackRadio(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					return searchResultsMsg(tracks)
				}
			} else if m.state != StateDeviceSelect && len(m.tracks) > 0 {
				track := m.tracks[m.cursor]
				return m, func() tea.Msg {
					tracks, err := m.client.GetTrackRadio(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					return tracksMsg(tracks)
				}
			}

		case "f":
			var favTrack *tidal.Track
			if m.state == StateSearch && len(m.searchTracks) > 0 {
				t := m.searchTracks[m.searchCursor]
				favTrack = &t
			} else if m.state != StateDeviceSelect && len(m.tracks) > 0 {
				t := m.tracks[m.cursor]
				favTrack = &t
			}
			if favTrack != nil {
				track := *favTrack
				isFav := m.favorites[track.ID]
				return m, func() tea.Msg {
					var err error
					if isFav {
						err = m.client.RemoveFavorite(m.ctx, track.ID)
					} else {
						err = m.client.AddFavorite(m.ctx, track.ID)
					}
					if err != nil {
						return errMsg(err)
					}
					return favoriteMsg{trackID: track.ID, added: !isFav}
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for the "  [MM:SS / MM:SS]" suffix (18 chars) plus indent (2) plus margin.
		barWidth := msg.Width - 22
		if barWidth < 10 {
			barWidth = 10
		}
		m.progress = progress.New(progress.WithDefaultGradient(), progress.WithWidth(barWidth))

	case nowPlayingMsg:
		m.advancing = false
		return m, waitForTrackDone(msg.done)

	case tickMsg:
		m.logoFrame++
		if m.isPlaying {
			m.currPos, _ = m.player.GetPosition()
			m.duration, _ = m.player.GetDuration()
		}
		return m, tickCmd()

	case trackDoneMsg:
		if !m.advancing {
			m.shufflePlayed = append(m.shufflePlayed, m.cursor)
			next := m.nextIndex()
			if next >= 0 {
				m.advancing = true
				m.cursor = next
				track := m.tracks[next]
				m.currentTrack = &track
				m.currPos = 0
				m.duration = 0
				_ = m.store.CacheTrack(track.ID, track)
				autoPlay := func() tea.Msg {
					url, err := m.client.GetStreamURL(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					done, err := m.player.Play(url)
					if err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{done: done}
				}
				return m, autoPlay
			}
			m.isPlaying = false
		}

	case favoritesLoadedMsg:
		tracks := []tidal.Track(msg)
		m.tracksOrder = tracks
		m.shuffleMode = ShuffleOff
		m.applyShuffle()
		m.state = StateBrowse
		m.cursor = 0
		for _, t := range tracks {
			m.favorites[t.ID] = true
		}

	case searchResultsMsg:
		m.searchTracks = msg
		m.searchCursor = 0
		m.searchLoading = false
		m.searchInput.Blur()

	case tracksMsg:
		m.tracksOrder = msg
		m.shuffleMode = ShuffleOff
		m.applyShuffle()
		// Don't yank focus away if the user is in the Search tab.
		if m.state != StateSearch {
			m.state = StateBrowse
			m.searchInput.Blur()
			m.cursor = 0
		}

	case favoriteMsg:
		if msg.added {
			m.favorites[msg.trackID] = true
		} else {
			delete(m.favorites, msg.trackID)
		}

	case mixesMsg:
		m.mixes = msg

	case errMsg:
		m.errText = msg.Error()
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearErrMsg{} })

	case clearErrMsg:
		m.errText = ""

	case mprisMsg:
		switch mpris.Cmd(msg) {
		case mpris.CmdPlayPause:
			_ = m.player.Pause()
			m.isPlaying = !m.isPlaying
		case mpris.CmdNext:
			if !m.advancing {
				m.shufflePlayed = append(m.shufflePlayed, m.cursor)
				next := m.nextIndex()
				if next >= 0 {
					m.advancing = true
					m.cursor = next
					track := m.tracks[next]
					m.currentTrack = &track
					m.currPos = 0
					m.duration = 0
					_ = m.store.CacheTrack(track.ID, track)
					play := func() tea.Msg {
						url, err := m.client.GetStreamURL(m.ctx, track.ID)
						if err != nil {
							return errMsg(err)
						}
						done, err := m.player.Play(url)
						if err != nil {
							return errMsg(err)
						}
						return nowPlayingMsg{done: done}
					}
					return m, tea.Batch(play, listenMPRIS(m.mprisCh))
				}
			}
		case mpris.CmdPrevious:
			prev := m.cursor - 1
			if prev >= 0 {
				m.advancing = false
				m.cursor = prev
				track := m.tracks[prev]
				m.currentTrack = &track
				m.currPos = 0
				m.duration = 0
				_ = m.store.CacheTrack(track.ID, track)
				play := func() tea.Msg {
					url, err := m.client.GetStreamURL(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					done, err := m.player.Play(url)
					if err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{done: done}
				}
				return m, tea.Batch(play, listenMPRIS(m.mprisCh))
			}
		}
		return m, listenMPRIS(m.mprisCh)
	}

	if m.state == StateSearch {
		m.searchInput, cmd = m.searchInput.Update(msg)
	}

	return m, cmd
}

func visibleWindow(cursor, total, height int) (start, end int) {
	if total == 0 {
		return 0, 0
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > total {
		end = total
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// logoLines is a 5-row ASCII art representation of "tidalt".
var logoLines = [5]string{
	` ████████╗██╗██████╗  █████╗ ██╗  ████████╗`,
	`    ██╔══╝██║██╔══██╗██╔══██╗██║     ██╔══╝`,
	`    ██║   ██║██║  ██║███████║██║     ██║   `,
	`    ██║   ██║██║  ██║██╔══██║██║     ██║   `,
	`    ██║   ██║██████╔╝██║  ██║███████╗██║   `,
}

// waveColors is the palette cycled across logo columns for the wave effect.
var waveColors = []lipgloss.Color{
	"#FF6AC1", // hot pink
	"#FF87D7", // light pink
	"#D7AFFF", // lavender
	"#87D7FF", // sky blue
	"#87FFFF", // cyan
	"#87FFD7", // mint
	"#AFFFAF", // light green
	"#D7FF87", // yellow-green
	"#FFD787", // peach
	"#FF875F", // salmon
}

// renderLogo returns the animated logo string. frame advances the wave by one
// column per call so the colours appear to scroll left-to-right.
func renderLogo(frame int) string {
	// Width of the logo in rune columns (all rows same length after padding).
	width := len([]rune(logoLines[0]))
	period := len(waveColors)

	var sb strings.Builder
	for _, row := range logoLines {
		runes := []rune(row)
		for col, r := range runes {
			if r == ' ' || r == '╗' || r == '╔' || r == '╝' || r == '╚' || r == '═' || r == '║' || r == '╠' || r == '╣' || r == '╦' || r == '╩' || r == '╬' {
				// Keep box-drawing and spaces uncoloured to preserve shape.
				sb.WriteRune(r)
				continue
			}
			// Wave: colour index shifts with frame and column position.
			idx := (col*period/width + frame) % period
			if idx < 0 {
				idx += period
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(waveColors[idx]).Render(string(r)))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func formatTime(seconds float64) string {
	minutes := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

func (m Model) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
	activeTab := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("205")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	inactiveTab := lipgloss.NewStyle().Padding(0, 1)

	s := renderLogo(m.logoFrame) + "\n"

	// Player Status
	if m.currentTrack != nil {
		status := "Playing"
		if !m.isPlaying {
			status = "Paused"
		}
		s += headerStyle.Render(fmt.Sprintf("%s: %s - %s", status, m.currentTrack.Title, m.currentTrack.Artist.Name)) + "\n"
		percent := 0.0
		if m.duration > 0 {
			percent = m.currPos / m.duration
		}
		s += fmt.Sprintf("\n  %s [%s / %s]\n", m.progress.ViewAs(percent), formatTime(m.currPos), formatTime(m.duration))
	} else {
		s += headerStyle.Render("Idle") + "\n"
	}
	deviceLabel := "auto"
	if m.currentDevice != "" {
		deviceLabel = m.currentDevice
	}
	s += fmt.Sprintf("  Volume: %.0f%%   Device: %s   Shuffle: %s\n", m.volume, deviceLabel, m.shuffleMode)

	// Error banner — shown inline, clears automatically after 5 s.
	if m.errText != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
		s += errStyle.Render("  ! "+m.errText) + "\n"
	}
	s += "\n"

	// Tabs
	tabs := []string{"My Music", "Daily Mixes", "Search"}
	for i, t := range tabs {
		if int(m.state) == i {
			s += activeTab.Render(t) + " "
		} else {
			s += inactiveTab.Render(t) + " "
		}
	}
	s += "\n\n"

	// List Content
	if m.state == StateSearch {
		s += "  " + m.searchInput.View() + "\n\n"
	}

	// overhead: logo(5) + blank(1) + player block(4) + tabs(1) + \n\n(2) + search(2 if visible) + footer(2) = ~17
	overhead := 17
	if m.state == StateSearch {
		overhead += 2
	}
	listHeight := m.height - overhead
	if listHeight < 1 {
		listHeight = 1
	}

	switch m.state {
	case StateDeviceSelect:
		if len(m.devices) == 0 {
			s += "  No playback devices found.\n"
		} else {
			start, end := visibleWindow(m.cursor, len(m.devices), listHeight)
			for i := start; i < end; i++ {
				d := m.devices[i]
				cur := " "
				if m.cursor == i {
					cur = ">"
				}
				selected := ""
				if d.HWName == m.currentDevice {
					selected = " ✓"
				}
				line := fmt.Sprintf(" %s %s  %s%s", cur, d.HWName, d.LongName, selected)
				if m.cursor == i {
					s += lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(line) + "\n"
				} else {
					s += line + "\n"
				}
			}
		}
		s += "\n [↑/↓] Navigate | [ENTER] Select | [ESC] Cancel | [q] Quit\n"
	case StateMixes:
		items := m.mixes
		start, end := visibleWindow(m.cursor, len(items), listHeight)
		for i := start; i < end; i++ {
			mix := items[i]
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}
			line := fmt.Sprintf(" %s %s (%s)", cursor, mix.Title, mix.SubTitle)
			if m.cursor == i {
				s += lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
		s += "\n [TAB] Switch Tab | [ENTER] Play/Select | [SPACE] Pause | [9/0] Vol | [d] Device | [q] Quit\n"
	case StateSearch:
		if m.searchLoading {
			s += "  Searching...\n"
		} else if len(m.searchTracks) == 0 && m.searchInput.Value() != "" {
			s += "  No results.\n"
		} else {
			start, end := visibleWindow(m.searchCursor, len(m.searchTracks), listHeight)
			for i := start; i < end; i++ {
				track := m.searchTracks[i]
				cur := " "
				if m.searchCursor == i {
					cur = ">"
				}
				fav := " "
				if m.favorites[track.ID] {
					fav = "♥"
				}
				line := fmt.Sprintf(" %s %s %s - %s  [%s]", cur, fav, track.Title, track.Artist.Name, track.Album.Title)
				if m.searchCursor == i {
					s += lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(line) + "\n"
				} else {
					s += line + "\n"
				}
			}
		}
		s += "\n [TAB] Switch Tab | [ENTER] Search / Play | [↑/↓] Navigate | [SPACE] Pause | [f] Favorite | [9/0] Vol | [d] Device | [q] Quit\n"
	default:
		items := m.tracks
		start, end := visibleWindow(m.cursor, len(items), listHeight)
		for i := start; i < end; i++ {
			track := items[i]
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}
			fav := " "
			if m.favorites[track.ID] {
				fav = "♥"
			}
			line := fmt.Sprintf(" %s %s %s - %s", cursor, fav, track.Title, track.Artist.Name)
			if m.cursor == i {
				s += lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
		s += "\n [TAB] Switch Tab | [ENTER] Play | [SPACE] Pause | [s] Shuffle | [r] Radio | [f] Favorite | [9/0] Vol | [d] Device | [q] Quit\n"
	}

	return s
}
