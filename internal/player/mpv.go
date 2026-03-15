package player

/*
#cgo LDFLAGS: -lasound
#include <alsa/asoundlib.h>
#include <stdlib.h>
#include <errno.h>

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

// open_hw_pcm opens an ALSA hw device and negotiates the best available
// format for the given bit depth, without enabling soft resampling.
// Format preference order:
//   16-bit source : S16_LE  → S32_LE
//   24-bit source : S24_3LE → S24_LE → S32_LE
// Returns 0 on success, a negative ALSA error code on failure.
static int open_hw_pcm(const char *device,
                       unsigned int channels, unsigned int rate, int bits,
                       snd_pcm_t **handle_out,
                       alsa_open_result_t *result) {
    int rc;

    rc = snd_pcm_open(handle_out, device, SND_PCM_STREAM_PLAYBACK, 0);
    if (rc < 0) return rc;

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
        snd_pcm_format_t *fmts   = (bits == 16) ? fmt16 : fmt24;
        int               nfmts  = (bits == 16) ? 4      : 3;
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
*/
import "C"

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/Benehiko/tidalt/internal/logger"

	"github.com/godbus/dbus/v5"
	"github.com/mewkiz/flac"
)

// knownDACs lists substrings to search for in /proc/asound/cards output.
// First match wins, so order determines priority.
var knownDACs = []string{"hidizs", "s9pro", "focusrite", "scarlett"}

// DeviceInfo describes an ALSA playback device.
type DeviceInfo struct {
	HWName   string // ALSA device string, e.g. "hw:1,0"
	CardName string // short name from brackets, e.g. "S9Pro"
	LongName string // description after " - ", e.g. "HiDizs S9 Pro"
}

// ListDevices returns all ALSA cards that have at least one playback PCM.
func ListDevices() ([]DeviceInfo, error) {
	cardData, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return nil, fmt.Errorf("cannot read /proc/asound/cards: %w", err)
	}
	pcmData, err := os.ReadFile("/proc/asound/pcm")
	if err != nil {
		return nil, fmt.Errorf("cannot read /proc/asound/pcm: %w", err)
	}

	// Collect card numbers that have at least one playback PCM.
	playback := make(map[int]bool)
	for _, line := range strings.Split(string(pcmData), "\n") {
		if !strings.Contains(line, "playback") {
			continue
		}
		var card, dev int
		if _, err := fmt.Sscanf(line, "%d-%d:", &card, &dev); err == nil {
			playback[card] = true
		}
	}

	var devices []DeviceInfo
	lines := strings.Split(string(cardData), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var cardNum int
		if _, err := fmt.Sscanf(trimmed, "%d", &cardNum); err != nil {
			continue // continuation line, not a card header
		}
		if !playback[cardNum] {
			continue
		}
		cardName := ""
		if s := strings.Index(line, "["); s != -1 {
			if e := strings.Index(line, "]"); e > s {
				cardName = strings.TrimSpace(line[s+1 : e])
			}
		}
		longName := ""
		if idx := strings.Index(line, " - "); idx != -1 {
			longName = strings.TrimSpace(line[idx+3:])
		}
		if longName == "" {
			longName = cardName
		}
		devices = append(devices, DeviceInfo{
			HWName:   fmt.Sprintf("hw:%d,0", cardNum),
			CardName: cardName,
			LongName: longName,
		})
	}
	return devices, nil
}

type Player struct {
	mu             sync.Mutex
	cancel         context.CancelFunc
	doneCh         chan struct{}
	deviceOverride string // set via SetDevice; empty = auto-detect
	currentURL     string // stored so Seek can signal the playback loop

	// seekCh carries seek targets (in samples) to the running playback loop.
	// Buffered 1 so Seek never blocks; the loop drains it before checking again.
	seekCh chan uint64

	// Track info — written by playbackLoop, read by UI tick
	muInfo        sync.RWMutex
	sampleRate    uint32
	channels      uint8
	bitsPerSample uint8
	totalSamples  uint64

	// Atomics: safe for concurrent access without a mutex
	samplesPlayed uint64
	paused        uint32 // 0 = playing, 1 = paused
	volumeBits    uint64 // float64 stored via math.Float64bits; range 0.0–1.0
}

// SetDevice sets the ALSA hw device to use for playback. Pass "" to revert to
// auto-detection from the known-DAC list.
func (p *Player) SetDevice(hwName string) {
	p.mu.Lock()
	p.deviceOverride = hwName
	p.mu.Unlock()
}

