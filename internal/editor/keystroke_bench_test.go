package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/largefile"
)

// Keystroke benchmarks (#2159): the large-file guard exists so per-keystroke
// cost stays flat in the file size. BenchmarkKeystrokeLargeFile types into a
// buffer past the base cliff (every per-edit service degraded) with an emitter
// installed — the configuration where an unguarded change event would re-join
// the whole buffer per key. Compare against BenchmarkKeystrokeSmallFile: the
// large run must not be orders of magnitude slower despite the 1000x buffer.

// benchEditor builds a focused insert-mode editor over content with an
// installed (discarding) emitter, mirroring the app wiring.
func benchEditor(b *testing.B, content string) Model {
	b.Helper()
	largefile.Reset()
	b.Cleanup(largefile.Reset)
	m := New()
	m.Configure(host.MapConfig{})
	m.RestoreText(content)
	m.SetSize(120, 40)
	m.SetFocused(true)
	m.SetEmitter(EmitterFunc(func(Event) {}))
	return send(m, key('i'))
}

func benchKeystrokes(b *testing.B, m Model) {
	k := key('x')
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, _ = m.Update(k)
	}
}

func BenchmarkKeystrokeSmallFile(b *testing.B) {
	benchKeystrokes(b, benchEditor(b, strings.Repeat("some line of text\n", 120)))
}

// BenchmarkKeystrokeLargeFile guards against per-keystroke full-buffer scans:
// 120k lines is past the default line cliff, so the change event ships no
// text, no parse schedules, and no tally scans.
func BenchmarkKeystrokeLargeFile(b *testing.B) {
	m := benchEditor(b, strings.Repeat("some line of text\n", 120_000))
	if !m.LargeFile() {
		b.Fatal("setup: the buffer must be past the base cliff")
	}
	benchKeystrokes(b, m)
}

// TestKeystrokeAvoidsFullBufferWorkWhenLarge is the deterministic guard next
// to the benchmark: a keystroke on a degraded document must neither join the
// buffer into the change event nor schedule a parse.
func TestKeystrokeAvoidsFullBufferWorkWhenLarge(t *testing.T) {
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	m := New()
	m.Configure(host.MapConfig{"files.large_file_lines": "5"})
	m.RestoreText(strings.Repeat("line\n", 10))
	m.SetSize(80, 20)
	m.SetFocused(true)
	var text string
	large := false
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventChange {
			text, large = e.Text, e.Large
		}
	}))
	m = send(m, key('i'))
	var cmd tea.Cmd
	m, cmd = m.Update(key('x'))
	if text != "" || !large {
		t.Fatalf("keystroke shipped %d bytes of text (Large=%v); the degraded path must ship none", len(text), large)
	}
	if cmd != nil {
		t.Fatalf("keystroke scheduled work (%T); the degraded path must not reparse", cmd)
	}
}
