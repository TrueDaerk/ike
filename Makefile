# Makefile for ike — build and install the terminal IDE.
#
# Usage:
#   make            # build ./ike
#   make install    # install to ~/.local/bin/ike
#   make install BINDIR=/usr/local/bin
#   make uninstall
#   make clean
#   make docs      # regenerate userdocs/reference from the source
#   make shots     # regenerate the feature screenshots in docs/screenshots
#   make docs-deploy  # build + publish the docs site to the gh-pages branch
#   make version   # print the version the next build will carry
#   make install-desktop  # desktop launcher: Ike.app (macOS) / ike.desktop (Linux)
#   make icons     # regenerate deploy/icon from the Go source (macOS tools)

BINARY  := ike
PACKAGE := ./cmd/ike
BINDIR  ?= $(HOME)/.local/bin
GO      ?= go

# Build stamp for `ike --version`. The version number itself lives in
# internal/version; only the commit and the dirty marker come from git, so a
# build outside a checkout still works (both fall back to empty).
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
DIRTY   := $(shell git diff --quiet 2>/dev/null || echo true)
VERPKG  := ike/internal/version
LDFLAGS := -X $(VERPKG).Commit=$(COMMIT) -X $(VERPKG).Dirty=$(DIRTY)

.PHONY: all build install uninstall clean test docs shots docs-deploy version install-desktop icons

all: build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PACKAGE)

install:
	mkdir -p $(BINDIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINDIR)/$(BINARY) $(PACKAGE)

uninstall:
	rm -f $(BINDIR)/$(BINARY)

clean:
	rm -f $(BINARY)

test:
	$(GO) test ./...

# Regenerate the documentation reference pages. CI fails when the checked-in
# output differs from a fresh run, so commit the result.
docs:
	$(GO) run ./cmd/docgen

# Regenerate the feature screenshots the user docs embed (#1634). Unlike the
# reference pages these are not CI-checked: rendering depends on the machine's
# fonts, so a shot is refreshed deliberately, when the UI it shows changed.
shots:
	$(GO) run ./cmd/shotgen

# Build the docs site strictly and force-push it to gh-pages (#1837), the same
# publish path CI's docs.yml uses on a main push — the local escape hatch when
# Actions is slow or unavailable (#1606). Needs `mkdocs` + `userdocs/requirements.txt`
# installed locally (pip install -r userdocs/requirements.txt) and push rights
# on the repo. One-time repo setup: Settings > Pages > Build and deployment >
# Source must be "Deploy from a branch" (gh-pages, /root), not "GitHub Actions"
# — branch deploys are silently ignored otherwise. Two concurrent gh-deploy
# pushes race on gh-pages; unlike CI's `concurrency: pages` group, that's on
# you locally.
docs-deploy:
	@command -v mkdocs >/dev/null 2>&1 || { \
		echo "error: mkdocs not found — run: pip install -r userdocs/requirements.txt" >&2; \
		exit 1; \
	}
	mkdocs gh-deploy --strict

# Print what `ike --version` will report for a build from this tree.
version: build
	./$(BINARY) --version

# Install the desktop launcher (#1567): dedicated Ghostty config + ike-gui,
# plus Ike.app (macOS) or ike.desktop with hicolor icons (Linux). Ghostty is
# a user-installed prerequisite; run `make install` first for the binary.
install-desktop:
	BINDIR=$(BINDIR) ./scripts/install-desktop.sh

# Regenerate the checked-in icon artefacts from the Go source. Needs macOS
# (sips + iconutil); the results are committed so installs stay tool-free.
icons:
	$(GO) run ./deploy/icon/gen
	for s in 16 24 32 48 64 128 256 512; do \
		mkdir -p deploy/icon/hicolor/$${s}x$${s}/apps; \
		sips -z $$s $$s deploy/icon/ike-1024.png --out deploy/icon/hicolor/$${s}x$${s}/apps/ike.png >/dev/null; \
	done
	rm -rf /tmp/ike.iconset && mkdir -p /tmp/ike.iconset
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s deploy/icon/ike-1024.png --out /tmp/ike.iconset/icon_$${s}x$${s}.png >/dev/null; \
		sips -z $$((s*2)) $$((s*2)) deploy/icon/ike-1024.png --out /tmp/ike.iconset/icon_$${s}x$${s}@2x.png >/dev/null; \
	done
	iconutil -c icns /tmp/ike.iconset -o deploy/icon/ike.icns
	rm -rf /tmp/ike.iconset
