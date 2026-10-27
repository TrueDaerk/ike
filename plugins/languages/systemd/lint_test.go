package langsystemd

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// lintOne lints text (lines joined by \n) as path and returns the notes.
func lintOne(path, text string) []lang.Note {
	return systemdLint(path, strings.Split(text, "\n"))
}

// wantNote asserts exactly one note on line li covering [start, end) whose
// message contains every fragment.
func wantNote(t *testing.T, notes []lang.Note, li, start, end int, fragments ...string) {
	t.Helper()
	if len(notes) != 1 {
		t.Fatalf("got %d notes %+v, want 1", len(notes), notes)
	}
	n := notes[0]
	if n.Line != li || n.StartCol != start || n.EndCol != end || n.Severity != lang.NoteWarn {
		t.Errorf("note = line %d [%d,%d) sev %d, want line %d [%d,%d) warn", n.Line, n.StartCol, n.EndCol, n.Severity, li, start, end)
	}
	for _, f := range fragments {
		if !strings.Contains(n.Message, f) {
			t.Errorf("message %q lacks %q", n.Message, f)
		}
	}
}

// TestLintUnknownDirective (#3065): an unknown directive in a known section
// is flagged, with a close match suggested — a typo, a transposition or a
// case slip — and without one when nothing is close.
func TestLintUnknownDirective(t *testing.T) {
	cases := []struct{ line, want string }{
		{"ExecStrat=/bin/true", "did you mean ExecStart=?"},
		{"Resatrt=always", "did you mean Restart=?"},
		{"execstart=/bin/true", "did you mean ExecStart=?"},
	}
	for _, c := range cases {
		notes := lintOne("a.service", "[Service]\n"+c.line)
		key, _, _ := strings.Cut(c.line, "=")
		wantNote(t, notes, 1, 0, len(key), "unknown directive "+key+"= in [Service]", c.want)
	}
	notes := lintOne("a.service", "[Service]\nFrobnicateWidgets=1")
	wantNote(t, notes, 1, 0, len("FrobnicateWidgets"), "unknown directive FrobnicateWidgets=")
	if strings.Contains(notes[0].Message, "did you mean") {
		t.Errorf("no close match, yet %q", notes[0].Message)
	}
}

// TestLintWrongSection (#3065): a known directive in another section names
// the section(s) it belongs in, narrowed to those the unit type takes.
func TestLintWrongSection(t *testing.T) {
	wantNote(t, lintOne("a.service", "[Unit]\n  ExecStart = /bin/true"), 1, 2, 11,
		"ExecStart= belongs in [Service], not [Unit]")
	wantNote(t, lintOne("a.service", "[Install]\nUser=nobody"), 1, 0, 4,
		"User= belongs in [Service], not [Install]")
	// No unit type known: every section that has it.
	wantNote(t, lintOne("unit.conf", "[Unit]\nUser=nobody"), 1, 0, 4,
		"User= belongs in [Service], [Socket], [Mount] or [Swap], not [Unit]")
	wantNote(t, lintOne("a.timer", "[Timer]\nDescription=x"), 1, 0, 11,
		"Description= belongs in [Unit], not [Timer]")
}

// TestLintInvalidValue (#3065): a value outside the enum of an enumerated or
// boolean directive is flagged on the value, with a close match or the
// expected values.
func TestLintInvalidValue(t *testing.T) {
	wantNote(t, lintOne("a.service", "[Service]\nRestart=allways"), 1, 8, 15,
		`invalid value "allways" for Restart=`, "did you mean always?")
	wantNote(t, lintOne("a.service", "[Service]\nType=daemonic"), 1, 5, 13,
		`invalid value "daemonic" for Type=`, "expected one of simple, exec, forking")
	notes := lintOne("a.service", "[Unit]\nFailureAction=explode")
	wantNote(t, notes, 1, 14, 21, ", …")
	wantNote(t, lintOne("a.service", "[Service]\nPrivateTmp=maybe"), 1, 11, 16,
		`invalid value "maybe" for PrivateTmp=`, "expected a boolean")
}

// TestLintValidValues (#3065): every spelling systemd accepts passes — the
// enum values, boolean spellings in any case, prefixed forms (`file:`,
// `fd:name`), booleans for yes/no enums and numbers where the parser falls
// back to one.
func TestLintValidValues(t *testing.T) {
	text := strings.Join([]string{
		"[Service]",
		"Type=notify",
		"Restart=on-failure",
		"PrivateTmp=YES",
		"NoNewPrivileges=1",
		"DynamicUser=y",
		"ProtectSystem=strict",
		"ProtectHome=true",
		"StandardOutput=file:/var/log/a.log",
		"StandardInput=fd:stdin",
		"SyslogLevel=3",
		"IOSchedulingClass=2",
		"ExecStart=/usr/bin/a --flag",
		"Environment=A=1 B=2",
	}, "\n")
	if notes := lintOne("a.service", text); len(notes) != 0 {
		t.Errorf("valid unit got notes %+v", notes)
	}
	if notes := lintOne("a.service", "[Service]\nType=7"); len(notes) != 1 {
		t.Errorf("Type=7: got %d notes, want 1 (no numeric fallback)", len(notes))
	}
}