// getDevice returns the configured device override or falls back to auto-detection.
func (p *Player) getDevice() (string, error) {
	p.mu.Lock()
	override := p.deviceOverride
	p.mu.Unlock()
	if override != "" {
		return override, nil
	}
	return detectDevice()
}

func NewPlayer() *Player {
	p := &Player{
		seekCh: make(chan uint64, 1),
	}
	atomic.StoreUint64(&p.volumeBits, math.Float64bits(1.0))
	return p
}

// Start is a no-op; the ALSA handle is opened per-track in Play.
func (p *Player) Start(_ context.Context) error { return nil }

// detectDevice scans /proc/asound/cards for a known DAC and returns the
// hw device string, e.g. "hw:1,0".
func detectDevice() (string, error) {
	data, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return "", fmt.Errorf("cannot read ALSA cards: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, name := range knownDACs {
			if !strings.Contains(lower, name) {
				continue
			}
			// The card number is the leading integer on the card's first line.
			// Search current line and the one above it.
			for j := i; j >= 0 && j >= i-1; j-- {
				var num int
				if _, err := fmt.Sscanf(strings.TrimSpace(lines[j]), "%d", &num); err == nil {
					return fmt.Sprintf("hw:%d,0", num), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no supported DAC found — connect a Hidizs S9 Pro or Focusrite Scarlett Solo")
}

// parseCardNum extracts the card number from an ALSA hw device string like "hw:1,0".
func parseCardNum(hwDevice string) (int, error) {
	var card, dev int
	// Accept "hw:N,M", "plughw:N,M", and "front:N".
	for _, prefix := range []string{"plughw:%d,%d", "hw:%d,%d"} {
		if _, err := fmt.Sscanf(hwDevice, prefix, &card, &dev); err == nil {
			return card, nil
		}
	}
	if _, err := fmt.Sscanf(hwDevice, "front:%d", &card); err == nil {
		return card, nil
	}
	return 0, fmt.Errorf("cannot parse card number from %q", hwDevice)
}

// reserveALSADevice acquires the org.freedesktop.ReserveDevice1.Audio{N} D-Bus
// name so that PipeWire/PulseAudio releases the hw: device before we open it.
// If D-Bus is unavailable the function returns a no-op release func and nil error
// so callers can proceed unconditionally.
func reserveALSADevice(cardNum int) (release func(), err error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		// No session bus — skip reservation and try to open ALSA directly.
		return func() {}, nil
	}

	name := fmt.Sprintf("org.freedesktop.ReserveDevice1.Audio%d", cardNum)
	objPath := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/ReserveDevice1/Audio%d", cardNum))

	releaseFunc := func() {
		_, _ = conn.ReleaseName(name)
		_ = conn.Close()
	}

	// Ask the current owner (WirePlumber) to release the device, then claim
	// the name ourselves with ReplaceExisting so WirePlumber cannot reopen it.
	obj := conn.Object(name, objPath)
	var released bool
	if callErr := obj.Call("org.freedesktop.ReserveDevice1.RequestRelease", 0, int32(math.MaxInt32)).Store(&released); callErr != nil || !released {
		_ = conn.Close()
		return nil, fmt.Errorf("audio device Audio%d is held by another process and refused to release", cardNum)
	}

	// Give WirePlumber a moment to close its ALSA handle before we claim the
	// name and open the device.
	time.Sleep(200 * time.Millisecond)

	// Claim the name with ReplaceExisting so we take it even if WirePlumber
	// still holds it, and AllowReplacement so it can be returned on release.
	reply, err := conn.RequestName(name,
		dbus.NameFlagReplaceExisting|dbus.NameFlagAllowReplacement)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to claim Audio%d reservation", cardNum)
	}

	return releaseFunc, nil
}

type alsaHandle struct {
	pcm             *C.snd_pcm_t
	format          C.snd_pcm_format_t
	bytesPerSample  int
	significantBits int // actual DAC bit depth
	rate            uint32
	periodSize      uint64
	bufferSize      uint64
	availMin        uint64
	startThreshold  uint64
	stopThreshold   uint64
}

// openALSA opens an ALSA hw device, negotiating the best available format for
// the source bit depth without enabling soft resampling (bit-perfect).
// Retries for up to 2 seconds to allow WirePlumber to fully close its handle
// after releasing the D-Bus reservation.
func openALSA(device string, channels uint8, rate uint32, bits uint8) (*alsaHandle, error) {
	cdev := C.CString(device)
	defer C.free(unsafe.Pointer(cdev))

	var handle *C.snd_pcm_t
	var result C.alsa_open_result_t

	if rc := C.open_hw_pcm(cdev,
		C.uint(channels), C.uint(rate), C.int(bits),
		&handle, &result,
	); rc < 0 {
		return nil, fmt.Errorf("open_hw_pcm(%s, ch=%d, rate=%d, bits=%d): %s",
			device, channels, rate, bits, C.GoString(C.snd_strerror(rc)))
	}

	return &alsaHandle{
		pcm:             handle,
		format:          result.format,
		bytesPerSample:  int(result.bytes_per_sample),
		significantBits: int(result.significant_bits),
		rate:            uint32(result.rate),
		periodSize:      uint64(result.period_size),
		bufferSize:      uint64(result.buffer_size),
		availMin:        uint64(result.avail_min),
		startThreshold:  uint64(result.start_threshold),
		stopThreshold:   uint64(result.stop_threshold),
	}, nil
}

// Play starts playback of the given URL and returns the done channel for this
// track. The channel is closed when playback finishes naturally. Callers should
// use the returned channel directly rather than calling Done() separately to
// avoid a race between stop() clearing doneCh and the new one being set.
func (p *Player) Play(url string) (<-chan struct{}, error) {
	p.stop()

	// Resolve device and acquire D-Bus reservation synchronously so we can
	// return an error to the caller if the device cannot be claimed.
	device, err := p.getDevice()
	if err != nil {
		return nil, err
	}

	cardNum, err := parseCardNum(device)
	if err != nil {
		return nil, err
	}

	releaseReservation, err := reserveALSADevice(cardNum)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	p.mu.Lock()
	p.cancel = cancel
	p.doneCh = doneCh
	p.currentURL = url
	p.mu.Unlock()

	atomic.StoreUint64(&p.samplesPlayed, 0)
	atomic.StoreUint32(&p.paused, 0)
	// Drain any pending seek so the new track starts from the beginning.
	select {
	case <-p.seekCh:
	default:
	}

	// releaseReservation is passed into playbackLoop which manages it for the
	// lifetime of playback (releasing on pause, reacquiring on resume, and
	// releasing again on final exit via defer).
	go func() {
		defer close(doneCh)
		p.playbackLoop(ctx, url, device, releaseReservation)
	}()
	return doneCh, nil
}

func (p *Player) stop() {
	p.mu.Lock()
	cancel := p.cancel
	doneCh := p.doneCh
	p.cancel = nil
	p.doneCh = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
		}
	}
}

