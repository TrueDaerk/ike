package langsystemd

import (
	"context"
	"slices"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// completeAt feeds text to a fresh source as the buffer at path and completes
// at the `|` marker, which is removed from the text.
func completeAt(t *testing.T, path, text string) (*source, complete.Request, []ilsp.CompletionItem) {
	t.Helper()
	li, col := -1, 0
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if c := strings.Index(l, "|"); c >= 0 {
			li, col = i, len([]rune(l[:c]))
			lines[i] = l[:c] + l[c+1:]
		}
	}
	if li < 0 {
		t.Fatal("no | marker")
	}
	s := newSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: strings.Join(lines, "\n")})
	req := complete.Request{Path: path, Line: li, Col: col}
	items, err := s.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return s, req, items
}

func labels(items []ilsp.CompletionItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func find(items []ilsp.CompletionItem, label string) (ilsp.CompletionItem, bool) {
	for _, it := range items {
		if it.Label == label {
			return it, true
		}
	}
	return ilsp.CompletionItem{}, false
}

// TestSectionCompletion: a `[` line offers the sections valid for the unit
// type — [Timer] only in a timer, [Unit]/[Install] everywhere — and a drop-in
// takes the unit type of its directory.
func TestSectionCompletion(t *testing.T) {
	cases := []struct {
		path      string
		want, not []string
	}{
		{"a.timer", []string{"[Unit]", "[Install]", "[Timer]"}, []string{"[Service]", "[Socket]"}},
		{"a.service", []string{"[Unit]", "[Install]", "[Service]"}, []string{"[Timer]", "[Mount]"}},
		{"/etc/systemd/system/a.socket.d/override.conf", []string{"[Socket]"}, []string{"[Service]"}},
	}
	for _, c := range cases {
		_, _, items := completeAt(t, c.path, "[Unit]\nDescription=x\n\n[|")
		got := labels(items)
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("%s: sections %v lack %s", c.path, got, w)
			}
		}
		for _, n := range c.not {
			if slices.Contains(got, n) {
				t.Errorf("%s: sections %v must not offer %s", c.path, got, n)
			}
		}
	}
	_, _, items := completeAt(t, "a.timer", "[Ti|")
	if it, _ := find(items, "[Timer]"); it.InsertText != "[Timer]" {
		t.Errorf("open header insert = %q, want [Timer]", it.InsertText)
	}
	_, _, items = completeAt(t, "a.timer", "[Ti|]")
	if it, _ := find(items, "[Timer]"); it.InsertText != "[Timer" {
		t.Errorf("auto-closed header insert = %q, want [Timer (the ] is already there)", it.InsertText)
	}
	if _, _, items := completeAt(t, "a.timer", "[Timer] |"); items != nil {
		t.Errorf("after a closed header: %v, want nothing", labels(items))
	}
}

// TestDirectiveCompletion: a key position offers the enclosing section's
// directives, described, inserting `Name=`.
func TestDirectiveCompletion(t *testing.T) {
	_, _, items := completeAt(t, "a.service", "[Unit]\nDescription=x\n\n[Service]\nRes|")
	it, ok := find(items, "Restart")
	if !ok {
		t.Fatalf("[Service] directives %v lack Restart", labels(items))
	}
	if it.InsertText != "Restart=" || it.Detail == "" {
		t.Errorf("Restart item = %+v, want insert Restart= and a description", it)
	}
	if _, ok := find(items, "OnCalendar"); ok {
		t.Error("[Service] must not offer the [Timer] directive OnCalendar")
	}
	_, _, items = completeAt(t, "a.service", "[Unit]\n|")
	if _, ok := find(items, "After"); !ok {
		t.Errorf("[Unit] directives %v lack After", labels(items))
	}
	seen := map[string]bool{}
	for _, l := range labels(items) {
		if seen[l] {
			t.Errorf("directive %s offered twice", l)
		}
		seen[l] = true
	}
	if _, _, items := completeAt(t, "a.service", "Res|"); items != nil {
		t.Errorf("before any section: %v, want nothing", labels(items))
	}
	if _, _, items := completeAt(t, "a.service", "[X-Custom]\nFo|"); items != nil {
		t.Errorf("extension section: %v, want nothing", labels(items))
	}
}

