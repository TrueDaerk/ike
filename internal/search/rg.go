package search

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// rg.go is the ripgrep backend: `rg --json` emits one JSON event per line;
// the "match" events carry the path, 1-based line number, line text, and
// byte-offset submatches, which map directly onto the Match shape (byte
// offsets converted to rune columns).

// rgEvent is the subset of ripgrep's JSON event stream the scanner reads.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// scanRG streams matches from a ripgrep child process until it exits, the
// collector refuses more results, or ctx is cancelled (which kills the child).
func scanRG(ctx context.Context, rg string, q Query, c *collector) error {
	// --no-require-git: respect .gitignore even outside a git repository, so
	// the backends (and IKE's behavior in non-git projects) stay consistent.
	args := []string{"--json", "--no-messages", "--no-require-git"}
	if q.CaseSensitive {
		args = append(args, "-s")
	} else {
		args = append(args, "-i")
	}
	if q.WholeWord {
		args = append(args, "-w")
	}
	if !q.Regex {
		args = append(args, "-F")
	}
	for _, g := range q.Include {
		args = append(args, "-g", g)
	}
	for _, g := range q.Exclude {
		args = append(args, "-g", "!"+g)
	}
	args = append(args, "--", q.Pattern, ".")

	cmd := exec.CommandContext(ctx, rg, args...)
	cmd.Dir = q.Root
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	stopped := false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // long lines (minified files)
	for sc.Scan() {
		var ev rgEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Type != "match" {
			continue
		}
		text := strings.TrimRight(ev.Data.Lines.Text, "\r\n")
		path := filepath.Join(q.Root, ev.Data.Path.Text)
		for _, sub := range ev.Data.Submatches {
			m := Match{
				Path:     path,
				Line:     ev.Data.LineNumber,
				Text:     text,
				StartCol: runeCol(text, sub.Start),
				EndCol:   runeCol(text, sub.End),
			}
			if !c.add(m) {
				stopped = true
				break
			}
		}
		if stopped {
			break
		}
	}
	if stopped {
		_ = cmd.Process.Kill() // result bound hit: stop scanning early
		_ = cmd.Wait()
		return nil
	}
	if err := cmd.Wait(); err != nil {
		// Exit code 1 is "no matches", a clean empty result; anything else
		// (code 2: bad pattern/glob) is a real failure.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil
		}
		if ctx.Err() != nil {
			return nil // killed by cancellation
		}
		return fmt.Errorf("rg: %w", err)
	}
	return sc.Err()
}

// runeCol converts a byte offset within text to a rune column, clamped to the
// text bounds (defensive against offsets past a trimmed line ending).
func runeCol(text string, byteOff int) int {
	if byteOff < 0 {
		return 0
	}
	if byteOff > len(text) {
		byteOff = len(text)
	}
	return utf8.RuneCountInString(text[:byteOff])
}
