# Debugging

## Enabling debug logs

Set the `TIDALT_DEBUG` environment variable to `true` before launching:

```bash
TIDALT_DEBUG=true tidalt
```

A timestamped log file is written to:

```
~/.local/share/tidalt/debug-YYYYMMDD-HHMMSS.log
```

Each run creates a new file. Old files are not automatically removed.

## What is logged

| Event | Fields |
|-------|--------|
| HTTP requests to the Tidal API | method, URL (query strings with tokens are redacted) |
| HTTP responses | status code, content-type, content-length |
| FLAC stream metadata | sample rate, channels, bit depth, total samples |
| ALSA device open | device name, PCM format, period size, buffer size |
| Playback loop start/stop | stream URL (redacted) |

## Reading the log

```bash
# Follow the most recent log in real time
tail -f ~/.local/share/tidalt/debug-*.log

# Show only API-related lines
grep 'msg="HTTP' ~/.local/share/tidalt/debug-*.log

# Show ALSA open parameters
grep 'msg="ALSA opened"' ~/.local/share/tidalt/debug-*.log
```

## Token redaction

All URLs that contain query-string parameters (including OAuth tokens and stream
signing parameters) have their query strings stripped before being written to
the log. The path and host are kept so requests are still identifiable.

Example — the raw URL:

```
https://lgf.audio.tidal.com/mediatracks/CAEaKw.../0.flac?token=abc&expires=123
```

Is logged as:

```
https://lgf.audio.tidal.com/mediatracks/CAEaKw.../0.flac
```

## Common error patterns

### `countryCode parameter missing`

The session's `CountryCode` field is empty. Re-authenticate to refresh the session:

```bash
rm -f ~/.config/tidalt/secrets
tidalt
```

### `Track [ID] not found`

The track is not available in your country or has been removed from Tidal. The
app skips unavailable tracks automatically when loading a Daily Mix.

### `failed to open bolt db: timeout`

Another instance of `tidalt` is already running and holds the database lock.
Quit the other instance first.

### Audio distortion on first playback

See [DAC Compatibility Notes](dac-compatibility.md) for known hardware issues,
particularly the Hidizs S9 Pro Plus PLL lock delay.
