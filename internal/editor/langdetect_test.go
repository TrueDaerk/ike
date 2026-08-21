package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/register"
	"ike/internal/lang"
)

func init() {
	// The detect tests resolve real language ids, which live in the language
	// plugins (not imported here) — register the handful they need.
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	lang.Register(lang.Language{ID: "csv", Extensions: []string{"csv"}})
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yaml", "yml"}})
}

// drain collects the NoticeMsg texts a command produced, so a test can assert
// on the one toast a detection is allowed to emit.
func drain(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	var out []string
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case NoticeMsg:
			out = append(out, msg.Text)
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		}
	}
	walk(cmd)
	return out
}

// TestPasteDetectsLanguage is the acceptance table of #2037: content pasted
// into an empty, file-less buffer classifies it, and the buffer then resolves
// as that language everywhere path-keyed lookups ask.
func TestPasteDetectsLanguage(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md"}})
	cases := []struct {
		name, paste, want, wantPath string
	}{
		{"json", "{\n  \"a\": 1\n}\n", "json", "buffer.json"},
		{"csv", "name,age,city\nada,36,london\n", "csv", "buffer.csv"},
		{"markdown", "# Notes\n\nsome prose\n", "markdown", "buffer.md"},
		{"prose stays typeless", "just a note to self\n", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fileless(t, "")
			m.PasteText(tc.paste)
			if got := m.LangOverride(); got != tc.want {
				t.Fatalf("LangOverride() = %q, want %q", got, tc.want)
			}
			if got := m.langPath(); got != tc.wantPath {
				t.Errorf("langPath() = %q, want %q", got, tc.wantPath)
			}
			if m.Path() != "" {
				t.Errorf("Path() = %q, want empty — detection must not invent a file", m.Path())
			}
		})
	}
}

// TestPasteDetectNotifiesOnce guards the feedback contract: exactly one
// unobtrusive toast naming the type, and nothing at all when the content is
// unrecognized.
func TestPasteDetectNotifiesOnce(t *testing.T) {
	m := fileless(t, "")
	notices := drain(m.PasteText("{\"a\":1}"))
	if len(notices) != 1 || !strings.Contains(notices[0], "json") {
		t.Fatalf("notices = %q, want one mentioning json", notices)
	}
	// The signal is drained, not repeated on the next change.
	m2, cmd := m.maybeReparse(m.docVersion-1, nil)
	if got := drain(cmd); len(got) != 0 {
		t.Errorf("second drain produced %q, want none", got)
	}
	_ = m2

	plain := fileless(t, "")
	if got := drain(plain.PasteText("just a note\n")); len(got) != 0 {
		t.Errorf("unrecognized paste notified %q, want silence", got)
	}
}

// TestPasteDetectSkipsClassifiedBuffers guards the three gates: a buffer with
// a file is classified by its path, a non-empty buffer is never retyped, and
// a language already chosen (#2033, or an earlier detect) wins over the
// content.
func TestPasteDetectSkipsClassifiedBuffers(t *testing.T) {
	t.Run("buffer with a file", func(t *testing.T) {
		m, _ := loaded(t, "")
		m.PasteText("{\"a\":1}")
		if got := m.LangOverride(); got != "" {
			t.Errorf("LangOverride() = %q, want empty", got)
		}
	})
	t.Run("non-empty buffer", func(t *testing.T) {
		m := fileless(t, "existing content\n")
		m.PasteText("{\"a\":1}")
		if got := m.LangOverride(); got != "" {
			t.Errorf("LangOverride() = %q, want empty", got)
		}
	})
	t.Run("language already chosen", func(t *testing.T) {
		m := fileless(t, "")
		if _, ok := m.SetLangOverride("markdown"); !ok {
			t.Fatal("SetLangOverride(markdown) refused")
		}
		m.PasteText("{\"a\":1}")
		if got := m.LangOverride(); got != "markdown" {
			t.Errorf("LangOverride() = %q, want markdown — a chosen type is never overwritten", got)
		}
	})
}

// TestPasteDetectIsCorrectable guards that a wrong verdict costs nothing: the
// #2033 picker changes it, and the empty id drops back to plain text — the
// text itself is untouched either way.
func TestPasteDetectIsCorrectable(t *testing.T) {
	m := fileless(t, "")
	m.PasteText("name,age,city\nada,36,london\n")
	if got := m.LangOverride(); got != "csv" {
		t.Fatalf("LangOverride() = %q, want csv", got)
	}
	before := strings.Join(m.buf.Lines(), "\n")
	if _, ok := m.SetLangOverride("yaml"); !ok {
		t.Fatal("SetLangOverride(yaml) refused")
	}
	if got := m.LangOverride(); got != "yaml" {
		t.Errorf("LangOverride() = %q, want yaml", got)
	}
	if _, ok := m.SetLangOverride(""); !ok {
		t.Fatal("SetLangOverride(\"\") refused")
	}
	if got := m.LangOverride(); got != "" {
		t.Errorf("LangOverride() = %q, want empty", got)
	}
	if after := strings.Join(m.buf.Lines(), "\n"); after != before {
		t.Errorf("text changed by retyping: %q -> %q", before, after)
	}
}

// TestVimPasteDetectsLanguage covers the other paste funnels: vim's `p` from a
// register, and a clipboard paste spliced into an open insert session.
func TestVimPasteDetectsLanguage(t *testing.T) {
	t.Run("normal-mode p", func(t *testing.T) {
		m := fileless(t, "")
		m.regs.Yank('+', register.Entry{Text: "name,age,city\nada,36,london\n", Linewise: true})
		m.paste('+', false, 1, false)
		if got := m.LangOverride(); got != "csv" {
			t.Errorf("LangOverride() = %q, want csv", got)
		}
	})
	t.Run("paste mid-insert", func(t *testing.T) {
		m := fileless(t, "")
		m = typeKeys(m, "i")
		m.pasteIntoInsert("{\n  \"a\": 1\n}")
		if got := m.LangOverride(); got != "json" {
			t.Errorf("LangOverride() = %q, want json", got)
		}
	})
}
