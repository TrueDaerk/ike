package dbgp

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// protocol.go holds the typed slices of the DBGp XML vocabulary IKE reads.
// Only attributes the client uses are declared; unknown attributes and
// elements are ignored by encoding/xml.

// Init is the engine's first packet after connecting.
type Init struct {
	XMLName         xml.Name `xml:"init"`
	FileURI         string   `xml:"fileuri,attr"`
	IDEKey          string   `xml:"idekey,attr"`
	Language        string   `xml:"language,attr"`
	ProtocolVersion string   `xml:"protocol_version,attr"`
	AppID           string   `xml:"appid,attr"`
}

// Response is one command response envelope. Continuation responses carry
// Status/Reason; command-specific payloads land in the slice fields.
type Response struct {
	XMLName xml.Name `xml:"response"`
	Command string   `xml:"command,attr"`
	TID     int      `xml:"transaction_id,attr"`
	Status  string   `xml:"status,attr"` // starting|running|break|stopping|stopped
	Reason  string   `xml:"reason,attr"` // ok|error|aborted|exception

	// ID is set on breakpoint_set responses.
	ID string `xml:"id,attr"`

	Err        *Error        `xml:"error"`
	Properties []Property    `xml:"property"`
	Stack      []StackEntry  `xml:"stack"`
	Contexts   []ContextName `xml:"context"`
	// Message carries the break location on stopped responses
	// (xdebug:message; the tag matches by local name across namespaces).
	Message *BreakMessage `xml:"message"`
}

// Error is a DBGp command error (`<error code="N"><message>…`).
type Error struct {
	Code    int    `xml:"code,attr"`
	Message string `xml:"message"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("dbgp: command error %d", e.Code)
	}
	return fmt.Sprintf("dbgp: %s (code %d)", e.Message, e.Code)
}

// BreakMessage is Xdebug's break-location extension on continuation
// responses (`<xdebug:message filename="file://…" lineno="12"/>`).
type BreakMessage struct {
	Filename  string `xml:"filename,attr"`
	Lineno    int    `xml:"lineno,attr"`
	Exception string `xml:"exception,attr"`
	Text      string `xml:",chardata"`
}

// StackEntry is one frame of a stack_get response; Lineno is 1-based.
type StackEntry struct {
	Level    int    `xml:"level,attr"`
	Type     string `xml:"type,attr"`
	Where    string `xml:"where,attr"`
	Filename string `xml:"filename,attr"` // file:// URI
	Lineno   int    `xml:"lineno,attr"`
}

// ContextName is one variable context of a frame (Locals, Superglobals, …).
type ContextName struct {
	Name string `xml:"name,attr"`
	ID   int    `xml:"id,attr"`
}

// Property is one variable (context_get/property_get); structured values
// nest children, possibly paged.
type Property struct {
	Name        string `xml:"name,attr"`
	Fullname    string `xml:"fullname,attr"`
	Type        string `xml:"type,attr"`
	ClassName   string `xml:"classname,attr"`
	Encoding    string `xml:"encoding,attr"`
	Size        int    `xml:"size,attr"`
	HasChildren int    `xml:"children,attr"`
	NumChildren int    `xml:"numchildren,attr"`
	Page        int    `xml:"page,attr"`
	PageSize    int    `xml:"pagesize,attr"`

	Raw      string     `xml:",chardata"`
	Children []Property `xml:"property"`
}

// Value returns the decoded scalar value: base64 per the Encoding
// attribute, raw text otherwise. Structured values return "".
func (p Property) Value() string {
	raw := strings.TrimSpace(p.Raw)
	if p.Encoding == "base64" {
		if dec, err := base64.StdEncoding.DecodeString(raw); err == nil {
			return string(dec)
		}
	}
	return raw
}

// Stream is an engine stream packet (stdout/stderr when redirected).
type Stream struct {
	XMLName  xml.Name `xml:"stream"`
	Type     string   `xml:"type,attr"` // stdout|stderr
	Encoding string   `xml:"encoding,attr"`
	Raw      string   `xml:",chardata"`
}

// Text returns the decoded stream content.
func (s Stream) Text() string {
	raw := strings.TrimSpace(s.Raw)
	if s.Encoding == "base64" {
		if dec, err := base64.StdEncoding.DecodeString(raw); err == nil {
			return string(dec)
		}
	}
	return raw
}

// ToURI converts an absolute path to the file:// form DBGp speaks.
func ToURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

// FromURI converts a DBGp file:// URI back to a local path; non-file URIs
// (eval://, dbgp://) come back unchanged so callers can detect them.
func FromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}

// parsePacket decodes one engine packet into its typed form: *Init,
// *Response, or *Stream. Unknown packet kinds return (nil, nil) — the
// protocol allows notifications the client does not consume.
func parsePacket(data []byte) (any, error) {
	root := rootElement(data)
	switch root {
	case "init":
		var init Init
		if err := xml.Unmarshal(data, &init); err != nil {
			return nil, err
		}
		return &init, nil
	case "response":
		var resp Response
		if err := xml.Unmarshal(data, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	case "stream":
		var st Stream
		if err := xml.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		return &st, nil
	default:
		return nil, nil
	}
}

// rootElement returns the local name of the document's root element.
func rootElement(data []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}
