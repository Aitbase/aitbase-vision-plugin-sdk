#!/bin/sh
# Installs the aitbase-plugin CLI and the WebAssembly toolchain it builds plugins with.
#
#   curl -fsSL https://raw.githubusercontent.com/Aitbase/aitbase-vision-plugin-sdk/main/install.sh | sh
#
# Linux and macOS. On Windows, use install.ps1 (see docs/cli.md#install).
#
# Settings (environment variables):
#   AITBASE_VERSION          release tag to install, for example v0.4.0 (default: the newest release)
#   AITBASE_INSTALL_DIR      where the CLI goes (default: $HOME/.local/bin)
#   AITBASE_SKIP_TOOLCHAIN=1 don't download the WebAssembly toolchain now (build --wasm does it later)
#
# The download is checked against the release's SHA256SUMS before it is installed.
set -eu

REPO="Aitbase/aitbase-vision-plugin-sdk"
INSTALL_DIR="${AITBASE_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
fail() { printf 'aitbase-plugin installer: %s\n' "$*" >&2; exit 1; }

fetch() {  # fetch <url> <file>
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$2" "$1"
    else
        fail "curl or wget is required"
    fi
}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) fail "this installer is for Linux and macOS; on Windows use install.ps1 (docs/cli.md#install)" ;;
esac
case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) fail "no aitbase-plugin build for $(uname -m)" ;;
esac
asset="aitbase-plugin-$os-$arch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

version="${AITBASE_VERSION:-}"
if [ -z "$version" ]; then
    # Preview releases are GitHub prereleases, which releases/latest skips: ask for the newest one.
    # The API is rate limited per IP, so fall back to the releases feed, which lists them too.
    if fetch "https://api.github.com/repos/$REPO/releases?per_page=1" "$tmp/releases.json" 2>/dev/null; then
        version="$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/releases.json" | head -n 1)"
    fi
    if [ -z "$version" ] && fetch "https://github.com/$REPO/releases.atom" "$tmp/releases.atom"; then
        version="$(grep -o 'releases/tag/[^"<]*' "$tmp/releases.atom" | head -n 1 | sed 's#releases/tag/##')"
    fi
    [ -n "$version" ] || fail "could not find the newest release; set AITBASE_VERSION, for example AITBASE_VERSION=v0.4.0"
fi

base="https://github.com/$REPO/releases/download/$version"
say "Downloading aitbase-plugin $version for $os/$arch..."
fetch "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" || fail "download failed: $base/SHA256SUMS"

expected="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/SHA256SUMS")"
[ -n "$expected" ] || fail "SHA256SUMS has no entry for $asset"
if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
else
    actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
fi
[ "$actual" = "$expected" ] || fail "checksum mismatch for $asset (expected $expected, got $actual); not installed"

mkdir -p "$INSTALL_DIR"
chmod +x "$tmp/$asset"
mv "$tmp/$asset" "$INSTALL_DIR/aitbase-plugin"
say "Installed $INSTALL_DIR/aitbase-plugin ($("$INSTALL_DIR/aitbase-plugin" version))"

if [ "${AITBASE_SKIP_TOOLCHAIN:-0}" != 1 ]; then
    "$INSTALL_DIR/aitbase-plugin" toolchain >/dev/null || fail "the WebAssembly toolchain download failed; run 'aitbase-plugin toolchain' to retry"
    say "WebAssembly toolchain ready: $("$INSTALL_DIR/aitbase-plugin" toolchain)"
fi

missing=""
command -v cmake >/dev/null 2>&1 || missing="$missing cmake"
command -v python3 >/dev/null 2>&1 || missing="$missing python3"
if [ -n "$missing" ]; then
    say ""
    say "Also install:$missing (aitbase-plugin build needs CMake 3.13+ and Python 3.8+)."
    if [ "$os" = darwin ]; then say "  brew install cmake python"; else say "  for example: sudo apt install cmake python3"; fi
fi

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        say ""
        say "Add $INSTALL_DIR to your PATH, for example:"
        say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.profile && . ~/.profile"
        ;;
esac
say ""
say "Next: aitbase-plugin init my-plugin --id yourname.my-plugin"
