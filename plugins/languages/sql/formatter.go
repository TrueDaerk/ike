package langsql

import (
	"errors"
	"strings"
)

// formatter.go is the built-in SQL formatter (Roadmap 0470, #1403). SQL is
// the language with no dependable external option — sqls must be installed
// and reformats at statement level at best — so IKE ships its own. The
// formatter is a pure-Go lexer + clause-layout printer (it compiles and runs
// without CGo); the Tree-sitter parse, where available, acts as the validity
// gate (parsecheck_cgo.go): SQL that fails to lex or parse is left untouched,
// never mangled.
//
// Layout contract (#1403): SELECT / FROM / JOIN / ON / WHERE / GROUP BY /
// HAVING / ORDER BY / LIMIT each start a line; select lists, SET assignments
// and CREATE TABLE column lists break one item per line, indented one level;
// AND/OR chains under WHERE/ON/HAVING break and indent; subquery parentheses
// open a block indented one level with the closing parenthesis on its own
// line. Keywords are re-cased per configuration (upper by default);
// identifiers, strings and quoted identifiers are never touched. Comments
// stay with their statement — trailing end-of-line comments remain trailing,
// standalone comments keep their own line. Statements separate with one
// blank line, `;` kept. The output is idempotent: formatting depends only on
// the token stream, which formatting preserves.

// sqlCase is the keyword casing mode ([format.sql] keywords).
type sqlCase int

const (
	caseUpper sqlCase = iota
	caseLower
	casePreserve
)

// sqlOptions carries the effective settings for one run.
type sqlOptions struct {
	Indent string // one indent unit, from editorconfig/settings
	Case   sqlCase
}

// --- lexer ------------------------------------------------------------------

type tkind int

const (
	tWord tkind = iota
	tNumber
	tString
	tQuoted // quoted identifier: "x", `x`, [x]
	tOp
	tLParen
	tRParen
	tComma
	tSemi
	tLineComment
	tBlockComment
)

type tok struct {
	kind tkind
	text string
	nl   int // newlines in the whitespace preceding this token
	line int // 0-based line the token starts on (range formatting, #1403)
}

// errMalformed is the "leave the buffer alone" verdict.
var errMalformed = errors.New("malformed SQL — buffer left unchanged")

// lexSQL tokenizes src; ok is false on an unterminated string, quoted
// identifier or block comment.
func lexSQL(src string) ([]tok, bool) {
	var toks []tok
	nl := 0
	line := 0
	i := 0
	n := len(src)
	push := func(kind tkind, text string) {
		toks = append(toks, tok{kind: kind, text: text, nl: nl, line: line})
		nl = 0
		line += strings.Count(text, "\n")
	}
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			nl++
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '-' && i+1 < n && src[i+1] == '-':
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			push(tLineComment, strings.TrimRight(src[i:j], " \t"))
			i = j
		case c == '/' && i+1 < n && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, false
			}
			end := i + 2 + j + 2
			push(tBlockComment, src[i:end])
			i = end
		case c == '\'':
			j := i + 1
			for {
				if j >= n {
					return nil, false
				}
				if src[j] == '\'' {
					if j+1 < n && src[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			push(tString, src[i:j+1])
			i = j + 1
		case c == '"' || c == '`':
			close := c
			j := i + 1
			for {
				if j >= n {
					return nil, false
				}
				if src[j] == close {
					if j+1 < n && src[j+1] == close {
						j += 2
						continue
					}
					break
				}
				j++
			}
			push(tQuoted, src[i:j+1])
			i = j + 1
		case c == '[':
			j := strings.IndexByte(src[i:], ']')
			if j < 0 {
				return nil, false
			}
			push(tQuoted, src[i:i+j+1])
			i += j + 1
		case c == '(':
			push(tLParen, "(")
			i++
		case c == ')':
			push(tRParen, ")")
			i++
		case c == ',':
			push(tComma, ",")
			i++
		case c == ';':
			push(tSemi, ";")
			i++
		case isWordStart(c):
			j := i + 1
			for j < n && isWordPart(src[j]) {
				j++
			}
			push(tWord, src[i:j])
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && (isWordPart(src[j]) || src[j] == '.') {
				j++
			}
			push(tNumber, src[i:j])
			i = j
		default:
			// operator: greedy two-char forms first
			if i+1 < n {
				two := src[i : i+2]
				switch two {
				case "<=", ">=", "<>", "!=", "||", "::", ":=":
					push(tOp, two)
					i += 2
					continue
				}
			}
			push(tOp, string(c))
			i++
		}
	}
	return toks, true
}

