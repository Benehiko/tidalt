# Media Key Control (MPRIS2)

`tidalt` registers itself on the D-Bus session bus as an MPRIS2 media player
(`org.mpris.MediaPlayer2.tidalt`). Any client that speaks the MPRIS2 protocol
can then control playback without the TUI needing to be focused.

## What is MPRIS2?

MPRIS2 (Media Player Remote Interfacing Specification) is a standard D-Bus
interface that desktop environments, status bars, and CLI tools use to
communicate with media players. When you press a media key, your desktop
environment translates the keypress into a D-Bus method call sent to whichever
MPRIS2 service is currently registered.

## Supported commands

| D-Bus method | Action |
|---|---|
| `PlayPause` | Toggle pause / resume |
| `Next` | Skip to next track |
| `Previous` | Go to previous track |
| `Play` | Resume (maps to PlayPause) |
| `Pause` | Pause (maps to PlayPause) |

## Arch Linux + KDE Plasma setup

### 1. Verify D-Bus is running

```bash
echo $DBUS_SESSION_BUS_ADDRESS
```

If the variable is empty you are not inside a desktop session — MPRIS2 will not
work. Under a normal KDE Plasma login (SDDM) this is always set.

### 2. Install playerctl

[`playerctl`](https://github.com/altdesktop/playerctl) is the recommended way
to control `tidalt` from keyboard shortcuts on Wayland. It works against any
MPRIS2 player and is available in the Arch extra repository:

```bash
sudo pacman -S playerctl
```

Verify it can see `tidalt` (while `tidalt` is running):

```bash
playerctl --list-all
# tidalt
```

Test the commands directly:

```bash
playerctl --player=tidalt play-pause
playerctl --player=tidalt next
playerctl --player=tidalt previous
```

### 3. Bind playerctl to keyboard shortcuts in KDE

On keyboards without dedicated media keys (e.g. 65% layouts), bind `playerctl`
commands to key combinations via KDE's custom shortcuts.

Open **System Settings → Shortcuts → Custom Shortcuts**, then for each entry
below click **Add → Global Shortcut → Command/URL**:

| Name | Shortcut | Command |
|---|---|---|
| tidalt: Play/Pause | `Alt+-` | `playerctl --player=tidalt play-pause` |
| tidalt: Next | `Alt+=` | `playerctl --player=tidalt next` |
| tidalt: Previous | `Alt+0` | `playerctl --player=tidalt previous` |

Click **Apply** after adding all three.

> **Choosing shortcuts:** The combinations above (`Alt+0`, `Alt+-`, `Alt+=`)
> work well on 65% keyboards — they are unlikely to conflict with applications
> and are reachable without leaving the home area. Avoid `Alt+Tab`, `Alt+F4`,
> and `Super+*` combos as these are reserved by KDE. Test that your chosen
> keys reach Wayland before binding them using `wev`.

### 4. Check that tidalt is visible on the bus

`qdbus6` (from `qt6-tools`) can send D-Bus method calls directly, which is
useful for debugging:

```bash
sudo pacman -S qt6-tools
```

With `tidalt` running, send a command:

```bash
qdbus6 org.mpris.MediaPlayer2.tidalt /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player.PlayPause
qdbus6 org.mpris.MediaPlayer2.tidalt /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player.Next
qdbus6 org.mpris.MediaPlayer2.tidalt /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player.Previous
```

If a command produces `org.freedesktop.DBus.Error.ServiceUnknown`, `tidalt` is
not registered on the bus — check the startup output for an `MPRIS unavailable:`
line.

### 5. Troubleshooting kglobalaccel

KDE's `kglobalaccel` daemon handles global shortcuts. It is D-Bus activated —
it starts on demand and exits when idle. Seeing it as `inactive (dead)` with
`status=0` is normal:

```bash
systemctl --user status plasma-kglobalaccel.service
```

If shortcuts stop working and the service shows `failed` (non-zero exit
status), check the logs:

```bash
journalctl --user -u plasma-kglobalaccel.service -e
```

### 6. Waybar / status bar integration

If you use Waybar you can add an MPRIS module to display the current track and
control playback:

```json
// ~/.config/waybar/config
"mpris": {
    "format": "{player_icon} {title} — {artist}",
    "player-icons": {
        "tidalt": ""
    }
}
```

## Fallback behaviour

If `tidalt` cannot connect to the session bus (e.g. running inside a plain TTY
without a desktop session), the MPRIS server silently degrades — the app starts
normally and the `Commands` channel is immediately closed. An `MPRIS
unavailable:` message is printed to stdout but playback is unaffected. All
in-TUI keybindings (`Space`, etc.) continue to work.
