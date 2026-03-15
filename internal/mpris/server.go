// Package mpris implements a minimal MPRIS2 D-Bus server so that desktop
// media-key bindings and tools like playerctl can control playback without
// the TUI being focused.
//
// Implemented interfaces:
//
//   - org.mpris.MediaPlayer2          (Identity, CanQuit, …)
//   - org.mpris.MediaPlayer2.Player   (PlayPause, Next, Previous, Play, Pause, Stop)
//   - org.freedesktop.DBus.Properties (Get, GetAll — required by playerctl)
//   - io.tidalt.App                   (OpenURL, GetState — used by client instances)
//
// Commands are forwarded to the caller via the Commands channel.
// Live playback state is pushed by the parent via Server.SetState and read by
// clients via Client.GetState.
package mpris

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/Benehiko/tidalt/internal/tidal"
)

// Cmd identifies which media control event occurred.
type Cmd int

const (
	CmdPlayPause Cmd = iota
	CmdNext
	CmdPrevious
	CmdOpenURL     // URL is in Event.URL
	CmdPlayTrackID // TrackID is in Event.TrackID
)

// Event is sent on the Commands channel for every media key press or URL
// forwarded from a second instance.
type Event struct {
	Cmd     Cmd
	URL     string // non-empty only for CmdOpenURL
	TrackID int    // non-zero only for CmdPlayTrackID
}

// PlayerState is the snapshot of playback state the parent broadcasts.
type PlayerState struct {
	// CurrentTrackJSON is the JSON-encoded tidal.Track currently playing, or "".
	CurrentTrackJSON string
	// PlaylistJSON is the JSON-encoded []tidal.Track of the current queue, or "".
	PlaylistJSON string
	// PlaybackStatus is "Playing", "Paused", or "Stopped".
	PlaybackStatus string
	// Position is the current playback position in seconds.
	Position float64
	// Duration is the total track duration in seconds.
	Duration float64
	// Volume is the playback volume (0–100).
	Volume float64
	// Device is the ALSA hw: device string, or "" for auto.
	Device string
	// ShuffleMode is the current shuffle mode string ("Off", "Shuffle", "Random").
	ShuffleMode string
}

// ErrAlreadyRunning is returned by Start when another tidalt instance already
// owns the MPRIS bus name. The caller should use NewClient and exit.
var ErrAlreadyRunning = errors.New("mpris: tidalt is already running")

const (
	busName    = "org.mpris.MediaPlayer2.tidalt"
	objectPath = "/org/mpris/MediaPlayer2"
	appIface   = "io.tidalt.App"
)

// Server is a running MPRIS2 D-Bus server. Stop it by cancelling the context
// passed to Start.
type Server struct {
	// Commands receives media control events from D-Bus.
	Commands <-chan Event

	conn  *dbus.Conn
	state *sharedState
}

// SetState pushes the current playback state so client instances can read it.
// trackJSON and playlistJSON are JSON-encoded tidal.Track / []tidal.Track.
func (s *Server) SetState(trackJSON, playlistJSON string, isPlaying bool, position, duration, volume float64, device, shuffleMode string) {
	status := "Stopped"
	if isPlaying {
		status = "Playing"
	} else if trackJSON != "" {
		status = "Paused"
	}
	s.state.set(PlayerState{
		CurrentTrackJSON: trackJSON,
		PlaylistJSON:     playlistJSON,
		PlaybackStatus:   status,
		Position:         position,
		Duration:         duration,
		Volume:           volume,
		Device:           device,
		ShuffleMode:      shuffleMode,
	})
}

// sharedState holds the mutable player state shared between the D-Bus handler
// goroutine and the main TUI goroutine.
type sharedState struct {
	mu sync.RWMutex
	ps PlayerState
}

func (s *sharedState) set(ps PlayerState) {
	s.mu.Lock()
	s.ps = ps
	s.mu.Unlock()
}

func (s *sharedState) get() PlayerState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ps
}

