package complexity

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ike/internal/complete/langindex"
	"ike/internal/highlight"
	"ike/internal/jinja"
	"ike/internal/lang"
)

// project.go is the walk the index and the headless `ike complexity` report
// (#2922) share: the same directory skip rules (langindex.WalkFiles), the
// same per-file classification (classify: a language with a spec, no exclude
// glob matching) and the same size cap, so the CLI and the tool window never
// disagree about which files count.

// MaxFileSize is the per-file byte cap of the walk: a larger file is not
// measured, by the index or by the CLI.
const MaxFileSize = indexMaxFileSize

// WalkProject walks root with the index's skip rules and calls fn for every
// file a language claims that no complexity.exclude glob matches, in walk
// order: measurable reports that the index would measure it (the language
// has a complexity spec). Files no language claims and excluded files are
// not reported. The walk stops at the index's file cap of measurable files.
func WalkProject(root string, exclude []string, fn func(path string, l lang.Language, measurable bool)) error {
	return WalkUnder(root, root, exclude, fn)
}

// WalkUnder is WalkProject over the directory dir inside the project root
// (#2936): dir is walked with the index's skip rules (dir itself is never
// skipped, even when hidden) while the exclude globs stay relative to root.
func WalkUnder(root, dir string, exclude []string, fn func(path string, l lang.Language, measurable bool)) error {
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	n := 0
	return langindex.WalkFiles(dir, nil, func(path string) error {
		l, ok := lang.ByPath(path)
		if !ok || Excluded(root, exclude, path) {
			return nil
		}
		measurable := classify(root, exclude, path) == indexLang
		if measurable {
			if n >= indexMaxFiles {
				return filepath.SkipAll
			}
			n++
		}
		fn(path, l, measurable)
		return nil
	})
}

// Measurable reports whether the index measures path: a language with a
// spec claims it and no exclude glob (relative to root) matches.
func Measurable(root string, exclude []string, path string) bool {
	return classify(root, exclude, path) == indexLang
}

// ReadSource reads a file for measuring under the walk's size cap; false
// for an unreadable or oversized file.
func ReadSource(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > MaxFileSize {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// SourceLines splits text into the lines Measure parses as the language of
// path: a Jinja template (#3059) has its Jinja blocks masked at the same
// offsets, so a {% for %} tag cannot break the inner parse and the measured
// lines and columns still point into the real text.
func SourceLines(path, text string) []string {
	return jinja.MaskPath(path, strings.Split(text, "\n"))
}

var (
	availOnce sync.Once
	avail     bool
)

// Available reports whether the build can parse at least one language with
// a spec (cgo + grammar). Without it nothing can be measured: the index
// never fills and the CLI refuses to run.
func Available() bool {
	availOnce.Do(func() {
		for _, l := range lang.All() {
			if l.Complexity != nil && highlight.SyntaxTree(l.ID, []string{""}) != nil {
				avail = true
				return
			}
		}
	})
	return avail
}
