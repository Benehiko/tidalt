#!/usr/bin/env bash
#
# Build a minimal, statically-linked FFmpeg for tidalt.
#
# Only the demuxers/decoders tidalt actually needs are enabled (FLAC, AAC/mp4,
# ALAC, PCM, Vorbis), which keeps the static archives small and free of the huge
# transitive codec dependency chain that the distro's shared FFmpeg drags in.
# Linking these archives statically (build the Go binary with `-tags staticav`)
# makes the resulting executable portable across distributions regardless of
# their FFmpeg version — the FFmpeg .so soname is not stable across releases.
#
# This is the single source of truth for the FFmpeg version and enabled
# components, shared by the packaging Dockerfiles and the release workflow.
#
# Usage:
#   build-static-ffmpeg.sh [PREFIX] [TARGETARCH]
#
#   PREFIX      install prefix (default: /opt/ffmpeg). Must match the path in
#               internal/player/avcodec_static.go.
#   TARGETARCH  Go-style arch of the build target: amd64 | arm64
#               (default: native — no cross-compilation).
#
# Requires: a C toolchain, nasm, curl, xz, and (for arm64 cross-builds)
# the aarch64-linux-gnu-* cross toolchain.
set -euo pipefail

FFMPEG_VERSION="${FFMPEG_VERSION:-7.1.1}"
PREFIX="${1:-/opt/ffmpeg}"
TARGETARCH="${2:-}"

case "${TARGETARCH}" in
    amd64) FF_ARCH=x86_64;  CROSS_PREFIX= ;;
    arm64) FF_ARCH=aarch64; CROSS_PREFIX=aarch64-linux-gnu- ;;
    "")    FF_ARCH=;        CROSS_PREFIX= ;;  # native build
    *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;;
esac

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

curl -fsSL "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz" \
    | tar -xJ -C "${workdir}"
cd "${workdir}/ffmpeg-${FFMPEG_VERSION}"

configure_args=(
    --prefix="${PREFIX}"
    --disable-everything --disable-programs --disable-doc --disable-network
    --disable-autodetect --disable-shared --enable-static --enable-pic
    --disable-avdevice --disable-swscale --disable-avfilter
    --enable-protocol=file
    --enable-demuxer=flac,mov,aac,wav,ogg
    --enable-decoder=flac,aac,aac_latm,alac,pcm_s16le,pcm_s24le,pcm_s32le,vorbis
    --enable-parser=flac,aac,aac_latm,vorbis
    --enable-swresample
)
if [ -n "${FF_ARCH}" ]; then
    configure_args+=(
        --enable-cross-compile --arch="${FF_ARCH}" --target-os=linux
        --cross-prefix="${CROSS_PREFIX}"
    )
fi

./configure "${configure_args[@]}"
make -j"$(nproc)"
make install
