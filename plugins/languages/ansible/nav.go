package langansible

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// nav.go wires the inventory index (#922) into navigation and completion:
// a LocalDefinition provider resolves `hosts:` / `delegate_to:` values and
// `groups[...]` references to their inventory definition (the LSP bridge
// consults it before the server, so it works without ansible installed), and
// a completion source offers the project's group/host names in those value
// positions.

// hostsPriority ranks inventory names above the symbol index and below the
// server: on a hosts: line they are the answer, not an echo.
const hostsPriority = 60

var (
	hostsKeyRe   = regexp.MustCompile(`^\s*-?\s*(hosts|delegate_to)\s*:\s*`)
	groupsExprRe = regexp.MustCompile(`groups\[['"]([A-Za-z0-9_.-]+)['"]\]|groups\.([A-Za-z0-9_]+)`)
	nameRunes    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-"
)

// hostsValueStart returns the rune column where the value of a hosts:/
// delegate_to: mapping starts, ok false when the line is no such mapping.
func hostsValueStart(lineText string) (int, bool) {
	loc := hostsKeyRe.FindStringIndex(lineText)
	if loc == nil {
		return 0, false
	}
	return len([]rune(lineText[:loc[1]])), true
}

// hostsRefAt extracts the host/group name under col: the pattern token on a
// hosts:/delegate_to: value (patterns separate on , : & !), or the name
// inside a groups['...']/groups.name expression anywhere on the line.
func hostsRefAt(lineText string, col int) (string, bool) {
	runes := []rune(lineText)
	if start, ok := hostsValueStart(lineText); ok && col >= start {
		s, e := min(col, len(runes)), min(col, len(runes))
		isName := func(r rune) bool { return strings.ContainsRune(nameRunes, r) }
		for s > start && isName(runes[s-1]) {
			s--
		}
		for e < len(runes) && isName(runes[e]) {
			e++
		}
		if name := strings.Trim(string(runes[s:e]), "."); name != "" {
			return name, true
		}
		return "", false
	}
	// groups['web'] / groups.web references, byte-indexed then converted.
	for _, loc := range groupsExprRe.FindAllStringSubmatchIndex(lineText, -1) {
		for _, g := range []int{2, 4} { // capture group byte ranges
			if loc[g] < 0 {
				continue
			}
			s := len([]rune(lineText[:loc[g]]))
			e := len([]rune(lineText[:loc[g+1]]))
			if col >= s && col <= e {
				return lineText[loc[g]:loc[g+1]], true
			}
		}
	}
	return "", false
}

// --- shared index cache ----------------------------------------------------

var invCache = struct {
	sync.Mutex
	root  string
	built time.Time
	ix    *InventoryIndex
}{}

// indexFor returns the (briefly cached) inventory index for the project
// containing dir. The 2s TTL keeps a completion keystroke burst on one scan
// while still noticing edited inventory files almost immediately.
func indexFor(dir string) *InventoryIndex {
	root := ProjectRoot(dir)
	invCache.Lock()
	defer invCache.Unlock()
	if invCache.ix != nil && invCache.root == root && time.Since(invCache.built) < 2*time.Second {
		return invCache.ix
	}
	invCache.ix = BuildInventoryIndex(root)
	invCache.root = root
	invCache.built = time.Now()
	return invCache.ix
}

// --- goto definition -------------------------------------------------------

// hostsDefinition is the LocalDefinition provider (#922): on an ansible file
// it resolves the name under the cursor through the inventory index. It only
// claims on a positive hit, so everything else still reaches the server.
func hostsDefinition(path string, line, col int, lineText string) (ilsp.DefinitionMsg, bool) {
	if l, ok := lang.ByPath(path); !ok || l.ID != "ansible" {
		return ilsp.DefinitionMsg{}, false
	}
	name, ok := hostsRefAt(lineText, col)
	if !ok {
		return ilsp.DefinitionMsg{}, false
	}
	d, ok := indexFor(filepath.Dir(path)).Lookup(name)
	if !ok {
		return ilsp.DefinitionMsg{}, false
	}
	return ilsp.DefinitionMsg{Path: d.Path, Line: d.Line, Col: 0}, true
}

// --- completion source -----------------------------------------------------

// hostsSource offers the inventory's group/host names in hosts:/delegate_to:
// value positions. It observes editor events for buffer text (like the emmet
// source) so the key check sees unsaved edits.
type hostsSource struct {
	mu    sync.Mutex
	lines map[string][]string
}

func newHostsSource() *hostsSource { return &hostsSource{lines: map[string][]string{}} }

func (s *hostsSource) Name() string  { return "ansible-hosts" }
func (s *hostsSource) Priority() int { return hostsPriority }

// Observe implements complete.EventObserver: keep the latest buffer text.
func (s *hostsSource) Observe(ev host.EditorEvent) {
	if ev.Text == "" || ev.Large {
		return
	}
	if l, ok := lang.ByPath(ev.Path); !ok || l.ID != "ansible" {
		return
	}
	s.mu.Lock()
	s.lines[ev.Path] = strings.Split(ev.Text, "\n")
	s.mu.Unlock()
}

func (s *hostsSource) lineAt(path string, line int) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, ok := s.lines[path]
	if !ok || line < 0 || line >= len(lines) {
		return "", false
	}
	return lines[line], true
}

// Complete implements complete.Source.
func (s *hostsSource) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	if l, ok := lang.ByPath(req.Path); !ok || l.ID != "ansible" {
		return nil, nil
	}
	lineText, ok := s.lineAt(req.Path, req.Line)
	if !ok {
		return nil, nil
	}
	start, ok := hostsValueStart(lineText)
	if !ok || req.Col < start {
		return nil, nil
	}
	// The typed pattern-token prefix filters the names.
	runes := []rune(lineText)
	ts := req.Col
	for ts > start && ts-1 < len(runes) && strings.ContainsRune(nameRunes, runes[ts-1]) {
		ts--
	}
	prefix := ""
	if ts < len(runes) && ts < req.Col {
		prefix = strings.ToLower(string(runes[ts:min(req.Col, len(runes))]))
	}

	var items []ilsp.CompletionItem
	for _, d := range indexFor(filepath.Dir(req.Path)).Defs() {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(d.Name), prefix) {
			continue
		}
		sort := "1" + d.Name
		if d.Kind == "group" {
			sort = "0" + d.Name // groups first: they are what hosts: usually names
		}
		items = append(items, ilsp.CompletionItem{
			Label:      d.Name,
			Detail:     "inventory " + d.Kind,
			InsertText: d.Name,
			SortText:   sort,
		})
	}
	return items, nil
}

func init() {
	ilsp.RegisterLocalDefinition(hostsDefinition)
	complete.RegisterSource(newHostsSource())
}
