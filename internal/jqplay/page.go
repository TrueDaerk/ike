package jqplay

// page.go is the progressive form of an evaluation (#2796). Run collects
// everything up to the caps before the playground sees a single value; for
// `.items[]` over a big file that meant a wait, then a wall of output ending
// in `(stopped at 500)` with the tail unreachable. Start instead yields the
// output in *pages* — PageOutputs values or PageBytes of text, whichever
// fills first — and keeps the iterator suspended on a goroutine between them,
// so the first page is on screen at once and the rest arrives as the reader
// scrolls toward it. The total budget (MaxOutputs / MaxResultBytes) is much
// larger than the old hard cap and only shows as `(stopped at N)` once it is
// really exhausted.
//
// Three shapes of cancellation end the producer, all without a leak:
//
//   - the host cancels ctx (a newer program, the playground closing or
//     parking) — the goroutine stops whether it is computing or suspended;
//   - a page takes longer than EvalTimeout to compute — the *page* fails
//     with the timeout message, exactly as a whole run used to, but time
//     spent suspended does not count: a reader who scrolls back to a paged
//     result an hour later gets the next page, not a timeout;
//   - the stream ends — exhausted, a runtime error, or the total budget.
//
// The xmq dialect has no iterator to suspend: its engine is the external
// binary, whose stdout is one text. That text is paged by lines instead, so
// the result buffer grows the same way and the host has one code path.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/itchyny/gojq"
)

// PageOutputs is how many values one page collects at most.
const PageOutputs = 200

// PageBytes is how much rendered text one page collects at most: a page of
// 200 megabyte-sized values would be no page at all.
const PageBytes = 64 << 10

// evalTimeout is EvalTimeout as the producer applies it per page; a variable
// so a test can shorten it.
var evalTimeout = EvalTimeout

// Page is one slice of a progressive evaluation: the values it produced, and
// how the stream stands after it.
type Page struct {
	// Outputs are the page's values, rendered as the dialect writes them; for
	// xmq they are line chunks of the one CLI output.
	Outputs []string
	// Err is the compile error, or the runtime error that stopped the stream
	// after the page's values (Result.Err's contract).
	Err string
	// Truncated reports that the stream stopped at the total budget.
	Truncated bool
	// Done reports that this is the last page: the stream is exhausted,
	// failed, or capped. A page is Done rather than followed by an empty
	// one, so a result of exactly one page never claims to have more.
	Done bool
	// Note is a remark about how the outputs were rendered (#2798): the
	// round-trip's reason for falling back to the plain form, "" when it
	// applied or was off.
	Note string
	// values are the decoded values behind Outputs (nil for xmq).
	values []any
}

// Producer is a suspended evaluation: Next pulls one page at a time, Stop
// ends it. Its goroutine exits on Stop, on the parent context's end, or with
// the last page.
type Producer struct {
	first  Result // the result's shape: dialect, extension, options
	ask    chan struct{}
	pages  chan Page
	done   chan struct{}
	cancel context.CancelFunc
	over   bool
}

// Start compiles program and begins running it over in on a goroutine that
// stays suspended until Next asks for a page. ctx is the host's cancellation
// (a newer program, the playground closing); the per-page timeout is the
// producer's own. Nothing is computed before the first Next.
func Start(ctx context.Context, program string, in *Input, opts Options) *Producer {
	ctx, cancel := context.WithCancel(ctx)
	p := &Producer{
		ask:    make(chan struct{}),
		pages:  make(chan Page),
		done:   make(chan struct{}),
		cancel: cancel,
	}
	if in.Dialect() == DialectXMQ {
		opts = Options{Vars: opts.Vars}
	} else if opts.Slurp && in != nil && len(in.values) > 0 {
		in = in.slurped()
	}
	p.first = Result{dialect: in.Dialect(), opts: opts}
	if in.Dialect() == DialectXMQ {
		// The output language is per command (#2414); the shape names it
		// before the binary runs, so the result path is right from page one.
		if args, err := ShellWords(strings.TrimSpace(program)); err == nil {
			p.first.ext = xmqOutputExt(args)
		}
	}
	go p.serve(ctx, program, in, opts)
	return p
}

