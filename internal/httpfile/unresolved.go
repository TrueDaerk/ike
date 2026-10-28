package httpfile

import "strings"

// UnresolvedError reports placeholders that had no value (#3086). It is a
// type rather than plain text so the dispatcher can classify the failure
// (telemetry reason `placeholder`) and the response pane can name the
// placeholder in a human message.
type UnresolvedError struct {
	Names []string // sorted, deduplicated placeholder names
}

func (e *UnresolvedError) Error() string {
	return "unresolved placeholders: " + strings.Join(e.Names, ", ")
}

// resolveError is ResolveVars' failure: the request-keyed text the user has
// always seen, with the merged UnresolvedError reachable via errors.As.
type resolveError struct {
	msg        string
	unresolved *UnresolvedError
}

func (e *resolveError) Error() string { return e.msg }

func (e *resolveError) Unwrap() error {
	if e.unresolved == nil || len(e.unresolved.Names) == 0 {
		return nil
	}
	return e.unresolved
}