// Start registers the MPRIS2 service on the session bus and returns a Server
// whose Commands channel delivers events. The server runs until ctx is
// cancelled, at which point the Commands channel is closed and the D-Bus name
// is released.
//
// Returns ErrAlreadyRunning when another tidalt already owns the bus name.
// In that case the caller should use NewClient.
func Start(ctx context.Context) (*Server, error) {
	ch := make(chan Event, 4)
	st := &sharedState{}
	srv := &Server{Commands: ch, state: st}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		close(ch)
		return srv, fmt.Errorf("mpris: no session bus: %w", err)
	}
	srv.conn = conn

	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		close(ch)
		return srv, fmt.Errorf("mpris: could not claim %s: %w", busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		close(ch)
		return srv, ErrAlreadyRunning
	}

	root := &mediaPlayer2{}
	player := &mediaPlayer2Player{ch: ch}
	props := &properties{state: st}
	app := &tidalApp{ch: ch, state: st}
	intro := &introspectable{}

	for _, export := range []struct {
		obj   any
		iface string
	}{
		{root, "org.mpris.MediaPlayer2"},
		{player, "org.mpris.MediaPlayer2.Player"},
		{props, "org.freedesktop.DBus.Properties"},
		{app, appIface},
		{intro, "org.freedesktop.DBus.Introspectable"},
	} {
		if err := conn.Export(export.obj, objectPath, export.iface); err != nil {
			_ = conn.Close()
			close(ch)
			return srv, err
		}
	}

	go func() {
		<-ctx.Done()
		_, _ = conn.ReleaseName(busName)
		_ = conn.Close()
		close(ch)
	}()

	return srv, nil
}

// Client holds a D-Bus connection to an already-running tidalt instance and
// exposes methods to control it. Close it when done.
type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// NewClient connects to the session bus and returns a Client targeting the
// running tidalt MPRIS server. Returns an error if no instance is running.
func NewClient() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("mpris: no session bus: %w", err)
	}
	return &Client{conn: conn, obj: conn.Object(busName, objectPath)}, nil
}

// Close releases the underlying D-Bus connection.
func (c *Client) Close() { _ = c.conn.Close() }

// SendURL forwards a stream URL to the running instance for immediate playback.
func (c *Client) SendURL(url string) error {
	return c.obj.Call(appIface+".OpenURL", 0, url).Err
}

// SendTrackID asks the running instance to look up and play a track by its
// Tidal track ID.
func (c *Client) SendTrackID(trackID int) error {
	return c.obj.Call(appIface+".PlayTrackID", 0, trackID).Err
}

// SendPlayPause toggles play/pause on the running instance.
func (c *Client) SendPlayPause() error {
	return c.obj.Call("org.mpris.MediaPlayer2.Player.PlayPause", 0).Err
}

// SendNext skips to the next track on the running instance.
func (c *Client) SendNext() error {
	return c.obj.Call("org.mpris.MediaPlayer2.Player.Next", 0).Err
}

// SendPrevious goes to the previous track on the running instance.
func (c *Client) SendPrevious() error {
	return c.obj.Call("org.mpris.MediaPlayer2.Player.Previous", 0).Err
}

// GetState fetches the current playback state from the running instance.
func (c *Client) GetState() (PlayerState, error) {
	var trackJSON, playlistJSON, status, device, shuffleMode string
	var position, duration, volume float64
	if err := c.obj.Call(appIface+".GetState", 0).Store(&trackJSON, &playlistJSON, &status, &position, &duration, &volume, &device, &shuffleMode); err != nil {
		return PlayerState{}, err
	}
	return PlayerState{
		CurrentTrackJSON: trackJSON,
		PlaylistJSON:     playlistJSON,
		PlaybackStatus:   status,
		Position:         position,
		Duration:         duration,
		Volume:           volume,
		Device:           device,
		ShuffleMode:      shuffleMode,
	}, nil
}

// SendURL is a convenience wrapper that opens a connection, sends the URL, and
// closes the connection. Use Client directly for multiple calls.
func SendURL(url string) error {
	c, err := NewClient()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.SendURL(url)
}