// TestLintSectionForUnitType (#3065): a section the unit type does not take
// is flagged on its name, also for a drop-in and through a template suffix;
// [Unit] and [Install] fit everywhere, and an unknown unit type checks none.
func TestLintSectionForUnitType(t *testing.T) {
	wantNote(t, lintOne("a.timer", "[Unit]\n[Service]\nType=simple"), 1, 1, 8,
		"[Service] is not valid in a .timer unit")
	wantNote(t, lintOne("/etc/systemd/system/a.timer.d/override.conf", "[Service]"), 0, 1, 8,
		"not valid in a .timer unit")
	wantNote(t, lintOne("a.timer.j2", "[Service]"), 0, 1, 8, "not valid in a .timer unit")
	for _, path := range []string{"a.target", "a.service", "a.slice"} {
		if notes := lintOne(path, "[Unit]\nDescription=x\n[Install]\nWantedBy=multi-user.target"); len(notes) != 0 {
			t.Errorf("%s: got notes %+v", path, notes)
		}
	}
	if notes := lintOne("unit.conf", "[Service]\n[Timer]"); len(notes) != 0 {
		t.Errorf("unknown unit type got notes %+v", notes)
	}
}

// TestLintUnknownSection (#3065): a section the table does not know is
// flagged with a close match; its directives are not checked against it.
func TestLintUnknownSection(t *testing.T) {
	wantNote(t, lintOne("a.service", "[Sevice]\nExecStart=/bin/true\nBogus=1"), 0, 1, 7,
		"unknown section [Sevice]", "did you mean [Service]?")
}

// TestLintExemptions (#3065): X- sections and directives, empty assignments,
// `%` specifiers and `$` variables in an enum value, continued values,
// comments and lines before any header raise nothing.
func TestLintExemptions(t *testing.T) {
	text := strings.Join([]string{
		"Restart=bogus", // before any header: a one-line pass cannot know
		"[X-Custom]",
		"Anything=goes",
		"[Service]",
		"X-Vendor-Flag=1",
		"ExecStart=",
		"Restart=",
		"Type=%i",
		"Restart=${RESTART}",
		"StandardOutput=$OUT",
		"# Restart=bogus",
		"; Bogus=1",
		"ExecStart=/bin/a \\",
		"  Bogus=1 \\",
		"  # comment inside the continuation",
		"  --last",
		"Restart=always",
		"not an assignment",
	}, "\n")
	if notes := lintOne("a.service", text); len(notes) != 0 {
		t.Errorf("exempt lines got notes %+v", notes)
	}
}

// TestLintContinuationEnds (#3065): a blank line ends a continuation, so the
// next directive is checked again.
func TestLintContinuationEnds(t *testing.T) {
	notes := lintOne("a.service", "[Service]\nExecStart=/bin/a \\\n\nBogus=1")
	wantNote(t, notes, 3, 0, 5, "unknown directive Bogus=")
}

// TestLintRuneColumns (#3065): note columns count runes, not bytes.
func TestLintRuneColumns(t *testing.T) {
	wantNote(t, lintOne("a.service", "[Unit]\nDescription=ü\n[Service]\nType=ünknown"), 3, 5, 12,
		`invalid value "ünknown"`)
}

// TestLintRegistered (#3065): the language carries the linter, so the
// editor's highlight pass picks it up through highlight.Lint, with the path.
func TestLintRegistered(t *testing.T) {
	l, ok := lang.ByPath("/etc/systemd/system/a.timer")
	if !ok || l.Lint == nil {
		t.Fatal("the systemd language must register a Lint")
	}
	notes := highlight.Lint("/etc/systemd/system/a.timer", []string{"[Service]"})
	if len(notes) != 1 {
		t.Errorf("highlight.Lint returned %d notes, want 1", len(notes))
	}
}

// TestEditDistance: the optimal-string-alignment distance the suggestions
// rank by.
func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "", 3}, {"", "ab", 2}, {"abc", "abc", 0},
		{"abc", "acb", 1}, {"kitten", "sitting", 3}, {"execstrat", "execstart", 1},
	}
	for _, c := range cases {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
