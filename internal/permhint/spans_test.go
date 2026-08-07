package permhint

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// spans_test.go covers the context half (#1656): which run of digits each
// producer claims as a file mode, and — at least as important — which it leaves
// alone, since a bare three-digit number is a port or a year far more often
// than a permission.

// only asserts that spans holds exactly one hint, covering [start, end) of line
// li with the given symbolic reading, and returns it.
func only(t *testing.T, spans []lang.Span, li, start, end int, want string) lang.Span {
	t.Helper()
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want exactly one", spans)
	}
	s := spans[0]
	if s.Line != li || s.StartCol != start || s.EndCol != end {
		t.Errorf("span at %d:[%d,%d), want %d:[%d,%d)", s.Line, s.StartCol, s.EndCol, li, start, end)
	}
	if s.Capture != Capture {
		t.Errorf("capture = %q, want %q", s.Capture, Capture)
	}
	if !strings.HasSuffix(s.Replace, Gap+want) {
		t.Errorf("Replace = %q, want it to end in %q", s.Replace, Gap+want)
	}
	return s
}

// --- shell -----------------------------------------------------------------

// TestShellChmod: the mode is chmod's first operand, options and the command
// path notwithstanding.
func TestShellChmod(t *testing.T) {
	cases := []struct {
		line       string
		start, end int
		want       string
	}{
		{"chmod 755 build.sh", 6, 9, "rwxr-xr-x"},
		{"chmod -R 0644 docs", 9, 13, "rw-r--r--"},
		{"/bin/chmod 4755 /usr/bin/tool", 11, 15, "rwsr-xr-x"},
		{"sudo chmod 1777 /tmp", 11, 15, "rwxrwxrwt"},
		{"  chmod 2775 shared", 8, 12, "rwxrwsr-x"},
	}
	for _, c := range cases {
		only(t, ShellSpans([]string{c.line}), 0, c.start, c.end, c.want)
	}
}

// TestShellModeFlag: install and mkdir carry the mode behind -m/--mode, in
// every spelling the tools accept.
func TestShellModeFlag(t *testing.T) {
	cases := []struct {
		line       string
		start, end int
	}{
		{"install -m 755 ike /usr/local/bin", 11, 14},
		{"install -m755 ike /usr/local/bin", 10, 13},
		{"install --mode=755 ike /usr/local/bin", 15, 18},
		{"install --mode 755 ike /usr/local/bin", 15, 18},
		{"mkdir -m 755 /srv/app", 9, 12},
		{"mkdir -pm 755 /srv/app", 10, 13},
	}
	for _, c := range cases {
		only(t, ShellSpans([]string{c.line}), 0, c.start, c.end, "rwxr-xr-x")
	}
}

// TestShellSeparators: the scan resets per command, so each chmod on a chained
// line gets its own hint and a mode never leaks across the separator.
func TestShellSeparators(t *testing.T) {
	spans := ShellSpans([]string{"mkdir -p x && chmod 700 x"})
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want one (mkdir carries no -m here)", spans)
	}
	if spans[0].StartCol != 20 || spans[0].EndCol != 23 {
		t.Errorf("span = [%d,%d), want [20,23)", spans[0].StartCol, spans[0].EndCol)
	}
}

// TestShellChmodFirstOperandOnly: chmod's mode is its first operand, so a file
// named like a mode after it is left alone.
func TestShellChmodFirstOperandOnly(t *testing.T) {
	only(t, ShellSpans([]string{"chmod 755 700"}), 0, 6, 9, "rwxr-xr-x")
}

