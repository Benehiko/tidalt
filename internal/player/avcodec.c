// avcodec.c — implementation of the FFmpeg decode pipeline declared in
// avcodec.h. See avcodec.go for the Go side of this boundary.
#include "avcodec.h"

#include <libavutil/opt.h>

#include <stdlib.h>
#include <string.h>

#define AVIO_BUF_SIZE ((size_t)32 * 1024)

int av_open(av_decoder_t *d, void *opaque) {
    memset(d, 0, sizeof(*d));

    unsigned char *avio_buf = (unsigned char *)av_malloc(AVIO_BUF_SIZE);
    if (!avio_buf) return AVERROR(ENOMEM);

    AVIOContext *avio = avio_alloc_context(
        avio_buf, (int)AVIO_BUF_SIZE,
        0, opaque,
        avio_read_cb, NULL, NULL
    );
    if (!avio) { av_free(avio_buf); return AVERROR(ENOMEM); }

    d->fmt_ctx = avformat_alloc_context();
    if (!d->fmt_ctx) {
        av_free(avio->buffer);
        avio_context_free(&avio);
        return AVERROR(ENOMEM);
    }
    d->fmt_ctx->pb    = avio;
    d->fmt_ctx->flags |= AVFMT_FLAG_CUSTOM_IO;

    int rc = avformat_open_input(&d->fmt_ctx, NULL, NULL, NULL);
    if (rc < 0) return rc;

    rc = avformat_find_stream_info(d->fmt_ctx, NULL);
    if (rc < 0) return rc;

    rc = av_find_best_stream(d->fmt_ctx, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
    if (rc < 0) return rc;
    d->stream_idx = rc;

    AVStream *st = d->fmt_ctx->streams[d->stream_idx];
    const AVCodec *codec = avcodec_find_decoder(st->codecpar->codec_id);
    if (!codec) return AVERROR_DECODER_NOT_FOUND;

    d->codec_ctx = avcodec_alloc_context3(codec);
    if (!d->codec_ctx) return AVERROR(ENOMEM);

    rc = avcodec_parameters_to_context(d->codec_ctx, st->codecpar);
    if (rc < 0) return rc;

    rc = avcodec_open2(d->codec_ctx, codec, NULL);
    if (rc < 0) return rc;

    // swresample: codec output → S32LE interleaved, same rate/channels.
    d->swr = swr_alloc();
    if (!d->swr) return AVERROR(ENOMEM);

    AVChannelLayout layout;
    av_channel_layout_copy(&layout, &d->codec_ctx->ch_layout);

    av_opt_set_chlayout  (d->swr, "in_chlayout",    &d->codec_ctx->ch_layout, 0);
    av_opt_set_int       (d->swr, "in_sample_rate",  d->codec_ctx->sample_rate, 0);
    av_opt_set_sample_fmt(d->swr, "in_sample_fmt",   d->codec_ctx->sample_fmt, 0);
    av_opt_set_chlayout  (d->swr, "out_chlayout",    &layout, 0);
    av_opt_set_int       (d->swr, "out_sample_rate", d->codec_ctx->sample_rate, 0);
    av_opt_set_sample_fmt(d->swr, "out_sample_fmt",  AV_SAMPLE_FMT_S32, 0);
    av_channel_layout_uninit(&layout);

    rc = swr_init(d->swr);
    if (rc < 0) return rc;

    d->pkt       = av_packet_alloc();
    d->frame     = av_frame_alloc();
    d->resampled = av_frame_alloc();
    if (!d->pkt || !d->frame || !d->resampled) return AVERROR(ENOMEM);

    d->sample_rate = (uint32_t)d->codec_ctx->sample_rate;
    d->channels    = (uint8_t)d->codec_ctx->ch_layout.nb_channels;

    // Estimate total samples from stream metadata.
    // nb_frames is a packet/frame count; multiply by frame_size to get PCM
    // samples per channel (frame_size is 0 for raw PCM codecs, so fall back
    // to the duration-based estimate in that case).
    if (st->nb_frames > 0 && d->codec_ctx->frame_size > 0) {
        d->n_samples = st->nb_frames * (int64_t)d->codec_ctx->frame_size;
    } else if (st->duration != AV_NOPTS_VALUE && st->time_base.den > 0) {
        d->n_samples = (int64_t)((double)st->duration
                                 * st->time_base.num / st->time_base.den
                                 * d->sample_rate);
    }
    return 0;
}

int av_read_samples(av_decoder_t *d, int32_t **out_buf, int *out_count) {
    for (;;) {
        int rc = avcodec_receive_frame(d->codec_ctx, d->frame);
        if (rc == 0) goto resample;
        if (rc != AVERROR(EAGAIN)) return rc;

        // Feed the decoder.
        for (;;) {
            rc = av_read_frame(d->fmt_ctx, d->pkt);
            if (rc < 0) { avcodec_send_packet(d->codec_ctx, NULL); break; }
            if (d->pkt->stream_index != d->stream_idx) { av_packet_unref(d->pkt); continue; }
            avcodec_send_packet(d->codec_ctx, d->pkt);
            av_packet_unref(d->pkt);
            break;
        }
        continue;

    resample:;
        d->resampled->ch_layout   = d->codec_ctx->ch_layout;
        d->resampled->sample_rate = d->frame->sample_rate;
        d->resampled->format      = AV_SAMPLE_FMT_S32;

        rc = swr_convert_frame(d->swr, d->resampled, d->frame);
        av_frame_unref(d->frame);
        if (rc < 0) return rc;

        int n  = d->resampled->nb_samples;
        int ch = d->codec_ctx->ch_layout.nb_channels;
        // Widen before multiplying so the sample count cannot overflow int.
        size_t nbytes = (size_t)n * (size_t)ch * sizeof(int32_t);
        int32_t *buf = (int32_t *)av_malloc(nbytes);
        if (!buf) { av_frame_unref(d->resampled); return AVERROR(ENOMEM); }
        memcpy(buf, d->resampled->data[0], nbytes);
        av_frame_unref(d->resampled);
        *out_buf   = buf;
        *out_count = n;
        return 0;
    }
}

void av_close(av_decoder_t *d) {
    if (d->pkt)       av_packet_free(&d->pkt);
    if (d->frame)     av_frame_free(&d->frame);
    if (d->resampled) av_frame_free(&d->resampled);
    if (d->swr)       swr_free(&d->swr);
    if (d->codec_ctx) avcodec_free_context(&d->codec_ctx);
    if (d->fmt_ctx) {
        AVIOContext *pb = d->fmt_ctx->pb;
        avformat_close_input(&d->fmt_ctx);
        if (pb) avio_context_free(&pb);
    }
}

void av_strerr(int rc, char *buf, int sz) {
    av_strerror(rc, buf, (size_t)sz);
}
