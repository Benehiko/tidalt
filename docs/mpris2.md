# MPRIS2 support

tidalt registers as an MPRIS2 media player on the D-Bus session bus. This lets
standard desktop tools — media keys, `playerctl`, status-bar widgets — control
playback without the TUI being focused, and without any tidalt-specific
configuration.

**Bus name:** `org.mpris.MediaPlayer2.tidalt`
**Object path:** `/org/mpris/MediaPlayer2`

---

## Implemented interfaces

### `org.mpris.MediaPlayer2`

The root interface. Advertises player identity and capabilities.

| Property | Value |
|---|---|
| `Identity` | `"tidalt"` |
| `CanQuit` | `false` |
| `CanRaise` | `false` |
| `HasTrackList` | `false` |
| `SupportedUriSchemes` | `["tidal"]` |
| `SupportedMimeTypes` | `[]` |

Methods `Raise` and `Quit` are no-ops (present to satisfy the spec).

---

### `org.mpris.MediaPlayer2.Player`

Playback control. All methods are forwarded to the server's internal queue.

| Method | Behaviour |
|---|---|
| `PlayPause` | Toggle play/pause |
| `Play` | Resume playback (alias for PlayPause) |
| `Pause` | Pause playback (alias for PlayPause) |
| `Next` | Skip to the next track in the queue |
| `Previous` | Return to the previous track |
| `Stop` | No-op (stopping is handled internally) |

| Property | Value |
|---|---|
| `PlaybackStatus` | Live: `"Playing"`, `"Paused"`, or `"Stopped"` |
| `CanPlay` | `true` |
| `CanPause` | `true` |
| `CanGoNext` | `true` |
| `CanGoPrevious` | `true` |
| `CanSeek` | `false` ¹ |
| `CanControl` | `true` |
| `Rate` | `1.0` |
| `MinimumRate` | `1.0` |
| `MaximumRate` | `1.0` |

> ¹ Seeking via MPRIS2 `Seek`/`SetPosition` is not yet implemented. Use the
> TUI `←`/`→` keys (10-second jumps) instead.

---

### `org.freedesktop.DBus.Properties`

`Get` and `GetAll` are implemented for both `org.mpris.MediaPlayer2` and
`org.mpris.MediaPlayer2.Player`. `Set` returns `PropertyReadOnly`.

---

## Not implemented

The following optional MPRIS2 interfaces are not implemented:

- `org.mpris.MediaPlayer2.TrackList`
- `org.mpris.MediaPlayer2.Playlists`
- `PropertiesChanged` D-Bus signal (status-bar widgets that poll `GetAll`
  work; widgets that rely on signals will not auto-update)

---

## Using playerctl

```bash
# One-off commands
playerctl --player=tidalt play-pause
playerctl --player=tidalt next
playerctl --player=tidalt previous

# Watch status
playerctl --player=tidalt status
playerctl --player=tidalt metadata

# Follow events
playerctl --player=tidalt --follow metadata
```

Omit `--player=tidalt` if tidalt is the only registered MPRIS2 player.

See [media-keys.md](media-keys.md) for binding these to keyboard shortcuts.

---

## Private interface: `io.tidalt.App`

This non-standard interface is used by `tidalt` client instances and the
`tidalt play` subcommand to talk to the running server. It is not part of
the MPRIS2 specification.

| Method | Signature | Description |
|---|---|---|
| `OpenURL` | `(url: string)` | Queue and play a `tidal://` or `https://tidal.com/` URL |
| `PlayTrackID` | `(trackID: int32)` | Play a track by its numeric Tidal ID |
| `GetState` | `() → (trackJSON, playlistJSON, status, position, duration, volume, device, shuffleMode)` | Read current playback state |

`GetState` return values:

| Field | Type | Description |
|---|---|---|
| `trackJSON` | `string` | JSON-encoded current track, or `""` |
| `playlistJSON` | `string` | JSON-encoded `[]Track` queue, or `""` |
| `status` | `string` | `"Playing"`, `"Paused"`, or `"Stopped"` |
| `position` | `float64` | Current position in seconds |
| `duration` | `float64` | Track duration in seconds |
| `volume` | `float64` | Volume 0–100 |
| `device` | `string` | ALSA `hw:` device string, or `""` for auto |
| `shuffleMode` | `string` | `"Off"`, `"Shuffle"`, or `"Random"` |