func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// --- keyword tables ---------------------------------------------------------

// sqlKeywords are the words the casing mode applies to. Deliberately the
// structural + common vocabulary — a word not listed here is treated as an
// identifier and never re-cased.
var sqlKeywords = wordSet(`select from where and or not null as on join left right full inner outer cross group by order having limit offset union all distinct insert into values update set delete create table drop alter add primary key foreign references default unique check constraint index view if exists between in is like ilike case when then else end asc desc with returning using natural except intersect cascade restrict temporary temp column to grant revoke begin commit rollback transaction explain analyze vacuum truncate rename replace trigger function procedure sequence schema database int integer bigint smallint serial bigserial varchar char text boolean bool date time timestamp timestamptz interval numeric decimal real float double precision json jsonb uuid bytea array current_timestamp current_date current_time coalesce nullif cast count sum avg min max`)

// clauseStarters begin a new line at the statement (or subquery) indent.
var clauseStarters = wordSet(`select from where having limit offset values returning union intersect except on set`)

// joinPrefixes may begin a JOIN sequence ("LEFT OUTER JOIN").
var joinPrefixes = wordSet(`left right full inner outer cross natural join`)

// listClauses are the clauses whose comma-separated items break one per line.
var listClauses = wordSet(`select set returning with`)

// boolClauses are the clauses whose AND/OR chains break and indent.
var boolClauses = wordSet(`where on having when`)

// parenSpaceWords keep a space before a following parenthesis (clause-like
// keywords); any other word glues to it (function calls, type parameters).
var parenSpaceWords = wordSet(`in on values as and or not where from select join exists between like ilike is then else when union all distinct set references check default using key into table add delete update insert with except intersect having group by order`)

func wordSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// --- printer ----------------------------------------------------------------

// frame is one parenthesis (or statement) context.
type frame struct {
	indent int
	block  bool   // block frame: content on own lines one level deeper
	inline bool   // inline parenthesis: no clause/boolean breaks inside
	clause string // active clause keyword (lower-case) inside this frame
	ddl    bool   // CREATE TABLE column list: commas break
}

type sqlPrinter struct {
	opts   sqlOptions
	toks   []tok
	pos    int
	lines  []string
	cur    strings.Builder
	curInd int
	frames []frame
	stmt   string // first word of the statement, lower-case
	lastK  tkind
	lastT  string // last emitted text (lower-cased for words)
	atBOL  bool
	// joinRun: the previous word was part of an already-broken JOIN sequence,
	// so its continuation words (OUTER, JOIN) stay on the same line.
	joinRun bool
	// prevPrevGlue: the token before the last +/- made it unary (trackUnary).
	prevPrevGlue bool
	// forceSpace forces one space before the next emitted token.
	forceSpace bool
}

// formatSQL formats the whole text. The caller has already gated on lexing
// and (where available) the Tree-sitter parse.
func formatSQL(text string, opts sqlOptions) (string, error) {
	toks, ok := lexSQL(text)
	if !ok || unbalanced(toks) {
		return "", errMalformed
	}
	if len(toks) == 0 {
		return text, nil
	}
	if bad, checked := parseHasErrors(text); checked && bad {
		return "", errMalformed
	}
	p := &sqlPrinter{opts: opts, toks: toks, atBOL: true}
	p.frames = []frame{{indent: 0}}
	p.run()
	out := strings.Join(p.lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

// unbalanced reports mismatched parentheses — the lexer-only malformedness
// check that always runs, also in CGo-less builds.
func unbalanced(toks []tok) bool {
	depth := 0
	for _, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
			if depth < 0 {
				return true
			}
		}
	}
	return depth != 0
}

func (p *sqlPrinter) top() *frame { return &p.frames[len(p.frames)-1] }