// MarshalTracks JSON-encodes a value for use with Server.SetState.
func MarshalTracks(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// --- org.mpris.MediaPlayer2 -------------------------------------------------

type mediaPlayer2 struct{}

func (m *mediaPlayer2) Raise() *dbus.Error { return nil }
func (m *mediaPlayer2) Quit() *dbus.Error  { return nil }

// --- org.mpris.MediaPlayer2.Player -----------------------------------------

type mediaPlayer2Player struct {
	ch chan<- Event
}

func (p *mediaPlayer2Player) PlayPause() *dbus.Error {
	p.send(Event{Cmd: CmdPlayPause})
	return nil
}

func (p *mediaPlayer2Player) Next() *dbus.Error {
	p.send(Event{Cmd: CmdNext})
	return nil
}

func (p *mediaPlayer2Player) Previous() *dbus.Error {
	p.send(Event{Cmd: CmdPrevious})
	return nil
}

// Play, Pause, and Stop are required by the MPRIS2 spec.
func (p *mediaPlayer2Player) Play() *dbus.Error  { p.send(Event{Cmd: CmdPlayPause}); return nil }
func (p *mediaPlayer2Player) Pause() *dbus.Error { p.send(Event{Cmd: CmdPlayPause}); return nil }
func (p *mediaPlayer2Player) Stop() *dbus.Error  { return nil }

func (p *mediaPlayer2Player) send(e Event) {
	select {
	case p.ch <- e:
	default:
		// Drop if the consumer is not keeping up — better than blocking D-Bus.
	}
}

// --- io.tidalt.App ----------------------------------------------------------

type tidalApp struct {
	ch    chan<- Event
	state *sharedState
}

// OpenURL is called by a second tidalt instance to forward a stream URL.
func (a *tidalApp) OpenURL(url string) *dbus.Error {
	select {
	case a.ch <- Event{Cmd: CmdOpenURL, URL: url}:
	default:
	}
	return nil
}

// PlayTrackID is called by a client instance to ask the parent to play a
// track by its Tidal track ID.
func (a *tidalApp) PlayTrackID(trackID int) *dbus.Error {
	select {
	case a.ch <- Event{Cmd: CmdPlayTrackID, TrackID: trackID}:
	default:
	}
	return nil
}

// GetState returns the current playback state for client instances.
func (a *tidalApp) GetState() (string, string, string, float64, float64, float64, string, string, *dbus.Error) {
	ps := a.state.get()
	return ps.CurrentTrackJSON, ps.PlaylistJSON, ps.PlaybackStatus, ps.Position, ps.Duration, ps.Volume, ps.Device, ps.ShuffleMode, nil
}

// --- org.freedesktop.DBus.Properties ----------------------------------------
//
// playerctl queries this interface before sending any command. We return a
// minimal set of properties sufficient for playerctl to consider the player
// capable of receiving commands.

type properties struct {
	state *sharedState
}

// buildMetadata constructs the MPRIS2 Metadata map from the current track JSON.
// Returns an empty map when no track is playing.
func buildMetadata(trackJSON string) map[string]dbus.Variant {
	meta := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")),
	}
	if trackJSON == "" {
		return meta
	}
	var t tidal.Track
	if err := json.Unmarshal([]byte(trackJSON), &t); err != nil {
		return meta
	}
	meta["mpris:trackid"] = dbus.MakeVariant(dbus.ObjectPath(fmt.Sprintf("/org/mpris/MediaPlayer2/Track/%d", t.ID)))
	meta["xesam:title"] = dbus.MakeVariant(t.Title)
	meta["xesam:artist"] = dbus.MakeVariant([]string{t.Artist.Name})
	meta["xesam:album"] = dbus.MakeVariant(t.Album.Title)
	// MPRIS2 requires length in microseconds.
	meta["mpris:length"] = dbus.MakeVariant(int64(t.Duration) * 1_000_000)
	return meta
}

// playerProps returns the live property map for org.mpris.MediaPlayer2.Player.
func (p *properties) playerProps() map[string]dbus.Variant {
	ps := p.state.get()
	status := ps.PlaybackStatus
	if status == "" {
		status = "Stopped"
	}
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(status),
		"Metadata":       dbus.MakeVariant(buildMetadata(ps.CurrentTrackJSON)),
		"CanPlay":        dbus.MakeVariant(true),
		"CanPause":       dbus.MakeVariant(true),
		"CanGoNext":      dbus.MakeVariant(true),
		"CanGoPrevious":  dbus.MakeVariant(true),
		"CanSeek":        dbus.MakeVariant(false),
		"CanControl":     dbus.MakeVariant(true),
		"Rate":           dbus.MakeVariant(float64(1.0)),
		"MinimumRate":    dbus.MakeVariant(float64(1.0)),
		"MaximumRate":    dbus.MakeVariant(float64(1.0)),
	}
}

// rootProps returns the fixed property map for org.mpris.MediaPlayer2.
func rootProps() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Identity":            dbus.MakeVariant("tidalt"),
		"CanQuit":             dbus.MakeVariant(false),
		"CanRaise":            dbus.MakeVariant(false),
		"HasTrackList":        dbus.MakeVariant(false),
		"SupportedUriSchemes": dbus.MakeVariant([]string{"tidal"}),
		"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
	}
}

func (p *properties) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	var m map[string]dbus.Variant
	switch iface {
	case "org.mpris.MediaPlayer2.Player":
		m = p.playerProps()
	case "org.mpris.MediaPlayer2":
		m = rootProps()
	default:
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", nil)
	}
	v, ok := m[prop]
	if !ok {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
	}
	return v, nil
}

func (p *properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	switch iface {
	case "org.mpris.MediaPlayer2.Player":
		return p.playerProps(), nil
	case "org.mpris.MediaPlayer2":
		return rootProps(), nil
	default:
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", nil)
	}
}

func (p *properties) Set(iface, prop string, val dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", nil)
}

