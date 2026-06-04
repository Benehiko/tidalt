# CLAUDE.md

## Build & tooling

- **Go version**: 1.26+
- **Format**: run `gofumpt -w .` after writing or editing any `.go` file
- **Lint**: run `golangci-lint run` (v2) before finishing a task; fix all reported issues
- **Build**: `go build ./...`
- **CGO**: required — the player package links against libasound (`-lasound`)

## Package overview

### `cmd/tidalt`
Entry point. Handles signal setup, session load/restore from the secrets store, OAuth2 device-flow login on first run, and launches the BubbleTea TUI program.

### `internal/tidal`
Tidal API client.
- `client.go` — OAuth2 device-flow authentication, token refresh, authenticated HTTP client
- `api.go` — REST calls: favorites, search, track lookup, stream URL (quality ladder: HI_RES_LOSSLESS → LOSSLESS → HIGH → LOW), mixes, mix tracks, artist albums/top-tracks/all-tracks

### `internal/player`
Bit-perfect FLAC playback via CGO + libasound.
- Opens ALSA `hw:` devices directly, bypassing PipeWire/PulseAudio
- Negotiates the best PCM format the DAC supports using `snd_pcm_hw_params` (no soft resampling)
- Format preference for 16-bit sources: S32_LE → S16_LE → S24_3LE → S24_LE (S32_LE first because some DACs, e.g. CS43198-based Hidizs S9 Pro Plus, have a broken S16_LE USB endpoint)
- Format preference for 24-bit sources: S24_3LE → S24_LE → S32_LE
- Acquires `org.freedesktop.ReserveDevice1.Audio{N}` on D-Bus before opening the device, asking PipeWire to release if it holds the reservation
- Demuxes and decodes the HTTP stream in-flight via FFmpeg (libavformat/libavcodec/libswresample, CGO) — FLAC, AAC/mp4, and ALAC — resampling to S32LE. A custom AVIO callback feeds bytes straight from the HTTP response. FFmpeg is linked dynamically for local/dev/CI builds (needs the distro's libav*-dev headers); the official distro packages (`packaging/`) bundle a minimal static FFmpeg built from source, selected with the `staticav` build tag
- Volume, pause, and position tracking via atomics
- Auto-detects known DACs (Hidizs S9 Pro, Hidizs S9 Pro Plus "Martha", Focusrite Scarlett Solo) from `/proc/asound/cards`

### `internal/store`
Persistent storage.
- OAuth2 session stored securely via `docker/secrets-engine` (system keychain, falling back to age-encrypted file at `~/.config/tidalt/secrets`)
- Volume, selected device, and track metadata cache stored in a bbolt database at `~/.local/share/tidalt/tidal-cache.db`

### `internal/ui`
BubbleTea TUI model (Model/Update/View).
- Five states: `StateBrowse`, `StateMixes`, `StateSearch`, `StateDeviceSelect`, `StateArtistAlbums`
- Scrollable track and mix lists with a visible window helper
- Artist view (`StateArtistAlbums`): opened with `a` on any track; lists the artist's albums plus "Play all tracks" / "Top tracks" entries, loading the chosen tracks into the browse queue
- Progress bar with playback position, volume display, and device label
- Auto-advances to the next track in the queue when playback finishes