// TestShellGuards: numbers that are not modes stay untouched — a port, an exit
// code, a symbolic chmod, a comment, and chmod's file operands.
func TestShellGuards(t *testing.T) {
	for _, line := range []string{
		"PORT=8080",
		"exit 755",
		"curl http://localhost:755/",
		"chmod u+x build.sh",
		"# chmod 755 build.sh",
		"echo 644 > out.txt",
		"mkdir -p 755",    // no -m: 755 is a directory name
		"install ike 755", // no -m either
	} {
		if spans := ShellSpans([]string{line}); len(spans) != 0 {
			t.Errorf("ShellSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// --- Dockerfile ------------------------------------------------------------

// TestDockerfileSpans: the --chmod= flag of COPY/ADD and the chmod calls inside
// RUN lines.
func TestDockerfileSpans(t *testing.T) {
	only(t, DockerfileSpans([]string{"COPY --chmod=755 entrypoint.sh /"}), 0, 13, 16, "rwxr-xr-x")
	only(t, DockerfileSpans([]string{"ADD --chmod=0644 app.conf /etc/"}), 0, 12, 16, "rw-r--r--")
	only(t, DockerfileSpans([]string{"RUN chmod 700 /root/.ssh"}), 0, 10, 13, "rwx------")
}

// TestDockerfileGuards: an instruction that carries no mode carries no hint,
// and the other COPY flags are not modes.
func TestDockerfileGuards(t *testing.T) {
	for _, line := range []string{
		"FROM golang:1.24",
		"EXPOSE 8080",
		"COPY --from=build /app /app",
		"ENV PORT=755",
	} {
		if spans := DockerfileSpans([]string{line}); len(spans) != 0 {
			t.Errorf("DockerfileSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// --- Go --------------------------------------------------------------------

// TestGoSpans: the octal literals in the argument list of a mode API, wherever
// in the list they sit.
func TestGoSpans(t *testing.T) {
	only(t, GoSpans([]string{"\tos.Chmod(path, 0o755)"}), 0, 16, 21, "rwxr-xr-x")
	only(t, GoSpans([]string{"\tos.WriteFile(path, data, 0644)"}), 0, 26, 30, "rw-r--r--")
	only(t, GoSpans([]string{"\tos.MkdirAll(dir, 0o700)"}), 0, 18, 23, "rwx------")
	only(t, GoSpans([]string{"\tmode := os.FileMode(0o600)"}), 0, 21, 26, "rw-------")
}

// TestGoNestedCallHintsOnce: an outer call consumes its argument region, so a
// literal wrapped in os.FileMode inside os.Chmod is annotated once — two
// stand-ins over the same columns would fight for the same cells.
func TestGoNestedCallHintsOnce(t *testing.T) {
	only(t, GoSpans([]string{"\tos.Chmod(p, os.FileMode(0o755))"}), 0, 25, 30, "rwxr-xr-x")
}

// TestGoGuards: a decimal literal is not a mode in Go, an unrelated call is not
// a mode context, and a quoted run is a string.
func TestGoGuards(t *testing.T) {
	for _, line := range []string{
		"\tos.Chmod(path, 755)", // decimal: Go octal needs the leading 0
		"\thttp.ListenAndServe(\":8080\", nil)",
		"\tfmt.Println(0644)", // not a mode API
		"\tos.WriteFile(\"0644.txt\", b, m)",
		"\tv := x0644",
	} {
		if spans := GoSpans([]string{line}); len(spans) != 0 {
			t.Errorf("GoSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// --- Python ----------------------------------------------------------------

// TestPythonSpans: the positional mode of os.chmod, the mode= keyword of
// os.makedirs, and a pathlib chmod behind a chained call.
func TestPythonSpans(t *testing.T) {
	only(t, PythonSpans([]string{"os.chmod(path, 0o644)"}), 0, 15, 20, "rw-r--r--")
	only(t, PythonSpans([]string{"os.makedirs(d, mode=0o755)"}), 0, 20, 25, "rwxr-xr-x")
	only(t, PythonSpans([]string{"Path(\"key\").chmod(0o600)"}), 0, 18, 23, "rw-------")
}

// TestPythonGuards: open() is not a mode context and a decimal argument is not
// a mode.
func TestPythonGuards(t *testing.T) {
	for _, line := range []string{
		"open(path, \"w\", 8192)",
		"os.chmod(path, 644)",
		"sock.bind((\"\", 8080))",
	} {
		if spans := PythonSpans([]string{line}); len(spans) != 0 {
			t.Errorf("PythonSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// --- YAML ------------------------------------------------------------------

// TestYAMLSpans: the mode-family keys, quoted or plain, in a mapping or under a
// sequence marker. The span covers the literal, not the quotes.
func TestYAMLSpans(t *testing.T) {
	only(t, YAMLSpans([]string{"    mode: 0644"}), 0, 10, 14, "rw-r--r--")
	only(t, YAMLSpans([]string{"    mode: '0755'"}), 0, 11, 15, "rwxr-xr-x")
	only(t, YAMLSpans([]string{`    mode: "0700"`}), 0, 11, 15, "rwx------")
	only(t, YAMLSpans([]string{"  - mode: 0o600"}), 0, 10, 15, "rw-------")
	only(t, YAMLSpans([]string{"      defaultMode: 0400"}), 0, 19, 23, "r--------")
	only(t, YAMLSpans([]string{"    directory_mode: 02775  # shared"}), 0, 20, 25, "rwxrwsr-x")
}

// TestYAMLDecimalStaysUnhinted is the collision guard against the number hints
// (#1627): `mode: 644` is the decimal 644 both YAML and Ansible read it as, so
// the permission hint steps aside and the radix hint's `= 01204` warning stands.
func TestYAMLDecimalStaysUnhinted(t *testing.T) {
	for _, line := range []string{
		"    mode: 644",
		"    mode: 2775",
		"    defaultMode: 420",
	} {
		if spans := YAMLSpans([]string{line}); len(spans) != 0 {
			t.Errorf("YAMLSpans(%q) = %+v, want none (decimal, not octal)", line, spans)
		}
	}
}

// TestYAMLGuards: only the mode-family keys claim their value, and a comment or
// a symbolic mode is never a hint.
func TestYAMLGuards(t *testing.T) {
	for _, line := range []string{
		"    port: 0644",
		"    year: 2024",
		"    # mode: 0644",
		"    mode: u=rw,g=r,o=r",
		"    mode: preserve",
		"    modes: 0644",
	} {
		if spans := YAMLSpans([]string{line}); len(spans) != 0 {
			t.Errorf("YAMLSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// TestSpansCarryLineNumbers: the producers annotate every line of a buffer, not
// only the first.
func TestSpansCarryLineNumbers(t *testing.T) {
	spans := ShellSpans([]string{"#!/bin/sh", "chmod 700 secrets", "echo done"})
	if len(spans) != 1 || spans[0].Line != 1 {
		t.Fatalf("spans = %+v, want one on line 1", spans)
	}
}
