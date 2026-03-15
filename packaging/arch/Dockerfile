# syntax=docker/dockerfile:1
FROM archlinux:latest AS package

ARG VERSION

RUN pacman -Syu --noconfirm && \
    pacman -S --noconfirm base-devel go alsa-lib git sudo && \
    useradd -m builder && \
    echo 'builder ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers

WORKDIR /home/builder
COPY packaging/arch/PKGBUILD ./PKGBUILD

RUN sed -i "s/^pkgver=.*/pkgver=${VERSION}/" PKGBUILD && \
    chown builder:builder PKGBUILD

# Copy the full source so makepkg can build from it instead of downloading.
# We override source= to point at a local tarball we create here.
COPY --chown=builder:builder . /src

RUN cd /home/builder && \
    # Create a source tarball from the repo so makepkg can use it.
    tar -czf tidalt-${VERSION}.tar.gz -C /src --transform "s,^,tidalt-${VERSION}/," . && \
    chown builder:builder tidalt-${VERSION}.tar.gz && \
    # Patch PKGBUILD to use the local tarball and set correct checksums.
    sed -i \
        -e "s|^source=.*|source=(\"tidalt-\${pkgver}.tar.gz\")|" \
        -e "s|^b2sums=.*|b2sums=('SKIP')|" \
        PKGBUILD && \
    sudo -u builder makepkg --noconfirm

# ── Export stage ─────────────────────────────────────────────────────────────
FROM scratch AS artifact
COPY --from=package /home/builder/*.pkg.tar.zst /
