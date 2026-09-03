package nbview

// notebook.go is the nbformat 4 model (#2425): the parse of an .ipynb file
// into cells and outputs, with nothing in it that knows about rendering. It
// is the shape a later edit mode would mutate, which is why sources are kept
// as plain strings rather than the wire format's line arrays and why every
// output carries its decoded payload instead of the raw MIME bundle.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Cell types as nbformat spells them.
const (
	CellMarkdown = "markdown"
	CellCode     = "code"
	CellRaw      = "raw"
)

// Output types as nbformat spells them.
const (
	OutStream        = "stream"
	OutExecuteResult = "execute_result"
	OutDisplayData   = "display_data"
	OutError         = "error"
)

// Notebook is one parsed .ipynb document.
type Notebook struct {
	// Lang is the notebook's programming language, from
	// metadata.language_info.name, falling back to metadata.kernelspec.
	// language and then to the empty string — code cells then render plain.
	Lang string
	// LangExt is metadata.language_info.file_extension without the dot
	// ("py"), the extension an "open cell in a scratch" allocates under. It
	// falls back to the registered extension of Lang, then to "txt".
	LangExt string
	// Format is "<nbformat>.<nbformat_minor>", shown in the footer.
	Format string
	Cells  []Cell
}

// Cell is one notebook cell. Source is the joined source text without a
// trailing newline; the wire format's line-array and plain-string spellings
// both land here.
type Cell struct {
	Type string
	// Source is the cell's text. Markdown cells render it through the
	// markdown renderer, code cells highlight it under the notebook language.
	Source string
	// ExecCount is the code cell's execution count, 0 when the cell has never
	// run (JSON null) or is not a code cell.
	ExecCount int
	Outputs   []Output
}

// Output is one rendered cell output, already reduced to what the viewer
// shows: text, an image, or an error triple. The MIME bundle's richer
// alternatives are resolved at parse time (image over HTML over plain text),
// so rendering never inspects the wire format again.
type Output struct {
	Type string
	// Name is a stream's channel, "stdout" or "stderr".
	Name string
	// Text is the output's textual body: the stream text, the text/plain
	// representation of a result, or the text degraded out of a text/html
	// one. Empty when the output is an image or an error.
	Text string
	// FromHTML marks Text as the degraded rendering of a text/html output,
	// which the viewer labels so nobody mistakes it for the real thing.
	FromHTML bool
	// ExecCount is an execute_result's own execution count, 0 when absent.
	ExecCount int
	// Image holds the decoded bytes of an image/png or image/jpeg output,
	// nil when the output carries no image; MIME names which it was.
	Image []byte
	MIME  string
	// Error fields of an error output.
	Ename     string
	Evalue    string
	Traceback []string
}

// HasImage reports whether the output carries decodable image bytes.
func (o Output) HasImage() bool { return len(o.Image) > 0 }

// ImageExt is the file extension an image output saves under, "" when the
// output holds no image.
func (o Output) ImageExt() string {
	switch o.MIME {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	}
	return ""
}

// wire mirrors the nbformat 4 JSON just closely enough to parse it. Every
// field the viewer does not show is left out on purpose: a notebook carrying
// unknown keys still parses, which is the whole point of a read-only viewer.
type wire struct {
	NBFormat      int             `json:"nbformat"`
	NBFormatMinor int             `json:"nbformat_minor"`
	Metadata      wireMeta        `json:"metadata"`
	Cells         []wireCell      `json:"cells"`
	Worksheets    json.RawMessage `json:"worksheets"`
}

type wireMeta struct {
	LanguageInfo struct {
		Name string `json:"name"`
		Ext  string `json:"file_extension"`
	} `json:"language_info"`
	KernelSpec struct {
		Language string `json:"language"`
	} `json:"kernelspec"`
}

type wireCell struct {
	CellType  string       `json:"cell_type"`
	Source    multiline    `json:"source"`
	ExecCount *int         `json:"execution_count"`
	Outputs   []wireOutput `json:"outputs"`
}

type wireOutput struct {
	OutputType string                     `json:"output_type"`
	Name       string                     `json:"name"`
	Text       multiline                  `json:"text"`
	Data       map[string]json.RawMessage `json:"data"`
	ExecCount  *int                       `json:"execution_count"`
	Ename      string                     `json:"ename"`
	Evalue     string                     `json:"evalue"`
	Traceback  []string                   `json:"traceback"`
}

