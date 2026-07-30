package langsql

import (
	"context"
	"strings"

	"ike/internal/config"
	"ike/internal/format"
)

// provider.go registers the built-in SQL formatter with the formatter
// registry (Roadmap 0470, #1403). It deliberately sits ABOVE the LSP tier:
// the 0470 chain puts built-ins last, but for SQL the spec pins the built-in
// over sqls — sqls must be installed and only reformats at statement level.
// `[format.sql] builtin = false` restores the LSP path; `[format.sql]` with a
// command still overrides everything (TierOverride).

func init() {
	format.Register(format.Provider{
		Name:      "built-in",
		Languages: []string{"sql"},
		Tier:      format.TierExternal,
		Available: func(path string) bool { return format.BuiltinEnabled("sql") },
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			out, err := formatSQL(strings.Join(req.Lines, "\n"), sqlOptionsFor(req.Options))
			if err != nil {
				return format.Result{}, err
			}
			return format.TextResult(out), nil
		},
		FormatRange: func(ctx context.Context, req format.Request, start, end format.Pos) (format.Result, error) {
			last := end.Line
			if end.Col == 0 && last > start.Line {
				last-- // end-exclusive line-wise selections stop before this line
			}
			out, err := formatRangeSQL(strings.Join(req.Lines, "\n"), start.Line, last, sqlOptionsFor(req.Options))
			if err != nil {
				return format.Result{}, err
			}
			return format.TextResult(out), nil
		},
	})
}

// sqlOptionsFor maps the registry options + [format.sql] config onto the
// formatter's options: indent unit from editorconfig/settings, keyword
// casing from `keywords = "upper" | "lower" | "preserve"` (upper default).
func sqlOptionsFor(o format.Options) sqlOptions {
	indent := "\t"
	if o.UseSpaces {
		w := o.TabWidth
		if w <= 0 {
			w = 4
		}
		indent = strings.Repeat(" ", w)
	}
	kw := caseUpper
	if c := config.Get(); c != nil {
		if raw, ok := c.Format["sql"]; ok {
			switch raw["keywords"] {
			case "lower":
				kw = caseLower
			case "preserve":
				kw = casePreserve
			}
		}
	}
	return sqlOptions{Indent: indent, Case: kw}
}
