# Tidal HiFi Project Standards & Architecture

This project is a high-fidelity Tidal terminal client written in Go (1.26+), focusing on bit-perfect audio playback via ALSA.

## Engineering Standards

- **Go Version:** 1.26.1 or higher.
- **Linting:** We use `golangci-lint` (v2.11.3+).
- **Formatting:** We use `gofumpt` for stricter formatting and `goimports` for import management.
- **Validation:** ALWAYS run `golangci-lint run` before committing. The configuration is managed in `.golangci.yml` (version 2).

## Project Structure

- `cmd/tidalt/`: The entry point. Handles initial authentication, vault restoration, and launches the Bubble Tea TUI.
- `internal/tidal/`: Core Tidal API client. Implements the OAuth2 Device Authorization Grant flow and all API interactions (search, favorites, mixes, stream URL resolution).
- `internal/player/`: The audio engine. Uses CGO to interface with ALSA for bit-perfect playback. Handles D-Bus device reservation to ensure exclusive access to the DAC.
- `internal/ui/`: The terminal user interface built with `bubbletea`. Manages state for browsing, searching, and playback control.
- `internal/store/`: Persistence layer. Uses `bbolt` for caching tracks/settings and `docker/secrets-engine` (with Keychain/Posixage fallbacks) for secure session storage.

## Core Workflows

### Tidal Authentication
We use the **OAuth2 Device Authorization Grant**.
1. The client requests a device code from Tidal.
2. The user is prompted to open a URL and enter the code.
3. The client polls for the access token.
4. Once authorized, the session is encrypted and saved to the system keychain via `internal/store`.

### Bit-Perfect Playback
Playback is designed for high-fidelity audio:
1. **Device Reservation:** The player uses D-Bus (`org.freedesktop.ReserveDevice1`) to ask PipeWire/PulseAudio to release the hardware device.
2. **FLAC Decoding:** Streams are decoded using `mewkiz/flac`.
3. **ALSA Output:** Decoded PCM data is written directly to the ALSA hardware device (`hw:X,Y`) using CGO.
4. **No Resampling:** The hardware is opened with the exact sample rate and bit depth of the source file to ensure bit-perfect delivery to the DAC.
5. **Volume Control:** Soft volume is applied during the PCM write loop if the volume is not at 100%.

## Development Cycle
1. **Act:** Implement changes or fixes.
2. **Format:** Ensure code is formatted with `gofumpt`.
3. **Lint:** Run `golangci-lint run` to verify quality and formatting.
4. **Validate:** Test playback and TUI interactions manually as ALSA requires hardware access.
