//go:build staticav

package player

// staticav build: link a self-contained, minimal FFmpeg built from source
// under /tmp/tidalt-ffmpeg-static (see the packaging Dockerfiles). The
// decoders/demuxers are statically linked into the binary, so the resulting
// executable carries no dynamic dependency on the host's FFmpeg .so files and
// is portable across distributions regardless of their FFmpeg version.
//
// The static archives are listed explicitly (rather than via -lavformat) so
// the linker pulls them in instead of any system shared library that may also
// be present in the build image. Order matters: avformat depends on avcodec,
// which depends on swresample and avutil.

// #cgo CFLAGS: -I/tmp/tidalt-ffmpeg-static/include
// #cgo LDFLAGS: /tmp/tidalt-ffmpeg-static/lib/libavformat.a /tmp/tidalt-ffmpeg-static/lib/libavcodec.a /tmp/tidalt-ffmpeg-static/lib/libswresample.a /tmp/tidalt-ffmpeg-static/lib/libavutil.a -lm -lpthread -latomic
import "C"
