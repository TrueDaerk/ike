package editor

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	_ "ike/plugins/languages/web"
)

// longHTML builds a multi-thousand-line HTML page with inline <style> and
// <script> blocks and plenty of markup (#2770).
func longHTML(sections int) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<title>Bench</title>\n<style>\n")
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&sb, ".sec-%d { color: #123456; margin: %dpx; }\n", i, i)
	}
	sb.WriteString("</style>\n<script>\n")
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&sb, "function f%d(a, b) { return a + b * %d; }\n", i, i)
	}
	sb.WriteString("</script>\n</head>\n<body>\n")
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&sb, "<div class=\"sec-%d\" id=\"s%d\" style=\"color: red\" onclick=\"f%d(1, 2)\">\n", i, i, i)
		for j := 0; j < 8; j++ {
			fmt.Fprintf(&sb, "  <p>Paragraph %d.%d with <a href=\"#s%d\">a link</a> and <em>emphasis</em>.</p>\n", i, j, i)
		}
		sb.WriteString("</div>\n")
	}
	sb.WriteString("</body>\n</html>\n")
	return sb.String()
}

func benchHTMLEditor(b *testing.B, atEnd bool) Model {
	b.Helper()
	m := benchEditorPath(b, "/tmp/bench.html", longHTML(400))
	if atEnd {
		// benchEditor leaves the model in insert mode: leave it, jump to
		// the last line, re-enter — no edit, so no parse is in flight.
		m = send(m, special(tea.KeyEscape), key('G'), key('i'))
	}
	return m
}

func benchEditorPath(b *testing.B, path, content string) Model {
	b.Helper()
	m := benchEditor(b, content)
	m.path = path
	return m
}

// BenchmarkKeystrokeHTMLUpdate measures the synchronous Update cost of one
// insert-mode keystroke at the end of a long HTML buffer.
func BenchmarkKeystrokeHTMLUpdate(b *testing.B) {
	benchKeystrokes(b, benchHTMLEditor(b, true))
}

// BenchmarkKeystrokeHTMLParse measures the off-loop parse a keystroke
// schedules for the same buffer.
func BenchmarkKeystrokeHTMLParse(b *testing.B) {
	benchParse(b, benchHTMLEditor(b, true))
}

// benchParse times the parse body of one keystroke's snapshot. It calls the
// body directly rather than the scheduled command: the gate (#2770) parses a
// snapshot once and a re-run command has nothing left to do.
func benchParse(b *testing.B, m Model) {
	b.Helper()
	m, _ = m.Update(key('x'))
	snap := parseSnapshot{key: m.ParseKey(), langPath: m.langPath(), version: m.docVersion, lines: m.buf.Lines()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := parseSnapshotMsg(snap, true)
		if sp, ok := msg.(highlight.SpansMsg); !ok || len(sp.Spans) == 0 {
			b.Fatalf("got %T with no spans", msg)
		}
	}
}

// BenchmarkKeystrokeHTMLSpans measures applying the parse result.
func BenchmarkKeystrokeHTMLSpans(b *testing.B) {
	m := benchHTMLEditor(b, true)
	var cmd tea.Cmd
	m, cmd = m.Update(key('x'))
	msg := runCmdMsg(cmd)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, _ = m.Update(msg)
	}
}

func BenchmarkKeystrokeHTMLView(b *testing.B) {
	m := benchHTMLEditor(b, true)
	var cmd tea.Cmd
	m, cmd = m.Update(key('x'))
	m, _ = m.Update(runCmdMsg(cmd))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkKeystrokeHTMLParseTop(b *testing.B) {
	benchParse(b, benchHTMLEditor(b, false))
}

func runCmdMsg(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	for {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c == nil {
					continue
				}
				if r := runCmdMsg(c); r != nil {
					if _, ok := r.(highlight.SpansMsg); ok {
						return r
					}
				}
			}
			return nil
		default:
			return msg
		}
	}
}
