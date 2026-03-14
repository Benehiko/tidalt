// Package mpris implements a minimal MPRIS2 D-Bus server so that desktop
// media-key bindings and tools like playerctl can control playback without
// the TUI being focused.
//
// Implemented interfaces:
//
//   - org.mpris.MediaPlayer2          (Identity, CanQuit, …)
//   - org.mpris.MediaPlayer2.Player   (PlayPause, Next, Previous, Play, Pause, Stop)
//   - org.freedesktop.DBus.Properties (Get, GetAll — required by playerctl)
//   - io.tidalt.App                   (OpenURL — used by a second tidalt instance to
//     forward a tidal:// URL to the running one)
//
// Commands are forwarded to the caller via the Commands channel.
package mpris

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// Cmd identifies which media control event occurred.
type Cmd int

const (
	CmdPlayPause Cmd = iota
	CmdNext
	CmdPrevious
	CmdOpenURL // URL is in Event.URL
)

// Event is sent on the Commands channel for every media key press or URL
// forwarded from a second instance.
type Event struct {
	Cmd Cmd
	URL string // non-empty only for CmdOpenURL
}

// ErrAlreadyRunning is returned by Start when another tidalt instance already
// owns the MPRIS bus name. The caller should use SendURL to forward the URL
// to that instance and then exit.
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

	conn *dbus.Conn
}

// Start registers the MPRIS2 service on the session bus and returns a Server
// whose Commands channel delivers events. The server runs until ctx is
// cancelled, at which point the Commands channel is closed and the D-Bus name
// is released.
//
// Returns ErrAlreadyRunning when another tidalt already owns the bus name.
// In that case the caller should use SendURL and exit.
func Start(ctx context.Context) (*Server, error) {
	ch := make(chan Event, 4)
	srv := &Server{Commands: ch}

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
	props := &properties{}
	app := &tidalApp{ch: ch}

	for _, export := range []struct {
		obj   interface{}
		iface string
	}{
		{root, "org.mpris.MediaPlayer2"},
		{player, "org.mpris.MediaPlayer2.Player"},
		{props, "org.freedesktop.DBus.Properties"},
		{app, appIface},
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

// SendURL connects to an already-running tidalt instance and calls OpenURL on
// it, forwarding the given URL for immediate playback. Returns an error if no
// instance is running or the call fails.
func SendURL(url string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("mpris: no session bus: %w", err)
	}
	defer func() { _ = conn.Close() }()

	obj := conn.Object(busName, objectPath)
	call := obj.Call(appIface+".OpenURL", 0, url)
	return call.Err
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
	ch chan<- Event
}

// OpenURL is called by a second tidalt instance to forward a tidal:// URL.
func (a *tidalApp) OpenURL(url string) *dbus.Error {
	select {
	case a.ch <- Event{Cmd: CmdOpenURL, URL: url}:
	default:
	}
	return nil
}

// --- org.freedesktop.DBus.Properties ----------------------------------------
//
// playerctl queries this interface before sending any command. We return a
// minimal set of properties sufficient for playerctl to consider the player
// capable of receiving commands.

type properties struct{}

// playerProps returns the fixed property map for org.mpris.MediaPlayer2.Player.
func playerProps() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant("Playing"),
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
		m = playerProps()
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
		return playerProps(), nil
	case "org.mpris.MediaPlayer2":
		return rootProps(), nil
	default:
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", nil)
	}
}

func (p *properties) Set(iface, prop string, val dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", nil)
}
