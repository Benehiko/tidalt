# Running tidalt in Docker

The official image is published to Docker Hub at `benehiko/tidalt` and is built
for `linux/amd64` and `linux/arm64`.

---

## Audio devices

tidalt opens ALSA `hw:` devices directly. Two things are needed to make that work
inside a container:

### 1. Expose `/dev/snd`

Pass `--device /dev/snd` to give the container access to all ALSA PCM and control
nodes. This also makes `/proc/asound` readable inside the container, which is how
tidalt discovers available cards.

### 2. Join the `audio` group

The devices under `/dev/snd` are owned by `root:audio` on the host
(`crw-rw---- 1 root audio`). The process inside the container must be in the
`audio` group to open them.

```bash
--group-add $(getent group audio | cut -d: -f3)
```

### List available devices on your host

```bash
cat /proc/asound/cards
```

The same output is visible inside a running container — no extra flags needed,
`/proc` is already mounted:

```bash
docker run --rm --device /dev/snd benehiko/tidalt:latest cat /proc/asound/cards
```

---

## Persistent data

tidalt writes two kinds of data that should survive container restarts:

| Path in container              | Contents                                    |
| ------------------------------ | ------------------------------------------- |
| `/root/.config/tidalt/`        | OAuth2 session (age-encrypted fallback)     |
| `/root/.local/share/tidalt/`   | Volume, device preference, metadata cache   |

Mount them from the host:

```
-v ~/.config/tidalt:/root/.config/tidalt
-v ~/.local/share/tidalt:/root/.local/share/tidalt
```

---

## D-Bus (WirePlumber / PipeWire)

Before opening a `hw:` device, tidalt asks WirePlumber to release it via D-Bus.
If D-Bus is unreachable the reservation is silently skipped and tidalt opens the
device directly — this is fine on most setups. If you run PipeWire and want tidy
hand-off, forward the session bus socket:

```bash
-v /run/user/$(id -u)/bus:/run/user/1000/bus
-e DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus
```

---

## Examples

### Interactive TUI

```bash
docker run -it --rm \
  --device /dev/snd \
  --group-add $(getent group audio | cut -d: -f3) \
  -v ~/.config/tidalt:/root/.config/tidalt \
  -v ~/.local/share/tidalt:/root/.local/share/tidalt \
  benehiko/tidalt:latest
```

### Headless daemon

```bash
docker run -d \
  --name tidalt \
  --restart unless-stopped \
  --device /dev/snd \
  --group-add $(getent group audio | cut -d: -f3) \
  -v ~/.config/tidalt:/root/.config/tidalt \
  -v ~/.local/share/tidalt:/root/.local/share/tidalt \
  benehiko/tidalt:latest daemon
```

Then attach the TUI from any terminal on the host:

```bash
tidalt  # connects to the running daemon over D-Bus
```

Or control playback with `playerctl` — see [mpris2.md](mpris2.md).

### With D-Bus forwarding

```bash
docker run -d \
  --name tidalt \
  --restart unless-stopped \
  --device /dev/snd \
  --group-add $(getent group audio | cut -d: -f3) \
  -v ~/.config/tidalt:/root/.config/tidalt \
  -v ~/.local/share/tidalt:/root/.local/share/tidalt \
  -v /run/user/$(id -u)/bus:/run/user/1000/bus \
  -e DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus \
  benehiko/tidalt:latest daemon
```

---

## Debug logging

Pass `TIDALT_DEBUG=true` to write a timestamped log to
`/root/.local/share/tidalt/debug-*.log`:

```bash
docker run -it --rm \
  --device /dev/snd \
  --group-add $(getent group audio | cut -d: -f3) \
  -v ~/.config/tidalt:/root/.config/tidalt \
  -v ~/.local/share/tidalt:/root/.local/share/tidalt \
  -e TIDALT_DEBUG=true \
  benehiko/tidalt:latest
```

See [debugging.md](debugging.md) for more detail.