// flushLine finishes the current line.
func (p *sqlPrinter) flushLine() {
	if p.atBOL {
		return
	}
	p.lines = append(p.lines, strings.TrimRight(p.cur.String(), " \t"))
	p.cur.Reset()
	p.atBOL = true
}

// newline starts a fresh line at the given indent.
func (p *sqlPrinter) newline(indent int) {
	p.flushLine()
	p.curInd = indent
}

// blankLine inserts one empty separator line.
func (p *sqlPrinter) blankLine() {
	p.flushLine()
	p.lines = append(p.lines, "")
}

// emit appends text to the current line, handling indent and spacing.
func (p *sqlPrinter) emit(text string, kind tkind) {
	if p.atBOL {
		p.cur.WriteString(strings.Repeat(p.opts.Indent, p.curInd))
		p.atBOL = false
	} else if p.forceSpace || p.spaceBefore(text, kind) {
		p.cur.WriteString(" ")
	}
	p.forceSpace = false
	p.cur.WriteString(text)
	p.lastK = kind
	p.lastT = text
	if kind == tWord {
		p.lastT = strings.ToLower(text)
	}
}

// spaceBefore decides the gap between the previous emitted token and text.
func (p *sqlPrinter) spaceBefore(text string, kind tkind) bool {
	switch kind {
	case tComma, tSemi, tRParen:
		return false
	}
	if text == "." || text == "::" {
		return false
	}
	switch p.lastK {
	case tLParen:
		return false
	case tOp:
		if p.lastT == "." || p.lastT == "::" {
			return false
		}
		// unary +/- glue to their operand
		if (p.lastT == "-" || p.lastT == "+") && p.unaryMinus() {
			return false
		}
	}
	if kind == tLParen {
		// clause-like keywords keep a space before their parenthesis
		// (IN (…), VALUES (…)); function calls and type parameters glue:
		// count(*), varchar(80)
		if p.lastK == tWord && parenSpaceWords[p.lastT] {
			return true
		}
		if p.lastK == tWord || p.lastK == tQuoted {
			return false
		}
	}
	return true
}

// unaryMinus reports whether the previous +/- was unary (its own predecessor
// makes an operand impossible). Tracked at emit time via prevPrev.
func (p *sqlPrinter) unaryMinus() bool { return p.prevPrevGlue }

// prevPrevGlue is maintained by run(): true when the token before the last
// +/- was an operator, comma, opening parenthesis or clause keyword.
// (Field on the printer to keep emit signature small.)

// run walks the token stream.
func (p *sqlPrinter) run() {
	for p.pos < len(p.toks) {
		t := p.toks[p.pos]
		switch t.kind {
		case tLineComment, tBlockComment:
			p.comment(t)
			p.pos++
			continue
		case tSemi:
			p.emit(";", tSemi)
			p.pos++
			// a trailing comment on the terminator line stays with it
			for p.pos < len(p.toks) && p.toks[p.pos].nl == 0 &&
				(p.toks[p.pos].kind == tLineComment || p.toks[p.pos].kind == tBlockComment) {
				p.forceSpace = true
				p.emit(p.toks[p.pos].text, p.toks[p.pos].kind)
				p.pos++
			}
			p.endStatement()
			continue
		}
		if p.atStatementStart() {
			p.beginStatement(t)
		}
		switch t.kind {
		case tWord:
			p.word(t)
		case tLParen:
			p.lparen()
		case tRParen:
			p.rparen()
		case tComma:
			p.comma()
		default:
			p.trackUnary(t)
			p.emit(t.text, t.kind)
			p.pos++
		}
	}
	p.flushLine()
	// trim trailing blank lines
	for len(p.lines) > 0 && p.lines[len(p.lines)-1] == "" {
		p.lines = p.lines[:len(p.lines)-1]
	}
}

// prevPrevGlue tracking for the unary +/- rule.
func (p *sqlPrinter) trackUnary(t tok) {
	if t.kind == tOp && (t.text == "-" || t.text == "+") {
		switch p.lastK {
		case tOp, tComma, tLParen:
			p.prevPrevGlue = true
		case tWord:
			p.prevPrevGlue = sqlKeywords[p.lastT]
		default:
			p.prevPrevGlue = p.atBOL
		}
	}
}

