package langenv

// lint.go flags duplicate keys (#1623). A dotenv file assigning the same key
// twice is not an error to any loader — dotenv, godotenv, docker compose and
// friends simply keep one of them, in practice the last — which is exactly
// what makes it expensive: the earlier assignment is still there, still reads
// as the setting in force, and silently does nothing. So every occurrence but
// the last is marked as a warning naming the line that wins.

import (
	"fmt"

	"ike/internal/lang"
)

// envLint returns one warning note per shadowed key occurrence. Keys are
// compared exactly (dotenv key lookup is case-sensitive on every platform IKE
// targets); comment and blank lines, and lines without `=`, take part in
// nothing.
func envLint(lines []string) []lang.Note {
	type occurrence struct {
		line     int
		startCol int
		endCol   int
	}
	order := make([]string, 0, len(lines))
	seen := make(map[string][]occurrence)
	for li, line := range lines {
		runes := []rune(line)
		start, end := trimIndex(runes)
		if start >= end || runes[start] == '#' {
			continue
		}
		e := parseEntry(runes, start, end)
		if e.sep < 0 || e.key == "" {
			continue
		}
		if _, ok := seen[e.key]; !ok {
			order = append(order, e.key)
		}
		seen[e.key] = append(seen[e.key], occurrence{line: li, startCol: e.keyStart, endCol: e.keyEnd})
	}
	// Emit in first-appearance order so the note list is stable across runs
	// (map iteration is not) — the editor indexes by line either way, but a
	// stable order keeps tests and any future list view deterministic.
	var out []lang.Note
	for _, key := range order {
		occ := seen[key]
		if len(occ) < 2 {
			continue
		}
		winner := occ[len(occ)-1]
		for _, o := range occ[:len(occ)-1] {
			out = append(out, lang.Note{
				Line:     o.line,
				StartCol: o.startCol,
				EndCol:   o.endCol,
				Severity: lang.NoteWarn,
				Message:  fmt.Sprintf("duplicate key %q — the assignment on line %d wins", key, winner.line+1),
			})
		}
	}
	return out
}
