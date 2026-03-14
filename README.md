![tidalt TUI](docs/tui.png)

A terminal UI for Tidal that delivers **bit-perfect, lossless audio** directly to your DAC — no PipeWire, no PulseAudio, no resampling.

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
- Volume control and output device selection, both persisted between sessions
- MPRIS2 registration — media keys and `playerctl` work without TUI focus

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
| `s`            | Cycle shuffle mode (Off → Shuffle → Random)  |
| `r`            | Load radio playlist for selected track       |
| `f`            | Toggle favorite on selected track            |
| `9`            | Volume down 5%                               |
| `0`            | Volume up 5%                                 |
| `d`            | Open output device selector                  |
| `Esc`          | Close device selector                        |
| `q` / `Ctrl+C` | Quit                                         |

### Global shortcuts (MPRIS2)

`tidalt` registers as an MPRIS2 media player so playback can be controlled without the TUI being focused. On keyboards without dedicated media keys, bind [`playerctl`](https://github.com/altdesktop/playerctl) to custom shortcuts via your desktop environment. Suggested bindings for 65% keyboards:

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
- [DAC compatibility](docs/dac-compatibility.md)
- [Media keys & MPRIS2 setup](docs/media-keys.md)
- [Debugging](docs/debugging.md)
