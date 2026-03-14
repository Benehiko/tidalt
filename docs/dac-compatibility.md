# DAC Compatibility Notes

## Hidizs S9 Pro Balanced

Works perfectly in exclusive `hw:` mode. Bit-perfect playback confirmed.

## Focusrite Scarlett Solo (3rd Gen)

Works perfectly in exclusive `hw:` mode. Bit-perfect playback confirmed.

## Hidizs S9 Pro Plus ("Martha")

**Status: Distorted audio in exclusive mode.**

### Symptoms

All audio is heavily distorted from the first frame of playback. The distortion is
consistent throughout — not intermittent dropouts — and sounds like clipping or
pitch corruption. The device is also left in a corrupted state after the app exits,
causing subsequent ffmpeg playback to be distorted until the device is unplugged
and replugged.

### Investigation

Extensive debugging ruled out the following as causes:

- PCM encoding (verified byte-for-byte correct against the mewkiz/flac library's own MD5)
- ALSA hw_params (period_size=87, buffer_size=88200 — identical to ffmpeg)
- sw_params (stop_threshold, avail_min — defaults match ffmpeg)
- Channel interleaving and sample byte order
- WirePlumber interference (`hw:0,0` used directly, D-Bus reservation held)
- USB altset selection (Altset 1 / S16_LE correctly negotiated)
- Buffer underruns (the PCM state remains RUNNING throughout)

The key observation: after our app opens and closes `hw:0,0`, the device is left in
a bad state. Even ffmpeg — which plays cleanly on a freshly reset device — produces
distorted output until the S9 Pro Plus is physically unplugged and replugged.

### Root Cause

The S9 Pro Plus uses an ESS9038Q2M DAC chip with a USB bridge controller. When the
UAC2 stream opens, the DAC's internal PLL must lock to the new sample rate clock.
On the S9 Pro Plus, the firmware's power-management and anti-pop muting logic
introduces enough delay that the PLL has not locked by the time the first audio
frames arrive. This corrupts the initial audio and, critically, leaves the USB
audio endpoint in an inconsistent state.

The older S9 Pro Balanced does not exhibit this behaviour, likely due to a different
USB bridge chip or firmware with a faster PLL lock sequence.

### Workarounds

- **Firmware update**: Hidizs may have addressed this in a newer S9 Pro Plus
  firmware release. Check the [Hidizs support page](https://www.hidizs.net) for
  updates.
- **Use via PipeWire**: WirePlumber handles the PLL lock delay internally, so
  playback through PipeWire works (at the cost of bit-perfect output).
- **Use a different device**: The Focusrite Scarlett Solo and Hidizs S9 Pro
  Balanced both work correctly with this application.
