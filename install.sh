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

base=https://github.com/$repo/releases/latest/download
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if command -v sha256sum >/dev/null; then sum="sha256sum"; else sum="shasum -a 256"; fi

echo "Downloading for ${os}/${arch}..."
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
mkdir -p "$bin_dir"
for b in moodle moodle-mcp; do
  file=${b}_${os}_${arch}
  curl -fsSL "$base/$file" -o "$tmp/$file"
  (cd "$tmp" && grep " $file\$" checksums.txt > want.txt && $sum -c want.txt >/dev/null) \
    || { echo "Checksum mismatch for $file, aborting." >&2; exit 1; }
  install -m 755 "$tmp/$file" "$bin_dir/$b"
done
echo "Installed moodle and moodle-mcp to $bin_dir"

# Claude Code users also get the skill that teaches Claude to use the tools.
if [ -d "$HOME/.claude" ]; then
  skill_dir=$HOME/.claude/skills/zewail-moodle
  mkdir -p "$skill_dir"
  if curl -fsSL "https://raw.githubusercontent.com/$repo/main/skills/zewail-moodle/SKILL.md" -o "$skill_dir/SKILL.md"; then
    echo "Installed the zewail-moodle skill to $skill_dir"
  fi
fi

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) echo "Add $bin_dir to your PATH, e.g. echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.zshrc" ;;
esac
echo
echo "Next:"
echo "  moodle auth"
echo "  claude mcp add -s user moodle -- \"$bin_dir/moodle-mcp\""
