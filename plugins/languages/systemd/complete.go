package langsystemd

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// complete.go is the systemd completion source (#3064), fed by the directive
// table: section names on a `[` line (only the sections valid for the file's
// unit type), the directives of the enclosing section at a key position,
// enumerated values, booleans, common targets and signals after `=`, and the
// `%` specifiers anywhere in a value. Where it has a table answer it claims
// the position (complete.PositionExclusiveSource), so buffer words never
// drown the entries; free-text values (`ExecStart=`, `Description=`) are left
// to the other sources.

// posKind is what the cursor position completes.
type posKind int

const (
	posNone      posKind = iota
	posSection           // inside a `[` header
	posDirective         // the key of a `Name=` line
	posValue             // after `=`
	posSpecifier         // right after a `%` in a value
)

// position is a classified cursor position: its kind, the directive name for
// a value, the text typed so far and the words already in a list value.
type position struct {
	kind      posKind
	directive string
	typed     string
	listed    []string
}

// classify decides what before — the line text left of the cursor —
// completes. It reads one line only; the enclosing section and the
// continuation state come from the caller.
func classify(before string) position {
	lead := strings.TrimLeft(before, " \t")
	switch {
	case strings.HasPrefix(lead, "#") || strings.HasPrefix(lead, ";"):
		return position{}
	case strings.HasPrefix(lead, "["):
		if strings.Contains(lead, "]") {
			return position{}
		}
		return position{kind: posSection, typed: lead}
	}
	key, value, ok := strings.Cut(lead, "=")
	if !ok {
		if strings.ContainsAny(lead, " \t") {
			return position{}
		}
		return position{kind: posDirective, typed: lead}
	}
	if typed, ok := specifierTyped(value); ok {
		return position{kind: posSpecifier, typed: typed}
	}
	words := strings.Fields(value)
	typed := ""
	if !strings.HasSuffix(value, " ") && !strings.HasSuffix(value, "\t") && len(words) > 0 {
		typed = words[len(words)-1]
		words = words[:len(words)-1]
	}
	return position{kind: posValue, directive: strings.TrimRight(key, " \t"), typed: typed, listed: words}
}

// specifierTyped reports whether value ends in a `%` specifier being typed —
// a lone `%`, or `%` plus one specifier letter — and returns that text. The
// second `%` of a `%%` run is the literal percent sign, not a specifier start.
func specifierTyped(value string) (string, bool) {
	i := strings.LastIndexByte(value, '%')
	if i < 0 || len(value)-i > 2 {
		return "", false
	}
	run := 0
	for j := i; j >= 0 && value[j] == '%'; j-- {
		run++
	}
	tail := value[i+1:]
	if run%2 == 0 || (tail != "" && !strings.Contains(specifiers, tail)) {
		return "", false
	}
	return value[i:], true
}

// sectionAt returns the name of the section header nearest above line li.
func sectionAt(lines []string, li int) string {
	for i := li - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			return t[1 : len(t)-1]
		}
	}
	return ""
}

// continued reports whether line li continues the value of the line above
// (a trailing `\`), skipping comment lines as systemd does.
func continued(lines []string, li int) bool {
	for i := li - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		return strings.HasSuffix(t, "\\")
	}
	return false
}

// unitType is the unit type a path holds: its own extension for a unit file,
// the drop-in directory's for `foo.service.d/override.conf`, "" when neither
// names a unit type (every section is offered then).
// A template suffix (`foo.service.j2`, `foo.service.in`) is looked through
// once.
func unitType(path string) string {
	if typ := ownUnitType(path); typ != "" {
		return typ
	}
	if ext := filepath.Ext(path); ext != "" {
		return ownUnitType(strings.TrimSuffix(path, ext))
	}
	return ""
}

// ownUnitType is unitType without the template-suffix fallback.
func ownUnitType(path string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if slices.Contains(UnitTypes, ext) {
		return ext
	}
	dir := filepath.Base(filepath.Dir(path))
	if base, ok := strings.CutSuffix(dir, ".d"); ok {
		if ext := strings.TrimPrefix(filepath.Ext(base), "."); slices.Contains(UnitTypes, ext) {
			return ext
		}
	}
	return ""
}

// validIn reports whether section s may appear in a unit of type typ.
func validIn(s Section, typ string) bool {
	return s.UnitTypes == nil || typ == "" || slices.Contains(s.UnitTypes, typ)
}

