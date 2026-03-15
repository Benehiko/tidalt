# syntax=docker/dockerfile:1

# ── Build stage: cross-compile the binary ────────────────────────────────────
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

# ── Package stage: assemble the .rpm with rpmbuild ───────────────────────────
FROM fedora:latest AS package

ARG TARGETARCH
ARG VERSION

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
