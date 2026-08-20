package httpclient

// capture.go evaluates a request's `# @capture name = <jq-expr>` directives
// (#1993) against the response that just arrived: the declarative answer to
// "start an async job, then poll it by the id the first response returned".
// Parsing the directives is the file layer's job (httpfile.Capture); running
// them needs a response, so it happens here, right after one exists.
//
// A capture never fails the exchange. The response is already on the wire and
// worth reading; a directive that matched nothing turns into a warning next
// to the response and — via the app layer — into an inline marker on the
// directive's own line.

import (
	"bytes"
	"errors"
	"fmt"

	"ike/internal/httpfile"
	"ike/internal/jqplay"
)

// CaptureResult is the outcome of one capture directive (#1993): the
// directive itself (name, expression and its 1-based line in the .http file),
// plus either the captured Value or the Err explaining why there is none.
// Exactly one of the two is set.
type CaptureResult struct {
	httpfile.Capture
	Value string
	Err   string
}

// OK reports whether the directive produced a value.
func (c CaptureResult) OK() bool { return c.Err == "" }

// CapturedValues collects the successful captures of a response as the
// name→value map the variable chain takes (httpfile.Vars.Captured). A name
// captured twice in one request takes its last value, the way a
// re-assignment reads.
func (r *Response) CapturedValues() map[string]string {
	if r == nil || len(r.Captures) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, c := range r.Captures {
		if c.OK() {
			out[c.Name] = c.Value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyCaptures runs req's capture directives against resp, filling
// resp.Captures and adding a warning per failed directive. Called for the
// dispatch paths that know the .http request; a re-send (#1832) has no
// directives — it repeats a stored snapshot, not a parsed request — and
// therefore captures nothing.
func applyCaptures(resp *Response, req *httpfile.Request) {
	if resp == nil || req == nil || len(req.Captures) == 0 {
		return
	}
	resp.Captures = runCaptures(req.Captures, resp.Body)
	for _, c := range resp.Captures {
		if !c.OK() {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("capture %s: %s", c.Name, c.Err))
		}
	}
}

// runCaptures evaluates every directive against the response body. Each is
// independent: one broken expression does not stop the others, which is what
// lets a request capture three values and only complain about the one that
// changed shape.
func runCaptures(captures []httpfile.Capture, body []byte) []CaptureResult {
	out := make([]CaptureResult, 0, len(captures))
	for _, c := range captures {
		res := CaptureResult{Capture: c}
		value, err := captureValue(c.Expr, body)
		if err != nil {
			res.Err = err.Error()
		} else {
			res.Value = value
		}
		out = append(out, res)
	}
	return out
}

// captureValue evaluates one jq expression against the response body,
// phrasing every failure in terms of the *response* — the thing the author of
// the directive is looking at.
func captureValue(expr string, body []byte) (string, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return "", errors.New("the response body is empty")
	}
	value, err := jqplay.EvaluateRaw(expr, string(body))
	switch {
	case err == nil:
		return value, nil
	case errors.Is(err, jqplay.ErrNoValue):
		return "", errors.New("the expression matched no value in the response body")
	default:
		var input *jqplay.InputError
		if errors.As(err, &input) {
			return "", fmt.Errorf("the response body is not JSON: %s", input.Detail)
		}
		return "", err
	}
}
