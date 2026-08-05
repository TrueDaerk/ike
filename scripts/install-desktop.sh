#!/bin/sh
# install-desktop.sh — install IKE's desktop launcher (#1567): a dedicated
# Ghostty config (ike.conf), the ike-gui launcher, and the platform shell —
# Ike.app on macOS, ike.desktop + hicolor icons on Linux. Ghostty stays a
# user-installed prerequisite; the ike binary itself is installed by
# `make install`. Run from the repository root: `make install-desktop`.
set -eu

DEPLOY="$(cd "$(dirname "$0")/../deploy" && pwd)"
CONF_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/ghostty"
CONF="$CONF_DIR/ike.conf"
BIN_DIR="${BINDIR:-$HOME/.local/bin}"

note() { printf '%s\n' "$*"; }

# --- shared core: ike.conf + ike-gui -----------------------------------------

# A Dock/desktop launch does not inherit the shell's PATH, so the installed
# config points at the resolved ike binary. Prefer BINDIR (where make install
# puts it), then PATH.
IKE_BIN=""
if [ -x "$BIN_DIR/ike" ]; then
  IKE_BIN="$BIN_DIR/ike"
elif command -v ike >/dev/null 2>&1; then
  IKE_BIN="$(command -v ike)"
fi
if [ -z "$IKE_BIN" ]; then
  note "warning: no ike binary found ($BIN_DIR/ike or PATH) — run 'make install' first;"
  note "         installing the config with 'command = ike' anyway."
  IKE_BIN="ike"
fi

mkdir -p "$CONF_DIR"
if [ -f "$CONF" ]; then
  printf 'overwrite existing %s? [y/N] ' "$CONF"
  read -r answer
  case "$answer" in
    y|Y) ;;
    *) note "keeping existing ike.conf"; SKIP_CONF=1 ;;
  esac
fi
if [ -z "${SKIP_CONF:-}" ]; then
  sed "s|^command = ike$|command = $IKE_BIN|" "$DEPLOY/ghostty/ike.conf" > "$CONF"
  note "installed $CONF (command = $IKE_BIN)"
fi

mkdir -p "$BIN_DIR"
install -m 0755 "$DEPLOY/ike-gui" "$BIN_DIR/ike-gui"
note "installed $BIN_DIR/ike-gui"

# --- platform shell ----------------------------------------------------------

case "$(uname -s)" in
  Darwin)
    if [ ! -d /Applications/Ghostty.app ]; then
      note "warning: /Applications/Ghostty.app not found — install Ghostty from https://ghostty.org/"
    fi
    APP=/Applications/Ike.app
    rm -rf "$APP"
    mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
    install -m 0644 "$DEPLOY/Info.plist" "$APP/Contents/Info.plist"
    install -m 0755 "$DEPLOY/ike-gui" "$APP/Contents/MacOS/ike-launcher"
    install -m 0644 "$DEPLOY/icon/ike.icns" "$APP/Contents/Resources/ike.icns"
    note "installed $APP"
    note "note: the running window shows the Ghostty Dock icon — the Ike icon"
    note "      applies to the launcher tile (Launchpad/Spotlight/Dock)."
    ;;
  *)
    if ! command -v ghostty >/dev/null 2>&1; then
      note "warning: ghostty not on PATH — ike-gui will fall back to \$TERMINAL/kitty/wezterm/foot"
    fi
    DATA="${XDG_DATA_HOME:-$HOME/.local/share}"
    mkdir -p "$DATA/applications"
    install -m 0644 "$DEPLOY/ike.desktop" "$DATA/applications/ike.desktop"
    note "installed $DATA/applications/ike.desktop"
    for dir in "$DEPLOY"/icon/hicolor/*; do
      s="$(basename "$dir")"
      mkdir -p "$DATA/icons/hicolor/$s/apps"
      install -m 0644 "$dir/apps/ike.png" "$DATA/icons/hicolor/$s/apps/ike.png"
    done
    note "installed hicolor icons under $DATA/icons/hicolor"
    # Refresh desktop caches where available; failures are cosmetic.
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$DATA/applications" 2>/dev/null || true
    command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q "$DATA/icons/hicolor" 2>/dev/null || true
    ;;
esac

note "done — launch IKE from your app grid/Launchpad, or run: ike-gui"
