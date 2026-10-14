package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ndjsonFixture generates an ndjson file of n lines, each about width bytes
// (#2996): one JSON object per line with a handful of typed members and a
// padding string that brings the line to the requested width. It writes the
// file under dir (the test's TempDir, normally) and returns its path. The
// benchmarks below and the async-load / line-scoped tests share it, so the
// fixture they measure is the one the tests assert on.
func ndjsonFixture(t testing.TB, dir string, n, width int) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("fixture-%d-%d.ndjson", n, width))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var sb strings.Builder
	sb.Grow((width + 1) * n)
	for i := 0; i < n; i++ {
		sb.WriteString(ndjsonLine(i, width))
		sb.WriteByte('\n')
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		t.Fatal(err)
	}
	return path
}

// ndjsonLine is one fixture record: index i, padded to about width bytes.
func ndjsonLine(i, width int) string {
	levels := [...]string{"info", "warn", "error"}
	head := fmt.Sprintf(`{"id": %d, "ts": %d, "level": "%s", "service": "svc-%d", "msg": "request handled", "tags": ["a", "b", "c"], "ok": true, "latency_ms": %d.%03d`,
		i, 1700000000+i, levels[i%3], i%17, i%100, (i*37)%1000)
	pad := width - len(head) - len(`, "payload": ""}`)
	if pad > 0 {
		head += `, "payload": "` + strings.Repeat("x", pad) + `"`
	}
	return head + "}"
}

// ndjsonLines builds the fixture's lines in memory (buffer-level tests).
func ndjsonLines(n, width int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = ndjsonLine(i, width)
	}
	return lines
}
