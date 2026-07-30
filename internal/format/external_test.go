package format

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script writes an executable shell script and returns its absolute path
// (used directly as External.Command — LookPath accepts absolute paths).
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func extReq(lines ...string) Request {
	return Request{
		Path:    "/tmp/x.txt",
		Lines:   lines,
		Options: Options{TabWidth: 3, UseSpaces: true, MaxLineLength: 88},
		Root:    "/tmp",
	}
}

// TestExternalStdinStdoutRoundtrip: the buffer goes in on stdin, the tool's
// stdout comes back as the formatted text.
func TestExternalStdinStdoutRoundtrip(t *testing.T) {
	e := External{Command: script(t, `tr 'a-z' 'A-Z'`)}
	res, err := e.Run(context.Background(), extReq("hello", "world"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == nil || *res.Text != "HELLO\nWORLD" {
		t.Fatalf("got %v", res.Text)
	}
}

// TestExternalPlaceholders: ${FILE}/${TAB_WIDTH}/${INDENT_STYLE}/
// ${MAX_LINE_LENGTH} expand per invocation.
func TestExternalPlaceholders(t *testing.T) {
	e := External{
		Command: script(t, `printf '%s|%s|%s|%s' "$1" "$2" "$3" "$4"`),
		Args:    []string{"${FILE}", "${TAB_WIDTH}", "${INDENT_STYLE}", "${MAX_LINE_LENGTH}"},
	}
	res, err := e.Run(context.Background(), extReq("x"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/tmp/x.txt|3|space|88"; *res.Text != want {
		t.Fatalf("got %q want %q", *res.Text, want)
	}
}

// TestExternalRangePlaceholders: RunRange passes 1-based inclusive lines; an
// end-exclusive line-wise selection stops before its terminal (line, 0).
func TestExternalRangePlaceholders(t *testing.T) {
	e := External{
		Command:   script(t, `printf '%s-%s' "$1" "$2"`),
		RangeArgs: []string{"${START_LINE}", "${END_LINE}"},
	}
	res, err := e.RunRange(context.Background(), extReq("a", "b", "c"), Pos{Line: 0}, Pos{Line: 2, Col: 0})
	if err != nil {
		t.Fatal(err)
	}
	if *res.Text != "1-2" {
		t.Fatalf("got %q want 1-2", *res.Text)
	}
	noRange := External{Command: "x"}
	if _, err := noRange.RunRange(context.Background(), extReq("a"), Pos{}, Pos{}); err == nil {
		t.Fatal("RunRange without RangeArgs must error")
	}
}

// TestExternalNonZeroExit: failure carries the tool's stderr, truncated.
func TestExternalNonZeroExit(t *testing.T) {
	e := External{Command: script(t, `echo "syntax error at line 3" >&2; exit 2`)}
	_, err := e.Run(context.Background(), extReq("x"))
	if err == nil || !strings.Contains(err.Error(), "syntax error at line 3") {
		t.Fatalf("error must carry stderr, got %v", err)
	}
}

// TestExternalTimeout: a hung tool is killed by the context and reported.
func TestExternalTimeout(t *testing.T) {
	e := External{Command: script(t, `sleep 5`)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := e.Run(ctx, extReq("x"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout must kill the tool promptly")
	}
}

// TestExternalTempFileMode: the tool formats a temp copy in place via
// ${FILE}; the copy is read back and the original file is never touched.
func TestExternalTempFileMode(t *testing.T) {
	orig := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(orig, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := External{
		Command:  script(t, `tr 'a-z' 'A-Z' < "$1" > "$1.new" && mv "$1.new" "$1"`),
		Args:     []string{"${FILE}"},
		TempFile: true,
	}
	req := extReq("abc")
	req.Path = orig
	res, err := e.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if *res.Text != "ABC" {
		t.Fatalf("got %q", *res.Text)
	}
	if data, _ := os.ReadFile(orig); string(data) != "original" {
		t.Fatalf("original file must stay untouched, got %q", data)
	}
}

// TestExternalSizeGuard: oversized buffers are refused before spawning.
func TestExternalSizeGuard(t *testing.T) {
	e := External{Command: script(t, `cat`)}
	big := strings.Repeat("x", maxExternalInput+1)
	_, err := e.Run(context.Background(), Request{Path: "/tmp/x", Lines: []string{big}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size-guard error, got %v", err)
	}
}

// TestExternalFromConfig: TOML-decoded maps ([]any values) parse into a spec.
func TestExternalFromConfig(t *testing.T) {
	e := ExternalFromConfig(map[string]any{
		"command":    "ruff",
		"args":       []any{"format", "-"},
		"range_args": []any{"format", "--range", "${START_LINE}-${END_LINE}"},
		"temp_file":  true,
		"install":    "pip install ruff",
	})
	if e.Command != "ruff" || len(e.Args) != 2 || len(e.RangeArgs) != 3 || !e.TempFile || e.Install != "pip install ruff" {
		t.Fatalf("parsed %+v", e)
	}
}

// TestExternalMissingBinaryHintOnce: an unavailable provider raises exactly
// one install hint per language+command and never resolves.
func TestExternalMissingBinaryHintOnce(t *testing.T) {
	ResetForTest()
	t.Cleanup(func() { ResetForTest(); SetNotifier(nil) })
	var hints []string
	SetNotifier(func(text string) { hints = append(hints, text) })
	e := External{Command: "definitely-not-installed-tool", Install: "brew install nope"}
	Register(e.Provider("python", TierExternal))

	for i := 0; i < 3; i++ {
		if _, ok := Resolve("python", "x.py"); ok {
			t.Fatal("missing binary must not resolve")
		}
	}
	if len(hints) != 1 {
		t.Fatalf("want exactly one hint, got %d: %v", len(hints), hints)
	}
	if !strings.Contains(hints[0], "definitely-not-installed-tool not found") || !strings.Contains(hints[0], "brew install nope") {
		t.Fatalf("hint must name the tool and install command, got %q", hints[0])
	}
}

// TestExternalEnabledHook: the config hook switches a language's external
// formatting off without touching the registry.
func TestExternalEnabledHook(t *testing.T) {
	ResetForTest()
	t.Cleanup(func() { ResetForTest(); SetExternalEnabled(nil) })
	e := External{Command: script(t, `cat`)}
	RegisterExternalDefault("python", e)

	if _, ok := Resolve("python", "x.py"); !ok {
		t.Fatal("enabled by default")
	}
	SetExternalEnabled(func(langID string) bool { return langID != "python" })
	if _, ok := Resolve("python", "x.py"); ok {
		t.Fatal("hook must disable the provider")
	}
}

// TestExternalDefaultRecorded: RegisterExternalDefault exposes the spec for
// the settings page.
func TestExternalDefaultRecorded(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	RegisterExternalDefault("shell", External{Command: "shfmt"})
	if e, ok := ExternalDefault("shell"); !ok || e.Command != "shfmt" {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
	langs := ExternalDefaultLangs()
	if len(langs) != 1 || langs[0] != "shell" {
		t.Fatalf("langs %v", langs)
	}
}
