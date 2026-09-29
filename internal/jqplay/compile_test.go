package jqplay

import (
	"strings"
	"testing"
	"time"
)

// TestCompileReportsRunsError (#2780): the compile-only check reports exactly
// the error Run would, and nothing for a program that compiles.
func TestCompileReportsRunsError(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ} {
		for _, prog := range []string{".a |", ".foo[", "undefined_fn(1)"} {
			got := Compile(d, prog)
			if got == "" {
				t.Errorf("%s: Compile(%q) = \"\", want an error", d.Name(), prog)
				continue
			}
			if want := EvaluateWith(d, prog, `{"a":1}`).Err; got != want {
				t.Errorf("%s: Compile(%q) = %q, Run reports %q", d.Name(), prog, got, want)
			}
		}
		for _, prog := range []string{"", "  ", ".a | .b", "[.[] | select(.x > 1)]"} {
			if got := Compile(d, prog); got != "" {
				t.Errorf("%s: Compile(%q) = %q, want no error", d.Name(), prog, got)
			}
		}
	}
}

// TestCompileXMQShellWords (#2780): xmq's compile step is the shell-word
// split — an unterminated quote fails, anything else (the empty command line
// included) is left to the binary.
func TestCompileXMQShellWords(t *testing.T) {
	if got := Compile(DialectXMQ, `select "//a`); got == "" {
		t.Error("an unterminated quote must fail to compile")
	}
	for _, prog := range []string{"", "to-json", `select '//a[@x="1"]'`} {
		if got := Compile(DialectXMQ, prog); got != "" {
			t.Errorf("Compile(xmq, %q) = %q, want no error", prog, got)
		}
	}
}

// TestCompileIsBoundedByProgram (#2780): the check never touches an input,
// so even a long program compiles far below the debounce the run waits for.
func TestCompileIsBoundedByProgram(t *testing.T) {
	prog := strings.Repeat(".a | ", 2000) + ".b"
	start := time.Now()
	if err := Compile(DialectJQ, prog); err != "" {
		t.Fatalf("long valid program: %s", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("compiling a 10k-character program took %v", d)
	}
}

// BenchmarkCompile measures the per-keystroke cost of the live syntax check.
func BenchmarkCompile(b *testing.B) {
	for _, bc := range []struct{ name, prog string }{
		{"short", `.items[] | select(.price > 10) | {name, price}`},
		{"long", strings.Repeat(".a | ", 200) + ".b"},
		{"broken", `.items[] | select(.price > `},
	} {
		b.Run(bc.name, func(b *testing.B) {
			for range b.N {
				Compile(DialectJQ, bc.prog)
			}
		})
	}
}
