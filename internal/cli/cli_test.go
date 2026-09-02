package cli

import (
	"reflect"
	"testing"
)

func TestParseGrammar(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Invocation
	}{
		{"zero args", nil, Invocation{}},
		{"plain path", []string{"file.go"}, Invocation{Targets: []Target{{Path: "file.go"}}}},
		{"path with line", []string{"file.go:42"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42}}}},
		{"path with line and col", []string{"file.go:42:7"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42, Col: 7}}}},
		{"colon inside path", []string{"weird:name.txt"}, Invocation{Targets: []Target{{Path: "weird:name.txt"}}}},
		{"colon path with line", []string{"a:b.txt:12"}, Invocation{Targets: []Target{{Path: "a:b.txt", Line: 12}}}},
		{"three numeric segments keep leftmost in path", []string{"a:12:5:7"}, Invocation{Targets: []Target{{Path: "a:12", Line: 5, Col: 7}}}},
		{"trailing colon is a plain path", []string{"file.go:"}, Invocation{Targets: []Target{{Path: "file.go:"}}}},
		{"trailing colon after number", []string{"file.go:42:"}, Invocation{Targets: []Target{{Path: "file.go:42:"}}}},
		{"zero line stays in path", []string{"file.go:0"}, Invocation{Targets: []Target{{Path: "file.go:0"}}}},
		{"signed suffix stays in path", []string{"file.go:+5"}, Invocation{Targets: []Target{{Path: "file.go:+5"}}}},
		{"leading colon stays in path", []string{":42"}, Invocation{Targets: []Target{{Path: ":42"}}}},
		{"vim-style +N", []string{"+42", "file.go"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42}}}},
		{"suffix wins over +N", []string{"+42", "file.go:7"}, Invocation{Targets: []Target{{Path: "file.go", Line: 7}}}},
		{"+N skips only the next path", []string{"+42", "a.go", "b.go"}, Invocation{Targets: []Target{{Path: "a.go", Line: 42}, {Path: "b.go"}}}},
		{"stdin only", []string{"-"}, Invocation{Stdin: true}},
		{"stdin with files keeps order", []string{"a.go", "-", "b.go:3"}, Invocation{Stdin: true, Targets: []Target{{Path: "a.go"}, {Path: "b.go", Line: 3}}}},
		{"multiple targets preserve order", []string{"b.go", "a.go:1"}, Invocation{Targets: []Target{{Path: "b.go"}, {Path: "a.go", Line: 1}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.args)
			if err != nil {
				t.Fatalf("Parse(%v) error: %v", c.args, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse(%v) = %+v, want %+v", c.args, got, c.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"dangling +N", []string{"file.go", "+42"}},
		{"bare +", []string{"+"}},
		{"non-numeric +x", []string{"+abc", "file.go"}},
		{"double sign", []string{"++42", "file.go"}},
		{"zero line flag", []string{"+0", "file.go"}},
		{"duplicate stdin", []string{"-", "-"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.args); err == nil {
				t.Fatalf("Parse(%v) must error", c.args)
			}
		})
	}
}

// TestParseVersionFlag covers `ike --version` / `ike -v` (#1214): the flag
// short-circuits, so it wins over anything else on the line — including an
// otherwise-invalid invocation, since the version banner never needs the rest
// of the arguments to be sound.
func TestParseVersionFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"long", []string{"--version"}},
		{"short", []string{"-v"}},
		{"before a path", []string{"-v", "file.go"}},
		{"after a path", []string{"file.go", "--version"}},
		{"wins over a malformed line", []string{"--version", "+abc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv, err := Parse(c.args)
			if err != nil {
				t.Fatalf("Parse(%v) errored: %v", c.args, err)
			}
			if !inv.Version {
				t.Fatalf("Parse(%v).Version = false, want true", c.args)
			}
		})
	}
}

// TestParseVersionFlagIsExact guards the neighbours: a lone "-" is stdin, and
// "-v" only counts as the flag when it is the whole argument.
func TestParseVersionFlagIsExact(t *testing.T) {
	for _, arg := range []string{"-", "-vv", "--versions", "v"} {
		inv, err := Parse([]string{arg})
		if err != nil {
			t.Fatalf("Parse([%q]) errored: %v", arg, err)
		}
		if inv.Version {
			t.Fatalf("Parse([%q]).Version = true, want false", arg)
		}
	}
}

// TestParseDeepLinkURL covers the ike:// routing arguments (#2396).
func TestParseDeepLinkURL(t *testing.T) {
	inv, err := Parse([]string{"ike://open?remote=git@github.com:a/b"})
	if err != nil || inv.URL != "ike://open?remote=git@github.com:a/b" || inv.URLSendOnly {
		t.Fatalf("got %+v, %v", inv, err)
	}
	inv, err = Parse([]string{"--url-send-only", "ike://open?project=x"})
	if err != nil || !inv.URLSendOnly || inv.URL != "ike://open?project=x" {
		t.Fatalf("got %+v, %v", inv, err)
	}
	if _, err = Parse([]string{"ike://open?project=a", "ike://open?project=b"}); err == nil {
		t.Error("duplicate URL accepted")
	}
	if _, err = Parse([]string{"--url-send-only"}); err == nil {
		t.Error("--url-send-only without URL accepted")
	}
	// A path that merely contains "ike:" mid-string stays a file target.
	inv, err = Parse([]string{"dir/ike:notes.txt"})
	if err != nil || inv.URL != "" || len(inv.Targets) != 1 {
		t.Fatalf("got %+v, %v", inv, err)
	}
}
