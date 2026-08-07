package player

import (
	"errors"
	"fmt"
	"testing"
)

// Only a format-negotiation refusal may downgrade a device to plughw:. A busy
// device is the case that matters most: after a pause releases the D-Bus
// reservation, WirePlumber can still hold its handle, and openALSARaw retries
// that as hw:. Treating it as a refusal would permanently downgrade a DAC that
// is perfectly capable of bit-perfect output.
func TestFormatRefusalIsDistinguishable(t *testing.T) {
	refusal := fmt.Errorf("configure_hw_pcm(hw:2,0): Invalid argument: %w", errFormatRefused)
	if !errors.Is(refusal, errFormatRefused) {
		t.Error("a configure_hw_pcm failure must report as a format refusal")
	}

	// snd_pcm_open failures (EBUSY, ENODEV, …) are reported without the
	// sentinel, so they never reach the plughw: fallback.
	busy := errors.New("snd_pcm_open(hw:2,0): Device or resource busy")
	if errors.Is(busy, errFormatRefused) {
		t.Error("a busy open must not report as a format refusal")
	}
}

// The plughw: fallback is memoised so pause/resume and gapless transitions do
// not re-pay a known-failing hw: open plus its reservation stall.
func TestPlugFallbackIsMemoised(t *testing.T) {
	const (
		hwDevice   = "hw:2,0"
		plugDevice = "plughw:2,0"
	)
	p := &Player{}

	if got := p.effectiveDevice(hwDevice); got != hwDevice {
		t.Fatalf("before any fallback: effectiveDevice() = %q, want %q", got, hwDevice)
	}

	p.rememberPlugFallback(hwDevice, plugDevice)
	if got := p.effectiveDevice(hwDevice); got != plugDevice {
		t.Errorf("after fallback: effectiveDevice() = %q, want %q", got, plugDevice)
	}

	// A device that opened successfully on hw: must not be recorded, otherwise
	// a working bit-perfect path would be replaced by the plug layer.
	p.rememberPlugFallback("hw:3,0", "hw:3,0")
	if got := p.effectiveDevice("hw:3,0"); got != "hw:3,0" {
		t.Errorf("successful hw: open was memoised: effectiveDevice() = %q, want %q", got, "hw:3,0")
	}
}

// AudioPath reports no downgrade until a device has actually been opened, so
// the UI never renders a "(converted)" badge on a fresh session.
func TestAudioPathDefaultsToBitPerfect(t *testing.T) {
	p := &Player{}
	device, bitPerfect := p.AudioPath()
	if device != "" || !bitPerfect {
		t.Errorf("AudioPath() = (%q, %v), want (\"\", true)", device, bitPerfect)
	}

	p.activeDevice = "plughw:2,0"
	p.bitPerfect = false
	if device, bitPerfect = p.AudioPath(); device != "plughw:2,0" || bitPerfect {
		t.Errorf("AudioPath() = (%q, %v), want (\"plughw:2,0\", false)", device, bitPerfect)
	}
}