// openStream fetches the FLAC HTTP stream and returns the response and decoder.
// The caller is responsible for closing resp.Body.
func openStream(ctx context.Context, url string) (*http.Response, *flac.Stream, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	stream, err := flac.New(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, err
	}
	return resp, stream, nil
}

// closeALSA drains and closes an ALSA handle.
func closeALSA(ah *alsaHandle) {
	C.snd_pcm_drain(ah.pcm)
	C.snd_pcm_close(ah.pcm)
}

func (p *Player) playbackLoop(ctx context.Context, url, device string, releaseReservation func()) {
	logger.L.Debug("playbackLoop start", "url", url)

	cardNum, err := parseCardNum(device)
	if err != nil {
		logger.L.Error("playbackLoop: cannot parse card number", "device", device, "err", err)
		releaseReservation()
		return
	}

	resp, stream, err := openStream(ctx, url)
	if err != nil {
		logger.L.Error("failed to open stream", "err", err)
		releaseReservation()
		return
	}
	defer func() { _ = resp.Body.Close() }()

	logger.L.Debug("HTTP response",
		"status", resp.StatusCode,
		"content-type", resp.Header.Get("Content-Type"),
		"content-length", resp.Header.Get("Content-Length"),
	)

	info := stream.Info
	sampleRate := info.SampleRate
	channels := info.NChannels
	bits := info.BitsPerSample

	logger.L.Debug("FLAC stream",
		"rate", sampleRate,
		"channels", channels,
		"bits", bits,
		"samples", info.NSamples,
	)

	p.muInfo.Lock()
	p.sampleRate = sampleRate
	p.channels = channels
	p.bitsPerSample = bits
	p.totalSamples = info.NSamples
	p.muInfo.Unlock()

	// reacquireALSA re-claims the D-Bus reservation and reopens the ALSA
	// device. Used after releasing on pause.
	reacquireALSA := func() (*alsaHandle, func(), error) {
		rel, rerr := reserveALSADevice(cardNum)
		if rerr != nil {
			return nil, nil, rerr
		}
		a, aerr := openALSA(device, channels, sampleRate, bits)
		if aerr != nil {
			rel()
			return nil, nil, aerr
		}
		return a, rel, nil
	}

	ah, err := openALSA(device, channels, sampleRate, bits)
	if err != nil {
		logger.L.Error("openALSA failed", "device", device, "err", err)
		releaseReservation()
		return
	}
	logger.L.Debug("ALSA opened",
		"device", device,
		"format", ah.format,
		"bps", ah.bytesPerSample,
		"significantBits", ah.significantBits,
		"srcBits", bits,
		"rate_requested", sampleRate,
		"rate_negotiated", ah.rate,
		"period_size", ah.periodSize,
		"buffer_size", ah.bufferSize,
	)
	// ah and releaseReservation are both reassigned on pause/resume.
	// The defer captures them by pointer so it always closes the current handle.
	defer func() {
		closeALSA(ah)
		releaseReservation()
	}()

	bps := ah.bytesPerSample

	// streamLoop runs the decode→ALSA pipeline for the current HTTP stream.
	// Returns (seekTarget, true) if a seek was requested, or (0, false) when
	// the stream ends naturally or the context is cancelled.
	type pcmBuf struct {
		data    []byte
		nFrames int
	}

	streamLoop := func(skipSamples uint64) (seekTarget uint64, doSeek bool) {
		stopDecode := make(chan struct{})
		pcmCh := make(chan pcmBuf, 2)

		go func() {
			defer close(pcmCh)
			var skipped uint64
			for skipped < skipSamples {
				select {
				case <-ctx.Done():
					return
				case <-stopDecode:
					return
				default:
				}
				frame, ferr := stream.ParseNext()
				if ferr != nil {
					return
				}
				skipped += uint64(frame.BlockSize)
			}
			atomic.StoreUint64(&p.samplesPlayed, skipped)

			for {
				select {
				case <-ctx.Done():
					return
				case <-stopDecode:
					return
				default:
				}
				frame, ferr := stream.ParseNext()
				if ferr != nil {
					logger.L.Debug("FLAC decode done", "err", ferr)
					return
				}
				n := int(frame.BlockSize)
				buf := make([]byte, n*int(channels)*bps)
				vol := math.Float64frombits(atomic.LoadUint64(&p.volumeBits))
				for i := 0; i < n; i++ {
					for ch := 0; ch < int(channels); ch++ {
						s := frame.Subframes[ch].Samples[i]
						if vol != 1.0 {
							s = int32(float64(s) * vol)
						}
						off := (i*int(channels) + ch) * bps
						switch ah.format {
						case C.SND_PCM_FORMAT_S16_LE:
							binary.LittleEndian.PutUint16(buf[off:], uint16(int16(s)))
						case C.SND_PCM_FORMAT_S24_3LE:
							buf[off] = byte(s)
							buf[off+1] = byte(s >> 8)
							buf[off+2] = byte(s >> 16)
						case C.SND_PCM_FORMAT_S24_LE:
							shift := uint(ah.significantBits - int(bits))
							binary.LittleEndian.PutUint32(buf[off:], uint32(int32(s)<<shift))
						case C.SND_PCM_FORMAT_S32_LE:
							shift := uint(ah.significantBits - int(bits))
							binary.LittleEndian.PutUint32(buf[off:], uint32(int32(s)<<shift))
						}
					}
				}
				select {
				case pcmCh <- pcmBuf{data: buf, nFrames: n}:
				case <-ctx.Done():
					return
				case <-stopDecode:
					return
				}
			}
		}()

		returnSeek := func(target uint64) (uint64, bool) {
			close(stopDecode)
			// Drain so the decode goroutine can unblock and exit.
			for range pcmCh {
			}
			C.snd_pcm_drop(ah.pcm)
			C.snd_pcm_prepare(ah.pcm)
			return target, true
		}

		for pcm := range pcmCh {
			framesDone := 0
			for framesDone < pcm.nFrames {
				// Check for a seek request.
				select {
				case target := <-p.seekCh:
					return returnSeek(target)
				default:
				}

				// Check pause: release the ALSA device so PipeWire / other
				// apps can use it while we are idle, then reacquire on resume.
				if atomic.LoadUint32(&p.paused) == 1 {
					C.snd_pcm_drop(ah.pcm)
					closeALSA(ah)
					releaseReservation()
					logger.L.Debug("paused: ALSA device released")

					for atomic.LoadUint32(&p.paused) == 1 {
						select {
						case target := <-p.seekCh:
							// Reacquire before handling the seek.
							newAH, newRel, raErr := reacquireALSA()
							if raErr != nil {
								logger.L.Error("reacquire ALSA after pause+seek failed", "err", raErr)
								close(stopDecode)
								for range pcmCh {
								}
								return 0, false
							}
							ah = newAH
							releaseReservation = newRel
							return returnSeek(target)
						case <-ctx.Done():
							close(stopDecode)
							for range pcmCh {
							}
							return 0, false
						case <-time.After(20 * time.Millisecond):
						}
					}

					// Resume: reacquire the device.
					newAH, newRel, raErr := reacquireALSA()
					if raErr != nil {
						logger.L.Error("reacquire ALSA on resume failed", "err", raErr)
						close(stopDecode)
						for range pcmCh {
						}
						return 0, false
					}
					ah = newAH
					releaseReservation = newRel
					logger.L.Debug("resumed: ALSA device reacquired")
					break
				}

				select {
				case <-ctx.Done():
					C.snd_pcm_drop(ah.pcm)
					close(stopDecode)
					for range pcmCh {
					}
					return 0, false
				default:
				}

				off := framesDone * int(channels) * bps
				written := C.snd_pcm_writei(ah.pcm, unsafe.Pointer(&pcm.data[off]), C.snd_pcm_uframes_t(pcm.nFrames-framesDone))
				if written < 0 {
					errStr := C.GoString(C.snd_strerror(C.int(written)))
					logger.L.Warn("snd_pcm_writei error, recovering", "err", errStr)
					if rc := C.snd_pcm_recover(ah.pcm, C.int(written), C.int(1)); rc < 0 {
						logger.L.Error("snd_pcm_recover failed, stopping playback",
							"err", C.GoString(C.snd_strerror(rc)))
						close(stopDecode)
						for range pcmCh {
						}
						return 0, false
					}
					continue
				}
				framesDone += int(written)
			}
			atomic.AddUint64(&p.samplesPlayed, uint64(pcm.nFrames))
		}
		return 0, false
	}

	seekTarget, doSeek := streamLoop(0)
	for doSeek {
		// Re-open the HTTP stream and skip to the seek target.
		// samplesPlayed is NOT reset here — streamLoop sets it after skipping,
		// so GetPosition() never briefly returns 0 between seeks.
		_ = resp.Body.Close()

		resp, stream, err = openStream(ctx, url)
		if err != nil {
			logger.L.Error("failed to reopen stream for seek", "err", err)
			return
		}

		seekTarget, doSeek = streamLoop(seekTarget)
	}
}

