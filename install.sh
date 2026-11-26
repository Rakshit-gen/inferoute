#!/bin/sh
# Installs inferouted with one command:
#
#   curl -fsSL https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/install.sh | sh
#
# Downloads the release binary for your OS and architecture, checks it
# against the release's checksums.txt, and puts it in ~/.local/bin. If no
# release exists yet, it falls back to `go install`.
#
#   INFEROUTE_VERSION   version to install (default: latest)
#   INFEROUTE_BIN_DIR   install directory (default: $HOME/.local/bin)
set -eu

REPO="Rakshit-gen/inferoute"
VERSION="${INFEROUTE_VERSION:-latest}"
BIN_DIR="${INFEROUTE_BIN_DIR:-$HOME/.local/bin}"

die() { echo "error: $*" >&2; exit 1; }

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -q "$1" -O "$2"
  else die "need curl or wget"; fi
}

go_install() {
  command -v go >/dev/null 2>&1 || die "no release found and Go is not installed (https://go.dev/dl/)"
  echo "No release found; building with go install..."
  GOBIN="$BIN_DIR" go install "github.com/$REPO/cmd/inferouted@latest"
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

main() {
  case "$(uname -s)" in
    Linux) os=linux ;; Darwin) os=darwin ;;
    *) die "unsupported OS $(uname -s): releases cover macOS and Linux" ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture $(uname -m)" ;;
  esac

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  mkdir -p "$BIN_DIR"

  tag="$VERSION"
  if [ "$tag" = latest ]; then
    if fetch "https://api.github.com/repos/$REPO/releases/latest" "$tmp/latest.json" 2>/dev/null; then
      tag="$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n1)"
    fi
    if [ -z "$tag" ] || [ "$tag" = latest ]; then
      go_install
      done_msg
      return
    fi
  fi
  case "$tag" in v*) ;; *) tag="v$tag" ;; esac

  archive="inferoute_${tag#v}_${os}_${arch}.tar.gz"
  base="https://github.com/$REPO/releases/download/$tag"
  echo "Downloading $archive..."
  fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
  fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "could not download checksums.txt"
  want="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
  [ -n "$want" ] || die "$archive is not listed in checksums.txt"
  [ "$want" = "$(sha256 "$tmp/$archive")" ] || die "checksum mismatch for $archive"

  tar -xzf "$tmp/$archive" -C "$tmp" inferouted
  mv "$tmp/inferouted" "$BIN_DIR/inferouted"
  chmod +x "$BIN_DIR/inferouted"
  done_msg
}

done_msg() {
  echo "Installed $("$BIN_DIR/inferouted" -version 2>/dev/null || echo inferouted) to $BIN_DIR/inferouted"
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) echo "Add it to your PATH: export PATH=\"$BIN_DIR:\$PATH\"" ;;
  esac
  echo "Then: curl -O https://raw.githubusercontent.com/$REPO/main/config.example.json && inferouted -config config.example.json"
}

main "$@"
