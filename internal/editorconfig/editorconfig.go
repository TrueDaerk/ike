// Package editorconfig implements the EditorConfig standard
// (https://editorconfig.org, #63): parsing .editorconfig files, matching
// their section globs against file paths, and resolving the effective
// settings for a file by walking from its directory upward until a file with
// "root = true" (or the filesystem root) stops the search.
//
// The editor consumes exactly the keys it already has behaviour for:
// indent_style, indent_size, tab_width, trim_trailing_whitespace,
// insert_final_newline, end_of_line and charset (#66). Unknown keys are
// carried in Settings but ignored by the typed accessors, per the spec's
// forward-compatibility rule.
//
// Parsed files are cached per directory; the file watcher (#53) invalidates a
// directory's entry when its .editorconfig changes (see Invalidate).
package editorconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// FileName is the well-known per-directory settings file name.
const FileName = ".editorconfig"

// Settings is the effective key→value map for one file, keys lowercased.
// A nil Settings behaves like an empty one in every accessor.
type Settings map[string]string

// section is one "[pattern]" block: the raw glob and its ordered key/value
// pairs. Pair order matters — a later duplicate key wins.
type section struct {
	pattern string
	pairs   [][2]string
}

// file is one parsed .editorconfig: its preamble root flag and its sections
// in file order.
type file struct {
	root     bool
	sections []section
}

// parse reads the INI-flavored EditorConfig syntax: full-line comments start
// with ';' or '#', sections are "[glob]", pairs are "key = value". Keys are
// lowercased; values keep their case except that accessors compare known
// values case-insensitively. Malformed lines are skipped, per the spec's
// lenient-consumer rule.
func parse(data string) *file {
	f := &file{}
	var cur *section
	sc := bufio.NewScanner(strings.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.LastIndexByte(line, ']')
			if end <= 0 {
				continue
			}
			f.sections = append(f.sections, section{pattern: line[1:end]})
			cur = &f.sections[len(f.sections)-1]
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		if cur == nil {
			// Preamble: only "root" is defined there.
			if key == "root" {
				f.root = strings.EqualFold(val, "true")
			}
			continue
		}
		cur.pairs = append(cur.pairs, [2]string{key, val})
	}
	return f
}

// Resolver resolves and caches .editorconfig files per directory. The zero
// value is ready to use; a nil entry in the cache records a directory known
// to have no .editorconfig, so unchanged directories are never re-stat'd.
type Resolver struct {
	mu    sync.Mutex
	cache map[string]*file // dir → parsed file; nil = none present
}

// load returns the parsed .editorconfig of dir, consulting the cache first.
func (r *Resolver) load(dir string) *file {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.cache[dir]; ok {
		return f
	}
	var f *file
	if data, err := os.ReadFile(filepath.Join(dir, FileName)); err == nil {
		f = parse(string(data))
	}
	if r.cache == nil {
		r.cache = map[string]*file{}
	}
	r.cache[dir] = f
	return f
}

// Invalidate drops the cache entry for the directory containing path (path
// may name the .editorconfig itself or its directory). The file watcher (#53)
// calls this when an .editorconfig changes, is created, or is removed.
func (r *Resolver) Invalidate(path string) {
	dir := path
	if filepath.Base(path) == FileName {
		dir = filepath.Dir(path)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	r.mu.Lock()
	delete(r.cache, dir)
	r.mu.Unlock()
}

// Resolve computes the effective settings for path: every .editorconfig from
// the filesystem root (or the nearest "root = true" file) down to the file's
// own directory contributes its matching sections, closer files and later
// sections overriding earlier ones key by key. The special value "unset"
// removes a key. A file with no matching sections anywhere yields an empty
// (nil) Settings.
func (r *Resolver) Resolve(path string) Settings {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	abs = filepath.Clean(abs)

	// Collect the chain of directories from the file upward, stopping after
	// the first root=true .editorconfig (it still applies itself).
	type level struct {
		dir string
		f   *file
	}
	var chain []level
	for dir := filepath.Dir(abs); ; {
		f := r.load(dir)
		chain = append(chain, level{dir, f})
		if f != nil && f.root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Apply outermost first so the .editorconfig closest to the file wins.
	var out Settings
	for i := len(chain) - 1; i >= 0; i-- {
		lv := chain[i]
		if lv.f == nil {
			continue
		}
		rel, err := filepath.Rel(lv.dir, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, sec := range lv.f.sections {
			if !match(sec.pattern, rel) {
				continue
			}
			for _, p := range sec.pairs {
				if out == nil {
					out = Settings{}
				}
				if strings.EqualFold(p[1], "unset") {
					delete(out, p[0])
					continue
				}
				out[p[0]] = p[1]
			}
		}
	}
	return out
}

// defaultResolver backs the package-level convenience functions; all editor
// views share it so an .editorconfig is parsed once per change, not per pane.
var defaultResolver Resolver

// Resolve resolves path against the shared default resolver.
func Resolve(path string) Settings { return defaultResolver.Resolve(path) }

// Invalidate invalidates the shared default resolver for path's directory.
func Invalidate(path string) { defaultResolver.Invalidate(path) }

// bool reads a true/false key, case-insensitively.
func (s Settings) boolKey(key string) (val, ok bool) {
	v, present := s[key]
	if !present {
		return false, false
	}
	switch strings.ToLower(v) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// UseSpaces maps indent_style: true for "space", false for "tab".
func (s Settings) UseSpaces() (useSpaces, ok bool) {
	switch strings.ToLower(s["indent_style"]) {
	case "space":
		return true, true
	case "tab":
		return false, true
	}
	return false, false
}

// IndentWidth is the effective indent/tab display width for an editor that
// (like IKE) uses one width for both: tab_width when set, otherwise a numeric
// indent_size (per the spec, tab_width defaults to indent_size). The value
// "tab" for indent_size defers to tab_width alone.
func (s Settings) IndentWidth() (width int, ok bool) {
	if n, err := strconv.Atoi(s["tab_width"]); err == nil && n > 0 {
		return n, true
	}
	if n, err := strconv.Atoi(s["indent_size"]); err == nil && n > 0 {
		return n, true
	}
	return 0, false
}

// TrimTrailingWhitespace reads trim_trailing_whitespace.
func (s Settings) TrimTrailingWhitespace() (trim, ok bool) {
	return s.boolKey("trim_trailing_whitespace")
}

// InsertFinalNewline reads insert_final_newline.
func (s Settings) InsertFinalNewline() (insert, ok bool) {
	return s.boolKey("insert_final_newline")
}

// EndOfLine reads end_of_line, normalized to lowercase ("lf", "crlf", "cr").
// The editor ignores values it cannot store ("cr" — textenc supports LF and
// CRLF, #66).
func (s Settings) EndOfLine() (eol string, ok bool) {
	v, present := s["end_of_line"]
	if !present {
		return "", false
	}
	switch v = strings.ToLower(v); v {
	case "lf", "crlf", "cr":
		return v, true
	}
	return "", false
}

// Charset reads charset, normalized to lowercase (e.g. "utf-8", "utf-8-bom",
// "utf-16le", "utf-16be", "latin1").
func (s Settings) Charset() (charset string, ok bool) {
	v, present := s["charset"]
	if !present {
		return "", false
	}
	return strings.ToLower(v), true
}
