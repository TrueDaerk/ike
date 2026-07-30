package langshell

import (
	"strconv"
	"strings"

	"ike/internal/format"
)

// format.go wires Shell's default external formatter (Roadmap 0470, #1405):
// shfmt. bash-language-server only formats when shfmt happens to be on PATH
// and stays silent otherwise; registering shfmt explicitly at the external
// tier means a missing binary produces the one-time install hint instead of
// a silent no-op. The indent flag derives from the buffer's effective
// editorconfig/settings and the dialect from the shebang; `[format.shell]`
// overrides the chain (#1402).

func init() {
	format.RegisterExternalDefaults("shell",
		format.External{
			Command: "shfmt",
			Args:    []string{"--filename", "${FILE}", "-"},
			Install: "brew install shfmt",
			Adjust:  shfmtAdjust,
		},
	)
}

// shfmtAdjust prepends the option-derived flags: `-i 0` for tabs or the
// effective indent width for spaces, and `-ln <dialect>` from the shebang.
func shfmtAdjust(req format.Request, argv []string) []string {
	indent := "0" // shfmt: 0 = tabs
	if req.Options.UseSpaces {
		w := req.Options.TabWidth
		if w <= 0 {
			w = 4
		}
		indent = strconv.Itoa(w)
	}
	flags := []string{"-i", indent}
	if d := shellDialect(req.Lines); d != "" {
		flags = append(flags, "-ln", d)
	}
	return append(flags, argv...)
}

// shellDialect maps the shebang interpreter onto shfmt's -ln values; ""
// leaves the choice to shfmt (filename-based detection).
func shellDialect(lines []string) string {
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "#!") {
		return ""
	}
	shebang := lines[0]
	base := shebang[2:]
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	fields := strings.Fields(base)
	if len(fields) == 0 {
		return ""
	}
	interp := fields[0]
	if interp == "env" && len(fields) > 1 {
		interp = fields[1]
	}
	switch interp {
	case "bash":
		return "bash"
	case "sh", "dash":
		return "posix"
	case "mksh":
		return "mksh"
	}
	return ""
}
