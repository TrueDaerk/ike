package langxml

import (
	"context"
	"strings"

	"ike/internal/format"
)

// provider.go registers the built-in XML formatter with the formatter
// registry (Roadmap 0470, #1404) at the built-in tier — XML has no server
// and usually no external tool, so the chain naturally lands here while an
// explicit [format.xml] command or a future external default still wins.
// `[format.xml] builtin = false` switches it off.

func init() {
	// Declares the `builtin` toggle for the Formatters settings page (#1662);
	// XML's built-in reads no extra config keys.
	format.RegisterBuiltin("xml")
	format.Register(format.Provider{
		Name:      "built-in",
		Languages: []string{"xml"},
		Tier:      format.TierBuiltin,
		Available: func(path string) bool { return format.BuiltinEnabled("xml") },
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			out, err := formatXML(strings.Join(req.Lines, "\n"), xmlOptionsFor(req.Options))
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
			out, err := formatRangeXML(strings.Join(req.Lines, "\n"), start.Line, last, xmlOptionsFor(req.Options))
			if err != nil {
				return format.Result{}, err
			}
			return format.TextResult(out), nil
		},
	})
}

// xmlOptionsFor maps registry options onto the printer's: indent unit and
// attribute-wrap width from the buffer's effective settings.
func xmlOptionsFor(o format.Options) xmlOptions {
	indent := "\t"
	w := o.TabWidth
	if w <= 0 {
		w = 4
	}
	if o.UseSpaces {
		indent = strings.Repeat(" ", w)
	}
	return xmlOptions{Indent: indent, TabWidth: w, MaxWidth: o.MaxLineLength}
}
