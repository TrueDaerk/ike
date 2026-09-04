package netlink

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ike/internal/deeplink"
)

// protocol.go is the wire format: one JSON object per line in each
// direction. Every request names a command in "cmd"; every response names
// its shape in "type" — "ok", "error", "hello", "challenge" or "paired".

// Request is what a client sends. Only cmd is required; the other fields
// belong to particular commands and are ignored elsewhere.
type Request struct {
	// Cmd is the command: hello, ping, pair, auth, open, unpair.
	Cmd string `json:"cmd"`
	// Token authenticates a paired client. It may ride on any request; once
	// a connection has presented a valid token it stays authenticated.
	Token string `json:"token,omitempty"`
	// Client is the self-chosen device name given with pair (and hello).
	Client string `json:"client,omitempty"`
	// Code is the pairing guess as glyphs; CodeText is the compact
	// "spade:red heart:black …" alternative for hand-typed clients.
	Code     []Glyph `json:"code,omitempty"`
	CodeText string  `json:"code_text,omitempty"`
	// URL is a complete ike:// link for open. Alternatively the link's
	// parts: exactly one of Project / Remote, plus optional File (with or
	// without ":line"), Line and Tool.
	URL     string `json:"url,omitempty"`
	Project string `json:"project,omitempty"`
	Remote  string `json:"remote,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Tool    string `json:"tool,omitempty"`
}

// Response is what the server answers.
type Response struct {
	// Type is the response shape: ok, error, hello, challenge, paired.
	Type string `json:"type"`
	// Error is a stable machine-readable code (type error only); Message is
	// the human-readable detail, present on errors and on informational
	// responses.
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`

	// hello
	Name          string `json:"name,omitempty"`
	Version       string `json:"version,omitempty"`
	Authenticated *bool  `json:"authenticated,omitempty"`

	// challenge
	Reason    string    `json:"reason,omitempty"`
	ExpiresIn int       `json:"expires_in,omitempty"`
	Alphabet  *Alphabet `json:"alphabet,omitempty"`

	// paired
	Token    string `json:"token,omitempty"`
	ClientID string `json:"client_id,omitempty"`

	// ok (open): the link as it was handed to the IDE.
	Link string `json:"link,omitempty"`
}

// Error codes carried in Response.Error.
const (
	CodeBadRequest   = "bad_request"  // unparseable line or unknown command
	CodeUnauthorized = "unauthorized" // no valid token on a guarded command
	CodeInvalidLink  = "invalid_link" // the open request does not form a valid ike:// link
	CodeBlocked      = "blocked"      // the address is blocked after misses or a refusal
	CodeNoChallenge  = "no_challenge" // a guess arrived while no code was live
	CodeTooLarge     = "too_large"    // the line exceeded the size cap
	CodeInternal     = "internal"     // token store failure and the like
)

// errorResponse builds a type=error response.
func errorResponse(code, msg string) Response {
	return Response{Type: "error", Error: code, Message: msg}
}

// challengeResponse renders a live challenge for the client: the reason,
// the seconds left and the alphabet to build an input UI from. The code
// itself, naturally, is not part of it.
func challengeResponse(c Challenge, now time.Time) Response {
	alpha := DefaultAlphabet()
	left := int(c.Expires.Sub(now).Round(time.Second) / time.Second)
	if left < 0 {
		left = 0
	}
	return Response{
		Type:      "challenge",
		Reason:    c.Reason,
		ExpiresIn: left,
		Alphabet:  &alpha,
		Message:   challengeMessage(c.Reason),
	}
}

// challengeMessage is the human-readable line for a challenge reason.
func challengeMessage(reason string) string {
	switch reason {
	case "wrong":
		return "wrong code — a new one is shown in IKE, try again"
	case "expired":
		return "the code expired — a new one is shown in IKE"
	default:
		return "read the code off IKE's popup and send it back with cmd=pair"
	}
}

// LinkFromRequest turns an open request into the ike:// URL the IDE's link
// pipeline consumes: a given URL is taken verbatim, otherwise the parts are
// assembled. Either way the result is parsed with the strict deeplink
// grammar so a network client can do exactly what a clicked link can — no
// more.
func LinkFromRequest(r Request) (string, error) {
	raw := strings.TrimSpace(r.URL)
	if raw == "" {
		q := url.Values{}
		if r.Project != "" {
			q.Set("project", r.Project)
		}
		if r.Remote != "" {
			q.Set("remote", r.Remote)
		}
		if r.File != "" {
			file := r.File
			if r.Line > 0 && !hasLineSuffix(file) {
				file += ":" + strconv.Itoa(r.Line)
			}
			q.Set("file", file)
		}
		if r.Tool != "" {
			q.Set("tool", r.Tool)
		}
		if len(q) == 0 {
			return "", fmt.Errorf("open needs url, or project/remote with optional file/line/tool")
		}
		raw = "ike://open?" + q.Encode()
	}
	if _, err := deeplink.Parse(raw); err != nil {
		return "", err
	}
	return raw, nil
}

// hasLineSuffix reports whether file already ends in ":<digits>".
func hasLineSuffix(file string) bool {
	i := strings.LastIndexByte(file, ':')
	if i < 0 || i == len(file)-1 {
		return false
	}
	for _, r := range file[i+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
