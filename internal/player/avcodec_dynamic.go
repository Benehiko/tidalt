//go:build !staticav

package player

// Default build: link the FFmpeg libraries shared on the host system.
// Used for local development, CI, and the Arch package (built natively
// against the distro's FFmpeg). The system FFmpeg .so soname must match
// what the binary was linked against, so this path is only safe when the
// build and runtime environments share the same distro release.

// #cgo LDFLAGS: -lavformat -lavcodec -lavutil -lswresample
import "C"