// commonTargets are the units offered for dependency and install directives.
var commonTargets = []string{
	"multi-user.target", "graphical.target", "default.target", "network.target",
	"network-online.target", "nss-lookup.target", "remote-fs.target", "local-fs.target",
	"sysinit.target", "basic.target", "sockets.target", "timers.target", "paths.target",
	"time-sync.target", "shutdown.target", "rescue.target", "emergency.target",
	"getty.target", "sound.target", "bluetooth.target", "printer.target",
}

// targetDirectives take the common targets as values.
var targetDirectives = map[string]bool{
	"WantedBy": true, "RequiredBy": true, "UpheldBy": true,
	"After": true, "Before": true, "Wants": true, "Requires": true, "Requisite": true,
	"BindsTo": true, "PartOf": true, "Upholds": true, "Conflicts": true,
}

// commonSignals are offered for signal directives.
var commonSignals = []string{
	"SIGTERM", "SIGKILL", "SIGINT", "SIGHUP", "SIGQUIT", "SIGUSR1", "SIGUSR2", "SIGCONT", "SIGSTOP", "SIGABRT",
}

// boolValues are the boolean spellings offered; systemd also accepts on/off
// and 1/0, which the highlighter recognises.
var boolValues = []string{"yes", "no", "true", "false"}

// specifierDocs is the meaning of every `%` specifier (systemd.unit(5)).
var specifierDocs = map[byte]string{
	'a': "Architecture", 'A': "OS image version", 'b': "Boot ID", 'B': "OS build ID",
	'C': "Cache directory root", 'd': "Credentials directory", 'D': "Shared data root",
	'E': "Configuration directory root", 'f': "Unescaped file name", 'g': "User group",
	'G': "User GID", 'h': "User home directory", 'H': "Host name", 'i': "Instance name",
	'I': "Unescaped instance name", 'j': "Final component of the prefix",
	'J': "Unescaped final component of the prefix", 'l': "Short host name",
	'L': "Log directory root", 'm': "Machine ID", 'M': "OS image identifier",
	'n': "Full unit name", 'N': "Unit name without the type suffix", 'o': "OS ID",
	'p': "Prefix name", 'P': "Unescaped prefix name", 'q': "Pretty host name",
	's': "User shell", 'S': "State directory root", 't': "Runtime directory root",
	'T': "Temporary directory", 'u': "User name", 'U': "User UID", 'v': "Kernel release",
	'V': "Directory for larger, persistent temporary files", 'w': "OS version ID",
	'W': "OS variant ID", 'y': "Path of the unit fragment", 'Y': "Directory of the unit fragment",
	'%': "Literal percent sign",
}

// completionCode is the language's lang.Language.CompletionCode hook: a value
// position this source answers from the table is code, whatever the value's
// string styling says, so `WantedBy=a.target ` and `%` open the popup. Header
// and key positions never carry a string capture and need no claim.
func completionCode(line string, col int) bool {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	p := classify(string(runes[:col]))
	switch p.kind {
	case posSpecifier:
		return true
	case posValue:
		return valueTable(p.directive) != nil
	}
	return false
}

// valueTable returns the values offered for directive name in any section —
// the line-only view completionCode has; the source narrows by section.
func valueTable(name string) []string {
	for _, s := range Sections {
		if dir, ok := Lookup(s.Name, name); ok {
			if v := valuesFor(dir); v != nil {
				return v
			}
		}
	}
	return nil
}

// valuesFor is the value list of a directive, nil for a free-form one.
func valuesFor(dir Directive) []string {
	switch {
	case dir.Kind == KindEnum:
		return dir.Values
	case dir.Kind == KindBool:
		return boolValues
	case dir.Kind == KindSignal:
		return commonSignals
	case dir.Kind == KindUnits && targetDirectives[dir.Name]:
		return commonTargets
	}
	return nil
}

// source implements complete.Source with the position-exclusive, context,
// trigger and observer extensions. It keeps the latest text of every systemd
// buffer, since the section and continuation state live on lines above.
type source struct {
	mu    sync.RWMutex
	lines map[string][]string
}

func newSource() *source { return &source{lines: map[string][]string{}} }

// Name implements complete.Source.
func (s *source) Name() string { return "systemd" }

// Priority implements complete.Source: above the symbol index, so a table
// entry wins a de-dup against a word of the same text.
func (s *source) Priority() int { return ilsp.PrioritySymbols + 5 }

// CompletesIn implements complete.ContextSource: the source decides per
// line, so a value styled as a string still completes on ctrl+space.
func (s *source) CompletesIn(ctx lang.CompletionContext) bool { return ctx != lang.CtxComment }

// TriggerChar claims the characters that open a table position: `[` starts a
// header, `=` a value, `%` a specifier and a space the next unit of a list.
func (s *source) TriggerChar(ch string) bool {
	return ch == "[" || ch == "=" || ch == "%" || ch == " "
}

