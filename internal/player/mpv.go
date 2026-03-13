package player

/*
#cgo LDFLAGS: -lasound
#include <alsa/asoundlib.h>
#include <stdlib.h>
#include <errno.h>

// alsa_open_result carries the negotiated format back to Go.
typedef struct {
    snd_pcm_format_t format;
    int              bytes_per_sample;
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
        snd_pcm_format_t fmt16[] = {SND_PCM_FORMAT_S16_LE,
                                    SND_PCM_FORMAT_S24_3LE,
                                    SND_PCM_FORMAT_S24_LE,
                                    SND_PCM_FORMAT_S32_LE};
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

    // dir=0: exact rate match only — no resampling.
    rc = snd_pcm_hw_params_set_rate(*handle_out, params, rate, 0);
    if (rc < 0) goto fail;

    rc = snd_pcm_hw_params(*handle_out, params);
    if (rc < 0) goto fail;

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
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/godbus/dbus/v5"
	"github.com/mewkiz/flac"
)

// knownDACs lists substrings to search for in /proc/asound/cards output.
// First match wins, so order determines priority.
var knownDACs = []string{"hidizs", "focusrite", "scarlett"}

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
	p := &Player{}
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
	if _, err := fmt.Sscanf(hwDevice, "hw:%d,%d", &card, &dev); err != nil {
		return 0, fmt.Errorf("cannot parse card number from %q: %w", hwDevice, err)
	}
	return card, nil
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
		conn.ReleaseName(name) //nolint:errcheck
		conn.Close()
	}

	// Watch NameOwnerChanged for this name before requesting it, so we don't
	// miss the signal between RequestRelease returning and us subscribing.
	sigCh := make(chan *dbus.Signal, 10)
	conn.Signal(sigCh)
	if matchErr := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, name),
	); matchErr != nil {
		conn.Close()
		return func() {}, nil
	}

	// Enter the queue (NameFlagAllowReplacement lets others ask us to release later).
	// Not setting DoNotQueue means D-Bus hands us the name automatically when
	// the current owner releases it — no polling needed.
	reply, err := conn.RequestName(name, dbus.NameFlagAllowReplacement)
	if err != nil {
		conn.Close()
		return func() {}, nil
	}

	if reply == dbus.RequestNameReplyPrimaryOwner {
		return releaseFunc, nil
	}

	// We are queued. Ask PipeWire to release.
	obj := conn.Object(name, objPath)
	var released bool
	if callErr := obj.Call("org.freedesktop.ReserveDevice1.RequestRelease", 0, int32(math.MaxInt32)).Store(&released); callErr != nil || !released {
		releaseFunc()
		return nil, fmt.Errorf("audio device Audio%d is held by another process and refused to release", cardNum)
	}

	// Wait for D-Bus to hand us the name (NameOwnerChanged with us as new owner).
	ourUniqueName := conn.Names()[0]
	timeout := time.After(3 * time.Second)
	for {
		select {
		case sig := <-sigCh:
			if len(sig.Body) >= 3 {
				if newOwner, ok := sig.Body[2].(string); ok && newOwner == ourUniqueName {
					return releaseFunc, nil
				}
			}
		case <-timeout:
			releaseFunc()
			return nil, fmt.Errorf("timeout waiting for Audio%d to be released by PipeWire", cardNum)
		}
	}
}

type alsaHandle struct {
	pcm            *C.snd_pcm_t
	format         C.snd_pcm_format_t
	bytesPerSample int
}

// openALSA opens an ALSA hw device, negotiating the best available format for
// the source bit depth without enabling soft resampling (bit-perfect).
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
		pcm:            handle,
		format:         result.format,
		bytesPerSample: int(result.bytes_per_sample),
	}, nil
}

func (p *Player) Play(url string) error {
	p.stop()

	// Resolve device and acquire D-Bus reservation synchronously so we can
	// return an error to the caller if the device cannot be claimed.
	device, err := p.getDevice()
	if err != nil {
		return err
	}

	cardNum, err := parseCardNum(device)
	if err != nil {
		return err
	}

	releaseReservation, err := reserveALSADevice(cardNum)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	p.mu.Lock()
	p.cancel = cancel
	p.doneCh = doneCh
	p.mu.Unlock()

	atomic.StoreUint64(&p.samplesPlayed, 0)
	atomic.StoreUint32(&p.paused, 0)

	go func() {
		defer close(doneCh)
		defer releaseReservation()
		p.playbackLoop(ctx, url, device)
	}()
	return nil
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

func (p *Player) playbackLoop(ctx context.Context, url, device string) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	stream, err := flac.New(resp.Body)
	if err != nil {
		return
	}

	info := stream.Info
	sampleRate := info.SampleRate
	channels := info.NChannels
	bits := info.BitsPerSample

	p.muInfo.Lock()
	p.sampleRate = sampleRate
	p.channels = channels
	p.bitsPerSample = bits
	p.totalSamples = info.NSamples
	p.muInfo.Unlock()

	ah, err := openALSA(device, channels, sampleRate, bits)
	if err != nil {
		return
	}
	defer func() {
		C.snd_pcm_drain(ah.pcm)
		C.snd_pcm_close(ah.pcm)
	}()

	bps := ah.bytesPerSample

	for {
		select {
		case <-ctx.Done():
			C.snd_pcm_drop(ah.pcm)
			return
		default:
		}

		// Pause: sleep in place without advancing the FLAC stream.
		for atomic.LoadUint32(&p.paused) == 1 {
			select {
			case <-ctx.Done():
				C.snd_pcm_drop(ah.pcm)
				return
			case <-time.After(20 * time.Millisecond):
			}
		}

		frame, err := stream.ParseNext()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}

		n := int(frame.Header.BlockSize)
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
					// 24-bit signed in a 4-byte container; upper byte is sign extension.
					// s is already sign-extended to int32 by the FLAC decoder.
					binary.LittleEndian.PutUint32(buf[off:], uint32(s))
				case C.SND_PCM_FORMAT_S32_LE:
					// Shift source content into the MSBs of the 32-bit container.
					shift := uint(32 - bits)
					binary.LittleEndian.PutUint32(buf[off:], uint32(s<<shift))
				}
			}
		}

		// Write frames to ALSA, retrying after xrun recovery.
		framesLeft := C.snd_pcm_uframes_t(n)
		framesDone := C.snd_pcm_uframes_t(0)
		for framesLeft > 0 {
			off := int(framesDone) * int(channels) * bps
			written := C.snd_pcm_writei(ah.pcm, unsafe.Pointer(&buf[off]), framesLeft)
			if written < 0 {
				// snd_pcm_recover handles EPIPE (underrun) and ESTRPIPE (suspend).
				if C.snd_pcm_recover(ah.pcm, C.int(written), C.int(1)) < 0 {
					return
				}
				continue
			}
			framesLeft -= C.snd_pcm_uframes_t(written)
			framesDone += C.snd_pcm_uframes_t(written)
		}

		atomic.AddUint64(&p.samplesPlayed, uint64(n))
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

func (p *Player) Close() {
	p.stop()
}
