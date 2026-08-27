package app

import (
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/pathcomplete"
	"ike/internal/ui"
)

// http_save.go writes the shown response's body to a file (#2059).
//
// Copying a body to the clipboard (#1266) hands over what the viewer *shows*
// — pretty-printed, folded, and text only. A download, a PDF or a captured
// fixture needs the opposite: the raw bytes exactly as they arrived. So
// http.saveResponse (palette, "S" in the focused viewer) opens a path prompt
// prefilled with a name derived from the request URL and the Content-Type,
// and writes httpclient.Response.Body verbatim — binary included.

// HTTPSaveResponseMsg runs http.saveResponse: prompt for a path and write the
// shown response body there.
type HTTPSaveResponseMsg struct{}

// startHTTPSaveResponse opens the path prompt. It refuses early — like the
// curl import (#1994) — when there is nothing to write, so a path is never
// typed for nothing.
func (m *Model) startHTTPSaveResponse() {
	resp, ok := m.httpResponseToSave()
	if !ok {
		return
	}
	m.httpSaveOpen = true
	m.httpSaveInput = httpResponseFileName(resp)
	m.httpSavePos = len([]rune(m.httpSaveInput))
	m.renderHTTPSavePrompt(nil)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// httpResponseToSave returns the response whose body a save would write, and
// says why there is none otherwise.
func (m *Model) httpResponseToSave() (*httpclient.Response, bool) {
	p := m.httpPanel()
	if p == nil {
		m.host.Notify(host.Info, "http: no response pane open")
		return nil, false
	}
	resp := p.CurrentResponse()
	if resp == nil {
		// A live stream has no finished body yet (#1776); an empty pane has
		// none at all.
		m.host.Notify(host.Info, "http: no response to save yet")
		return nil, false
	}
	if resp.BodyBytes() == 0 {
		m.host.Notify(host.Info, "http: the response has an empty body")
		return nil, false
	}
	return resp, true
}

// httpSavePromptOpen reports whether the shell shows the save prompt.
func (m Model) httpSavePromptOpen() bool { return m.httpSaveOpen && m.shell.IsOpen() }

// renderHTTPSavePrompt (re)fills the shell for the current input; candidates
// (from the last tab press) render underneath, as in the other path prompts.
func (m *Model) renderHTTPSavePrompt(candidates []string) {
	line := "> " + ui.CursorView(m.httpSaveInput, m.httpSavePos)
	const maxLines = 8
	var sug string
	if n := len(candidates); n > 0 {
		shown := candidates
		if n > maxLines {
			shown = candidates[:maxLines]
		}
		sug = "\n\n  " + strings.Join(shown, "\n  ")
		if n > maxLines {
			sug += fmt.Sprintf("\n  … +%d more", n-maxLines)
		}
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Save response body to file",
		Body: func() string {
			return line + sug + "\n\nrelative to the project root · tab complete · enter save · esc cancel"
		},
	})
}

// updateHTTPSavePrompt consumes every key while the save prompt is open: tab
// completes the path, everything else is shared line editing (ui.EditKey).
func (m Model) updateHTTPSavePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.httpSaveOpen = false
		m.httpSaveInput = ""
		m.httpSavePos = 0
		m.shell.Close()
	}
	var candidates []string
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		target := strings.TrimSpace(m.httpSaveInput)
		closePrompt()
		if target == "" {
			return m, nil
		}
		m.saveHTTPResponseBody(target)
		return m, nil
	case msg.Code == tea.KeyTab:
		res := pathcomplete.Complete(m.httpSaveInput)
		m.httpSaveInput = res.Completed
		m.httpSavePos = len([]rune(m.httpSaveInput))
		candidates = res.Candidates
	default:
		if out, pos, handled, _ := ui.EditKey(msg, m.httpSaveInput, m.httpSavePos); handled {
			m.httpSaveInput, m.httpSavePos = out, pos
		}
	}
	m.renderHTTPSavePrompt(candidates)
	return m, nil
}

// pasteHTTPSavePrompt inserts a paste into the path input at its cursor
// (#1873), like every other single-field prompt.
func (m *Model) pasteHTTPSavePrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.httpSaveInput, m.httpSavePos, strings.TrimSpace(text))
	if !changed {
		return false
	}
	m.httpSaveInput, m.httpSavePos = out, pos
	m.renderHTTPSavePrompt(nil)
	return true
}

