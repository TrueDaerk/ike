package jqplay

// xmq.go is the third playground engine (#2414): where jq and yq run gojq in
// process, xmq runs the external `xmq` binary (github.com/libxmq/xmq) over an
// XML or HTML buffer. The query line holds an xmq *command line* — `select
// //item[@id='3']`, `to-json`, `delete //script` — which is split into
// arguments with shell-word rules and handed to the CLI with the buffer on
// stdin. There is no Go port of xmq, so the process boundary is the engine;
// everything around it — debounce, history, caps, the result buffer — is the
// shared playground machinery.
//
// Three consequences of the boundary:
//
//   - **The input is not decoded here.** The binary parses the document
//     itself, so parseXMQ keeps the raw text and only rejects emptiness. A
//     malformed document is the CLI's diagnostic on the info row, phrased by
//     the tool that actually read it.
//   - **The output language is per command.** `to-json` writes JSON,
//     `to-html` HTML, `to-xml` XML; without a `to-*` command xmq prints its
//     own notation. The run records the extension on the Result, which is
//     what resolves the result buffer's highlighting.
//   - **The binary may be absent.** The host checks before opening (an
//     actionable dialog with the install hint); a binary that disappears
//     mid-session surfaces as an error line carrying the same hint.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// XMQInstallHint is the one-line remedy for a missing binary, shown by the
// host's dialog and by the run error alike.
const XMQInstallHint = "brew install xmq"

// xmqWaitDelay bounds how long a run waits for the killed process's pipes
// after its context ended. Without it a binary that spawned a child holding
// stdout open would park the evaluation goroutine forever.
const xmqWaitDelay = time.Second

// xmqPathMu guards xmqPath: the settings UI writes it on the event loop while
// evaluation goroutines read it.
var (
	xmqPathMu sync.Mutex
	xmqPath   string
)

// SetXMQPath installs the configured xmq binary path (playground.xmq.path).
// Empty means "resolve `xmq` on PATH".
func SetXMQPath(p string) {
	xmqPathMu.Lock()
	defer xmqPathMu.Unlock()
	xmqPath = strings.TrimSpace(p)
}

// XMQBinary is the command the engine will run: the configured path, or the
// bare name for PATH resolution.
func XMQBinary() string {
	xmqPathMu.Lock()
	defer xmqPathMu.Unlock()
	if xmqPath != "" {
		return xmqPath
	}
	return "xmq"
}

// LookupXMQ resolves the xmq binary the way a run would, so the host can
// answer "is the playground available?" before mounting it (#2414).
func LookupXMQ() (string, error) { return exec.LookPath(XMQBinary()) }

// parseXMQ is the xmq half of Dialect.Parse: no decoding happens on this
// side — the binary reads the document itself — so the "parse" keeps the raw
// text and rejects only emptiness. values carries one placeholder so the
// shared "did the input hold anything" checks count it as a document.
func parseXMQ(text string) (*Input, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New(DialectXMQ.emptyInput())
	}
	return &Input{values: []any{nil}, size: len(text), dialect: DialectXMQ, raw: text}, nil
}

// runXMQ is the xmq engine: split the command line into arguments, run the
// binary with the snapshot on stdin, and shape stdout/stderr into the Result
// the playground renders. ctx carries the host's EvalTimeout and is cancelled
// when a newer keystroke supersedes the run; CommandContext kills the process
// then.
func runXMQ(ctx context.Context, program string, in *Input) Result {
	args, err := ShellWords(program)
	if err != nil {
		return Result{Err: err.Error(), dialect: DialectXMQ}
	}
	res := Result{dialect: DialectXMQ, ext: xmqOutputExt(args)}
	cmd := exec.CommandContext(ctx, XMQBinary(), args...)
	cmd.Stdin = strings.NewReader(in.raw)
	cmd.WaitDelay = xmqWaitDelay
	var stdout capWriter
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		res.Err = contextError(ctx)
		return res
	}
	if runErr != nil {
		res.Err = xmqRunError(runErr, &stderr)
		return res
	}
	out := strings.TrimRight(stdout.String(), "\n")
	if out != "" {
		res.Outputs = []string{out}
	}
	res.Truncated = stdout.truncated
	return res
}