// serve is the producer goroutine: wait to be asked, compute a page under
// the per-page deadline, hand it over, repeat until the stream ends or ctx
// does. Every exit closes done, so a test can prove there is no leak.
func (p *Producer) serve(ctx context.Context, program string, in *Input, opts Options) {
	defer close(p.done)
	defer p.cancel()
	pctx := newPageContext(ctx)
	defer pctx.end()
	var src pageSource
	for {
		select {
		case <-p.ask:
		case <-ctx.Done():
			return
		}
		if src == nil {
			src = openPageSource(program, in, opts)
		}
		pctx.resume(evalTimeout)
		pg := src.page(pctx)
		pctx.pause()
		select {
		case p.pages <- pg:
		case <-ctx.Done():
			return
		}
		if pg.Done {
			return
		}
	}
}

// Next asks for the next page and waits for it. ok is false once the
// producer is over — its last page was delivered, it was stopped, or ctx
// ended while waiting — and the page is then empty.
func (p *Producer) Next(ctx context.Context) (pg Page, ok bool) {
	if p.over {
		return Page{}, false
	}
	select {
	case p.ask <- struct{}{}:
	case <-p.done:
		p.over = true
		return Page{}, false
	case <-ctx.Done():
		return Page{}, false
	}
	select {
	case pg = <-p.pages:
	case <-p.done:
		p.over = true
		return Page{}, false
	case <-ctx.Done():
		return Page{}, false
	}
	if pg.Done {
		p.over = true
	}
	return pg, true
}

// Stop ends the producer: the goroutine exits, computing or suspended.
func (p *Producer) Stop() { p.cancel() }

