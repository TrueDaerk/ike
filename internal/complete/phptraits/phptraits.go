// Package phptraits is the PHP trait-member completion source (Epic 0520,
// #2668). Intelephense resolves `$this` inside a trait body as the trait
// itself, so `$this->` there offers only the trait's own members — every
// member living on the trait's consumers or on the sibling traits those
// consumers use is missing, although the code legitimately calls it.
//
// The declaration index (internal/phpindex, #2667) knows those members; this
// source turns them into completion items. It answers only inside a trait
// body and only right after `$this->`, `self::` or `static::` (optionally
// followed by a partial identifier) — everywhere else it returns nothing, so
// an ordinary PHP position keeps exactly the popup it had.
//
// Its priority sits below the LSP server's: a member the server already
// knows keeps the server's item when the editor merges the batches, and only
// the members the server cannot see are added.
//
// Inert when php.trait_index is off and in a build without the PHP grammar
// (no cgo): the index holds nothing, so every query answers nothing.
package phptraits

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/phpindex"
)

// maxResults bounds one answer; a wide consumer scope (a trait used by a
// dozen fat models) must not flood the popup.
const maxResults = 200

// detailSep separates the member's signature from its declaring type in the
// item detail: `abc(int $times = 1): string  —  class B`.
const detailSep = "  —  "

// access is the member-access syntax the cursor sits behind.
type access int

const (
	accessNone   access = iota
	accessArrow         // `$this->`
	accessStatic        // `self::` / `static::`
)