// xmqRunError phrases a failed run: the CLI's own stderr where it said
// anything — the parse error, the bad command — and the exec error otherwise.
// A binary that vanished mid-session gets the same install hint the opening
// dialog shows.
func xmqRunError(err error, stderr *bytes.Buffer) string {
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return firstLine(msg)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%s: not found — install it (%s) or set playground.xmq.path", XMQBinary(), XMQInstallHint)
	}
	return err.Error()
}

// firstLine keeps a multi-line diagnostic to its head — the info row is one
// row, and xmq's first line is where it names the problem.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// capWriter collects stdout up to MaxResultBytes — the same cap the gojq
// dialects apply — and drops the rest, so a command that multiplies the
// document cannot blow up the result buffer. The overflow is recorded, never
// silently eaten: Result.Truncated is what the info row warns with.
type capWriter struct {
	buf       bytes.Buffer
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := MaxResultBytes - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	// The full length is acknowledged so the process never sees a write
	// error — it finishes normally and only the tail is dropped.
	return len(p), nil
}

func (w *capWriter) String() string { return w.buf.String() }

// xmqOutputExt names the language a command line's stdout is written in, by
// its last output-naming command: `to-json` writes JSON, `to-html` and
// `render-html` HTML, `to-xml` XML. Everything else — a bare `select`, a
// `delete`, an empty line — prints xmq's own notation, which the dialect's
// default extension stands for.
func xmqOutputExt(args []string) string {
	ext := ""
	for _, a := range args {
		switch a {
		case "to-json":
			ext = "json"
		case "to-html", "render-html":
			ext = "html"
		case "to-xml":
			ext = "xml"
		case "to-text", "to-clines", "statistics", "tokenize":
			ext = "txt"
		case "to-xmq", "to-htmq", "print", "render-terminal", "render-tex", "render-raw":
			ext = ""
		}
	}
	return ext
}

// ShellWords splits an xmq command line into arguments with the shell's word
// rules: whitespace separates, single quotes take everything literally,
// double quotes group with backslash escapes, a bare backslash escapes the
// next rune. It is what lets `select //item[@name='a b']` arrive at the CLI
// as one argument. An unterminated quote is an error the query line shows
// rather than a guess.
func ShellWords(line string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		started bool
	)
	r := []rune(line)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == ' ' || c == '\t':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		case c == '\'':
			started = true
			j := i + 1
			for j < len(r) && r[j] != '\'' {
				cur.WriteRune(r[j])
				j++
			}
			if j >= len(r) {
				return nil, errors.New("unterminated ' quote")
			}
			i = j
		case c == '"':
			started = true
			j := i + 1
			for j < len(r) && r[j] != '"' {
				if r[j] == '\\' && j+1 < len(r) {
					j++
				}
				cur.WriteRune(r[j])
				j++
			}
			if j >= len(r) {
				return nil, errors.New(`unterminated " quote`)
			}
			i = j
		case c == '\\':
			if i+1 >= len(r) {
				return nil, errors.New("trailing backslash")
			}
			started = true
			i++
			cur.WriteRune(r[i])
		default:
			started = true
			cur.WriteRune(c)
		}
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}

// XMQCommand is one entry of the command list the completion popup and the
// cheatsheet offer for the xmq query line (#2414). The list is authored — the
// CLI has no machine-readable `builtins` — and covers the commands a query
// line plausibly types; flags and rarities stay with `xmq --help`.
type XMQCommand struct {
	Name string
	Doc  string
}