func (p *Player) Pause() error {
	if atomic.LoadUint32(&p.paused) == 0 {
		atomic.StoreUint32(&p.paused, 1)
	} else {
		atomic.StoreUint32(&p.paused, 0)
	}
	return nil
}

func (p *Player) SetVolume(vol float64) error {
	atomic.StoreUint64(&p.volumeBits, math.Float64bits(vol/100.0))
	return nil
}

func (p *Player) GetVolume() (float64, error) {
	return math.Float64frombits(atomic.LoadUint64(&p.volumeBits)) * 100.0, nil
}

func (p *Player) GetPosition() (float64, error) {
	p.muInfo.RLock()
	sr := p.sampleRate
	p.muInfo.RUnlock()
	if sr == 0 {
		return 0, nil
	}
	return float64(atomic.LoadUint64(&p.samplesPlayed)) / float64(sr), nil
}

func (p *Player) GetDuration() (float64, error) {
	p.muInfo.RLock()
	sr := p.sampleRate
	ts := p.totalSamples
	p.muInfo.RUnlock()
	if sr == 0 {
		return 0, nil
	}
	return float64(ts) / float64(sr), nil
}

// Seek jumps to the given absolute position in seconds without interrupting
// the ALSA device or D-Bus reservation. The playback loop re-fetches the HTTP
// stream and skips to the target in-place.
func (p *Player) Seek(seconds float64) error {
	p.muInfo.RLock()
	sr := p.sampleRate
	ts := p.totalSamples
	p.muInfo.RUnlock()
	if sr == 0 {
		return nil
	}

	if seconds < 0 {
		seconds = 0
	}
	maxSeconds := float64(ts) / float64(sr)
	if seconds > maxSeconds {
		seconds = maxSeconds
	}

	target := uint64(seconds * float64(sr))

	// Non-blocking send: drop a stale pending seek if the loop hasn't consumed
	// it yet, then send the new target.
	select {
	case <-p.seekCh:
	default:
	}
	p.seekCh <- target
	return nil
}

// Done returns a channel that is closed when the current track finishes
// playing naturally (not when stopped or cancelled). Returns a nil channel
// if no track is playing, which blocks forever in a select — safe to use
// as a sentinel.
func (p *Player) Done() <-chan struct{} {
	p.mu.Lock()
	ch := p.doneCh
	p.mu.Unlock()
	return ch
}

func (p *Player) Close() {
	p.stop()
}
