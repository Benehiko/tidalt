Name:           tidalt
Version:        %{version_macro}
Release:        1%{?dist}
Summary:        Tidal music player for Linux — bit-perfect FLAC via ALSA

License:        Apache-2.0
URL:            https://github.com/Benehiko/tidalt

# The binary is pre-built before rpmbuild is invoked.
# No Go toolchain is required at package-build time.

BuildRequires:  alsa-lib

Requires:       alsa-lib
Recommends:     playerctl
Suggests:       kdeconnect

%description
tidalt is a Tidal music player that delivers bit-perfect, lossless audio
directly to your DAC via ALSA hw: — bypassing PipeWire and PulseAudio
entirely. It can run as an interactive TUI, a headless daemon controlled
via any MPRIS2 client, or a lightweight client that forwards commands to
a running daemon over D-Bus.

%install
install -Dm755 %{_sourcedir}/tidalt        %{buildroot}%{_bindir}/tidalt
install -Dm644 %{_sourcedir}/tidalt.desktop %{buildroot}%{_datadir}/applications/tidalt.desktop

%files
%{_bindir}/tidalt
%{_datadir}/applications/tidalt.desktop

%changelog
* Sun Mar 15 2026 Benehiko <https://github.com/Benehiko> - %{version_macro}-1
- Initial RPM packaging
