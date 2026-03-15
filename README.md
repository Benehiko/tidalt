![tidalt TUI](docs/tui.png)

**tidalt** is a Tidal music player for Linux that delivers **bit-perfect, lossless audio** directly to your DAC — no PipeWire, no PulseAudio, no resampling.

It is built on top of the Tidal API and can run in three ways:

- **Interactive TUI** — browse, search, and control playback from the terminal
- **Daemon** — headless background process, controlled via the TUI or any MPRIS2 client
- **Client** — lightweight TUI that forwards commands to a running daemon over D-Bus

All three modes share the same playback engine. The daemon holds exclusive access to the audio device only while a track is actually playing — releasing it on pause so other applications can use it freely.

> 100% vibe coded with [Claude](https://claude.ai) and [Gemini](https://gemini.google.com).

**Linux only.** Requires a Tidal HiFi or HiFi Plus subscription.

---

## Install

### Dependencies

Requires Go 1.21+ and ALSA development headers:

```bash
sudo pacman -S alsa-lib        # Arch
sudo apt install libasound2-dev # Debian / Ubuntu
sudo dnf install alsa-lib-devel # Fedora
```

### Install

```bash
go install github.com/Benehiko/tidalt/cmd/tidalt@latest
```

### Register the tidal:// URL handler

The `setup` subcommand installs the `.desktop` file and registers the
`tidal://` scheme so clicking **"Open in desktop app"** on tidal.com opens
the track directly in tidalt. It prints every action before running it:

```
$ tidalt setup
  -> Creating directory /home/user/.local/share/applications
  -> Writing /home/user/.local/share/applications/tidalt.desktop
  -> $ xdg-mime default tidalt.desktop x-scheme-handler/tidal
  -> $ update-desktop-database /home/user/.local/share/applications

Setup complete.
Clicking "Open in desktop app" on tidal.com will now open tidalt.
```

Some browsers (notably Firefox and Librewolf) apply additional protocol
filtering on top of XDG and require a one-time browser-side configuration
step. See [docs/browser-url-handler.md](docs/browser-url-handler.md) for
per-browser instructions and troubleshooting.

### Run as a background daemon (optional)

Install tidalt as a systemd user service so it runs silently in the background
from login, with no terminal window:

```bash
tidalt setup --daemon
```

Then open the TUI from any terminal with `tidalt`, or control playback with
`playerctl`. See [docs/client-server.md](docs/client-server.md) for details.

---

On first launch you will be prompted to log in via the Tidal OAuth2 device flow. Your session is saved to the system keychain (or an age-encrypted file at `~/.config/tidalt/secrets`) and reused on subsequent runs.

---

## Features

- Browse favorites and Daily Mixes
- Search tracks by name or paste a Tidal track URL
- Song radio — load a playlist of similar tracks for any song
- Shuffle (Fisher-Yates pre-shuffle or random pick)
- Favorite / unfavorite tracks
- Bit-perfect FLAC playback via direct ALSA `hw:` — bypasses PipeWire/PulseAudio entirely
- Auto-negotiates the best PCM format your DAC supports
- Auto-advances through the queue; respects shuffle mode
- Seek forward/back 10 seconds with `←`/`→`
- Volume control and output device selection, both persisted between sessions
- Session and playback position restored on next launch
- MPRIS2 registration — media keys and `playerctl` work without TUI focus
- Daemon mode — run headless in the background, control via TUI client or playerctl

---

## Keybindings

### In-TUI

| Key            | Action                                       |
| -------------- | -------------------------------------------- |
| `Tab`          | Cycle tabs (My Music → Daily Mixes → Search) |
| `↑` / `k`      | Move cursor up                               |
| `↓` / `j`      | Move cursor down                             |
| `Enter`        | Play selected track / load mix / confirm     |
| `Space`        | Pause / resume                               |
| `←`            | Seek back 10 seconds                         |
| `→`            | Seek forward 10 seconds                      |
| `s`            | Cycle shuffle mode (Off → Shuffle → Random)  |
| `r`            | Load radio playlist for selected track       |
| `f`            | Toggle favorite on selected track            |
| `9`            | Volume down 5%                               |
| `0`            | Volume up 5%                                 |
| `d`            | Open output device selector                  |
| `Esc`          | Close device selector                        |
| `q` / `Ctrl+C` | Quit                                         |

### Global shortcuts (MPRIS2)

`tidalt` registers as an MPRIS2 media player so playback can be controlled without the TUI being focused — including from the daemon. These shortcuts work as long as tidalt (or `tidalt daemon`) is running, with no TUI open.

#### Standard media keys

Many keyboards and desktop environments map dedicated media keys directly to MPRIS2. These work automatically with no configuration:

| Key        | Action         |
| ---------- | -------------- |
| `fn` + `.` | Play / pause   |
| `fn` + `,` | Previous track |
| `fn` + `/` | Next track     |

These are handled by your desktop environment via MPRIS2 — tidalt does not implement any special key capture itself.

#### Custom bindings (65% keyboards)

On keyboards without dedicated media keys, bind [`playerctl`](https://github.com/altdesktop/playerctl) to custom shortcuts via your desktop environment:

| Shortcut | Action         |
| -------- | -------------- |
| `Alt+0`  | Previous track |
| `Alt+-`  | Play / pause   |
| `Alt+=`  | Next track     |

See [docs/media-keys.md](docs/media-keys.md) for full setup instructions.

---

## Supported DACs

Auto-detection scans `/proc/asound/cards`. Any ALSA-visible device can be selected manually with the `d` key.

| DAC                     |  Auto-detected   |
| ----------------------- | :--------------: |
| Hidizs S9 Pro           |       Yes        |
| Focusrite Scarlett Solo |       Yes        |
| Any ALSA-visible device | Manual (`d` key) |

---

## Data & Storage

| What                       | Where                                                         |
| -------------------------- | ------------------------------------------------------------- |
| OAuth2 session             | System keychain or `~/.config/tidalt/secrets` (age-encrypted) |
| Volume & device preference | `~/.local/share/tidalt/tidal-cache.db`                        |
| Track metadata cache       | Same database                                                 |

---

## Further reading

- [Architecture & audio pipeline](docs/architecture.md)
- [Client-server architecture & daemon mode](docs/client-server.md)
- [MPRIS2 support](docs/mpris2.md)
- [DAC compatibility](docs/dac-compatibility.md)
- [Media keys & MPRIS2 setup](docs/media-keys.md)
- [Browser URL handler troubleshooting](docs/browser-url-handler.md)
- [Debugging](docs/debugging.md)
