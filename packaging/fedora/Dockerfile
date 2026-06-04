# syntax=docker/dockerfile:1

# ── FFmpeg stage: build a minimal, static FFmpeg for the target arch ──────────
# See packaging/debian/Dockerfile for the rationale. Building FFmpeg statically
# is what lets the cross-compiled (Debian-toolchain) binary run on Fedora — the
# Fedora and Debian FFmpeg sonames differ, so a dynamically-linked binary would
# fail to load the matching .so on the target system.
FROM --platform=$BUILDPLATFORM debian:trixie-slim AS ffmpeg
ARG TARGETARCH

RUN dpkg --add-architecture ${TARGETARCH} && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential nasm pkg-config curl xz-utils ca-certificates \
        crossbuild-essential-${TARGETARCH} && \
    rm -rf /var/lib/apt/lists/*

COPY packaging/build-static-ffmpeg.sh /build-static-ffmpeg.sh
RUN bash /build-static-ffmpeg.sh /opt/ffmpeg ${TARGETARCH}

# ── Build stage: cross-compile the binary, static-linking the FFmpeg above ────
FROM --platform=$BUILDPLATFORM golang:1.26-trixie AS build

ARG TARGETARCH
ARG VERSION

WORKDIR /src
COPY . .
COPY --from=ffmpeg /opt/ffmpeg /opt/ffmpeg

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

# ── Package stage: assemble the .rpm with rpmbuild ───────────────────────────
FROM fedora:latest AS package

ARG TARGETARCH
ARG VERSION

# FFmpeg is statically linked into the binary, so only alsa-lib is required.
RUN dnf install -y rpm-build alsa-lib && \
    dnf clean all

# Map Docker arch names to RPM arch names.
RUN case "${TARGETARCH}" in \
        amd64) echo "x86_64" > /rpm_arch ;; \
        arm64) echo "aarch64" > /rpm_arch ;; \
        *) echo "unsupported arch: ${TARGETARCH}" && exit 1 ;; \
    esac

RUN mkdir -p /root/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS}

COPY --from=build /out/tidalt               /root/rpmbuild/SOURCES/tidalt
COPY cmd/tidalt/tidalt.desktop              /root/rpmbuild/SOURCES/tidalt.desktop
COPY packaging/fedora/tidalt.spec           /root/rpmbuild/SPECS/tidalt.spec

RUN RPM_ARCH=$(cat /rpm_arch) && \
    rpmbuild -bb \
        --target "${RPM_ARCH}" \
        --define "version_macro ${VERSION}" \
        --define "_build_id_links none" \
        /root/rpmbuild/SPECS/tidalt.spec

# ── Export stage ─────────────────────────────────────────────────────────────
FROM scratch AS artifact
COPY --from=package /root/rpmbuild/RPMS/*/*.rpm /
