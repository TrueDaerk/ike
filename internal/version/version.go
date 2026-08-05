// Package version holds IKE's release version and the build stamp printed by
// `ike --version`. It is a leaf package with no dependencies, so anything may
// import it.
//
// Version is the source of truth for the shipped number and is bumped here.
// Commit and Dirty are empty in a plain `go build` and filled in by the
// Makefile (and any release build) through -ldflags:
//
//	go build -ldflags "-X ike/internal/version.Commit=$(git rev-parse --short HEAD)" ./cmd/ike
//
// Version itself is a var rather than a const for the same reason: a release
// build stamps the tag it is cutting, so a binary built from a tag and one
// built from the same commit on a branch can be told apart.
package version

import (
	"fmt"
	"runtime"
	"strings"
)

// Version is the release version, following semantic versioning. For IKE the
// "public API" SemVer talks about is the settings-file format, the keymap
// schema and the plugin ABI — not a Go package API, since IKE ships as a
// binary and is not importable.
var Version = "0.3.6"

// Commit is the short git commit the binary was built from, stamped at build
// time. Empty when built without the Makefile.
var Commit = ""

// Dirty is "true" when the working tree carried uncommitted changes at build
// time. A string rather than a bool because -ldflags -X can only set strings.
var Dirty = ""

// Short returns just the version number, e.g. "0.1.0".
func Short() string { return Version }

// Full returns the one-line version banner: the number, the build stamp when
// there is one, and the toolchain and platform the binary was built for.
//
//	ike 0.1.0 (a1b2c3d, dirty) go1.26.0 darwin/arm64
func Full() string {
	var b strings.Builder
	b.WriteString("ike ")
	b.WriteString(Version)
	if stamp := buildStamp(); stamp != "" {
		b.WriteString(" (")
		b.WriteString(stamp)
		b.WriteString(")")
	}
	fmt.Fprintf(&b, " %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return b.String()
}

// buildStamp renders the commit and dirty marker, or "" when neither is set.
func buildStamp() string {
	switch {
	case Commit == "":
		return ""
	case Dirty == "true":
		return Commit + ", dirty"
	default:
		return Commit
	}
}
