#!/bin/sh
# kmap installer — https://kmap.aymenkrifa.com
#   curl -LsSf https://kmap.aymenkrifa.com/install.sh | sh
#
# Downloads the release tarball for this machine from the latest GitHub
# release, verifies it against the published checksums, and drops the kmap
# binary in ~/.local/bin. Re-run any time to update — it tells you which
# version you came from and landed on. Override the destination with
# KMAP_BIN_DIR=/somewhere, or silence the progress lines with KMAP_QUIET=1
# (errors still print).
set -eu

# Everything below is function definitions only; nothing executes until the
# `main "$@"` on the last line. If the download is cut off mid-transfer, a
# truncated script is a syntax error or a no-op — it can never run half an
# install.

quiet() { [ -n "${KMAP_QUIET:-}" ]; }
line() { quiet || printf '%s\n' "$*"; }
ok()   { quiet || printf '  %s✓%s %-10s %s\n' "$grn" "$rst" "$1" "$2"; }
warn() { quiet || printf '  %s!%s %s\n' "$ylw" "$rst" "$*"; }
die()  { printf '\n  %serror%s %s\n' "${red:-}" "${rst:-}" "$*" >&2; exit 1; }

fetch() { curl --proto '=https' --tlsv1.2 -fsSL "$1" -o "$2"; }

# sha256 of a file, with whichever tool this system has.
hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# Print the version an installed kmap reports, or nothing. kmap's version
# subcommand just prints a line and exits, so — unlike reaper's TUI — there is
# nothing here that can hijack the terminal and no setsid guard is needed.
probe_version() {
  [ -x "$1" ] || return 0
  "$1" version 2>/dev/null || true
}

# Closing guidance — shared by the early "up to date" exit and the full
# install path. Warns when this kmap isn't the one PATH resolves to.
closing() {
  line ""
  case ":$PATH:" in
    *":$BIN_DIR:"*)
      # A different kmap earlier on PATH would silently keep winning.
      shadow="$(command -v kmap 2>/dev/null || true)"
      if [ -n "$shadow" ] && [ "$shadow" != "$BIN_DIR/kmap" ]; then
        warn "another kmap at ${b}$shadow${rst} comes first on your PATH and will shadow this one."
      fi
      line "  ${b}kmap${rst} is ready — run ${b}kmap init${rst} to write a starter config from your kubeconfig."
      ;;
    *)
      warn "${b}$BIN_DIR${rst} is not on your PATH yet."
      line "     this session:  ${b}export PATH=\"$BIN_DIR:\$PATH\"${rst}"
      line "     to keep it:     append that line to ~/.bashrc or ~/.zshrc"
      line "  then run ${b}kmap init${rst} to write a starter config from your kubeconfig."
      ;;
  esac
}

main() {
  REPO="aymenkrifa/kmap"

  # Readable output: colour only on a real terminal (honours NO_COLOR), plain
  # when piped to a file. KMAP_QUIET=1 hushes everything but errors, which
  # always go to stderr.
  if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    b=$(printf '\033[1m');   dim=$(printf '\033[2m'); grn=$(printf '\033[32m')
    ylw=$(printf '\033[33m'); red=$(printf '\033[31m'); rst=$(printf '\033[0m')
  else
    b=; dim=; grn=; ylw=; red=; rst=
  fi

  # Where to put the binary: an explicit override wins; root gets a system dir
  # that's already on PATH (so 'kmap' just works, no profile edits); everyone
  # else gets a no-sudo user dir.
  if [ -n "${KMAP_BIN_DIR:-}" ]; then
    BIN_DIR="$KMAP_BIN_DIR"
  elif [ "$(id -u)" = 0 ]; then
    BIN_DIR="/usr/local/bin"
  else
    BIN_DIR="$HOME/.local/bin"
  fi

  # Platform. Binaries are published for these four targets only.
  case "$(uname -s)" in
    Linux)  OS=linux ;;
    Darwin) OS=darwin ;;
    *) die "kmap ships linux and macos binaries; build from source with: go install github.com/aymenkrifa/kmap@latest" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64)  ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac

  command -v curl >/dev/null 2>&1 || die "curl is required."
  command -v tar  >/dev/null 2>&1 || die "tar is required."
  command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 \
    || die "need sha256sum or shasum to verify the download."

  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT

  line ""
  line "  ${b}kmap${rst}${dim} · your services, your names, every cluster${rst}"
  line ""
  ok "target" "${OS}_${ARCH}"

  # What's already installed, so the end of the run can say whether this was
  # a fresh install, an update, or a no-op.
  old_ver=""
  [ -x "$BIN_DIR/kmap" ] && old_ver="$(probe_version "$BIN_DIR/kmap")"

  # The asset names carry the tag, so the tag has to be resolved before any
  # download URL exists. One redirect, no body: /releases/latest lands on
  # /releases/tag/vX.Y.Z.
  latest_url="$(curl --proto '=https' --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/$REPO/releases/latest" 2>/dev/null || true)"
  case "$latest_url" in
    */releases/tag/*) TAG="${latest_url##*/tag/}" ;;
    *) die "could not determine the latest release — is there one yet?" ;;
  esac

  # Version reporting is a straight string compare against the tag: release
  # binaries are built with -ldflags -X cmd.version=$TAG, so an installed
  # kmap's own "version" output already matches the tag verbatim.
  if [ -n "$old_ver" ] && [ "$old_ver" = "$TAG" ]; then
    ok "up to date" "already on the latest release ($TAG)"
    closing
    return 0
  fi

  STEM="kmap_${TAG}_${OS}_${ARCH}"
  TARBALL="$STEM.tar.gz"
  BASE="https://github.com/$REPO/releases/download/$TAG"

  fetch "$BASE/$TARBALL" "$TMP/$TARBALL" || die "could not download $TARBALL — is there a release for $OS/$ARCH?"
  fetch "$BASE/checksums.txt" "$TMP/checksums.txt" || die "could not download checksums.txt."
  ok "downloaded" "$TARBALL"

  # One checksums.txt covers every asset in the release, so the expected hash
  # is picked out by filename rather than assumed to be the file's only line.
  expected="$(awk -v f="$TARBALL" '$2 == f { print $1 }' "$TMP/checksums.txt")"
  [ -n "$expected" ] || die "checksums.txt has no entry for $TARBALL."
  actual="$(hash_file "$TMP/$TARBALL")"
  [ "$expected" = "$actual" ] || die "checksum mismatch — refusing to install."
  ok "verified" "sha256 checksum"

  # The binary sits one directory deep, inside a folder named after the stem.
  tar -xzf "$TMP/$TARBALL" -C "$TMP"
  [ -f "$TMP/$STEM/kmap" ] || die "archive did not contain the kmap binary."
  mkdir -p "$BIN_DIR"
  install -m 755 "$TMP/$STEM/kmap" "$BIN_DIR/kmap"

  if [ -z "$old_ver" ]; then
    ok "installed" "$BIN_DIR/kmap  ($TAG)"
  else
    ok "updated" "$old_ver → $TAG"
  fi

  closing
}

main "$@"
