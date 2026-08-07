package player

/*
#include "avcodec.h"
*/
import "C" //nolint:gocritic // dupImport false positive: cgo "C" pseudo-package aliases unsafe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"unsafe" //nolint:gocritic // dupImport false positive: cgo "C" pseudo-package aliases unsafe
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
	rc := C.av_open(&dec.d, unsafe.Pointer(dec.readerID)) //nolint:govet // readerID is an opaque integer handle, not a Go heap pointer
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
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
