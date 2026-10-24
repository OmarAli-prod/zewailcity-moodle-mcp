#!/bin/sh
# Installs the latest moodle and moodle-mcp binaries on macOS or Linux.
#   curl -fsSL https://raw.githubusercontent.com/OmarAli-prod/zewailcity-moodle-mcp/main/install.sh | sh
# Set BIN_DIR to choose where they go (default ~/.local/bin).
set -eu

repo=OmarAli-prod/zewailcity-moodle-mcp
bin_dir=${BIN_DIR:-$HOME/.local/bin}

case $(uname -s) in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Unsupported OS: $(uname -s). Use 'go install github.com/$repo/cmd/...@latest'." >&2; exit 1 ;;
esac
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m)." >&2; exit 1 ;;
esac

name=moodle-mcp_${os}_${arch}
base=https://github.com/$repo/releases/latest/download
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $name..."
curl -fsSL "$base/$name.tar.gz" -o "$tmp/$name.tar.gz"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
if command -v sha256sum >/dev/null; then sum="sha256sum"; else sum="shasum -a 256"; fi
(cd "$tmp" && grep " $name.tar.gz\$" checksums.txt > want.txt && $sum -c want.txt >/dev/null) \
  || { echo "Checksum mismatch, aborting." >&2; exit 1; }

mkdir -p "$bin_dir"
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
for b in moodle moodle-mcp; do
  install -m 755 "$tmp/$b" "$bin_dir/$b"
done
echo "Installed moodle and moodle-mcp to $bin_dir"

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) echo "Add $bin_dir to your PATH, e.g. echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.zshrc" ;;
esac
echo
echo "Next:"
echo "  moodle auth"
echo "  claude mcp add -s user moodle -- \"$bin_dir/moodle-mcp\""
