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
// its shape in "type" — "ok", "error", "hello", "challenge", "paired" or
// "status".

// Request is what a client sends. Only cmd is required; the other fields
// belong to particular commands and are ignored elsewhere.
type Request struct {
	// Cmd is the command: hello, ping, pair, auth, open, unpair, status.
	Cmd string `json:"cmd"`
	// Token authenticates a paired client. It may ride on any request; once
	// a connection has presented a valid token it stays authenticated.
	Token string `json:"token,omitempty"`
	// Client is the self-chosen device name given with pair (and hello).
	Client string `json:"client,omitempty"`
	// Code is the pairing guess, the six digits as a string ("481936");
	// CodeText is the same thing under the name earlier clients used.
	Code     string `json:"code,omitempty"`
	CodeText string `json:"code_text,omitempty"`
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
	// Type is the response shape: ok, error, hello, challenge, paired,
	// status.
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

	// challenge: Kind names the code shape ("pin"), Length the number of
	// positions, Alphabet the symbols allowed in each.
	Reason    string    `json:"reason,omitempty"`
	ExpiresIn int       `json:"expires_in,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Length    int       `json:"length,omitempty"`
	Alphabet  *Alphabet `json:"alphabet,omitempty"`

	// paired
	Token    string `json:"token,omitempty"`
	ClientID string `json:"client_id,omitempty"`

	// ok (open): the link as it was handed to the IDE.
	// status: a ready-to-send ike://open link for what IKE currently shows.
	Link string `json:"link,omitempty"`

	// status (#2529): what the IDE currently has open. Project and Root
	// (and Link) are always set; Remote is absent for a project without a
	// git remote, and File / Line / Col are absent while no editor is
	// focused.
	Project string `json:"project,omitempty"`
	Root    string `json:"root,omitempty"`
	Remote  string `json:"remote,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
}

// Status is the IDE state a status request reports. The IDE hands the
// server a getter (Options.State) that fills it from the live model; an
// empty Root means "nothing to report" and answers unavailable.
type Status struct {
	// Project is the project root's directory name — the plain name
	// ike://open?project= accepts.
	Project string
	// Root is the absolute project root path.
	Root string
	// Remote is the normalised key (deeplink.NormalizeRemote) of the root's
	// origin/first remote; "" when the project has no git remote.
	Remote string
	// File is the active editor's path relative to Root; "" when no editor
	// is focused (or its buffer has no file yet).
	File string
	// Line and Col are the active editor's 1-based cursor; 0 with File "".
	Line, Col int
}

// statusResponse renders a status snapshot, link included.
func statusResponse(st Status) Response {
	return Response{
		Type:    "status",
		Project: st.Project,
		Root:    st.Root,
		Remote:  st.Remote,
		File:    st.File,
		Line:    st.Line,
		Col:     st.Col,
		Link:    LinkFromStatus(st),
	}
}

// LinkFromStatus renders a status snapshot as the ike://open URL another
// IKE can consume unchanged: the remote when there is one (the normalised
// key is spelled as an https:// URL so the strict grammar accepts it and
// normalises it right back), else the project name, plus file:line when an
// editor is focused. "" when the snapshot names neither.
func LinkFromStatus(st Status) string {
	q := url.Values{}
	switch {
	case st.Remote != "":
		q.Set("remote", "https://"+st.Remote)
	case st.Project != "":
		q.Set("project", st.Project)
	default:
		return ""
	}
	if st.File != "" {
		file := st.File
		if st.Line > 0 && !hasLineSuffix(file) {
			file += ":" + strconv.Itoa(st.Line)
		}
		q.Set("file", file)
	}
	return "ike://open?" + q.Encode()
}

// Error codes carried in Response.Error.
const (
	CodeBadRequest   = "bad_request"  // unparseable line or unknown command
	CodeUnauthorized = "unauthorized" // no valid token on a guarded command
	CodeInvalidLink  = "invalid_link" // the open request does not form a valid ike:// link
	CodeBlocked      = "blocked"      // the address is blocked after misses or a refusal
	CodeNoChallenge  = "no_challenge" // a guess arrived while no code was live
	CodeTooLarge     = "too_large"    // the line exceeded the size cap
	CodeUnavailable  = "unavailable"  // the IDE state a command reports is not available
	CodeInternal     = "internal"     // token store failure and the like
)

// errorResponse builds a type=error response.
func errorResponse(code, msg string) Response {
	return Response{Type: "error", Error: code, Message: msg}
}

// challengeResponse renders a live challenge for the client: the reason,
// the seconds left and the code shape (kind, length, alphabet) to build an
// input UI from. The code itself, naturally, is not part of it.
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
		Kind:      CodeKind,
		Length:    CodeLength,
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
		return "read the PIN off IKE's popup and send it back with cmd=pair"
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