// atStatementStart reports a fresh statement boundary.
func (p *sqlPrinter) atStatementStart() bool { return p.stmt == "" && len(p.frames) == 1 }

// beginStatement opens a statement: separator blank line and base indent.
func (p *sqlPrinter) beginStatement(t tok) {
	if (len(p.lines) > 0 || !p.atBOL) && !p.lastLineIsComment() {
		p.blankLine()
	}
	p.newline(0)
	if t.kind == tWord {
		p.stmt = strings.ToLower(t.text)
	} else {
		p.stmt = "?"
	}
	p.top().clause = p.stmt
}

// lastLineIsComment reports whether the previous emitted line is a comment —
// a standalone comment right above a statement belongs to it, with no blank
// separator in between.
func (p *sqlPrinter) lastLineIsComment() bool {
	if !p.atBOL || len(p.lines) == 0 {
		return false
	}
	l := strings.TrimSpace(p.lines[len(p.lines)-1])
	return strings.HasPrefix(l, "--") || strings.HasPrefix(l, "/*") || strings.HasSuffix(l, "*/")
}

// endStatement closes the current one.
func (p *sqlPrinter) endStatement() {
	p.flushLine()
	p.stmt = ""
	p.frames = p.frames[:1]
	p.top().clause = ""
}

// word handles keywords, clause breaks and casing.
func (p *sqlPrinter) word(t tok) {
	lower := strings.ToLower(t.text)
	f := p.top()

	if !f.inline && p.isClauseStart(lower) {
		if p.clauseName(lower) == "join" && p.joinRun {
			// LEFT already broke the line; OUTER/JOIN continue it
		} else {
			if !p.atBOL || p.cur.Len() > 0 {
				p.newline(f.indent)
			}
			f.clause = p.clauseName(lower)
		}
	} else if !f.inline && (lower == "and" || lower == "or") && boolClauses[f.clause] {
		p.newline(f.indent + 1)
	}
	p.joinRun = joinPrefixes[lower]

	p.emit(p.caseWord(t.text), tWord)
	p.pos++
}


// isClauseStart reports whether lower begins a clause line here: plain clause
// starters, GROUP/ORDER + BY, and join sequences.
func (p *sqlPrinter) isClauseStart(lower string) bool {
	if clauseStarters[lower] {
		// SET only in UPDATE statements; ON only after a join has been seen
		if lower == "set" && p.stmt != "update" {
			return false
		}
		// DELETE FROM stays one line — FROM is the statement here, not a clause
		if lower == "from" && p.stmt == "delete" && p.top().clause == "delete" {
			return false
		}
		if lower == "values" && p.stmt != "insert" {
			return false
		}
		return true
	}
	if lower == "group" || lower == "order" {
		return p.peekWordIs(1, "by")
	}
	if joinPrefixes[lower] {
		// begins a join sequence when a JOIN follows within two words
		if lower == "join" {
			return true
		}
		return p.peekWordIs(1, "join") || (joinPrefixes[p.peekWord(1)] && p.peekWordIs(2, "join"))
	}
	if lower == "with" && p.stmt == "with" {
		return true
	}
	return false
}

// clauseName normalizes the clause key for the break tables.
func (p *sqlPrinter) clauseName(lower string) string {
	if joinPrefixes[lower] {
		return "join"
	}
	if lower == "group" || lower == "order" {
		return lower + " by"
	}
	return lower
}

// peekWord returns the pos+n-th non-comment token's lower-case word text.
func (p *sqlPrinter) peekWord(n int) string {
	seen := 0
	for i := p.pos + 1; i < len(p.toks); i++ {
		if p.toks[i].kind == tLineComment || p.toks[i].kind == tBlockComment {
			continue
		}
		seen++
		if seen == n {
			if p.toks[i].kind == tWord {
				return strings.ToLower(p.toks[i].text)
			}
			return ""
		}
	}
	return ""
}

func (p *sqlPrinter) peekWordIs(n int, want string) bool { return p.peekWord(n) == want }

