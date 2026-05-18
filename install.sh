#!/bin/sh
# mkv2mp4 installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/yashptel/mkv2mp4/main/install.sh | sh
#
# Set MKV2MP4_VERSION=vX.Y.Z to install a specific release; default is "latest".
# Set MKV2MP4_PREFIX=/path to override the install directory (default /usr/local/bin).

set -eu

REPO="yashptel/mkv2mp4"
PREFIX="${MKV2MP4_PREFIX:-/usr/local/bin}"
VERSION="${MKV2MP4_VERSION:-latest}"

err() { printf '\033[31merror:\033[0m %s\n' "$1" >&2; exit 1; }
info() { printf '%s\n' "$1"; }

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux) ;;
  *) err "unsupported OS: $OS (only darwin and linux are supported by this script; Windows users should download from GitHub Releases)" ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) err "unsupported arch: $ARCH" ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | head -n1 \
    | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || err "could not resolve latest release tag"
fi

# goreleaser default: {name}_{version}_{os}_{arch}.tar.gz with version dropping the leading "v".
ASSET="mkv2mp4_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

info "Downloading ${URL}"

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t mkv2mp4)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "$TMP/${ASSET}" || err "download failed; check that ${ASSET} exists in the release"

# Optional: verify against checksums.txt if available.
CSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
if curl -fsSL "$CSUM_URL" -o "$TMP/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP" && grep " ${ASSET}\$" checksums.txt | sha256sum -c -) \
      || err "checksum verification failed"
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$TMP" && grep " ${ASSET}\$" checksums.txt | shasum -a 256 -c -) \
      || err "checksum verification failed"
  fi
fi

tar -xzf "$TMP/${ASSET}" -C "$TMP"
[ -x "$TMP/mkv2mp4" ] || err "extracted archive did not contain mkv2mp4 binary"

DEST="${PREFIX}/mkv2mp4"
if [ -w "$PREFIX" ]; then
  mv "$TMP/mkv2mp4" "$DEST"
elif command -v sudo >/dev/null 2>&1; then
  info "Installing to ${DEST} (sudo)"
  sudo mv "$TMP/mkv2mp4" "$DEST"
else
  err "cannot write to ${PREFIX} and sudo is not available. Re-run with MKV2MP4_PREFIX=~/bin"
fi

chmod +x "$DEST" 2>/dev/null || true
info "Installed ${DEST}"
"$DEST" --version
