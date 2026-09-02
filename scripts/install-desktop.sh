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
    # ike:// scheme handler (#2396). Ike.app's executable is a shell script,
    # and LaunchServices delivers URLs as an Apple Event (GetURL), which a
    # script cannot receive — so the scheme is registered on a small compiled
    # AppleScript applet whose `open location` handler forwards the URL to
    # ike-gui (which hands it to a running IKE over the deeplink socket, or
    # starts one). Best-effort: osacompile ships with macOS.
    HANDLER="/Applications/Ike Link Handler.app"
    if command -v osacompile >/dev/null 2>&1; then
      rm -rf "$HANDLER"
      osacompile -o "$HANDLER" -e "on open location theURL
  do shell script \"'$BIN_DIR/ike-gui' \" & quoted form of theURL
end open location"
      PB=/usr/libexec/PlistBuddy
      PLIST="$HANDLER/Contents/Info.plist"
      "$PB" -c 'Set :CFBundleIdentifier dev.ike.linkhandler' "$PLIST" 2>/dev/null || \
        "$PB" -c 'Add :CFBundleIdentifier string dev.ike.linkhandler' "$PLIST"
      "$PB" -c 'Add :CFBundleURLTypes array' "$PLIST" 2>/dev/null || true
      "$PB" -c 'Add :CFBundleURLTypes:0 dict' "$PLIST" 2>/dev/null || true
      "$PB" -c 'Add :CFBundleURLTypes:0:CFBundleURLName string IKE deep link' "$PLIST" 2>/dev/null || true
      "$PB" -c 'Add :CFBundleURLTypes:0:CFBundleURLSchemes array' "$PLIST" 2>/dev/null || true
      "$PB" -c 'Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string ike' "$PLIST" 2>/dev/null || true
      # The applet should never clutter the Dock while it relays a click.
      "$PB" -c 'Add :LSUIElement bool true' "$PLIST" 2>/dev/null || true
      # Nudge LaunchServices to pick up the fresh registration.
      /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister \
        -f "$HANDLER" 2>/dev/null || true
      note "installed $HANDLER (ike:// links)"
    else
      note "warning: osacompile not found — ike:// links will not be registered"
    fi
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
    # ike:// scheme handler (#2396): the .desktop entry declares
    # x-scheme-handler/ike and takes the URL via %u; make it the default.
    command -v xdg-mime >/dev/null 2>&1 && xdg-mime default ike.desktop x-scheme-handler/ike 2>/dev/null || true
    # Refresh desktop caches where available; failures are cosmetic.
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$DATA/applications" 2>/dev/null || true
    command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q "$DATA/icons/hicolor" 2>/dev/null || true
    ;;
esac

note "done — launch IKE from your app grid/Launchpad, or run: ike-gui"
