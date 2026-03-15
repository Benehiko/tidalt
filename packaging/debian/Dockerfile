# syntax=docker/dockerfile:1

# ── Build stage: compile the binary ──────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build

ARG TARGETARCH
ARG VERSION

WORKDIR /src
COPY . .

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
        -trimpath \
        -buildmode=pie \
        -ldflags "-s -w -linkmode=external -X main.version=v${VERSION}" \
        -o /out/tidalt \
        ./cmd/tidalt

# ── Package stage: assemble the .deb with dpkg-deb ───────────────────────────
FROM debian:stable-slim AS package

ARG TARGETARCH
ARG VERSION

# dpkg-deb is part of dpkg (already installed in debian:stable-slim).
# We only need dpkg-shlibdeps → libasound2 for shlibs resolution.
RUN apt-get update && \
    apt-get install -y --no-install-recommends libasound2t64 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build /out/tidalt /pkg/usr/bin/tidalt
COPY cmd/tidalt/tidalt.desktop /pkg/usr/share/applications/tidalt.desktop

# Compute the installed size in kibibytes (required by Debian policy).
RUN INSTALLED_SIZE=$(du -sk /pkg | cut -f1) && \
    mkdir -p /pkg/DEBIAN && \
    cat > /pkg/DEBIAN/control <<EOF
Package: tidalt
Version: ${VERSION}-1
Architecture: ${TARGETARCH}
Maintainer: Benehiko <https://github.com/Benehiko>
Installed-Size: ${INSTALLED_SIZE}
Depends: libasound2t64
Recommends: playerctl
Suggests: kdeconnect
Section: sound
Priority: optional
Homepage: https://github.com/Benehiko/tidalt
Description: Tidal music player for Linux — bit-perfect FLAC via ALSA
 tidalt is a Tidal music player that delivers bit-perfect, lossless audio
 directly to your DAC via ALSA hw: — bypassing PipeWire and PulseAudio
 entirely.
EOF

RUN chmod 755 /pkg/usr/bin/tidalt && \
    chmod 644 /pkg/usr/share/applications/tidalt.desktop && \
    dpkg-deb --root-owner-group --build /pkg /tidalt_${VERSION}-1_${TARGETARCH}.deb

# ── Export stage ─────────────────────────────────────────────────────────────
FROM scratch AS artifact
ARG TARGETARCH
ARG VERSION
COPY --from=package /tidalt_${VERSION}-1_${TARGETARCH}.deb /
