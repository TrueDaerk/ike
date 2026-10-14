#!/usr/bin/env bash
# package.sh — build and package one platform's release archives (#3002).
#
#   scripts/release/package.sh <goos> <goarch>    (from the repository root)
#
# Builds the full IDE (`make build`) and the complexity-only binary
# (`make build-complexity`, #2939) for <goos>/<goarch> with CGo — so the
# commit / dirty stamp comes from the Makefile's LDFLAGS — and writes
#
#   dist/ike_<version>_<goos>_<goarch>.{zip,tar.gz}
#   dist/ike-complexity_<version>_<goos>_<goarch>.{zip,tar.gz}
#
# .zip on Windows, .tar.gz elsewhere; files sit at the archive root. The full
# archive carries ike, keyprobe, README.md and LICENSE, plus the Windows
# launcher pieces on Windows (ike-gui.cmd, install-desktop.ps1,
# wezterm-ike.lua, ike.ico). The complexity archive carries its ike, README.md
# and LICENSE.
#
# Cross-arch CGo needs a matching C compiler in CC (the workflow sets
# `clang -arch x86_64` for darwin/amd64). When the target is the host
# platform the binaries are smoke-tested: `ike --version` must carry the
# commit stamp, and `ike complexity --json .` must report functions.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <goos> <goarch>" >&2
  exit 2
fi
goos="$1"
goarch="$2"
export GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=1
# macOS tar would otherwise add AppleDouble entries for extended attributes.
export COPYFILE_DISABLE=1
export GOFLAGS="${GOFLAGS:-} -trimpath"

version="$(GOOS= GOARCH= go run ./cmd/release version)"
exe=""
[ "$goos" = windows ] && exe=".exe"

stage="dist/stage-${goos}-${goarch}"
full="$stage/full"
cplx="$stage/complexity"
rm -rf "$stage"
mkdir -p "$full" "$cplx"

make build BINARY="$full/ike$exe"
go build -o "$full/keyprobe$exe" ./cmd/keyprobe
make build-complexity BINARY="$cplx/ike$exe"

for d in "$full" "$cplx"; do
  cp README.md LICENSE "$d/"
done
if [ "$goos" = windows ]; then
  cp deploy/ike-gui.cmd deploy/install-desktop.ps1 deploy/icon/ike.ico "$full/"
  cp deploy/wezterm/ike.lua "$full/wezterm-ike.lua"
fi

hostos="$(GOOS= GOARCH= go env GOHOSTOS)"
hostarch="$(GOOS= GOARCH= go env GOHOSTARCH)"
if [ "$goos" = "$hostos" ] && [ "$goarch" = "$hostarch" ]; then
  commit="$(git rev-parse --short HEAD)"
  banner="$("$full/ike$exe" --version)"
  echo "$banner"
  case "$banner" in
    "ike $version ($commit"*) ;;
    *) echo "package: version banner lacks 'ike $version ($commit': $banner" >&2; exit 1 ;;
  esac
  cbanner="$("$cplx/ike$exe" --version)"
  echo "$cbanner"
  case "$cbanner" in
    *"(complexity-only)"*) ;;
    *) echo "package: complexity build banner lacks '(complexity-only)': $cbanner" >&2; exit 1 ;;
  esac
  report="$(mktemp)"
  "$cplx/ike$exe" complexity --json . > "$report"
  grep -q '"functions"' "$report" || { echo "package: complexity report has no functions" >&2; exit 1; }
  rm -f "$report"
else
  echo "package: $goos/$goarch is not the host ($hostos/$hostarch) — smoke test skipped"
fi

archive() { # <src dir> <archive base name>
  local src="$1" base="$2"
  if [ "$goos" = windows ]; then
    rm -f "dist/$base.zip"
    if command -v zip >/dev/null 2>&1; then
      (cd "$src" && zip -qr "../../$base.zip" .)
    else
      (cd "$src" && 7z a -tzip -bso0 "../../$base.zip" .)
    fi
    echo "dist/$base.zip"
  else
    tar -C "$src" -czf "dist/$base.tar.gz" $(ls "$src")
    echo "dist/$base.tar.gz"
  fi
}
archive "$full" "ike_${version}_${goos}_${goarch}"
archive "$cplx" "ike-complexity_${version}_${goos}_${goarch}"
rm -rf "$stage"
