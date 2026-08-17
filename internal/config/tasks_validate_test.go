package config

import (
	"strings"
	"testing"
)

// [[tasks.matcher]] validation (#1915): a broken pattern, missing required
// group or duplicate name is dropped with a diagnostic carrying the matcher
// compiler's own message; valid entries survive untouched.
func TestValidateTaskMatchersDropsBadEntries(t *testing.T) {
	c := defaults()
	c.Tasks.Matchers = []MatcherEntry{
		{Name: "ok", Pattern: `^(\S+):(\d+): (.+)$`, File: 1, Line: 2, Message: 3},
		{Name: "badre", Pattern: `([unclosed`, File: 1, Line: 2, Message: 3},
		{Name: "norange", Pattern: `^(\S+):(\d+): (.+)$`, File: 1, Line: 2, Message: 7},
		{Name: "ok", Pattern: `^(\S+):(\d+): (.+)$`, File: 1, Line: 2, Message: 3},
	}
	diags := validate(c)
	if len(c.Tasks.Matchers) != 1 || c.Tasks.Matchers[0].Name != "ok" {
		t.Fatalf("kept = %+v", c.Tasks.Matchers)
	}
	var msgs []string
	for _, d := range diags {
		if d.Field == "tasks.matcher" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) != 3 {
		t.Fatalf("want 3 matcher diagnostics, got %d (%v)", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "invalid regex") {
		t.Errorf("bad-regex diagnostic must carry the compiler message, got %q", msgs[0])
	}
	if !strings.Contains(msgs[2], "duplicate matcher name") {
		t.Errorf("duplicate diagnostic = %q", msgs[2])
	}
}
