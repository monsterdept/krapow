#!/usr/bin/env bash
# krapow installer — downloads the latest GitHub release for the detected
# OS/arch and drops the binary in ~/.local/bin (override via KRAPOW_INSTALL_DIR).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rossturk/krapow/main/install.sh | bash
set -euo pipefail

REPO="rossturk/krapow"
BIN="krapow"
INSTALL_DIR="${KRAPOW_INSTALL_DIR:-$HOME/.local/bin}"

err()  { echo "krapow-install: error: $*" >&2; exit 1; }
info() { echo "krapow-install: $*"; }

case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *)      err "unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)    ARCH=amd64 ;;
  arm64|aarch64)   ARCH=arm64 ;;
  *)               err "unsupported arch: $(uname -m)" ;;
esac

info "detected $OS/$ARCH"

# Resolve the latest version via the redirect target of /releases/latest —
# avoids the API rate limit you'd hit on `api.github.com/.../releases/latest`
# from a shared CI IP.
LATEST_URL=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
  "https://github.com/$REPO/releases/latest") \
  || err "could not query latest release"
VERSION="${LATEST_URL##*/v}"
[ -n "$VERSION" ] && [ "$VERSION" != "$LATEST_URL" ] \
  || err "could not parse version from $LATEST_URL (no releases yet?)"

info "installing v$VERSION"

TARBALL="${BIN}_${VERSION}_${OS}_${ARCH}.tar.gz"
CHECKSUMS="${BIN}_${VERSION}_checksums.txt"
BASE="https://github.com/$REPO/releases/download/v${VERSION}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

info "downloading $TARBALL"
curl -fsSL -o "$TMPDIR/$TARBALL" "$BASE/$TARBALL" \
  || err "download failed: $BASE/$TARBALL"

# Best-effort checksum verification — proceeds with a warning if the
# checksums file is missing (older releases) or sha256 tooling isn't around.
if curl -fsSL -o "$TMPDIR/$CHECKSUMS" "$BASE/$CHECKSUMS" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    SHA_CMD="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    SHA_CMD="shasum -a 256"
  else
    SHA_CMD=""
  fi
  if [ -n "$SHA_CMD" ]; then
    info "verifying checksum"
    ( cd "$TMPDIR" && grep " $TARBALL\$" "$CHECKSUMS" | $SHA_CMD -c - >/dev/null ) \
      || err "checksum mismatch for $TARBALL"
  else
    info "warn: no sha256 tool found; skipping checksum verification"
  fi
else
  info "warn: checksums file not published; skipping verification"
fi

info "extracting"
tar -xzf "$TMPDIR/$TARBALL" -C "$TMPDIR"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMPDIR/$BIN" "$INSTALL_DIR/$BIN"
info "installed $INSTALL_DIR/$BIN"

# PATH hint — only nag if the dir really isn't already resolvable.
if ! command -v "$BIN" >/dev/null 2>&1 \
   || [ "$(command -v "$BIN")" != "$INSTALL_DIR/$BIN" ]; then
  cat <<EOF

note: $INSTALL_DIR is not on your PATH (or is shadowed). Add this to
      your shell profile:

    export PATH="$INSTALL_DIR:\$PATH"

EOF
fi

info "next: run \`krapow doctor\` to check host readiness"
