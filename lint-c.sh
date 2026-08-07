#!/usr/bin/env bash
#
# lint-c.sh — run clang-tidy over the hand-written C in internal/player.
#
# The C sources are compiled by cgo as part of the player package, so they have
# no standalone build system. This script invokes clang-tidy directly with the
# same include paths and flags cgo uses, inside a container so the host needs
# no clang or native dev headers installed.
#
# Usage:
#   ./lint-c.sh            # lint every C source
#   ./lint-c.sh a.c b.c    # lint only the given files (used by the pre-commit hook)
#
# Set LINT_C_NATIVE=1 to run against a locally installed clang-tidy instead of
# a container (useful in CI, which already provisions the dev headers).
set -euo pipefail

cd "$(dirname "$0")"

IMAGE="${LINT_C_IMAGE:-docker.io/silkeh/clang:19}"

# Default to every C source in the package when no arguments are given.
if [ "$#" -eq 0 ]; then
    mapfile -t SOURCES < <(find internal/player -name '*.c' | sort)
else
    SOURCES=("$@")
fi

if [ "${#SOURCES[@]}" -eq 0 ]; then
    echo "lint-c: no C sources to lint"
    exit 0
fi

# Compiler flags mirroring the cgo build: C11, the package directory on the
# include path (for "alsa.h" / "avcodec.h"), plus the system ALSA and FFmpeg
# headers. pkg-config resolves the FFmpeg include dir, which is
# multiarch-dependent on Debian/Ubuntu.
build_flags() {
    # _GNU_SOURCE matches cgo's own default. Without it, <alsa/global.h>
    # redefines struct timespec and the translation unit fails to parse.
    local flags=(-std=c11 -D_GNU_SOURCE -Iinternal/player)
    local pc
    if pc=$(pkg-config --cflags libavformat libavcodec libavutil libswresample alsa 2>/dev/null); then
        # shellcheck disable=SC2206 # intentional word splitting of pkg-config output
        flags+=($pc)
    fi
    printf '%s\n' "${flags[@]}"
}

run_native() {
    mapfile -t FLAGS < <(build_flags)
    echo "lint-c: clang-tidy ${SOURCES[*]}"
    clang-tidy --warnings-as-errors='*' "${SOURCES[@]}" -- "${FLAGS[@]}"
}

run_container() {
    local runtime
    for candidate in nerdctl docker podman; do
        if command -v "$candidate" >/dev/null 2>&1; then
            runtime=$candidate
            break
        fi
    done
    if [ -z "${runtime:-}" ]; then
        echo "lint-c: no container runtime (nerdctl/docker/podman) found." >&2
        echo "lint-c: install one, or set LINT_C_NATIVE=1 to use a local clang-tidy." >&2
        exit 1
    fi

    echo "lint-c: clang-tidy ${SOURCES[*]} (via $runtime)"
    "$runtime" run --rm \
        -v "$PWD:/src:ro" \
        -w /src \
        -e LINT_C_NATIVE=1 \
        "$IMAGE" \
        bash -c '
            set -euo pipefail
            apt-get update -qq
            apt-get install -y -qq --no-install-recommends \
                libasound2-dev libavformat-dev libavcodec-dev \
                libavutil-dev libswresample-dev pkg-config >/dev/null
            exec bash lint-c.sh "$@"
        ' _ "${SOURCES[@]}"
}

if [ "${LINT_C_NATIVE:-0}" = "1" ]; then
    run_native
else
    run_container
fi
