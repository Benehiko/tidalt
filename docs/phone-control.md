# Controlling tidalt from your phone

MPRIS2 is a local D-Bus protocol — it does not have a network transport. To
control tidalt from a phone on the same LAN you need a bridge application that
speaks MPRIS2 on the desktop side and a custom protocol (over Wi-Fi or
Bluetooth) on the phone side.

Two well-established open-source projects do this:

| Project | Desktop | Phone |
|---|---|---|
| [KDE Connect](https://kdeconnect.kde.org/) | Linux daemon + KDE/GNOME integration | Android & iOS app |
| [GSConnect](https://github.com/GSConnect/gnome-shell-extension-gsconnect) | GNOME Shell extension (wraps KDE Connect protocol) | Android & iOS app (same KDE Connect app) |

Both use the same phone app and the same LAN pairing protocol. The only
difference is the desktop side.

---

## How it works with tidalt

```
Phone (KDE Connect app)
  │  Wi-Fi LAN (KDE Connect protocol)
  ▼
Desktop bridge (kdeconnectd / GSConnect)
  │  D-Bus session bus (MPRIS2)
  ▼
tidalt (org.mpris.MediaPlayer2.tidalt)
  │
  ▼
ALSA hw: → DAC → speakers
```

The bridge daemon watches the D-Bus session bus for registered MPRIS2 players.
When it sees `org.mpris.MediaPlayer2.tidalt` it exposes tidalt's controls
(play, pause, next, previous, current track title/artist) in the phone app's
media widget. No tidalt-specific configuration is needed — it is discovered
automatically via MPRIS2.

---

## KDE Connect (KDE Plasma)

### Install

```bash
sudo pacman -S kdeconnect   # Arch
sudo apt install kdeconnect  # Debian / Ubuntu
sudo dnf install kdeconnect  # Fedora
```

The `kdeconnectd` daemon starts automatically with your KDE Plasma session. On
other desktop environments start it manually:

```bash
/usr/lib/kdeconnectd &
```

Or enable it as a systemd user service:

```bash
systemctl --user enable --now kdeconnect.service
```

### Install the phone app

- **Android:** [KDE Connect on Google Play](https://play.google.com/store/apps/details?id=org.kde.kdeconnect_tp) or [F-Droid](https://f-droid.org/packages/org.kde.kdeconnect_tp/)
- **iOS:** [KDE Connect on the App Store](https://apps.apple.com/app/kde-connect/id1580245991)

### Pair

1. Make sure your phone and desktop are on the same Wi-Fi network.
2. Open the KDE Connect app on your phone — your desktop should appear in the
   device list within a few seconds.
3. Tap the device and tap **Pair**. Accept the pairing request on the desktop
   (a notification appears, or run `kdeconnect-cli --pair -d <deviceID>`).

### Verify MPRIS2 bridge

With tidalt running and the device paired, check that the bridge is active:

```bash
kdeconnect-cli --list-devices --id-only | xargs -I{} kdeconnect-cli -d {} --list-available-plugins
```

Look for `mprisremote` in the output. If it appears, the bridge is ready.

In the phone app, open the paired device and look for the **Multimedia control**
widget — it will show the current track and play/pause/next/previous buttons.

### Troubleshoot

```bash
# List paired devices and their IDs
kdeconnect-cli --list-devices

# Check if tidalt is visible as an MPRIS player
kdeconnect-cli -d <deviceID> --list-available-plugins

# Manually send a command (useful for testing without the phone)
kdeconnect-cli -d <deviceID> --action play-pause
```

If tidalt does not appear in the phone's media widget:
1. Confirm tidalt is running and registered: `playerctl --list-all` should show `tidalt`.
2. Restart `kdeconnectd`: `systemctl --user restart kdeconnect.service`.
3. On the phone, close and reopen the KDE Connect app.

---

## GSConnect (GNOME)

GSConnect is a GNOME Shell extension that implements the KDE Connect protocol.
It uses the **same phone app** as KDE Connect.

### Install

The easiest way is via the GNOME Extensions website:

1. Install the browser extension from [extensions.gnome.org](https://extensions.gnome.org).
2. Go to the [GSConnect page](https://extensions.gnome.org/extension/1319/gsconnect/) and toggle it on.

Or install the package directly:

```bash
sudo apt install gnome-shell-extension-gsconnect   # Debian / Ubuntu
sudo dnf install gnome-shell-extension-gsconnect   # Fedora
```

Then enable the extension:

```bash
gnome-extensions enable gsconnect@andyholmes.github.io
```

### Pair

Same procedure as KDE Connect — phone and desktop on the same network, open
the KDE Connect app on your phone, tap the desktop device, tap **Pair**.
GSConnect shows a desktop notification to accept.

### MPRIS2 bridge

GSConnect's MPRIS plugin works the same way as KDE Connect's. Once paired the
phone app's media widget will show tidalt controls automatically when tidalt is
running.

---

## Daemon mode + phone control

The recommended setup for always-on phone control:

```bash
# 1. Install tidalt as a background daemon
tidalt setup --daemon

# 2. Install and start KDE Connect (or GSConnect)
systemctl --user enable --now kdeconnect.service

# 3. Pair your phone once
```

After this, tidalt starts at login with no terminal window. Open the KDE
Connect app on your phone any time to browse, play, pause, and skip tracks.
To load a specific playlist or search for tracks, run `tidalt` in any terminal
— it opens in client mode and forwards commands to the daemon.

---

## What the phone can control

Through the KDE Connect / GSConnect MPRIS bridge:

| Control | Supported |
|---|---|
| Play / pause | Yes |
| Next track | Yes |
| Previous track | Yes |
| See current track title & artist | Yes (via MPRIS2 `Metadata`) |
| Volume | No ¹ |
| Seek | No ² |
| Browse / search | No (use the `tidalt` TUI) |

> ¹ Volume control via MPRIS2 `Volume` property is not yet implemented.
>
> ² MPRIS2 `Seek` / `SetPosition` are not implemented; use the TUI `←`/`→`
> keys instead.