// completeXMQ is the xmq half of Complete (#2414): the candidates are the
// CLI's commands, offered on the word under the cursor. A word mid-typing
// filters the list by prefix; the manual request (ctrl+space) opens the full
// list on an empty word. Inside a quoted argument nothing is offered — an
// XPath is not a command.
func completeXMQ(program string, pos int, manual bool) (items []Candidate, start int) {
	r := []rune(program)
	if pos < 0 {
		pos = 0
	}
	if pos > len(r) {
		pos = len(r)
	}
	start = pos
	for start > 0 && xmqWordRune(r[start-1]) {
		start--
	}
	partial := string(r[start:pos])
	if partial == "" && !manual {
		return nil, 0
	}
	if start > 0 && r[start-1] != ' ' && r[start-1] != '\t' {
		return nil, 0 // mid-argument (a path, a quoted string, an option value)
	}
	for _, c := range XMQCommands() {
		if !strings.HasPrefix(c.Name, partial) {
			continue
		}
		items = append(items, Candidate{Label: c.Name, Insert: c.Name, Detail: "command", Doc: c.Doc})
	}
	return items, start
}

// xmqWordRune reports whether c can be part of an xmq command word.
func xmqWordRune(c rune) bool {
	return c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// xmqCheatsheet is the authored sheet behind Cheatsheet(DialectXMQ): the
// command reference plus a few complete example lines. No builtin section —
// the CLI has no machine-readable builtin list to mirror.
func xmqCheatsheet() []CheatEntry {
	out := []CheatEntry{
		{Kind: CheatExample, Title: "keep the nodes matching an XPath", Program: "select //user[@id='2']", Doc: "select keeps only the matched nodes (and their ancestors)"},
		{Kind: CheatExample, Title: "print the document as JSON", Program: "to-json", Doc: "the result buffer highlights per output language"},
		{Kind: CheatExample, Title: "print the document as XML", Program: "to-xml", Doc: "without a to-* command xmq prints its own compact notation"},
		{Kind: CheatExample, Title: "drop nodes and print the rest", Program: "delete //password to-xml", Doc: "commands chain left to right"},
		{Kind: CheatExample, Title: "extract the text content", Program: "select //name to-text", Doc: "to-text prints the text nodes only"},
	}
	for _, c := range XMQCommands() {
		out = append(out, CheatEntry{Kind: CheatBuiltin, Title: c.Name, Program: c.Name, Doc: c.Doc})
	}
	return out
}

// cheatSampleXML is the xmq sheet's example document — the users/meta shape
// the jq and yq samples describe, spelled as XML so the example programs
// above have something to select from.
const cheatSampleXML = `<report>
  <meta page="1" total="2"/>
  <users>
    <user id="1" active="true"><name>Ada</name></user>
    <user id="2" active="false"><name>Grace</name></user>
  </users>
</report>`

// XMQCommands is the authored command list, in reading order.
func XMQCommands() []XMQCommand {
	return []XMQCommand{
		{"select", "keep only the nodes matching an XPath: select //item[@id='3']"},
		{"delete", "remove the nodes matching an XPath: delete //script"},
		{"replace", "replace matched nodes with an entity or file: replace //name --with-text-file=f"},
		{"substitute", "substitute entities with content"},
		{"transform", "apply an XSLT transform: transform style.xslt"},
		{"for-each", "run a shell command per matched node: for-each //item --shell='echo $x'"},
		{"sort", "sort the children of the matched nodes"},
		{"add-root", "wrap the document in a new root element: add-root items"},
		{"statistics", "print statistics about the document"},
		{"to-xmq", "print as compact xmq (the default view)"},
		{"to-xml", "print as XML"},
		{"to-html", "print as HTML"},
		{"to-json", "print as JSON"},
		{"to-text", "print the text nodes only"},
		{"page", "page the output"},
		{"validate", "validate against an XSD: validate --xsd=schema.xsd"},
		{"tokenize", "print the token stream"},
	}
}
