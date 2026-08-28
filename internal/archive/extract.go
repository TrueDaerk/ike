package archive

// extract.go writes archive members back out to disk (#2249). Listing and the
// read-only preview never touch the file system; extraction does, so every
// rule that keeps a hostile archive harmless lives here:
//
//   - member paths are sanitized against traversal ("../", absolute, Windows
//     volume names) and skipped with a reason rather than clamped silently;
//   - links (sym- and hard) and device/fifo entries are never materialized —
//     a symlink is the second half of every traversal escape;
//   - the total extracted byte count is capped like the gzip bomb guard, and
//     the cap is enforced against the *written* bytes, not the header sizes a
//     corrupt archive supplies;
//   - existing files are reported by PlanExtract before anything is written,
//     so the caller can ask before overwriting.
//
// The two-phase shape (PlanExtract, then Extract) is what makes the overwrite
// question askable: the plan reads headers only, Extract writes what the plan
// selected.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DefaultExtractLimit caps the total bytes one extraction may write. It is the
// tar-shaped twin of the gzip bomb guard: a few kilobytes of archive can
// unpack to whatever it likes, so the ceiling is on the output, not the input.
const DefaultExtractLimit int64 = 1 << 30 // 1 GiB

// ErrExtractTooLarge is returned by PlanExtract and Extract when the selection
// crosses the byte cap.
var ErrExtractTooLarge = errors.New("extraction exceeds the size cap")

// Skip reasons, spelled out once so the pane's summary and the tests agree.
const (
	SkipUnsafePath = "unsafe path"
	SkipLink       = "link entry"
	SkipSpecial    = "special file"
	SkipExists     = "exists"
)

// Skipped is one member the extraction refused, with the reason to report.
type Skipped struct {
	Name   string
	Reason string
}

// Plan is what an extraction would do: the members it selected, where they
// land, which of them already exist on disk, and what it declined.
type Plan struct {
	// Archive and Dest are the source archive and the target directory.
	Archive string
	Dest    string
	// Files are the regular members to write, Dirs the directory members to
	// create (both hold archive-relative names).
	Files []Entry
	Dirs  []string
	// Conflicts are the archive-relative names of Files whose target already
	// exists; the caller confirms before Extract is called with Overwrite.
	Conflicts []string
	// Skipped holds the members the plan refused, with reasons.
	Skipped []Skipped
	// Bytes is the declared total size of Files.
	Bytes int64
}

// Empty reports whether the plan would write nothing.
func (p Plan) Empty() bool { return len(p.Files) == 0 && len(p.Dirs) == 0 }

// Options steers Extract.
type Options struct {
	// Overwrite allows replacing existing files; without it a member whose
	// target exists is skipped with SkipExists.
	Overwrite bool
	// MaxBytes caps the total written bytes (0 or negative disables the cap).
	MaxBytes int64
}

// Result reports what an extraction achieved.
type Result struct {
	Files   int
	Dirs    int
	Bytes   int64
	Skipped []Skipped
}

// PlanExtract lists the archive at p and works out what extracting members
// into dest would do. An empty members slice selects the whole archive; a
// named directory selects its whole subtree, so the pane can offer "extract
// what the cursor is on" for a folder row too.
//
// A listing error is returned together with the plan built from the entries
// that were readable — a truncated archive extracts what it has, the same way
// the pane lists what it has.
func PlanExtract(p, dest string, members []string, maxBytes int64) (Plan, error) {
	l, listErr := List(p)
	pl := Plan{Archive: p, Dest: dest}
	sel := selector(members)
	for _, e := range l.Entries {
		if !sel(e.Name) {
			continue
		}
		target, ok := SafeTarget(dest, e.Name)
		if !ok {
			pl.Skipped = append(pl.Skipped, Skipped{Name: e.Name, Reason: SkipUnsafePath})
			continue
		}
		switch {
		case e.IsDir:
			pl.Dirs = append(pl.Dirs, e.Name)
		case e.Link != "":
			pl.Skipped = append(pl.Skipped, Skipped{Name: e.Name, Reason: SkipLink})
		case !e.Mode.IsRegular():
			pl.Skipped = append(pl.Skipped, Skipped{Name: e.Name, Reason: SkipSpecial})
		default:
			pl.Files = append(pl.Files, e)
			pl.Bytes += e.Size
			if st, err := os.Lstat(target); err == nil && !st.IsDir() {
				pl.Conflicts = append(pl.Conflicts, e.Name)
			}
		}
	}
	if maxBytes > 0 && pl.Bytes > maxBytes {
		return pl, ErrExtractTooLarge
	}
	return pl, listErr
}

