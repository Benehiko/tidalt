package ui

import (
	"context"
	"fmt"
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

	err error

	// Data
	tracks []tidal.Track
	mixes  []tidal.Mix
	cursor int

	// Search
	searchInput textinput.Model

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
}

func InitialModel(ctx context.Context, client *tidal.Client, mprisCh <-chan mpris.Cmd) Model {
	ti := textinput.New()
	ti.Placeholder = "Search for a song..."
	ti.CharLimit = 156
	ti.Width = 30

	s := store.NewSecretsStore()
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
	errMsg             error
	tickMsg            time.Time
	nowPlayingMsg      struct{}
	trackDoneMsg       struct{}
	mprisMsg           mpris.Cmd
	favoriteMsg        struct {
		trackID int
		added   bool
	}
)

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// waitForTrackDone returns a command that blocks until the player's current
// track finishes, then sends a trackDoneMsg.
func waitForTrackDone(p interface{ Done() <-chan struct{} }) tea.Cmd {
	return func() tea.Msg {
		<-p.Done()
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
					m.err = err
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
				query := m.searchInput.Value()
				return m, func() tea.Msg {
					if strings.Contains(query, "tidal.com/track/") {
						parts := strings.Split(query, "/")
						trackID := strings.Split(parts[len(parts)-1], "?")[0]
						track, err := m.client.GetTrack(m.ctx, trackID)
						if err != nil {
							return errMsg(err)
						}
						return tracksMsg([]tidal.Track{*track})
					}

					tracks, err := m.client.Search(m.ctx, query)
					if err != nil {
						return errMsg(err)
					}
					return tracksMsg(tracks)
				}
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
					if err := m.player.Play(url); err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{}
				}
				return m, tea.Batch(play, waitForTrackDone(m.player))
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
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

		case "r":
			if m.state != StateDeviceSelect && len(m.tracks) > 0 {
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
			if m.state != StateDeviceSelect && len(m.tracks) > 0 {
				track := m.tracks[m.cursor]
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

	case tickMsg:
		m.logoFrame++
		if m.isPlaying {
			m.currPos, _ = m.player.GetPosition()
			m.duration, _ = m.player.GetDuration()
		}
		return m, tickCmd()

	case trackDoneMsg:
		if !m.advancing {
			next := m.cursor + 1
			if next < len(m.tracks) {
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
					if err := m.player.Play(url); err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{}
				}
				return m, tea.Batch(autoPlay, waitForTrackDone(m.player))
			}
			m.isPlaying = false
		}

	case favoritesLoadedMsg:
		tracks := []tidal.Track(msg)
		m.tracks = tracks
		m.state = StateBrowse
		m.cursor = 0
		for _, t := range tracks {
			m.favorites[t.ID] = true
		}

	case tracksMsg:
		m.tracks = msg
		m.state = StateBrowse
		m.searchInput.Blur()
		m.cursor = 0

	case favoriteMsg:
		if msg.added {
			m.favorites[msg.trackID] = true
		} else {
			delete(m.favorites, msg.trackID)
		}

	case mixesMsg:
		m.mixes = msg

	case errMsg:
		m.err = msg

	case mprisMsg:
		switch mpris.Cmd(msg) {
		case mpris.CmdPlayPause:
			_ = m.player.Pause()
			m.isPlaying = !m.isPlaying
		case mpris.CmdNext:
			if !m.advancing {
				next := m.cursor + 1
				if next < len(m.tracks) {
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
						if err := m.player.Play(url); err != nil {
							return errMsg(err)
						}
						return nowPlayingMsg{}
					}
					return m, tea.Batch(play, waitForTrackDone(m.player), listenMPRIS(m.mprisCh))
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
					if err := m.player.Play(url); err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{}
				}
				return m, tea.Batch(play, waitForTrackDone(m.player), listenMPRIS(m.mprisCh))
			}
		}
		return m, listenMPRIS(m.mprisCh)
	}

	if m.state == StateSearch && m.state != StateDeviceSelect {
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
	if m.err != nil {
		return fmt.Sprintf("Error: %v (Press q to quit)", m.err)
	}

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
	s += fmt.Sprintf("  Volume: %.0f%%   Device: %s\n\n", m.volume, deviceLabel)

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
		s += "\n [TAB] Switch Tab | [ENTER] Play | [SPACE] Pause | [r] Radio | [f] Favorite | [9/0] Vol | [d] Device | [q] Quit\n"
	}

	return s
}
