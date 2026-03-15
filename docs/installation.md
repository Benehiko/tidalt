# Installation

## From a release (recommended)

Pre-built packages are attached to every [GitHub release](https://github.com/Benehiko/tidalt/releases).

### Arch Linux

Download the `.pkg.tar.zst` from the latest release and install with pacman:

```bash
sudo pacman -U tidalt-*.pkg.tar.zst
```

Or install the build files and build it yourself with `makepkg`:

```bash
# Clone just the packaging directory
curl -LO https://github.com/Benehiko/tidalt/releases/latest/download/PKGBUILD
makepkg -si
```

### Debian / Ubuntu

Download the `.deb` from the latest release:

```bash
sudo dpkg -i tidalt_*.deb
sudo apt-get install -f   # resolve any missing dependencies
```

---

## From source

### Prerequisites

Go 1.26+ and ALSA development headers:

```bash
sudo pacman -S go alsa-lib          # Arch
sudo apt install golang libasound2-dev  # Debian / Ubuntu
sudo dnf install golang alsa-lib-devel  # Fedora
```

### go install

```bash
go install github.com/Benehiko/tidalt/cmd/tidalt@latest
```

The binary is placed in `$GOPATH/bin` (typically `~/go/bin`). Make sure that
directory is on your `PATH`.

### Git clone

```bash
git clone https://github.com/Benehiko/tidalt.git
cd tidalt
go build -o tidalt ./cmd/tidalt
sudo install -Dm755 tidalt /usr/local/bin/tidalt
```

---

## Post-install setup

### Register the tidal:// URL handler

The `setup` subcommand installs the `.desktop` file and registers the
`tidal://` scheme so clicking **"Open in desktop app"** on tidal.com opens
the track directly in tidalt:

```bash
tidalt setup
```

Output:

```
  -> Creating directory /home/user/.local/share/applications
  -> Writing /home/user/.local/share/applications/tidalt.desktop
  -> $ xdg-mime default tidalt.desktop x-scheme-handler/tidal
  -> $ update-desktop-database /home/user/.local/share/applications

Setup complete.
Clicking "Open in desktop app" on tidal.com will now open tidalt.
```

Some browsers (notably Firefox and Librewolf) require an extra one-time step.
See [browser-url-handler.md](browser-url-handler.md) for per-browser
instructions.

### Run as a background daemon (optional)

Install tidalt as a systemd user service so it starts at login with no
terminal window:

```bash
tidalt setup --daemon
```

Then open the TUI from any terminal with `tidalt`, or control playback with
`playerctl`. See [client-server.md](client-server.md) for details.

---

## Building packages locally

The `packaging/` directory at the root of the repository contains ready-to-use
build recipes for Arch and Debian.

### Arch — makepkg

```bash
cd packaging/arch
makepkg -si
```

`makepkg` downloads the release tarball, compiles, runs tests, and installs
via `pacman`. To generate real checksums before publishing:

```bash
makepkg -g >> PKGBUILD
```

### Debian — dpkg-buildpackage

Install build dependencies:

```bash
sudo apt install debhelper libasound2-dev
```

Go 1.26+ is required to compile the binary. Install it from
[go.dev/dl](https://go.dev/dl/) and ensure it is first on your `PATH`.

Then build from a release tarball:

```bash
VERSION=3.0.0
curl -L "https://github.com/Benehiko/tidalt/archive/refs/tags/v${VERSION}.tar.gz" \
    | tar xz
cd "tidalt-${VERSION}"

# Compile the binary first — the debian/rules file installs it directly.
CGO_ENABLED=1 go build -trimpath -buildmode=pie \
    -ldflags "-s -w -linkmode=external" \
    -o tidalt-linux-amd64 ./cmd/tidalt

cp -r /path/to/repo/packaging/debian debian
dpkg-buildpackage -us -uc -b
sudo dpkg -i ../tidalt_${VERSION}-1_amd64.deb
```
