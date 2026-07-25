# Makefile for ike — build and install the terminal IDE.
#
# Usage:
#   make            # build ./ike
#   make install    # install to ~/.local/bin/ike
#   make install BINDIR=/usr/local/bin
#   make uninstall
#   make clean
#   make docs      # regenerate userdocs/reference from the source

BINARY  := ike
PACKAGE := ./cmd/ike
BINDIR  ?= $(HOME)/.local/bin
GO      ?= go

.PHONY: all build install uninstall clean test docs

all: build

build:
	$(GO) build -o $(BINARY) $(PACKAGE)

install:
	mkdir -p $(BINDIR)
	$(GO) build -o $(BINDIR)/$(BINARY) $(PACKAGE)

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
