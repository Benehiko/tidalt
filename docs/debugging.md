# Debugging

## Enabling debug logs

Set the `TIDALT_DEBUG` environment variable to `true` before launching:

```bash
TIDALT_DEBUG=true tidalt
```

A timestamped log file is written to:

```
~/.local/share/tidalt/tidalt-YYYYMMDD-HHMMSS.log
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
| Write blocking distribution | per-track histogram of `snd_pcm_writei` blocking times, stall count, worst stall |

## Reading the log

```bash
# Follow the most recent log in real time
tail -f ~/.local/share/tidalt/tidalt-*.log

# Show only API-related lines
grep 'msg="HTTP' ~/.local/share/tidalt/tidalt-*.log

# Show ALSA open parameters
grep 'msg="ALSA opened"' ~/.local/share/tidalt/tidalt-*.log

# Show the per-track write blocking distribution
grep 'msg="write blocking distribution for track"' ~/.local/share/tidalt/tidalt-*.log
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

### `write blocking distribution for track`

Logged once per track, always. `snd_pcm_writei` blocks until the device accepts
the frames, so how long it blocks measures the headroom the DAC had left. Such
a write still returns a positive frame count and raises no ALSA error, so
without this tally an audible cut leaves no trace at all.

```
INFO write blocking distribution for track writes=6103 instant=6081 brief=19 overPeriod=2 overQuarterBuffer=1 overBuffer=0 stalls=3 worst=41ms totalStalled=118ms periodPlayTime=23ms bufferPlayTime=93ms
```

Read the buckets, and read them against what healthy playback looks like:

| Shape | Means |
|-------|-------|
| `overPeriod` / `overQuarterBuffer` dominant, `overBuffer=0` | **Normal.** A write blocking for about a period is ALSA applying backpressure, not a fault: with a four-period buffer the steady state is that the buffer fills, the write waits for one period to drain, then returns. A healthy track measured 10511 of 10639 writes in these buckets. |
| `instant` ≈ `writes` | The app fed ALSA on time and never had to wait. Any cut you hear happened *past* the write — see below. |
| `overBuffer` above zero | The device took longer than the entire buffer to accept a write, so it had nothing left to play. This is the bucket worth alarming on, and the only one that raises a warning. |

A run where nearly every write is `instant` and you still hear cuts points
outside the app entirely: a congested USB bus shared with other streaming audio
devices, an intermediate hub, or the host failing to deliver isochronous frames
on time. Isochronous transfers are never retransmitted, so a frame missed on
the wire is simply lost — silently, with nothing to log.

This is not an xrun. An xrun logs `snd_pcm_writei error, recovering` with
`Broken pipe` and means the buffer emptied between writes; a stall means the
write itself was held up.

### Audio distortion on first playback

See [DAC Compatibility Notes](dac-compatibility.md) for known hardware issues,
particularly the Hidizs S9 Pro Plus PLL lock delay.
