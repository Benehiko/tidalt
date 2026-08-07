# Development

## Prerequisites

- Go 1.26+
- A C toolchain and the native development headers, since the player package
  uses CGO:

  ```bash
  # Debian / Ubuntu
  sudo apt-get install build-essential pkg-config \
      libasound2-dev libavformat-dev libavcodec-dev \
      libavutil-dev libswresample-dev

  # Arch
  sudo pacman -S base-devel pkgconf alsa-lib ffmpeg
  ```

## Common tasks

A `Makefile` wraps the usual commands:

```bash
make build      # go build ./...
make test       # go test ./...
make fmt        # gofumpt -w .
make lint       # both linters (Go and C)
make lint-go    # golangci-lint run
make lint-c     # clang-tidy over the C sources
make hooks      # activate the tracked git hooks for this clone
```

## Git hooks

The repository tracks a pre-commit hook under `.githooks/`. Activate it once per
clone:

```bash
make hooks
# equivalent to: git config core.hooksPath .githooks
```

The hook is a thin dispatcher over the same tooling CI runs, so passing the hook
and passing CI stay in lockstep. On the staged file types only, it checks:

- **Go** — `gofumpt` formatting, `go build ./...`, and `golangci-lint run`
- **C** — `clang-tidy` via `lint-c.sh`

It runs only the fast checks; the full test suite and any security scanning are
left to CI so a commit never blocks for minutes. The formatting check reports
drift rather than rewriting files, so a commit always reflects exactly what was
staged — if `gofumpt` flags something, run `gofumpt -w .` and re-stage.

To bypass the hook for a single commit:

```bash
git commit --no-verify
```

## The C sources

`internal/player` contains hand-written C compiled by cgo as part of the
package. It is split into standalone translation units rather than living in
cgo preamble comments, so it can be read, diffed, and linted as ordinary C:

| File | Purpose | Go side |
| --- | --- | --- |
| `alsa.h` / `alsa.c` | Raw ALSA PCM open and format negotiation | `mpv.go` |
| `avcodec.h` / `avcodec.c` | FFmpeg demux/decode/resample to S32LE | `avcodec.go` |

The corresponding Go files keep only a minimal cgo preamble that includes the
header and carries the `#cgo` linker directives:

```go
/*
#cgo LDFLAGS: -lasound
#include "alsa.h"
*/
import "C"
```

`avcodec.c` calls `avio_read_cb`, which is implemented in Go and exported to C
via `//export` in `avcodec.go`. It is declared in `avcodec.h` so the C side
compiles; cgo places both into the same package archive, so it links normally.

FFmpeg linker flags stay in the build-tag-gated Go files — `avcodec_dynamic.go`
(default, system shared libraries) and `avcodec_static.go` (`-tags staticav`,
the bundled static FFmpeg under `/opt/ffmpeg` used for distro packages).

### Linting the C

`clang-tidy` is configured in `.clang-tidy` and driven by `lint-c.sh`:

```bash
./lint-c.sh                          # lint every C source
./lint-c.sh internal/player/alsa.c   # lint specific files
```

By default the script runs clang-tidy inside a container (`nerdctl`, `docker`,
or `podman`), so no clang or native headers are needed on the host. If you have
clang-tidy installed locally and want to skip the container:

```bash
LINT_C_NATIVE=1 ./lint-c.sh
```

The C sources have no standalone build system — cgo compiles them — so the
script reconstructs the compile flags cgo would use: C11, the package directory
on the include path, `-D_GNU_SOURCE` (without which `<alsa/global.h>` redefines
`struct timespec`), plus the ALSA and FFmpeg include paths from `pkg-config`.

The check selection favours real defects — leaks, null dereferences,
uninitialised reads, bad casts — over naming and style opinions, which would
fight the surrounding ALSA/FFmpeg idiom. All findings are errors
(`WarningsAsErrors: '*'`), and `HeaderFilterRegex` keeps diagnostics scoped to
this project's own headers rather than the system ones.

> **Note:** the `Checks:` value in `.clang-tidy` is a YAML folded scalar and
> cannot contain comments — a `#` line inside it is folded into the check list
> as literal text and silently corrupts the entries that follow. Rationale for
> each disabled check is kept in the comment block above it instead.

## CI

`.github/workflows/lint.yml` runs two jobs: `golangci-lint` over the Go code and
`clang-tidy` over the C sources. Both must pass before merge.