// saveHTTPResponseBody writes the raw body to target, reporting both
// outcomes. The response is re-read here rather than captured when the prompt
// opened: what lands in the file is the body on show when the path is
// confirmed.
func (m *Model) saveHTTPResponseBody(target string) {
	resp, ok := m.httpResponseToSave()
	if !ok {
		return
	}
	dest := httpSavePath(target, httpResponseFileName(resp))
	written, err := writeResponseBody(resp, dest)
	if err != nil {
		m.host.Notify(host.Error, "http: save failed — "+err.Error())
		return
	}
	notice := fmt.Sprintf("http: wrote %s to %s", byteCountLabel(written), displayPath(dest))
	level := host.Info
	if resp.Truncated {
		// The body was cut on receipt (MaxBodyBytes), so the file is short
		// too — only the response pane says so otherwise.
		notice += " — body was truncated on receipt"
		level = host.Warn
	}
	m.host.Notify(level, notice)
}

// writeResponseBody writes the raw body to dest and reports how many bytes
// landed there. A spooled body (#2157) is *streamed* off its file rather than
// read into memory first — the save exists precisely for the responses too
// large to hold, so pulling one back in to write it out would defeat it.
func writeResponseBody(resp *httpclient.Response, dest string) (int64, error) {
	src, err := resp.BodyReader()
	if err != nil {
		return 0, err
	}
	defer src.Close()
	out, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return n, copyErr
	}
	return n, closeErr
}

// httpSavePath resolves the typed target: "~" expands, a relative path is
// project-relative (IKE runs in the project root), and a directory receives
// the proposed file name rather than failing the write.
func httpSavePath(target, fallbackName string) string {
	dest := expandHome(target)
	dir := strings.HasSuffix(dest, string(os.PathSeparator))
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(projectRoot(), dest)
	}
	if st, err := os.Stat(dest); dir || (err == nil && st.IsDir()) {
		dest = filepath.Join(dest, fallbackName)
	}
	return dest
}

// byteCountLabel renders a body size the way a notification should read.
func byteCountLabel(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}

// httpResponseFileName proposes a file name for a response body: the last
// segment of the request URL, extended by the Content-Type's extension when
// the URL carries none. Without a usable URL the name falls back to
// "response", so the prompt always offers something writable.
func httpResponseFileName(resp *httpclient.Response) string {
	name := "response"
	if resp.Request != nil {
		if base := urlFileName(resp.Request.URL); base != "" {
			name = base
		}
	}
	if path.Ext(name) != "" {
		return name
	}
	return name + contentTypeExt(resp.Headers.Get("Content-Type"))
}

// urlFileName is the last path segment of a URL, without query or fragment;
// "" when the URL has none ("https://api.example.com/" or an unparsable one).
func urlFileName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(strings.TrimSuffix(u.EscapedPath(), "/"))
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if unescaped, err := url.PathUnescape(base); err == nil {
		base = unescaped
	}
	// A path segment may legally hold characters no file name should carry.
	return strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, base)
}

// contentTypeExt maps a Content-Type onto a file extension, "" when nothing
// sensible is known. The common web types are spelled out — the system mime
// database is not guaranteed to carry them, and answers "text/plain" with
// ".asc" where it does — and everything else falls back to that database.
func contentTypeExt(ct string) string {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	}
	switch mt {
	case "":
		return ""
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	case "text/html", "application/xhtml+xml":
		return ".html"
	case "text/csv":
		return ".csv"
	case "application/xml", "text/xml":
		return ".xml"
	case "application/javascript", "text/javascript":
		return ".js"
	case "text/css":
		return ".css"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "image/jpeg":
		return ".jpg"
	case "image/svg+xml":
		return ".svg"
	case "application/octet-stream":
		return ".bin"
	}
	// Structured syntax suffixes (application/problem+json, …) follow their
	// base format.
	if strings.HasSuffix(mt, "+json") {
		return ".json"
	}
	if strings.HasSuffix(mt, "+xml") {
		return ".xml"
	}
	if exts, err := mime.ExtensionsByType(mt); err == nil && len(exts) > 0 {
		// Sorted by the standard library, so the proposal stays stable.
		return exts[0]
	}
	return ""
}