// Observe implements complete.EventObserver.
func (s *source) Observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange || !isSystemd(ev.LangName()) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Large || ev.Text == "" {
		delete(s.lines, ev.BufKey())
		return
	}
	s.lines[ev.BufKey()] = strings.Split(ev.Text, "\n")
}

func isSystemd(name string) bool {
	l, ok := lang.ByPath(name)
	return ok && l.ID == "systemd"
}

// ExclusiveAt implements complete.PositionExclusiveSource: the position is
// claimed whenever the table has entries for it.
func (s *source) ExclusiveAt(req complete.Request) bool {
	return len(s.items(req)) > 0
}

// Complete implements complete.Source.
func (s *source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	return s.items(req), nil
}

// items computes the table entries for req's position.
func (s *source) items(req complete.Request) []ilsp.CompletionItem {
	if req.LangID() != "systemd" {
		return nil
	}
	s.mu.RLock()
	lines := s.lines[req.BufKey()]
	s.mu.RUnlock()
	if req.Line < 0 || req.Line >= len(lines) {
		return nil
	}
	runes := []rune(lines[req.Line])
	col := min(max(req.Col, 0), len(runes))
	before := string(runes[:col])
	if continued(lines, req.Line) {
		// A continuation line is value text, not a new key: only its
		// specifiers complete.
		if _, ok := specifierTyped(before); ok {
			return specifierItems()
		}
		return nil
	}
	p := classify(before)
	switch p.kind {
	case posSection:
		return sectionItems(unitType(req.LangName()), strings.HasPrefix(string(runes[col:]), "]"))
	case posDirective:
		return directiveItems(sectionAt(lines, req.Line))
	case posValue:
		return valueItems(sectionAt(lines, req.Line), p)
	case posSpecifier:
		return specifierItems()
	}
	return nil
}

// sectionItems offers the sections valid for the unit type, inserting the
// closing bracket unless the buffer already carries it (auto-pairs).
func sectionItems(typ string, closed bool) []ilsp.CompletionItem {
	var out []ilsp.CompletionItem
	for _, sec := range Sections {
		if !validIn(sec, typ) {
			continue
		}
		label := "[" + sec.Name + "]"
		insert := label
		if closed {
			insert = "[" + sec.Name
		}
		detail := "section"
		if sec.UnitTypes != nil {
			detail = "section (." + strings.Join(sec.UnitTypes, ", .") + ")"
		}
		out = append(out, ilsp.CompletionItem{
			Label: label, FilterText: label, InsertText: insert, Detail: detail, SortText: label,
		})
	}
	return out
}

// directiveItems offers the directives of section, each inserting `Name=`.
func directiveItems(section string) []ilsp.CompletionItem {
	sec, ok := LookupSection(section)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []ilsp.CompletionItem
	for _, dir := range sec.Directives {
		if seen[dir.Name] {
			continue
		}
		seen[dir.Name] = true
		out = append(out, ilsp.CompletionItem{
			Label: dir.Name, FilterText: dir.Name, InsertText: dir.Name + "=", Detail: dir.Doc, SortText: dir.Name,
		})
	}
	return out
}

// valueItems offers the values of the directive at p in section, skipping
// the words a list value already holds. The table order is kept: an enum
// lists its values the way the man page does, the targets the most common
// first.
func valueItems(section string, p position) []ilsp.CompletionItem {
	dir, ok := Lookup(section, p.directive)
	if !ok {
		return nil
	}
	var out []ilsp.CompletionItem
	for i, v := range valuesFor(dir) {
		if slices.Contains(p.listed, v) {
			continue
		}
		out = append(out, ilsp.CompletionItem{
			Label: v, FilterText: v, InsertText: v, Detail: dir.Name + " value", SortText: sortKey(i),
		})
	}
	return out
}

// specifierItems offers every `%` specifier with its meaning.
func specifierItems() []ilsp.CompletionItem {
	out := make([]ilsp.CompletionItem, 0, len(specifiers))
	for i := 0; i < len(specifiers); i++ {
		label := "%" + string(specifiers[i])
		out = append(out, ilsp.CompletionItem{
			Label: label, FilterText: label, InsertText: label, Detail: specifierDocs[specifiers[i]], SortText: sortKey(i),
		})
	}
	return out
}

// sortKey keeps the table order through the popup's sortText ranking.
func sortKey(i int) string {
	return string(rune('a'+i/26)) + string(rune('a'+i%26))
}

func init() { complete.RegisterSource(newSource()) }
