// alsa.c — implementation of the raw ALSA open/negotiation helpers declared in
// alsa.h. See mpv.go for the Go side of this boundary.
#include "alsa.h"

#include <errno.h>

int open_hw_device(const char *device, snd_pcm_t **handle_out) {
    return snd_pcm_open(handle_out, device, SND_PCM_STREAM_PLAYBACK, 0);
}

int configure_hw_pcm(unsigned int channels, unsigned int rate, int bits,
                     snd_pcm_t **handle_out,
                     alsa_open_result_t *result) {
    int rc;

    snd_pcm_hw_params_t *params;
    snd_pcm_hw_params_alloca(&params);

    rc = snd_pcm_hw_params_any(*handle_out, params);
    if (rc < 0) goto fail;

    rc = snd_pcm_hw_params_set_access(*handle_out, params,
                                       SND_PCM_ACCESS_RW_INTERLEAVED);
    if (rc < 0) goto fail;

    // Negotiate format — try preferred formats for the source bit depth.
    {
        // Prefer S32_LE for 16-bit sources: many USB DACs (e.g. CS43198-based
        // devices) have a buggy or non-functional S16_LE USB endpoint but work
        // correctly via their native 32-bit endpoint.  The 16-bit samples are
        // left-shifted to fill the MSB (standard MSB-aligned convention).
        snd_pcm_format_t fmt16[] = {SND_PCM_FORMAT_S32_LE,
                                    SND_PCM_FORMAT_S16_LE,
                                    SND_PCM_FORMAT_S24_3LE,
                                    SND_PCM_FORMAT_S24_LE};
        snd_pcm_format_t fmt24[] = {SND_PCM_FORMAT_S24_3LE,
                                    SND_PCM_FORMAT_S24_LE,
                                    SND_PCM_FORMAT_S32_LE};
        snd_pcm_format_t fmt32[] = {SND_PCM_FORMAT_S32_LE};
        snd_pcm_format_t *fmts   = (bits == 16) ? fmt16 : (bits == 24) ? fmt24 : fmt32;
        int               nfmts  = (bits == 16) ? 4      : (bits == 24) ? 3     : 1;
        snd_pcm_format_t  chosen = SND_PCM_FORMAT_UNKNOWN;

        for (int i = 0; i < nfmts; i++) {
            if (snd_pcm_hw_params_set_format(*handle_out, params, fmts[i]) == 0) {
                chosen = fmts[i];
                break;
            }
        }
        if (chosen == SND_PCM_FORMAT_UNKNOWN) { rc = -EINVAL; goto fail; }

        result->format = chosen;
        switch (chosen) {
            case SND_PCM_FORMAT_S16_LE:  result->bytes_per_sample = 2; break;
            case SND_PCM_FORMAT_S24_3LE: result->bytes_per_sample = 3; break;
            default:                     result->bytes_per_sample = 4; break;
        }
    }

    rc = snd_pcm_hw_params_set_channels(*handle_out, params, channels);
    if (rc < 0) goto fail;

    rc = snd_pcm_hw_params_set_rate_near(*handle_out, params, &rate, 0);
    if (rc < 0) goto fail;
    result->rate = rate;

    // Set period size first so the DAC gets a sane interrupt rate (~23ms at
    // 44100 Hz), then set the buffer to 4× the negotiated period.  Setting
    // buffer first and then querying period_size_min can return absurdly small
    // values on some USB DACs (e.g. 87 frames on the Hidizs S9 Pro Plus
    // "Martha"), which causes ~1000 interrupts/s and severe distortion.
    {
        snd_pcm_uframes_t period_size = 1024;
        rc = snd_pcm_hw_params_set_period_size_near(*handle_out, params, &period_size, NULL);
        if (rc < 0) goto fail;

        snd_pcm_uframes_t buffer_size = period_size * 4;
        rc = snd_pcm_hw_params_set_buffer_size_near(*handle_out, params, &buffer_size);
        if (rc < 0) goto fail;
    }

    rc = snd_pcm_hw_params(*handle_out, params);
    if (rc < 0) goto fail;

    // Query the hardware's actual significant bit depth (e.g. 24 for a DAC
    // that uses S32_LE as a 24-bit MSB-aligned container).
    result->significant_bits = snd_pcm_hw_params_get_sbits(params);

    // Read back period/buffer for logging — use ALSA defaults for sw_params.
    {
        snd_pcm_uframes_t period_size, buffer_size;
        snd_pcm_hw_params_get_period_size(params, &period_size, NULL);
        snd_pcm_hw_params_get_buffer_size(params, &buffer_size);
        result->period_size = period_size;
        result->buffer_size = buffer_size;

        snd_pcm_sw_params_t *sw;
        snd_pcm_sw_params_alloca(&sw);
        snd_pcm_sw_params_current(*handle_out, sw);
        snd_pcm_sw_params_get_avail_min(sw, &result->avail_min);
        snd_pcm_sw_params_get_start_threshold(sw, &result->start_threshold);
        snd_pcm_sw_params_get_stop_threshold(sw, &result->stop_threshold);
    }

    return 0;

fail:
    snd_pcm_close(*handle_out);
    *handle_out = NULL;
    return rc;
}
