// alsa.h — raw ALSA PCM open and format negotiation used by the bit-perfect
// playback path. The implementation lives in alsa.c; mpv.go calls these
// functions through cgo.
#ifndef TIDALT_ALSA_H
#define TIDALT_ALSA_H

#include <alsa/asoundlib.h>

// alsa_open_result carries the negotiated format back to Go.
typedef struct {
    snd_pcm_format_t  format;
    int               bytes_per_sample;
    int               significant_bits; // actual DAC bit depth (e.g. 24 for Scarlett, 32 for S9 Pro Plus)
    unsigned int      rate;             // negotiated sample rate (may differ from requested)
    snd_pcm_uframes_t period_size;
    snd_pcm_uframes_t buffer_size;
    snd_pcm_uframes_t avail_min;
    snd_pcm_uframes_t start_threshold;
    snd_pcm_uframes_t stop_threshold;
} alsa_open_result_t;

// open_hw_device opens the raw PCM handle only. It is split out from
// configure_hw_pcm so the caller can retry a busy open (EBUSY) without the
// retry also covering format negotiation — a device that answers the open but
// rejects our parameters fails deterministically and must not be reopened in a
// loop.
int open_hw_device(const char *device, snd_pcm_t **handle_out);

// configure_hw_pcm negotiates the best available format for the given bit
// depth on an already-open handle, without enabling soft resampling.
// Format preference order:
//   16-bit source : S32_LE  → S16_LE → S24_3LE → S24_LE
//   24-bit source : S24_3LE → S24_LE → S32_LE
// Returns 0 on success, a negative ALSA error code on failure. On failure the
// handle is closed and *handle_out is set to NULL.
int configure_hw_pcm(unsigned int channels, unsigned int rate, int bits,
                     snd_pcm_t **handle_out,
                     alsa_open_result_t *result);

#endif // TIDALT_ALSA_H