// listHasComma reports whether the just-started clause carries a top-level
// comma before the next clause boundary (single-item lists stay inline).
func (p *sqlPrinter) listHasComma() bool {
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		t := p.toks[i]
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			if depth == 0 {
				return false
			}
			depth--
		case tComma:
			if depth == 0 {
				return true
			}
		case tSemi:
			return false
		case tWord:
			if depth == 0 && p.isClauseStartAt(i) {
				return false
			}
		}
	}
	return false
}

// isClauseStartAt is isClauseStart evaluated at an arbitrary index.
func (p *sqlPrinter) isClauseStartAt(i int) bool {
	save := p.pos
	p.pos = i
	lower := strings.ToLower(p.toks[i].text)
	ok := p.isClauseStart(lower)
	p.pos = save
	return ok
}

// caseWord applies the configured keyword casing.
func (p *sqlPrinter) caseWord(w string) string {
	if !sqlKeywords[strings.ToLower(w)] {
		return w
	}
	switch p.opts.Case {
	case caseUpper:
		return strings.ToUpper(w)
	case caseLower:
		return strings.ToLower(w)
	}
	return w
}

// lparen opens a frame: block for subqueries and DDL column lists, inline
// otherwise.
func (p *sqlPrinter) lparen() {
	f := p.top()
	next := p.peekWord(1)
	sub := next == "select" || next == "with" || next == "values"
	ddl := p.stmt == "create" && len(p.frames) == 1
	if sub || ddl {
		if p.lastK == tWord || p.lastK == tQuoted {
			p.forceSpace = true
		}
		p.emit("(", tLParen)
		// nest from the line the parenthesis opened on, not the frame base:
		// a scalar subquery inside an indented select list nests one deeper
		nf := frame{indent: p.curInd + 1, block: true, ddl: ddl}
		p.frames = append(p.frames, nf)
		p.newline(nf.indent)
	} else {
		if p.stmt == "insert" && len(p.frames) == 1 && (p.lastK == tWord || p.lastK == tQuoted) {
			p.forceSpace = true // INSERT INTO t (a, b): the column list reads better spaced
		}
		p.emit("(", tLParen)
		p.frames = append(p.frames, frame{indent: f.indent, inline: true, clause: f.clause})
	}
	p.pos++
}

// rparen closes the innermost frame.
func (p *sqlPrinter) rparen() {
	if len(p.frames) > 1 {
		f := *p.top()
		p.frames = p.frames[:len(p.frames)-1]
		if f.block {
			p.newline(f.indent - 1)
		}
	}
	p.emit(")", tRParen)
	p.pos++
}

// comma breaks in list contexts, stays inline otherwise.
func (p *sqlPrinter) comma() {
	p.emit(",", tComma)
	p.pos++
	f := p.top()
	switch {
	case f.ddl:
		p.newline(f.indent)
	case (len(p.frames) == 1 || f.block) && f.clause == "with":
		// the next CTE aligns with the first one, at the WITH line's level
		p.newline(f.indent)
	case (len(p.frames) == 1 || f.block) && listClauses[f.clause]:
		p.newline(f.indent + 1)
	}
}

// comment emits a comment: trailing stays on the line, standalone keeps its
// own line; block comments pass through verbatim.
func (p *sqlPrinter) comment(t tok) {
	standalone := t.nl > 0 || (p.atBOL && len(p.lines) == 0 && p.cur.Len() == 0)
	indent := p.top().indent
	if p.stmt == "" {
		indent = 0
		// a comment opening the next statement separates from the previous
		// one like the statement itself would
		if standalone && len(p.lines) > 0 && !p.lastLineIsComment() {
			p.blankLine()
		}
	}
	if t.kind == tBlockComment && strings.Contains(t.text, "\n") {
		p.flushLine()
		for _, l := range strings.Split(t.text, "\n") {
			p.lines = append(p.lines, strings.TrimRight(l, " \t"))
		}
		p.atBOL = true
		p.curInd = indent
		return
	}
	if standalone {
		p.newline(indent)
		p.emit(t.text, t.kind)
		p.newline(indent)
		return
	}
	p.emit(t.text, t.kind)
	if t.kind == tLineComment {
		p.newline(indent)
	}
}