// selector turns the requested member names into a membership test: nothing
// requested selects everything, a named entry selects itself and — when it is
// a directory — everything below it.
func selector(members []string) func(string) bool {
	if len(members) == 0 {
		return func(string) bool { return true }
	}
	want := make(map[string]bool, len(members))
	for _, m := range members {
		want[strings.TrimSuffix(path.Clean(m), "/")] = true
	}
	return func(name string) bool {
		if want[name] {
			return true
		}
		for dir := path.Dir(name); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
			if want[dir] {
				return true
			}
		}
		return false
	}
}

// SafeTarget resolves an archive member name against the destination
// directory, refusing everything that would escape it: absolute paths, any
// "../" component, and Windows volume names. The resolved path is checked
// against dest a second time, so a name that survives the syntactic tests but
// still lands outside is refused too.
func SafeTarget(dest, name string) (string, bool) {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "" || clean == "." || path.IsAbs(clean) {
		return "", false
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	local := filepath.FromSlash(clean)
	if filepath.VolumeName(local) != "" || filepath.IsAbs(local) {
		return "", false
	}
	target := filepath.Join(dest, local)
	root := filepath.Clean(dest)
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", false
	}
	return target, true
}

// Extract writes the plan's members under its destination directory. It
// re-reads the archive rather than trusting anything cached: the plan carries
// names, the stream carries bytes.
//
// The byte cap is enforced on what is actually written — a header claiming
// 1 KB for a 1 GB stream stops at the ceiling with ErrExtractTooLarge, with
// the partial file removed. Every other per-member failure is a Skipped entry,
// not an aborted run: one unreadable member must not cost the other 400.
func Extract(pl Plan, opts Options) (Result, error) {
	res := Result{Skipped: append([]Skipped(nil), pl.Skipped...)}
	if err := os.MkdirAll(pl.Dest, 0o755); err != nil {
		return res, err
	}
	for _, d := range pl.Dirs {
		target, ok := SafeTarget(pl.Dest, d)
		if !ok {
			res.Skipped = append(res.Skipped, Skipped{Name: d, Reason: SkipUnsafePath})
			continue
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			res.Skipped = append(res.Skipped, Skipped{Name: d, Reason: err.Error()})
			continue
		}
		res.Dirs++
	}
	want := make(map[string]bool, len(pl.Files))
	for _, e := range pl.Files {
		want[e.Name] = true
	}
	if len(want) == 0 {
		return res, nil
	}
	tr, closer, _, err := reader(pl.Archive)
	if err != nil {
		return res, err
	}
	defer closer.Close()
	budget := opts.MaxBytes
	for len(want) > 0 {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return res, fmt.Errorf("read archive: %w", err)
		}
		e, ok := entryOf(h)
		if !ok || !want[e.Name] {
			continue
		}
		delete(want, e.Name)
		n, skip, err := writeMember(pl.Dest, e, tr, opts, budget)
		switch {
		case err != nil:
			return res, err
		case skip != "":
			res.Skipped = append(res.Skipped, Skipped{Name: e.Name, Reason: skip})
		default:
			res.Files++
			res.Bytes += n
			if opts.MaxBytes > 0 {
				budget -= n
			}
		}
	}
	return res, nil
}

// writeMember writes one regular member, reporting the bytes written, a skip
// reason (the member was declined, the run continues), or a hard error (the
// byte cap, which stops the whole extraction).
func writeMember(dest string, e Entry, src io.Reader, opts Options, budget int64) (int64, string, error) {
	target, ok := SafeTarget(dest, e.Name)
	if !ok {
		return 0, SkipUnsafePath, nil
	}
	st, statErr := os.Lstat(target)
	switch {
	case statErr == nil && st.IsDir():
		return 0, "target is a directory", nil
	case statErr == nil && !opts.Overwrite:
		return 0, SkipExists, nil
	case statErr == nil && st.Mode()&os.ModeSymlink != 0:
		// Overwriting through a symlink would write wherever it points —
		// outside dest, in the interesting case. Replace the link itself.
		if err := os.Remove(target); err != nil {
			return 0, err.Error(), nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err.Error(), nil
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm(e.Mode))
	if err != nil {
		return 0, err.Error(), nil
	}
	// One byte past the remaining budget separates "exactly at the cap" from
	// "over it", the same trick ReadEntry uses for a single member.
	var r io.Reader = src
	if opts.MaxBytes > 0 {
		r = io.LimitReader(src, budget+1)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if opts.MaxBytes > 0 && n > budget {
		os.Remove(target)
		return 0, "", ErrExtractTooLarge
	}
	if copyErr != nil {
		os.Remove(target)
		return 0, copyErr.Error(), nil
	}
	if closeErr != nil {
		return n, closeErr.Error(), nil
	}
	if !e.ModTime.IsZero() {
		_ = os.Chtimes(target, e.ModTime, e.ModTime)
	}
	return n, "", nil
}

// filePerm is the mode an extracted file gets: the member's permission bits,
// but always readable and writable by the owner so the extraction cannot
// produce files the user cannot open.
func filePerm(m os.FileMode) os.FileMode {
	p := m.Perm() | 0o600
	return p
}
