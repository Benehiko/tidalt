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
	
	"tidal-tui/internal/player"
	"tidal-tui/internal/store"
	"tidal-tui/internal/tidal"
)

type State int

const (
	StateBrowse State = iota
	StateSearch
	StateMixes
	StateDeviceSelect
)

type Model struct {
	ctx       context.Context
	client    *tidal.Client
	store     *store.SecretsStore
	player    *player.Player
	state     State
	
	err        error

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
}

func InitialModel(ctx context.Context, client *tidal.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Search for a song..."
	ti.CharLimit = 156
	ti.Width = 30

	s := store.NewSecretsStore()
	p := player.NewPlayer()

	vol := 100.0
	if v, err := s.LoadVolume(); err == nil {
		vol = v
	}
	p.SetVolume(vol)

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
	}
}

// Messages
type tracksMsg []tidal.Track
type mixesMsg []tidal.Mix
type errMsg error
type tickMsg time.Time
type nowPlayingMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			tracks, err := m.client.GetFavorites(m.ctx, 50)
			if err != nil {
				return errMsg(err)
			}
			return tracksMsg(tracks)
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
	)
}

func (m Model) waitForContextCancel() tea.Cmd {
	return func() tea.Msg {
		<-m.ctx.Done()
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
			if m.state == StateBrowse {
				m.state = StateMixes
			} else if m.state == StateMixes {
				m.state = StateSearch
				m.searchInput.Focus()
			} else {
				m.state = StateBrowse
				m.searchInput.Blur()
			}
			m.cursor = 0

		case "enter":
			if m.state == StateDeviceSelect && len(m.devices) > 0 {
				chosen := m.devices[m.cursor]
				m.currentDevice = chosen.HWName
				m.player.SetDevice(chosen.HWName)
				m.store.SaveDevice(chosen.HWName)
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
				m.store.CacheTrack(track.ID, track)
				
				return m, func() tea.Msg {
					url, err := m.client.GetStreamURL(m.ctx, track.ID)
					if err != nil {
						return errMsg(err)
					}
					if err := m.player.Play(url); err != nil {
						return errMsg(err)
					}
					return nowPlayingMsg{}
				}
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			max := len(m.tracks)
			if m.state == StateMixes {
				max = len(m.mixes)
			} else if m.state == StateDeviceSelect {
				max = len(m.devices)
			}
			if m.cursor < max-1 {
				m.cursor++
			}
		
		case " ":
			m.player.Pause()
			m.isPlaying = !m.isPlaying

		case "9":
			m.volume -= 5
			if m.volume < 0 {
				m.volume = 0
			}
			m.player.SetVolume(m.volume)
			m.store.SaveVolume(m.volume)
		case "0":
			m.volume += 5
			if m.volume > 100 {
				m.volume = 100
			}
			m.player.SetVolume(m.volume)
			m.store.SaveVolume(m.volume)
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
		if m.isPlaying {
			m.currPos, _ = m.player.GetPosition()
			m.duration, _ = m.player.GetDuration()

			if !m.advancing && m.duration > 0 && m.currPos >= m.duration {
				next := m.cursor + 1
				if next < len(m.tracks) {
					m.advancing = true
					m.cursor = next
					track := m.tracks[next]
					m.currentTrack = &track
					m.currPos = 0
					m.duration = 0
					m.store.CacheTrack(track.ID, track)
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
					return m, tea.Batch(tickCmd(), autoPlay)
				}
				m.isPlaying = false
			}
		}
		return m, tickCmd()

	case tracksMsg:
		m.tracks = msg
		m.state = StateBrowse
		m.searchInput.Blur()
		m.cursor = 0
	
	case mixesMsg:
		m.mixes = msg

	case errMsg:
		m.err = msg
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

	s := "\n"
	
	// Player Status
	if m.currentTrack != nil {
		s += headerStyle.Render(fmt.Sprintf("Playing: %s - %s", m.currentTrack.Title, m.currentTrack.Artist.Name)) + "\n"
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

	// overhead: leading \n(1) + player block(4) + tabs(1) + \n\n(2) + search(2 if visible) + footer(2) = ~12
	overhead := 12
	if m.state == StateSearch {
		overhead += 2
	}
	listHeight := m.height - overhead
	if listHeight < 1 {
		listHeight = 1
	}

	if m.state == StateDeviceSelect {
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
	} else if m.state == StateMixes {
		items := m.mixes
		start, end := visibleWindow(m.cursor, len(items), listHeight)
		for i := start; i < end; i++ {
			mix := items[i]
			cursor := " "
			if m.cursor == i { cursor = ">" }
			line := fmt.Sprintf(" %s %s (%s)", cursor, mix.Title, mix.SubTitle)
			if m.cursor == i {
				s += lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
		s += "\n [TAB] Switch Tab | [ENTER] Play/Select | [SPACE] Pause | [9/0] Vol | [d] Device | [q] Quit\n"
	} else {
		items := m.tracks
		start, end := visibleWindow(m.cursor, len(items), listHeight)
		for i := start; i < end; i++ {
			track := items[i]
			cursor := " "
			if m.cursor == i { cursor = ">" }
			line := fmt.Sprintf(" %s %s - %s", cursor, track.Title, track.Artist.Name)
			if m.cursor == i {
				s += lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
		s += "\n [TAB] Switch Tab | [ENTER] Play/Select | [SPACE] Pause | [9/0] Vol | [d] Device | [q] Quit\n"
	}

	return s
}
