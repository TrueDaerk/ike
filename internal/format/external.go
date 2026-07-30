package format

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// external.go is the generic external-command formatter (Roadmap 0470, #1402):
// most languages are formatted best by their ecosystem CLI (ruff, prettier,
// shfmt), and IKE only needs one way to invoke them. An External spec declares
// the command line with placeholders; Run feeds the buffer through
// stdin/stdout (or a temp file for tools that cannot read stdin) with the
// project root as cwd, a caller-supplied timeout context and a size guard.
// The original file is never written — output flows back through the registry
// as buffer text and lands as one undo-able edit like every other provider.

// Argument placeholders, expanded per invocation:
//
//	${FILE}            the file the tool formats: the buffer's real path in
//	                   stdin mode (context only — content still arrives on
//	                   stdin, e.g. prettier --stdin-filepath), the temp copy
//	                   in temp-file mode
//	${TAB_WIDTH}       Options.TabWidth
//	${INDENT_STYLE}    "space" or "tab" per Options.UseSpaces
//	${MAX_LINE_LENGTH} Options.MaxLineLength (0 when unlimited)
//	${START_LINE}      1-based first selected line (RangeArgs only)
//	${END_LINE}        1-based last selected line (RangeArgs only)

// External declares one external command formatter.
type External struct {
	// Command is the binary probed on PATH; empty disables the spec.
	Command string
	// Args is the whole-file argument list (placeholders above).
	Args []string
	// RangeArgs, when set, enables reformat-selection with these arguments
	// (typically Args plus a line-range flag). Absent means no range support
	// — the registry falls back per #1401.
	RangeArgs []string
	// TempFile switches to temp-file mode for tools that cannot read stdin:
	// the buffer is written to a temp copy (same extension, so the tool
	// detects the language), the command formats it in place via ${FILE},
	// and the copy is read back as the result.
	TempFile bool
	// Install is the one-line install hint shown when Command is missing
	// (the #1067 companion-hint pattern), e.g. "pip install ruff".
	Install string
}

// maxExternalInput guards runaway invocations: buffers beyond this size are
// not piped through an external tool (the large-file mode threshold is far
// below this anyway).
const maxExternalInput = 10 << 20

// stderrTruncate caps how much tool stderr a failure surfaces.
const stderrTruncate = 300

// lookPath probes PATH; a package var so tests substitute a fake.
var lookPath = exec.LookPath

// Ok reports whether the spec is usable right now: a command declared and on
// PATH.
func (e External) Ok() bool {
	if e.Command == "" {
		return false
	}
	_, err := lookPath(e.Command)
	return err == nil
}

// Run formats the whole request through the external command.
func (e External) Run(ctx context.Context, req Request) (Result, error) {
	return e.run(ctx, req, e.Args, nil)
}

// RunRange formats the [start, end) span using RangeArgs. The tool decides
// line granularity; start/end expand to 1-based inclusive line numbers.
func (e External) RunRange(ctx context.Context, req Request, start, end Pos) (Result, error) {
	if len(e.RangeArgs) == 0 {
		return Result{}, errors.New(e.Command + " has no range arguments")
	}
	lastLine := end.Line
	if end.Col == 0 && lastLine > start.Line {
		lastLine-- // end-exclusive line-wise selections stop before this line
	}
	return e.run(ctx, req, e.RangeArgs, map[string]string{
		"${START_LINE}": strconv.Itoa(start.Line + 1),
		"${END_LINE}":   strconv.Itoa(lastLine + 1),
	})
}

// run executes the command: content on stdin (or a temp copy), project root
// as cwd, output as the formatted text. Any failure leaves the buffer
// untouched by construction — an error simply carries the tool's (truncated)
// stderr up to the caller.
func (e External) run(ctx context.Context, req Request, args []string, extra map[string]string) (Result, error) {
	if e.Command == "" {
		return Result{}, errors.New("no formatter command configured")
	}
	content := strings.Join(req.Lines, "\n")
	if len(content) > maxExternalInput {
		return Result{}, errors.New("buffer too large for an external formatter")
	}

	file := req.Path
	var tempPath string
	if e.TempFile {
		tmp, err := os.CreateTemp("", "ike-format-*"+filepath.Ext(req.Path))
		if err != nil {
			return Result{}, err
		}
		tempPath = tmp.Name()
		defer os.Remove(tempPath)
		if _, err := tmp.WriteString(content); err != nil {
			tmp.Close()
			return Result{}, err
		}
		if err := tmp.Close(); err != nil {
			return Result{}, err
		}
		file = tempPath
	}

	repl := map[string]string{
		"${FILE}":            file,
		"${TAB_WIDTH}":       strconv.Itoa(req.Options.TabWidth),
		"${INDENT_STYLE}":    map[bool]string{true: "space", false: "tab"}[req.Options.UseSpaces],
		"${MAX_LINE_LENGTH}": strconv.Itoa(req.Options.MaxLineLength),
	}
	for k, v := range extra {
		repl[k] = v
	}
	expanded := make([]string, len(args))
	for i, a := range args {
		for k, v := range repl {
			a = strings.ReplaceAll(a, k, v)
		}
		expanded[i] = a
	}

	cmd := exec.CommandContext(ctx, e.Command, expanded...)
	// A killed tool's children can keep the output pipes open (sh + sleep);
	// WaitDelay force-closes them shortly after the context ends so a timeout
	// reports promptly instead of hanging on the pipe.
	cmd.WaitDelay = time.Second
	cmd.Dir = req.Root
	if cmd.Dir == "" && req.Path != "" {
		cmd.Dir = filepath.Dir(req.Path)
	}
	var stdout, stderr bytes.Buffer
	if !e.TempFile {
		cmd.Stdin = strings.NewReader(content)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, errors.New(e.Command + " timed out")
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > stderrTruncate {
			msg = msg[:stderrTruncate] + "…"
		}
		if msg == "" {
			msg = err.Error()
		}
		return Result{}, errors.New(e.Command + ": " + msg)
	}

	if e.TempFile {
		out, err := os.ReadFile(tempPath)
		if err != nil {
			return Result{}, err
		}
		return TextResult(string(out)), nil
	}
	return TextResult(stdout.String()), nil
}

