package httppane

// redirects.go renders the followed redirect chain (#2716). The pane used to
// show the final answer and nothing else: a 301 → 302 → 200 looked like a
// direct 200 whose timing line reported the accumulated setup of three
// exchanges, with nothing to say where the extra DNS and handshakes came
// from. The block under the status line is that explanation — one step per
// hop, with the method and URL that went out, the status and Location that
// came back, and the connection facts of the hop underneath.
//
// It is a fold like every other foldable row (fold.go): collapsed by default
// once the chain is long enough to push the body off screen, toggled with
// enter, copied whole with y.

import (
	"fmt"
	"net/url"
	"strings"

	"ike/internal/highlight"
	"ike/internal/httpclient"
)

// chainFoldThreshold is how many hops a chain may have before the block
// composes collapsed: up to three steps are worth reading at a glance, a
// longer chain would push the response body off the screen.
const chainFoldThreshold = 3

// redirectGlyph leads every step of the chain — the "and then it went here"
// arrow the block is read by.
const redirectGlyph = "↳"

// minChainURL is the smallest URL column the block will squeeze into, so a
// narrow pane truncates hard rather than composing a column of ellipses.
const minChainURL = 24

// defaultChainWidth stands in for the pane width while none is known yet (a
// response composed before the first SetSize, which tests do).
const defaultChainWidth = 100

// pluralRedirects spells the counter the status line carries; the count is
// *redirects*, one less than the hops, since the chain's last entry is the
// answer rather than a further redirect.
func pluralRedirects(n int) string {
	if n == 1 {
		return "1 redirect"
	}
	return fmt.Sprintf("%d redirects", n)
}

// redirectRows composes the chain block: the header row the fold hangs on,
// then per hop the step line and — when the hop has any — its connection
// line. width is the pane width the URLs are truncated against.
func redirectRows(hops []httpclient.Hop, finalURL string, width int) []row {
	if len(hops) == 0 {
		return nil
	}
	if width <= 0 {
		width = defaultChainWidth
	}
	methodW := 0
	for _, h := range hops {
		methodW = max(methodW, len([]rune(h.Method)))
	}
	// The URL column gets what is left of the pane once the fixed parts of a
	// step line are accounted for: the gutter and glyph, the method column and
	// the " → 301" tail. The Location that follows may still run past the
	// edge — the pane pans sideways for that (#1290) — but the status the eye
	// looks for stays on screen.
	budget := max(minChainURL, width-methodW-14)
	urlW := 0
	shown := make([]string, len(hops))
	for i, h := range hops {
		shown[i] = truncateURL(h.URL, budget)
		urlW = max(urlW, len([]rune(shown[i])))
	}

	header := pluralRedirects(len(hops) - 1)
	if finalURL != "" {
		header += " · ended at " + truncateURL(finalURL, budget)
	}
	rows := []row{{kind: kindRedirects, text: header}}
	for i, h := range hops {
		rows = append(rows, row{kind: kindRedirects, text: stepLine(h, shown[i], methodW, urlW)})
		if line := connLine(h); line != "" {
			rows = append(rows, row{kind: kindRedirects, text: line})
		}
	}
	return rows
}

// stepLine is a hop's first line: what went out, what came back.
func stepLine(h httpclient.Hop, shownURL string, methodW, urlW int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-*s  %-*s  → ", redirectGlyph, methodW, h.Method, urlW, shownURL)
	if h.Status > 0 {
		fmt.Fprintf(&b, "%d", h.Status)
	} else {
		b.WriteString("?")
	}
	if h.Location != "" {
		b.WriteString("  Location: " + h.Location)
	}
	if h.MethodChanged {
		// The 303 (and the de-facto 301/302) POST → GET rewrite drops the body
		// with it, which is why the next hop's answer may look nothing like
		// the one the request was written for.
		b.WriteString("  method changed")
	}
	return strings.TrimRight(b.String(), " ")
}

// connLine is a hop's second line: how the request reached the host. "" when
// httptrace reported nothing for the hop — a response restored from history,
// or a transport that never opened a connection of its own.
func connLine(h httpclient.Hop) string {
	if !h.HasConn() {
		return ""
	}
	var parts []string
	if len(h.DNSAddrs) > 0 {
		host := h.Host()
		if host == "" {
			host = "?"
		}
		parts = append(parts, "dns "+host+" → "+strings.Join(h.DNSAddrs, ", "))
	}
	if h.RemoteAddr != "" {
		parts = append(parts, "connected "+h.RemoteAddr)
	}
	if tls := strings.TrimSpace(h.TLSServerName + " " + h.Proto); tls != "" {
		parts = append(parts, "tls "+tls)
	}
	if h.Reused {
		// A reused connection resolved no name and shook no hands, so its
		// empty facts read as "did not happen" rather than "not measured".
		parts = append(parts, "(reused: yes)")
	}
	return "   " + strings.Join(parts, "   ")
}

// truncateURL shortens a URL to at most n runes with the host always visible:
// the scheme and host stay, the path loses its head. A URL whose host alone
// is already too long keeps the host and drops the rest — knowing *where* the
// hop went matters more than knowing which path it asked for.
func truncateURL(raw string, n int) string {
	r := []rune(raw)
	if n <= 0 || len(r) <= n {
		return raw
	}
	prefix := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		prefix = u.Scheme + "://" + u.Host
	}
	p := []rune(prefix)
	if len(p)+1 >= n {
		return prefix + "…"
	}
	tail := r[len(r)-(n-len(p)-1):]
	return prefix + "…" + string(tail)
}

// setChain records the composed block's fold: the header row and the last
// step row, collapsed up front once the chain is longer than the threshold.
// The fold is kept apart from the body's own (setFolds replaces those with
// every syntax pass) and re-added by setFolds, so a landing highlight pass
// never drops it.
func (m *Model) setChain(header, end int, hops int) {
	m.chainFold = highlight.Fold{HeaderLine: header, EndLine: end}
	m.chainOK = end > header
	if !m.chainOK {
		return
	}
	m.folds = append(m.folds, m.chainFold)
	if hops > chainFoldThreshold {
		if m.folded == nil {
			m.folded = map[int]int{}
		}
		m.folded[header] = end
	}
}

// chainRow is the row the chain block is headed by, -1 when the shown
// response followed no redirect.
func (m *Model) chainRow() int {
	if !m.chainOK {
		return -1
	}
	return m.chainFold.HeaderLine
}

// chainTargeted reports whether the chain block is what the keyboard commands
// act on — the fold rule of fold.go: the one at the top of the view, or the
// first below it. It is what makes enter and y mean "this chain" without the
// pane having a cursor.
func (m *Model) chainTargeted() bool {
	row := m.chainRow()
	return row >= 0 && m.targetFold() == row
}

// ChainText is the whole block as plain text — what "y" puts on the clipboard
// while the chain is the targeted fold, collapsed or not.
func (m *Model) ChainText() string {
	if !m.chainOK {
		return ""
	}
	return m.foldRangeText(m.chainFold.HeaderLine, m.chainFold.EndLine)
}

// RedirectRows reports the row range of the chain block and whether there is
// one at all (tests).
func (m *Model) RedirectRows() (start, end int, ok bool) {
	return m.chainFold.HeaderLine, m.chainFold.EndLine, m.chainOK
}