// Wait blocks until the producer's goroutine has exited, or d passed; it
// reports which. Tests use it to prove a cancelled producer does not leak.
func (p *Producer) Wait(d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

// Shape is the empty Result the pages extend: the dialect, extension and
// options the run was started with, with no outputs yet.
func (p *Producer) Shape() Result { return p.first }

// Append extends the result by one page: its outputs and values are added,
// its error and truncation become the result's, and Partial records whether
// more pages are pending after it.
func (r *Result) Append(pg Page) {
	r.Outputs = append(r.Outputs, pg.Outputs...)
	r.values = append(r.values, pg.values...)
	if pg.Err != "" {
		r.Err = pg.Err
	}
	if pg.Truncated {
		r.Truncated = true
	}
	if pg.Note != "" {
		r.note = pg.Note
	}
	r.partial = !pg.Done
}

// Note is the remark the run left about its rendering (#2798): why the
// round-trip fell back to the plain form for some output, "" otherwise.
func (r Result) Note() string { return r.note }

// Partial reports that the result is the pages loaded so far and the
// producer holds more (#2796) — what the info row's `+` says.
func (r Result) Partial() bool { return r.partial }

// Count is the number of values the result holds as the reader counts them:
// the outputs for jq and yq, one for an xmq run that wrote anything — its
// outputs are line chunks of the one CLI result, not values.
func (r Result) Count() int {
	if r.dialect == DialectXMQ {
		if len(r.Outputs) > 0 {
			return 1
		}
		return 0
	}
	return len(r.Outputs)
}

// FoldsSince returns the folds of the outputs appended from index n on,
// with lines counted in the whole result, so a host extends its fold list
// instead of re-scanning the pages it already has. An xmq result's chunks
// split one document, whose folds can cross a chunk boundary: for it the
// whole result is re-scanned and every fold returned (the caller replaces).
func (r Result) FoldsSince(n int) []Fold {
	if r.opts.Raw || r.opts.Compact || n >= len(r.Outputs) {
		return nil
	}
	if r.dialect == DialectXMQ || n <= 0 {
		return r.Folds()
	}
	line := r.lineOfOutput(n)
	tail := Result{dialect: r.dialect, opts: r.opts, ext: r.ext, Outputs: r.Outputs[n:]}
	folds := tail.Folds()
	for i := range folds {
		folds[i].HeaderLine += line
		folds[i].EndLine += line
	}
	return folds
}

// lineOfOutput is the 0-based line in Text() output n starts on.
func (r Result) lineOfOutput(n int) int {
	line := 0
	sepLines := strings.Count(r.separator(), "\n")
	for _, o := range r.Outputs[:n] {
		line += strings.Count(o, "\n") + sepLines
	}
	return line
}

// TextSince is the text the outputs from index n on add to the result: the
// separator that joins them to the previous output, then the outputs joined
// as Text joins them. "" when n is past the end.
func (r Result) TextSince(n int) string {
	if n >= len(r.Outputs) {
		return ""
	}
	tail := strings.Join(r.Outputs[n:], r.separator())
	if n > 0 {
		return r.separator() + tail
	}
	return tail
}

// pageSource yields the pages of one evaluation.
type pageSource interface {
	page(ctx context.Context) Page
}

// openPageSource compiles the program into the dialect's page source. A
// compile failure is a source whose only page carries the error, so the
// producer has one shape for every outcome.
func openPageSource(program string, in *Input, opts Options) pageSource {
	program = strings.TrimSpace(program)
	vars, err := ParseVars(opts.Vars)
	if err != nil {
		return failedSource{err.Error()}
	}
	if in.Dialect() == DialectXMQ {
		return &xmqPages{program: program, in: in, vars: vars}
	}
	if program == "" {
		program = in.Dialect().identity()
	}
	if in == nil || len(in.values) == 0 {
		return failedSource{in.Dialect().emptyInput()}
	}
	query, err := gojq.Parse(program)
	if err != nil {
		return failedSource{err.Error()}
	}
	code, err := compileWith(query, vars)
	if err != nil {
		return failedSource{err.Error()}
	}
	return &stream{in: in, code: code, args: vars.Values(), opts: opts, rt: newRoundTripper(in, opts)}
}

// failedSource is a compile failure as a source: one page, the error.
type failedSource struct{ err string }

func (f failedSource) page(context.Context) Page { return Page{Err: f.err, Done: true} }

// stream walks the outputs of a compiled program over every input value,
// one at a time, counting them against the total budget. Both Run and the
// producer read it — the difference is only how many outputs a call
// collects before it hands back.
type stream struct {
	in   *Input
	code *gojq.Code
	args []any
	opts Options

	idx  int       // the input value the next iterator opens on
	iter gojq.Iter // nil before the first value and after the last
	// total / bytes are the outputs collected so far and their rendered
	// size, checked against MaxOutputs / MaxResultBytes.
	total, bytes int
	// head is an output pulled ahead of its page: the one that proved the
	// stream had more after a page filled, with its rendered text.
	head     any
	headText string
	hasHead  bool
	// lastIdx is the input value the last pulled output came from.
	lastIdx int
	// rt is the round-trip state (#2798), nil when the toggle is off.
	rt *roundTripper
}

// next yields the next output with its rendered text, or the runtime error
// that ended the stream, or exhaustion. The round-trip (#2798) renders by
// patching the source document; everything else is the dialect's encoding
// under the toggles.
func (s *stream) next(ctx context.Context) (out any, text string, errMsg string, ok bool) {
	if s.rt != nil {
		return s.rt.next(ctx, s)
	}
	out, errMsg, ok = s.pull(ctx)
	if !ok {
		return nil, "", errMsg, false
	}
	return out, s.in.dialect.encodeWith(out, s.opts), "", true
}

// pull yields the next raw output, or the runtime error that ended the
// stream (a clean `halt` counts as the end), or exhaustion.
func (s *stream) pull(ctx context.Context) (out any, errMsg string, ok bool) {
	for {
		if s.iter == nil {
			if s.idx >= len(s.in.values) {
				return nil, "", false
			}
			s.iter = s.code.RunWithContext(ctx, s.in.values[s.idx], s.args...)
			s.lastIdx = s.idx
			s.idx++
		}
		out, ok := s.iter.Next()
		if !ok {
			s.iter = nil
			if ctx.Err() != nil {
				s.idx = len(s.in.values)
				return nil, contextError(ctx), false
			}
			continue
		}
		if err, ok := out.(error); ok {
			s.iter, s.idx = nil, len(s.in.values)
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				return nil, "", false // `halt`: a clean stop, not a diagnostic
			}
			return nil, runtimeError(ctx, err), false
		}
		return out, "", true
	}
}