// multiline decodes nbformat's "string or array of strings" fields. The array
// spelling is the common one (a notebook stores one element per line, newline
// included); a plain string is equally legal and some tools write it.
type multiline string

// UnmarshalJSON implements json.Unmarshaler for the two legal spellings. A
// null or any other shape decodes to the empty string rather than failing the
// whole notebook — one odd field must not cost the user the document.
func (m *multiline) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*m = multiline(s)
		return nil
	}
	var parts []string
	if err := json.Unmarshal(data, &parts); err == nil {
		*m = multiline(strings.Join(parts, ""))
		return nil
	}
	*m = ""
	return nil
}

// Parse reads an .ipynb document. A JSON syntax error is returned verbatim
// (the viewer shows it and points at "open as text"); a well-formed JSON
// document that is not an nbformat 4 notebook is refused with its own reason
// rather than rendered as an empty notebook.
func Parse(data []byte) (Notebook, error) {
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return Notebook{}, err
	}
	if w.Cells == nil {
		if len(w.Worksheets) > 0 {
			return Notebook{}, fmt.Errorf("nbformat %d notebooks (worksheets) are not supported — only nbformat 4", w.NBFormat)
		}
		return Notebook{}, fmt.Errorf("not a Jupyter notebook: no \"cells\" array")
	}
	nb := Notebook{
		Lang:    w.Metadata.LanguageInfo.Name,
		LangExt: strings.TrimPrefix(w.Metadata.LanguageInfo.Ext, "."),
		Format:  fmt.Sprintf("%d.%d", w.NBFormat, w.NBFormatMinor),
	}
	if nb.Lang == "" {
		nb.Lang = w.Metadata.KernelSpec.Language
	}
	nb.Cells = make([]Cell, 0, len(w.Cells))
	for _, wc := range w.Cells {
		nb.Cells = append(nb.Cells, cellOf(wc))
	}
	return nb, nil
}

// cellOf converts one wire cell.
func cellOf(wc wireCell) Cell {
	c := Cell{Type: wc.CellType, Source: strings.TrimRight(string(wc.Source), "\n")}
	if c.Type == "" {
		c.Type = CellRaw
	}
	if wc.ExecCount != nil {
		c.ExecCount = *wc.ExecCount
	}
	for _, wo := range wc.Outputs {
		if o, ok := outputOf(wo); ok {
			c.Outputs = append(c.Outputs, o)
		}
	}
	return c
}

// outputOf converts one wire output, resolving its MIME bundle. Preference is
// image over plain text over degraded HTML: the pixels are the output the
// notebook author saw, and text/plain is the representation Jupyter itself
// falls back to, so HTML only wins where nothing else exists. An output the
// viewer can show nothing of is dropped.
func outputOf(wo wireOutput) (Output, bool) {
	o := Output{Type: wo.OutputType, Name: wo.Name}
	if wo.ExecCount != nil {
		o.ExecCount = *wo.ExecCount
	}
	switch wo.OutputType {
	case OutStream:
		o.Text = strings.TrimRight(string(wo.Text), "\n")
		return o, o.Text != ""
	case OutError:
		o.Ename, o.Evalue, o.Traceback = wo.Ename, wo.Evalue, wo.Traceback
		return o, o.Ename != "" || o.Evalue != "" || len(o.Traceback) > 0
	case OutExecuteResult, OutDisplayData:
		for _, mime := range []string{"image/png", "image/jpeg"} {
			if raw, ok := wo.Data[mime]; ok {
				if img := decodeB64(raw); len(img) > 0 {
					o.Image, o.MIME = img, mime
					return o, true
				}
			}
		}
		if raw, ok := wo.Data["text/plain"]; ok {
			o.Text = strings.TrimRight(bundleText(raw), "\n")
			if o.Text != "" {
				return o, true
			}
		}
		if raw, ok := wo.Data["text/html"]; ok {
			o.Text, o.FromHTML = strings.TrimRight(htmlToText(bundleText(raw)), "\n"), true
			return o, o.Text != ""
		}
	}
	return o, false
}

// bundleText decodes one MIME-bundle entry as text, in either the array or
// the plain-string spelling.
func bundleText(raw json.RawMessage) string {
	var ml multiline
	_ = ml.UnmarshalJSON(raw)
	return string(ml)
}

// decodeB64 decodes an image bundle entry: base64 in either spelling, with
// the line breaks notebooks wrap it at removed before decoding.
func decodeB64(raw json.RawMessage) []byte {
	s := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, bundleText(raw))
	if s == "" {
		return nil
	}
	out, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return out
}