// --- org.freedesktop.DBus.Introspectable ------------------------------------
//
// Returning introspection XML lets tools like busctl, d-feet, and playerctl
// discover exactly which interfaces, methods, signals, and properties tidalt
// exposes. This is the canonical machine-readable record of MPRIS2 support.

type introspectable struct{}

// introspectionXML describes every interface implemented at /org/mpris/MediaPlayer2.
// Derived from the MPRIS2 specification; only the subset actually implemented
// is listed — omitted methods/properties (e.g. TrackList, Playlists, Seek,
// SetPosition) are intentionally absent so clients know not to call them.
const introspectionXML = `<!DOCTYPE node PUBLIC
  "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
  "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node name="/org/mpris/MediaPlayer2">

  <!-- ── org.mpris.MediaPlayer2 ─────────────────────────────────────── -->
  <interface name="org.mpris.MediaPlayer2">
    <method name="Raise"/>
    <method name="Quit"/>
    <property name="Identity"            type="s"  access="read"/>
    <property name="CanQuit"             type="b"  access="read"/>
    <property name="CanRaise"            type="b"  access="read"/>
    <property name="HasTrackList"        type="b"  access="read"/>
    <property name="SupportedUriSchemes" type="as" access="read"/>
    <property name="SupportedMimeTypes"  type="as" access="read"/>
  </interface>

  <!-- ── org.mpris.MediaPlayer2.Player ─────────────────────────────── -->
  <interface name="org.mpris.MediaPlayer2.Player">
    <!-- Implemented methods -->
    <method name="PlayPause"/>
    <method name="Play"/>
    <method name="Pause"/>
    <method name="Stop"/>
    <method name="Next"/>
    <method name="Previous"/>

    <!-- NOT implemented: Seek, SetPosition, OpenUri -->

    <!-- Live properties (via org.freedesktop.DBus.Properties) -->
    <property name="PlaybackStatus" type="s"  access="read"/>
    <property name="Rate"           type="d"  access="read"/>
    <property name="MinimumRate"    type="d"  access="read"/>
    <property name="MaximumRate"    type="d"  access="read"/>
    <property name="CanControl"     type="b"  access="read"/>
    <property name="CanPlay"        type="b"  access="read"/>
    <property name="CanPause"       type="b"  access="read"/>
    <property name="CanGoNext"      type="b"  access="read"/>
    <property name="CanGoPrevious"  type="b"  access="read"/>
    <property name="CanSeek"        type="b"  access="read"/>

    <property name="Metadata"       type="a{sv}" access="read"/>

    <!-- NOT implemented: Shuffle, LoopStatus, Volume, Position -->
    <!-- NOT implemented: Seeked signal -->
  </interface>

  <!-- ── org.freedesktop.DBus.Properties ───────────────────────────── -->
  <interface name="org.freedesktop.DBus.Properties">
    <method name="Get">
      <arg name="interface_name" type="s" direction="in"/>
      <arg name="property_name"  type="s" direction="in"/>
      <arg name="value"          type="v" direction="out"/>
    </method>
    <method name="GetAll">
      <arg name="interface_name" type="s"     direction="in"/>
      <arg name="props"          type="a{sv}" direction="out"/>
    </method>
    <method name="Set">
      <arg name="interface_name" type="s" direction="in"/>
      <arg name="property_name"  type="s" direction="in"/>
      <arg name="value"          type="v" direction="in"/>
    </method>
  </interface>

  <!-- ── org.freedesktop.DBus.Introspectable ───────────────────────── -->
  <interface name="org.freedesktop.DBus.Introspectable">
    <method name="Introspect">
      <arg name="xml_data" type="s" direction="out"/>
    </method>
  </interface>

  <!-- ── io.tidalt.App (private) ────────────────────────────────────── -->
  <!-- Used by tidalt client instances and the 'tidalt play' subcommand. -->
  <interface name="io.tidalt.App">
    <method name="OpenURL">
      <arg name="url" type="s" direction="in"/>
    </method>
    <method name="PlayTrackID">
      <arg name="trackID" type="i" direction="in"/>
    </method>
    <method name="GetState">
      <arg name="currentTrackJSON" type="s" direction="out"/>
      <arg name="playlistJSON"     type="s" direction="out"/>
      <arg name="playbackStatus"   type="s" direction="out"/>
      <arg name="position"         type="d" direction="out"/>
      <arg name="duration"         type="d" direction="out"/>
      <arg name="volume"           type="d" direction="out"/>
      <arg name="device"           type="s" direction="out"/>
      <arg name="shuffleMode"      type="s" direction="out"/>
    </method>
  </interface>

</node>`

func (i *introspectable) Introspect() (string, *dbus.Error) {
	return introspectionXML, nil
}
