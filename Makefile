# Makefile for ike — build and install the terminal IDE.
#
# Usage:
#   make            # build ./ike
#   make install    # install to ~/.local/bin/ike
#   make install BINDIR=/usr/local/bin
#   make uninstall
#   make clean
#   make docs      # regenerate userdocs/reference from the source
#   make version   # print the version the next build will carry

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

.PHONY: all build install uninstall clean test docs version

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

# Print what `ike --version` will report for a build from this tree.
version: build
	./$(BINARY) --version