// TestValueCompletion: after `=` come the enum values, the booleans, common
// targets for dependency and install directives, and signals.
func TestValueCompletion(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"[Service]\nType=|", []string{"simple", "oneshot", "notify"}},
		{"[Service]\nRestart=on-f|", []string{"on-failure", "always"}},
		{"[Service]\nKillMode=|", []string{"control-group", "process", "mixed"}},
		{"[Service]\nRemainAfterExit=|", []string{"yes", "no"}},
		{"[Service]\nKillSignal=|", []string{"SIGTERM", "SIGKILL"}},
		{"[Install]\nWantedBy=|", []string{"multi-user.target", "network-online.target"}},
		{"[Install]\nRequiredBy=|", []string{"multi-user.target"}},
		{"[Unit]\nAfter=|", []string{"network-online.target"}},
		{"[Unit]\nBefore=|", []string{"shutdown.target"}},
	}
	for _, c := range cases {
		_, _, items := completeAt(t, "a.service", c.text)
		got := labels(items)
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("%q: values %v lack %s", c.text, got, w)
			}
		}
	}
	// A list value skips what it already holds.
	_, _, items := completeAt(t, "a.service", "[Unit]\nAfter=network-online.target |")
	if _, ok := find(items, "network-online.target"); ok {
		t.Error("a listed target must not be offered again")
	}
	if _, ok := find(items, "multi-user.target"); !ok {
		t.Error("the next list word must still offer the other targets")
	}
	// Free-form values are left to the other sources.
	for _, text := range []string{
		"[Service]\nExecStart=/usr/bin/fo|",
		"[Unit]\nDescription=My|",
		"[Timer]\nUnit=|", // a unit of its own, not a common target
		"[Service]\nExecStart=/bin/a \\\n  --fla|",
	} {
		if _, _, items := completeAt(t, "a.service", text); items != nil {
			t.Errorf("%q: %v, want nothing", text, labels(items))
		}
	}
}

// TestSpecifierCompletion: a `%` in a value offers every specifier with its
// meaning; `%%` is the literal percent sign.
func TestSpecifierCompletion(t *testing.T) {
	_, _, items := completeAt(t, "a.service", "[Service]\nExecStart=/bin/echo %|")
	if len(items) != len(specifiers) {
		t.Fatalf("specifiers = %d, want %d", len(items), len(specifiers))
	}
	for _, it := range items {
		if it.Detail == "" {
			t.Errorf("specifier %s has no meaning", it.Label)
		}
	}
	if it, ok := find(items, "%n"); !ok || it.Detail != "Full unit name" {
		t.Errorf("%%n = %+v, %v", it, ok)
	}
	if _, _, items := completeAt(t, "a.service", "[Service]\nEnvironment=X=%i|"); len(items) != len(specifiers) {
		t.Errorf("a typed specifier letter: %d items, want the specifiers", len(items))
	}
	if _, _, items := completeAt(t, "a.service", "[Service]\nExecStart=/bin/printf 50%%|"); items != nil {
		t.Errorf("after %%%%: %v, want nothing", labels(items))
	}
	if _, _, items := completeAt(t, "a.service", "[Service]\nExecStart=/bin/a \\\n  %|"); len(items) == 0 {
		t.Error("a continuation line still completes specifiers")
	}
}

// TestTableEntriesClaimThePosition: where the table answers, the source
// claims the position so buffer words do not drown the entries; free text
// stays shared.
func TestTableEntriesClaimThePosition(t *testing.T) {
	for _, text := range []string{"[|", "[Service]\nTy|", "[Service]\nType=|", "[Service]\nExecStart=%|"} {
		s, req, _ := completeAt(t, "a.service", text)
		if !s.ExclusiveAt(req) {
			t.Errorf("%q: table position not claimed", text)
		}
	}
	s, req, _ := completeAt(t, "a.service", "[Service]\nExecStart=/usr/b|")
	if s.ExclusiveAt(req) {
		t.Error("a free-text value must not be claimed")
	}
	s, req, _ = completeAt(t, "a.yaml", "[|")
	if s.ExclusiveAt(req) {
		t.Error("a non-systemd buffer must not be claimed")
	}
	if s.Priority() <= ilsp.PrioritySymbols {
		t.Errorf("priority %d must rank above the word and symbol indexes", s.Priority())
	}
	for _, ch := range []string{"[", "=", "%", " "} {
		if !s.TriggerChar(ch) {
			t.Errorf("TriggerChar(%q) = false", ch)
		}
	}
}

// TestCompletionCodeHook: the language marks the table value positions as
// code, so the string styling of a value does not shut the auto-popup.
func TestCompletionCodeHook(t *testing.T) {
	l, ok := lang.ByPath("a.service")
	if !ok || l.CompletionCode == nil {
		t.Fatal("systemd declares no CompletionCode")
	}
	cases := []struct {
		line string
		want lang.CompletionContext
	}{
		{"WantedBy=multi-user.target ", lang.CtxCode},
		{"Restart=on-f", lang.CtxCode},
		{"ExecStart=/bin/echo %", lang.CtxCode},
		{"ExecStart=/bin/ech", lang.CtxString},
		{"Description=My serv", lang.CtxString},
	}
	for _, c := range cases {
		if got := lang.CompletionContextAt(l, "string", c.line, len(c.line)); got != c.want {
			t.Errorf("%q = %q, want %q", c.line, got, c.want)
		}
	}
}
