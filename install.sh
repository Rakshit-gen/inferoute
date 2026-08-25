#!/bin/sh
# Installs inferouted with one command:
#
#   curl -fsSL https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/install.sh | sh
#
# Wraps `go install` rather than shipping prebuilt binaries — inferoute
# doesn't cut tagged releases yet, and `go install ...@latest` already
# resolves to the latest commit on the default branch without one.
set -eu

if ! command -v go >/dev/null 2>&1; then
  echo "inferouted needs the Go toolchain to install (go install under the hood)." >&2
  echo "Install Go from https://go.dev/dl/, then re-run this script." >&2
  exit 1
fi

echo "Installing inferouted (go install .../cmd/inferouted@latest)..."
go install github.com/Rakshit-gen/inferoute/cmd/inferouted@latest

bin_dir=$(go env GOBIN)
if [ -z "$bin_dir" ]; then
  bin_dir="$(go env GOPATH)/bin"
fi

echo "Installed to $bin_dir/inferouted"
case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *)
    export_line="export PATH=\"$bin_dir:\$PATH\""
    rc_file=""
    case "${SHELL:-}" in
      */zsh) rc_file="$HOME/.zshrc" ;;
      */bash) rc_file="$HOME/.bashrc" ;;
    esac
    if [ -n "$rc_file" ] && [ -f "$rc_file" ] && ! grep -qF "$export_line" "$rc_file"; then
      printf '\n# added by inferoute install.sh\n%s\n' "$export_line" >> "$rc_file"
      echo "Added it to your PATH in $rc_file — open a new shell (or run: $export_line)"
    else
      echo "Add it to your PATH: $export_line"
    fi
    ;;
esac
echo "Then: curl -O https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/config.example.json && inferouted -config config.example.json"
