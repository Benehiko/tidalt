# DAC Compatibility Notes

## Hidizs S9 Pro Balanced

Works perfectly in exclusive `hw:` mode. Bit-perfect playback confirmed.

## Focusrite Scarlett Solo (3rd Gen)

Works perfectly in exclusive `hw:` mode. Bit-perfect playback confirmed.

## Hidizs S9 Pro Plus ("Martha")

Works in exclusive `hw:` mode. Bit-perfect playback confirmed.

### Notes

The Martha uses a **CS43198** DAC chip (native 32-bit). Despite advertising S16_LE
support via ALSA, its S16_LE USB audio endpoint produces severe distortion — a
known quirk of this chip family. tidalt works around this by preferring S32_LE for
16-bit sources: the 16-bit samples are MSB-aligned in a 32-bit container, which
routes through the DAC's native 32-bit USB endpoint and plays cleanly.

The older S9 Pro Balanced uses a different DAC chip whose S16_LE endpoint works
correctly, so no workaround is needed there.

Additionally, the Martha reports an abnormally small minimum period size (87 frames)
when ALSA's buffer is set first. tidalt negotiates the period size before the buffer
size to avoid this, ensuring a sane interrupt rate (~23 ms at 44100 Hz).
