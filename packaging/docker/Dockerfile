# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
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

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM debian:stable-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends libasound2t64 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build /out/tidalt /usr/local/bin/tidalt

ENTRYPOINT ["/usr/local/bin/tidalt"]
