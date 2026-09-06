# syntax=docker/dockerfile:1

# ── FFmpeg stage: build a minimal, static FFmpeg for the target arch ──────────
# See packaging/debian/Dockerfile for the rationale behind statically linking a
# trimmed-down FFmpeg instead of depending on the distro's shared libraries.
FROM --platform=$BUILDPLATFORM debian:trixie-slim AS ffmpeg
ARG TARGETARCH

RUN dpkg --add-architecture ${TARGETARCH} && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential nasm pkg-config curl xz-utils ca-certificates \
        crossbuild-essential-${TARGETARCH} && \
    rm -rf /var/lib/apt/lists/*

COPY packaging/build-static-ffmpeg.sh /build-static-ffmpeg.sh
RUN bash /build-static-ffmpeg.sh /tmp/tidalt-ffmpeg-static ${TARGETARCH}

# ── Build stage ───────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26-trixie AS build

ARG TARGETARCH
ARG VERSION

WORKDIR /src
COPY . .
COPY --from=ffmpeg /tmp/tidalt-ffmpeg-static /tmp/tidalt-ffmpeg-static

RUN dpkg --add-architecture ${TARGETARCH} && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        crossbuild-essential-${TARGETARCH} \
        libasound2-dev:${TARGETARCH} && \
    rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH}" in \
        amd64) CC=x86_64-linux-gnu-gcc ;; \
        arm64) CC=aarch64-linux-gnu-gcc ;; \
        *) echo "unsupported arch: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    CGO_ENABLED=1 CC=${CC} GOARCH=${TARGETARCH} go build \
        -tags staticav \
        -trimpath \
        -buildmode=pie \
        -ldflags "-s -w -linkmode=external -X main.version=v${VERSION}" \
        -o /out/tidalt \
        ./cmd/tidalt

# ── Runtime stage ─────────────────────────────────────────────────────────────
# FFmpeg is statically linked into the binary, so only ALSA is needed at runtime.
FROM debian:stable-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends libasound2t64 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build /out/tidalt /usr/local/bin/tidalt

ENTRYPOINT ["/usr/local/bin/tidalt"]