// collect appends outputs to pg until the page holds maxOutputs values or
// maxBytes of text, the total budget is spent, or the stream ends. A page
// that fills pulls one output more to learn whether it is the last, and
// keeps that output as the next page's head; a budget hit marks the page
// Truncated and Done, the way a whole run used to be capped.
func (s *stream) collect(ctx context.Context, pg *Page, maxOutputs, maxBytes int) {
	if s.rt != nil {
		defer func() { pg.Note = s.rt.note }()
	}
	n, size := 0, 0
	for {
		var out any
		var text string
		if s.hasHead {
			out, text, s.hasHead = s.head, s.headText, false
		} else {
			var errMsg string
			var ok bool
			out, text, errMsg, ok = s.next(ctx)
			if !ok {
				pg.Err, pg.Done = errMsg, true
				return
			}
		}
		if s.total >= MaxOutputs || s.bytes >= MaxResultBytes {
			pg.Truncated, pg.Done = true, true
			return
		}
		pg.Outputs = append(pg.Outputs, text)
		pg.values = append(pg.values, out)
		s.total++
		s.bytes += len(text)
		n++
		size += len(text)
		if n >= maxOutputs || size >= maxBytes {
			out, text, errMsg, ok := s.next(ctx)
			if !ok {
				pg.Err, pg.Done = errMsg, true
				return
			}
			if s.total >= MaxOutputs || s.bytes >= MaxResultBytes {
				pg.Truncated, pg.Done = true, true
				return
			}
			s.head, s.headText, s.hasHead = out, text, true
			return
		}
	}
}

// page is one producer page: PageOutputs / PageBytes of the stream.
func (s *stream) page(ctx context.Context) Page {
	var pg Page
	s.collect(ctx, &pg, PageOutputs, PageBytes)
	return pg
}

// xmqPages pages the xmq CLI's one output by lines: the binary runs on the
// first page (there is no iterator to suspend) and the text is handed out
// PageBytes at a time, cut at line ends so every chunk joins back with the
// dialect's separator into the text the CLI wrote.
type xmqPages struct {
	program string
	in      *Input
	vars    Vars

	ran       bool
	lines     []string
	err       string
	truncated bool
}

func (x *xmqPages) page(ctx context.Context) Page {
	if !x.ran {
		x.ran = true
		res := runXMQ(ctx, x.program, x.in, x.vars)
		x.err, x.truncated = res.Err, res.Truncated
		if len(res.Outputs) > 0 {
			x.lines = strings.Split(res.Outputs[0], "\n")
		}
	}
	var pg Page
	chunk, rest := cutLines(x.lines, PageBytes)
	x.lines = rest
	if len(chunk) > 0 {
		pg.Outputs = []string{strings.Join(chunk, "\n")}
	}
	if len(rest) == 0 {
		pg.Err, pg.Truncated, pg.Done = x.err, x.truncated, true
	}
	return pg
}

// cutLines takes lines off the head while their joined size stays within
// maxBytes; at least one line goes even when it alone is longer.
func cutLines(lines []string, maxBytes int) (chunk, rest []string) {
	size, n := 0, 0
	for n < len(lines) {
		l := len(lines[n]) + 1
		if n > 0 && size+l > maxBytes {
			break
		}
		size += l
		n++
	}
	return lines[:n], lines[n:]
}

// pageContext is a context whose deadline runs only while a page is being
// computed. The parent's cancellation passes through at once; the deadline
// is a timer resume starts and pause stops, so a producer suspended between
// pages is never timed out for waiting on the reader.
type pageContext struct {
	parent context.Context
	done   chan struct{}
	mu     sync.Mutex
	err    error
	timer  *time.Timer
	once   sync.Once
}

func newPageContext(parent context.Context) *pageContext {
	c := &pageContext{parent: parent, done: make(chan struct{})}
	go func() {
		select {
		case <-parent.Done():
			c.fail(parent.Err())
		case <-c.done:
		}
	}()
	return c
}

func (c *pageContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *pageContext) Done() <-chan struct{}       { return c.done }
func (c *pageContext) Value(key any) any           { return c.parent.Value(key) }

func (c *pageContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// fail ends the context with err, once.
func (c *pageContext) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
	})
}

// resume starts the page deadline.
func (c *pageContext) resume(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timer = time.AfterFunc(d, func() { c.fail(context.DeadlineExceeded) })
}

// pause stops the page deadline; a timer that already fired has ended the
// context and stays ended.
func (c *pageContext) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

// end releases the watcher goroutine when the producer exits normally.
func (c *pageContext) end() {
	c.pause()
	c.fail(context.Canceled)
}
