package player

/*
#cgo LDFLAGS: -lavformat -lavcodec -lavutil -lswresample
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/avutil.h>
#include <libavutil/opt.h>
#include <libswresample/swresample.h>
#include <stdlib.h>
#include <string.h>

// avio_read_cb is the AVIO read callback. opaque is a uintptr_t (cast to
// void*) identifying the Go reader registered in the readerMap.
// Implemented in Go below via CGO export.
extern int avio_read_cb(void *opaque, uint8_t *buf, int buf_size);

#define AVIO_BUF_SIZE (32 * 1024)

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
static int av_open(av_decoder_t *d, void *opaque) {
    memset(d, 0, sizeof(*d));

    unsigned char *avio_buf = (unsigned char *)av_malloc(AVIO_BUF_SIZE);
    if (!avio_buf) return AVERROR(ENOMEM);

    AVIOContext *avio = avio_alloc_context(
        avio_buf, AVIO_BUF_SIZE,
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

// av_read_samples decodes the next block and resamples to S32LE interleaved.
// *out_buf is av_malloc'd by this function; caller must av_free it.
// *out_count is samples per channel. Returns 0, AVERROR_EOF, or error.
static int av_read_samples(av_decoder_t *d, int32_t **out_buf, int *out_count) {
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
        int32_t *buf = (int32_t *)av_malloc((size_t)(n * ch) * sizeof(int32_t));
        if (!buf) { av_frame_unref(d->resampled); return AVERROR(ENOMEM); }
        memcpy(buf, d->resampled->data[0], (size_t)(n * ch) * sizeof(int32_t));
        av_frame_unref(d->resampled);
        *out_buf   = buf;
        *out_count = n;
        return 0;
    }
}

static void av_close(av_decoder_t *d) {
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

static void av_strerr(int rc, char *buf, int sz) {
    av_strerror(rc, buf, (size_t)sz);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"unsafe"
)

// streamInfo holds the audio parameters of an opened stream.
type streamInfo struct {
	SampleRate    uint32
	NChannels     uint8
	BitsPerSample uint8 // always 32 (S32LE output from avcodec path)
	NSamples      uint64
}

// audioStream wraps an avcodec decoder and its associated HTTP response.
// It is the type returned by openStream and consumed by playbackLoop.
type audioStream struct {
	Info    streamInfo
	decoder *avDecoder
}

// ReadSamples returns the next block of interleaved S32LE samples.
// Returns nil, io.EOF at end of stream.
func (s *audioStream) ReadSamples() ([]int32, error) {
	return s.decoder.readSamples()
}

// Close frees decoder resources. The caller is still responsible for closing
// the HTTP response body separately.
func (s *audioStream) Close() {
	s.decoder.close()
}

// --- reader registry ---------------------------------------------------------
// AVIO callbacks are C functions; they cannot capture Go values. We pass a
// numeric ID as the opaque pointer and look up the Go io.Reader from a map.

var (
	readerMu     sync.Mutex
	readerMap            = map[uintptr]io.Reader{}
	readerNextID uintptr = 1
)

func registerReader(r io.Reader) uintptr {
	readerMu.Lock()
	defer readerMu.Unlock()
	id := readerNextID
	readerNextID++
	readerMap[id] = r
	return id
}

func unregisterReader(id uintptr) {
	readerMu.Lock()
	defer readerMu.Unlock()
	delete(readerMap, id)
}

//export avio_read_cb
func avio_read_cb(opaque unsafe.Pointer, buf *C.uint8_t, bufSize C.int) C.int {
	id := uintptr(opaque)
	readerMu.Lock()
	r := readerMap[id]
	readerMu.Unlock()
	if r == nil {
		return C.int(C.AVERROR_EOF)
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(bufSize))
	n, err := r.Read(dst)
	if n > 0 {
		return C.int(n)
	}
	if errors.Is(err, io.EOF) {
		return C.int(C.AVERROR_EOF)
	}
	return C.int(C.AVERROR_EOF)
}

// --- avDecoder ---------------------------------------------------------------

type avDecoder struct {
	d        C.av_decoder_t
	readerID uintptr
	closed   uint32
}

func newAvDecoder(r io.Reader) (*avDecoder, error) {
	dec := &avDecoder{}
	dec.readerID = registerReader(r)

	// Pass the numeric reader ID as the AVIO opaque pointer.
	// dec.readerID is a plain integer, not a pointer into Go heap memory,
	// so this conversion is safe despite the unsafeptr vet warning.
	rc := C.av_open(&dec.d, unsafe.Pointer(dec.readerID)) //nolint:govet
	if rc < 0 {
		unregisterReader(dec.readerID)
		return nil, avErr("avcodec open", rc)
	}
	return dec, nil
}

func (d *avDecoder) readSamples() ([]int32, error) {
	var outBuf *C.int32_t
	var outCount C.int

	rc := C.av_read_samples(&d.d, &outBuf, &outCount)
	if rc < 0 {
		if rc == C.int(C.AVERROR_EOF) {
			return nil, io.EOF
		}
		return nil, avErr("av_read_samples", rc)
	}

	n := int(outCount) * int(d.d.channels)
	samples := make([]int32, n)
	src := unsafe.Slice((*int32)(unsafe.Pointer(outBuf)), n)
	copy(samples, src)
	C.av_free(unsafe.Pointer(outBuf))
	return samples, nil
}

func (d *avDecoder) close() {
	if !atomic.CompareAndSwapUint32(&d.closed, 0, 1) {
		return
	}
	C.av_close(&d.d)
	unregisterReader(d.readerID)
}

func avErr(op string, rc C.int) error {
	buf := make([]byte, 256)
	C.av_strerr(rc, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	return fmt.Errorf("%s: %s", op, buf)
}

// --- openStream --------------------------------------------------------------

// openStream fetches the audio stream at url via HTTP and opens an avcodec
// decoder for it. The caller must call stream.Close() and resp.Body.Close().
func openStream(ctx context.Context, url string) (*http.Response, *audioStream, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("HTTP %d fetching stream", resp.StatusCode)
	}

	dec, err := newAvDecoder(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, err
	}

	info := streamInfo{
		SampleRate:    uint32(dec.d.sample_rate),
		NChannels:     uint8(dec.d.channels),
		BitsPerSample: 32,
		NSamples:      uint64(dec.d.n_samples),
	}
	return resp, &audioStream{Info: info, decoder: dec}, nil
}