// Provider wraps the spec as a registry entry for langID at tier. Available
// gates on the binary being on PATH (raising the one-time install hint when
// it is not) and on the external-enabled hook — `[format.<lang>] enabled =
// false` switches the language's external formatting off.
func (e External) Provider(langID string, tier Tier) Provider {
	p := Provider{
		Name:      e.Command,
		Languages: []string{langID},
		Tier:      tier,
		Available: func(path string) bool {
			if !externalEnabled(langID) {
				return false
			}
			if !e.Ok() {
				hintMissing(langID, e)
				return false
			}
			return true
		},
		Format: func(ctx context.Context, req Request) (Result, error) {
			return e.Run(ctx, req)
		},
	}
	if len(e.RangeArgs) > 0 {
		p.FormatRange = func(ctx context.Context, req Request, start, end Pos) (Result, error) {
			return e.RunRange(ctx, req, start, end)
		}
	}
	return p
}

// ExternalFromConfig parses a `[format.<languageID>]` config entry into a
// spec (mirroring lsp.Overlay's tolerance: wrong-typed fields are ignored).
// The `enabled` key is not part of the spec — callers gate on it separately.
func ExternalFromConfig(raw map[string]any) External {
	e := External{}
	if s, ok := raw["command"].(string); ok {
		e.Command = s
	}
	e.Args = configStrings(raw["args"])
	e.RangeArgs = configStrings(raw["range_args"])
	if b, ok := raw["temp_file"].(bool); ok {
		e.TempFile = b
	}
	if s, ok := raw["install"].(string); ok {
		e.Install = s
	}
	return e
}

// configStrings coerces a decoded TOML array into []string.
func configStrings(v any) []string {
	switch vv := v.(type) {
	case []string:
		return vv
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// HintMissing raises the one-time missing-binary install hint for a spec —
// exported for providers assembled outside this package (the config-override
// provider in plugins/format).
func HintMissing(langID string, e External) { hintMissing(langID, e) }

// RegisterExternalDefault registers a language plugin's default external
// formatter (TierExternal) and records the spec for the settings page
// (#1402). Call from init(), like Register.
func RegisterExternalDefault(langID string, e External) {
	extMu.Lock()
	externalDefaults[langID] = e
	extMu.Unlock()
	Register(e.Provider(langID, TierExternal))
}

// ExternalDefault returns the plugin-declared default external spec for a
// language, when one was registered.
func ExternalDefault(langID string) (External, bool) {
	extMu.Lock()
	defer extMu.Unlock()
	e, ok := externalDefaults[langID]
	return e, ok
}

// ExternalDefaultLangs lists the languages with a registered default external
// formatter (sorted registration-independent output is the caller's job).
func ExternalDefaultLangs() []string {
	extMu.Lock()
	defer extMu.Unlock()
	out := make([]string, 0, len(externalDefaults))
	for id := range externalDefaults {
		out = append(out, id)
	}
	return out
}

var (
	extMu            sync.Mutex
	externalDefaults = map[string]External{}
	notifier         func(text string)
	enabledHook      func(langID string) bool
	hinted           = map[string]bool{}
)

// SetNotifier installs the user-visible hint sink (the app wires it to a
// toast). Nil silences hints.
func SetNotifier(f func(text string)) {
	extMu.Lock()
	notifier = f
	extMu.Unlock()
}

// SetExternalEnabled installs the config hook answering whether external
// formatting is enabled for a language (`[format.<lang>] enabled`). Nil means
// always enabled.
func SetExternalEnabled(f func(langID string) bool) {
	extMu.Lock()
	enabledHook = f
	extMu.Unlock()
}

func externalEnabled(langID string) bool {
	extMu.Lock()
	f := enabledHook
	extMu.Unlock()
	return f == nil || f(langID)
}

// hintMissing raises the missing-binary install hint, once per language and
// command for the process lifetime (the #1067 companion-hint pattern).
func hintMissing(langID string, e External) {
	extMu.Lock()
	key := langID + "\x00" + e.Command
	f := notifier
	if f == nil || hinted[key] {
		extMu.Unlock()
		return
	}
	hinted[key] = true
	extMu.Unlock()
	text := e.Command + " not found — " + langID + " reformat falls back"
	if e.Install != "" {
		text += " (install: " + e.Install + ")"
	}
	f(text)
}