var (
	// arrowRe matches `$this->` plus an optional partial member name at the
	// end of the text before the cursor. PHP allows whitespace around the
	// operator, so a formatted `$this ->foo` still completes.
	arrowRe = regexp.MustCompile(`\$this\s*->\s*([A-Za-z_][A-Za-z0-9_]*)?$`)
	// staticRe matches `self::` / `static::` (both keywords are
	// case-insensitive in PHP) plus an optional partial member name, which
	// may carry the `$` of a static property. The leading group keeps
	// `myself::` from matching.
	staticRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_$\\])(?i:self|static)\s*::\s*(\$?[A-Za-z_][A-Za-z0-9_]*|\$)?$`)
)

// Source is the trait-member completion source. It implements
// complete.Source, complete.ContextSource, complete.TriggerSource and
// complete.EventObserver.
type Source struct {
	idx *phpindex.Index

	mu sync.RWMutex
	// texts is the latest text of every observed buffer, keyed by the
	// request's BufKey: the source needs the line before the cursor, which
	// the Request itself does not carry. A large-file change drops the entry.
	texts map[string]string
	// answered is called with the item count of every non-empty answer
	// (telemetry op php.trait.complete); nil until SetTelemetry.
	answered func(int)
}

// New returns the source over the PHP declaration index. A nil index makes
// the source permanently inert.
func New(idx *phpindex.Index) *Source {
	return &Source{idx: idx, texts: map[string]string{}}
}

// SetTelemetry installs the callback for a non-empty answer's item count
// (#2668). It is called off the UI goroutine, from Complete. Pass nil to
// remove it.
func (s *Source) SetTelemetry(fn func(items int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answered = fn
}

// Name implements complete.Source.
func (s *Source) Name() string { return "phptraits" }

// Priority implements complete.Source: below the server, so a member it
// already knows keeps the server's item; above the symbol index, because
// these members are in scope here while a project-wide symbol merely shares
// the name.
func (s *Source) Priority() int { return ilsp.PriorityPHPTraits }

// CompletesIn implements complete.ContextSource: code and declaration
// positions only, which the engine grants without asking. A member access
// inside a comment, a string literal or an import line is text, not code —
// the source stays silent there.
func (s *Source) CompletesIn(lang.CompletionContext) bool { return false }

// TriggerChar implements complete.TriggerSource: the `>` closing a `->` and
// the `:` closing a `::` are the positions this source exists for, so the
// popup opens on the operator instead of waiting for the first letter. Both
// characters are otherwise reserved for the LSP bridge; a buffer of another
// language is filtered out by Complete.
func (s *Source) TriggerChar(ch string) bool { return ch == ">" || ch == ":" }

// Observe implements complete.EventObserver: a PHP buffer's changes stash
// the text the line before the cursor is read from. Cheap — nothing is parsed
// here; the index does its own extraction on its own debounce. Buffers of
// other languages are not kept: the index only ever indexes PHP files, so
// their positions could never resolve to a trait body anyway.
func (s *Source) Observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange || !isPHP(ev.LangName()) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Large {
		delete(s.texts, ev.BufKey())
		return
	}
	s.texts[ev.BufKey()] = ev.Text
}

// isPHP reports whether the name resolves to the PHP language — a file path
// or the synthetic name of a buffer switched to PHP (#2048).
func isPHP(name string) bool {
	l, ok := lang.ByPath(name)
	return ok && l.ID == "php"
}

// Complete implements complete.Source: the members of the enclosing trait's
// consumer scope the access syntax before the cursor can reach. The checks
// run cheapest first — language, then the text before the cursor, then the
// setting — so a non-PHP buffer and an ordinary PHP position never reach the
// index.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	if s.idx == nil || req.LangID() != "php" {
		return nil, nil
	}
	acc, prefix := accessAt(s.lineAt(req.BufKey(), req.Line), req.Col)
	if acc == accessNone {
		return nil, nil
	}
	if !s.idx.Options().Enabled {
		return nil, nil // php.trait_index = false
	}
	scope, ok := s.idx.ScopeAt(req.Path, phpindex.Pos{Line: req.Line, Col: req.Col})
	if !ok || !scope.IsTrait() {
		return nil, nil // only a trait body has a consumer scope
	}
	items := s.items(scope.FQN, acc, prefix)
	if len(items) > 0 {
		s.report(len(items))
	}
	return items, nil
}

// items renders the visible members the access reaches, in the index's
// precedence order (nearest declaration first), prefix-filtered.
func (s *Source) items(traitFQN string, acc access, prefix string) []ilsp.CompletionItem {
	members := s.idx.VisibleMembers(traitFQN)
	if len(members) == 0 {
		return nil
	}
	kinds := s.declKinds(members)
	var items []ilsp.CompletionItem
	for _, m := range members {
		if !offers(acc, m) {
			continue
		}
		text := itemText(acc, m)
		if !matchesPrefix(text, prefix) {
			continue
		}
		items = append(items, ilsp.CompletionItem{
			Label:      text,
			InsertText: text,
			FilterText: text,
			Detail:     detail(m, kinds[m.Declaring]),
			Doc:        m.Doc,
			Kind:       itemKind(m.Kind),
			// The index orders by precedence — a consumer's own member
			// before the trait's, the trait's before a parent's — and the
			// batch position keeps that order as the popup's tie-break.
			SortText: fmt.Sprintf("%04d", len(items)),
		})
		if len(items) >= maxResults {
			break
		}
	}
	return items
}

// declKinds maps the declaring FQNs of the members to their declaration kind
// ("class", "trait", …), resolved once per distinct type.
func (s *Source) declKinds(members []phpindex.Member) map[string]string {
	out := map[string]string{}
	for _, m := range members {
		if m.Declaring == "" {
			continue
		}
		if _, done := out[m.Declaring]; done {
			continue
		}
		out[m.Declaring] = ""
		for _, d := range s.idx.DeclarationsNamed(m.Declaring) {
			if d.FQN == m.Declaring {
				out[m.Declaring] = d.Kind.String()
				break
			}
		}
	}
	return out
}

// report hands a non-empty answer's item count to the telemetry callback.
func (s *Source) report(n int) {
	s.mu.RLock()
	fn := s.answered
	s.mu.RUnlock()
	if fn != nil {
		fn(n)
	}
}

// lineAt returns the observed buffer's line, "" when the buffer is unknown
// (nothing was typed in it yet) or the line is out of range.
func (s *Source) lineAt(key string, line int) string {
	s.mu.RLock()
	text := s.texts[key]
	s.mu.RUnlock()
	if text == "" || line < 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if line >= len(lines) {
		return ""
	}
	return lines[line]
}

// accessAt classifies the text before col: the member-access syntax the
// cursor sits behind and the partial member name already typed ("" right
// after the operator). accessNone means the position is none of the source's
// business.
func accessAt(line string, col int) (access, string) {
	if col < 0 {
		return accessNone, ""
	}
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	head := string(runes[:col])
	if m := arrowRe.FindStringSubmatch(head); m != nil {
		return accessArrow, m[1]
	}
	if m := staticRe.FindStringSubmatch(head); m != nil {
		return accessStatic, m[1]
	}
	return accessNone, ""
}

// offers is the single rule for which member an access syntax reaches — the
// one function the table test pins:
//
//   - `$this->` reaches every method, static ones included (PHP allows
//     `$this->staticMethod()`), and the non-static properties; a class
//     constant and an enum case need `::`.
//   - `self::` / `static::` reach the static members plus the constants and
//     enum cases, which have no instance form; a non-static method or
//     property is not offered there.
func offers(a access, m phpindex.Member) bool {
	switch a {
	case accessArrow:
		switch m.Kind {
		case phpindex.MemberMethod:
			return true
		case phpindex.MemberProperty:
			return !m.Static
		}
	case accessStatic:
		switch m.Kind {
		case phpindex.MemberConst, phpindex.MemberCase:
			return true
		case phpindex.MemberMethod, phpindex.MemberProperty:
			return m.Static
		}
	}
	return false
}

// itemText renders one member's label — which is also its insert and filter
// text: a method as `name(` so accepting it opens the call, a property bare
// after `->` and with its `$` after `::` (a static property is written
// `self::$x`), a constant and an enum case by name.
func itemText(a access, m phpindex.Member) string {
	name := m.Name
	switch m.Kind {
	case phpindex.MemberMethod:
		return name + "("
	case phpindex.MemberProperty:
		if a == accessStatic {
			if !strings.HasPrefix(name, "$") {
				name = "$" + name
			}
			return name
		}
		return strings.TrimPrefix(name, "$")
	}
	return name
}

// detail is the item's one-line description: the member's signature without
// its modifiers, followed by the type declaring it — the whole point of the
// source being that this type is not the trait the cursor sits in.
func detail(m phpindex.Member, declKind string) string {
	var sb strings.Builder
	switch m.Kind {
	case phpindex.MemberMethod:
		sb.WriteString(m.Name)
		if m.Params != "" {
			sb.WriteString(m.Params)
		} else {
			sb.WriteString("()")
		}
		if m.ReturnType != "" {
			sb.WriteString(": ")
			sb.WriteString(m.ReturnType)
		}
	case phpindex.MemberProperty:
		if m.Type != "" {
			sb.WriteString(m.Type)
			sb.WriteByte(' ')
		}
		sb.WriteString(m.Name)
	default:
		sb.WriteString(m.Name)
	}
	if m.Declaring != "" {
		sb.WriteString(detailSep)
		if declKind != "" {
			sb.WriteString(declKind)
			sb.WriteByte(' ')
		}
		sb.WriteString(shortName(m.Declaring))
	}
	return sb.String()
}

// itemKind maps a member kind to its LSP completion item kind.
func itemKind(k phpindex.MemberKind) int {
	switch k {
	case phpindex.MemberProperty:
		return protocol.KindProperty
	case phpindex.MemberConst:
		return protocol.KindConstant
	case phpindex.MemberCase:
		return protocol.KindEnumMember
	}
	return protocol.KindMethod
}

// matchesPrefix reports whether the member's name starts with what the user
// typed. PHP matches method names case-insensitively itself, and a
// case-exact filter would hide `fromC` behind a typed `fromc`.
func matchesPrefix(name, prefix string) bool {
	if prefix == "" || prefix == "$" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
}

// shortName is the last segment of a qualified PHP name.
func shortName(fqn string) string {
	if i := strings.LastIndex(fqn, `\`); i >= 0 {
		return fqn[i+1:]
	}
	return fqn
}
