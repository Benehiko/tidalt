// avcodec.h — FFmpeg-based demux/decode/resample pipeline used to turn the
// HTTP stream into interleaved S32LE PCM. The implementation lives in
// avcodec.c; avcodec.go calls these functions through cgo.
//
// Linker and include flags are provided by build-tag-gated Go files:
//   avcodec_dynamic.go (default) links the system-shared FFmpeg libraries.
//   avcodec_static.go  (-tags staticav) links a self-contained static FFmpeg
//   built under /opt/ffmpeg, for portable distro packages.
#ifndef TIDALT_AVCODEC_H
#define TIDALT_AVCODEC_H

#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/avutil.h>
#include <libswresample/swresample.h>

#include <stdint.h>

// avio_read_cb is the AVIO read callback. opaque is a uintptr_t (cast to
// void*) identifying the Go reader registered in the readerMap.
// Implemented in Go via a CGO export in avcodec.go.
extern int avio_read_cb(void *opaque, uint8_t *buf, int buf_size);

typedef struct {
    AVFormatContext  *fmt_ctx;
    AVCodecContext   *codec_ctx;
    SwrContext       *swr;
    AVPacket         *pkt;
    AVFrame          *frame;
    AVFrame          *resampled;
    int               stream_idx;

    // Populated after av_open succeeds.
    uint32_t          sample_rate;
    uint8_t           channels;
    int64_t           n_samples;   // total samples/channel; 0 if unknown
} av_decoder_t;

// av_open opens an AVFormatContext using a custom AVIO context that calls
// avio_read_cb(opaque, ...) to read data. Returns 0 on success.
int av_open(av_decoder_t *d, void *opaque);

// av_read_samples decodes the next block and resamples to S32LE interleaved.
// *out_buf is av_malloc'd by this function; caller must av_free it.
// *out_count is samples per channel. Returns 0, AVERROR_EOF, or error.
int av_read_samples(av_decoder_t *d, int32_t **out_buf, int *out_count);

// av_close frees every resource held by the decoder.
void av_close(av_decoder_t *d);

// av_strerr writes the human-readable form of an FFmpeg error code into buf.
void av_strerr(int rc, char *buf, int sz);

#endif // TIDALT_AVCODEC_H
