# Client-server architecture

tidalt uses a single-server, many-client model built on D-Bus. One process owns
the ALSA device and the MPRIS2 bus name; every other `tidalt` invocation
becomes a lightweight client that forwards commands to it.

---

## Why this design?

### Only one process touches the DAC

ALSA `hw:` devices cannot be shared between processes. If two programs both try
to open `hw:1,0` the second one fails. tidalt solves this by making the first
instance the exclusive owner of the device for the duration of its lifetime.
Subsequent invocations detect the running server over D-Bus and operate as
clients — they never attempt to open the sound card themselves.

### Browser `tidal://` links work without a full TUI

When you click **"Open in desktop app"** on tidal.com, the OS invokes
`tidalt play tidal://track/<id>`. If a server is already running the `play`
subcommand delivers the track ID over D-Bus in milliseconds and exits — no
terminal, no second TUI. Without this design the browser handler would need
to open a terminal and start a second, conflicting player instance.

### Daemon mode: music in the background

`tidalt daemon` runs the full playback engine with no terminal and no UI. You
can start it at login via systemd (`tidalt setup --daemon`), then control it
any time using:

- `tidalt` — opens the full TUI in client mode
- `playerctl` — standard MPRIS2 CLI
- media keys — via MPRIS2 (works system-wide, no TUI focus needed)
- browser links — forwarded automatically by `tidalt play`

---

## How instances discover each other

tidalt claims the D-Bus name `org.mpris.MediaPlayer2.tidalt` on the session
bus at startup. If the name is already taken (`ErrAlreadyRunning`) the process
switches to client mode instead of exiting.

```
tidalt (server)          tidalt (client)         tidalt play <url>
──────────────           ───────────────         ─────────────────
owns ALSA hw:            no audio device         no audio device
owns D-Bus name          connects to name        sends one D-Bus call
runs playback loop       forwards commands        exits immediately
serves MPRIS2            shows TUI               (spawned by browser)
```

---

## Running modes

| Invocation | Behaviour |
|---|---|
| `tidalt` | Full TUI. First instance → server. Second instance → client TUI. |
| `tidalt daemon` | Headless server. No TUI, no terminal required. |
| `tidalt play <url>` | Forwards URL to running server; starts a terminal TUI if none is running. |
| `tidalt setup` | Registers the `tidal://` URL handler with XDG. |
| `tidalt setup --daemon` | Installs and starts a systemd `--user` service. |

---

## Daemon mode

### Starting manually

```bash
tidalt daemon
```

The process stays in the foreground (so you can see log output). Press
`Ctrl+C` or send `SIGTERM` to stop.

The ALSA device and D-Bus audio reservation are **not** acquired at startup.
The daemon holds no audio hardware until a track actually begins playing, at
which point it claims the device exclusively. The device is released again when
playback stops or the daemon exits.

### Installing as a systemd user service

```bash
tidalt setup --daemon
```

This writes `~/.config/systemd/user/tidalt.service`, then runs:

```
systemctl --user daemon-reload
systemctl --user enable tidalt.service
systemctl --user start  tidalt.service
```

The service starts automatically after your graphical session (display
manager) is ready and restarts on failure.

Useful commands:

```bash
systemctl --user status tidalt          # is it running?
journalctl --user -u tidalt -f          # live logs
systemctl --user stop tidalt            # stop now
systemctl --user disable --now tidalt   # stop and remove from autostart
```

### Controlling the daemon

Once `tidalt daemon` is running, open the TUI from any terminal:

```bash
tidalt          # full TUI in client mode
```

Or use playerctl:

```bash
playerctl --player=tidalt play-pause
playerctl --player=tidalt next
playerctl --player=tidalt previous
```

Or click **"Open in desktop app"** on tidal.com — it calls `tidalt play
tidal://track/<id>` which the daemon handles without opening a terminal.

---

## D-Bus interfaces

### Standard MPRIS2 (`org.mpris.MediaPlayer2.*`)

Exposed on bus name `org.mpris.MediaPlayer2.tidalt`, object path
`/org/mpris/MediaPlayer2`.

### Private interface (`io.tidalt.App`)

Used by client instances to communicate with the server. Not part of the
MPRIS2 spec.

| Method | Arguments | Description |
|---|---|---|
| `OpenURL` | `url: string` | Queue and play a `tidal://` or `https://tidal.com/` URL |
| `PlayTrackID` | `trackID: int32` | Play a track by its Tidal numeric ID |
| `GetState` | — | Returns current track JSON, playlist JSON, status, position, duration, volume, device, shuffle mode |

See [mpris2.md](mpris2.md) for the full MPRIS2 interface documentation.
