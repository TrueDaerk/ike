package datasrc

import "strings"

// MissingToolError reports that a backend needs an external program that is
// not installed — the DuckDB engine (#1765) drives the `duckdb` CLI, and the
// Parquet viewer (#1766) rides the same seam. It is an actionable state, not
// a plain failure: the pane renders it as a centered dialog naming the tool
// and how to install it, rather than a one-line notice.
type MissingToolError struct {
	// Tool is the program that was looked for ("duckdb").
	Tool string
	// Why explains in one line what the tool is needed for.
	Why string
	// Hints are the install commands or URLs shown in the dialog, in order.
	Hints []string
}

func (e *MissingToolError) Error() string {
	msg := e.Tool + " not found on PATH"
	if e.Why != "" {
		msg += ": " + e.Why
	}
	if len(e.Hints) > 0 {
		msg += " (" + strings.Join(e.Hints, "; ") + ")"
	}
	return msg
}
