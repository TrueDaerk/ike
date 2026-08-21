package intention

import (
	"ike/internal/concealfilter"
	"ike/internal/docpath"
	"ike/internal/httpfile"
	"ike/internal/jwt"
)

// catalog.go is the built-in intention catalog (#2020): one provider per
// subsystem, each a pure function over Context that maps caret facts to the
// existing commands. Adding an entry is adding an Item line — the probes and
// the behavior both live elsewhere.

// Builtins returns the built-in providers, in the order their kinds should
// first appear in the popup.
func Builtins() []Provider {
	return []Provider{
		docPathProvider(),
		httpProvider(),
		curlProvider(),
		jwtProvider(),
		concealProvider(),
		diagnosticProvider(),
		vcsProvider(),
		testProvider(),
		editProvider(),
		bufferLangProvider(),
	}
}

// docPathProvider offers the JSON/YAML path actions (#1660) when the caret
// sits on a value: the three copy flavours, and — for buffers jq itself can
// read, so not for YAML — the playground seeded with the caret's path.
func docPathProvider() Provider {
	return Provider{
		ID: "app.docpath",
		Items: func(cx Context) []Item {
			if !cx.DocPath {
				return nil
			}
			items := []Item{
				{Title: "Copy Path as jq Expression", Kind: "copy", CommandID: "editor.copyDocPathJQ"},
				{Title: "Copy Path as yq Expression", Kind: "copy", CommandID: "editor.copyDocPathYQ"},
				{Title: "Copy Path", Kind: "copy", CommandID: "editor.copyDocPath"},
			}
			if !docpath.IsYAML(cx.LangID) {
				items = append(items, Item{Title: "jq Playground at Cursor Path", Kind: "jq", CommandID: "json.jqPlaygroundAtPath"})
			}
			return items
		},
	}
}

// httpProvider offers the request actions when the caret sits inside an
// .http request block (#1131 and friends); the app precomputes the block
// probe (httpfile.RequestAt) into Context.
func httpProvider() Provider {
	return Provider{
		ID: "app.http",
		Items: func(cx Context) []Item {
			if !cx.HTTPRequest {
				return nil
			}
			return []Item{
				{Title: "Run Request", Kind: "http", CommandID: "http.run"},
				{Title: "Copy as curl", Kind: "http", CommandID: "http.copyAsCurl"},
				{Title: "Copy Response Body", Kind: "http", CommandID: "http.copyBody"},
				{Title: "Copy Response Headers", Kind: "http", CommandID: "http.copyHeaders"},
				{Title: "Re-send Stored Request", Kind: "http", CommandID: "http.resend"},
				{Title: "Select Environment", Kind: "http", CommandID: "http.selectEnvironment"},
			}
		},
	}
}

// curlProvider offers the caret line as an .http request when it is a curl
// command (#1994's parser), in any buffer — the shell-history paste becomes
// a runnable block.
func curlProvider() Provider {
	return Provider{
		ID: "app.curl",
		Items: func(cx Context) []Item {
			if !httpfile.IsCurlCommand(cx.LineText) {
				return nil
			}
			return []Item{{Title: "Insert as HTTP Request", Kind: "http", CommandID: "http.insertCurlAsRequest"}}
		},
	}
}

// jwtProvider offers the decode popup (#1619) when the caret line holds a
// JWT.
func jwtProvider() Provider {
	return Provider{
		ID: "app.jwt",
		Items: func(cx Context) []Item {
			if _, ok := jwt.At(cx.LineText, cx.Col); !ok {
				return nil
			}
			return []Item{{Title: "Decode JWT at Caret", Kind: "decode", CommandID: "editor.decodeJWT"}}
		},
	}
}

// concealToggles maps a concealfilter family to the view command that flips
// it, so the explain entry travels with "turn this decoding on/off" for the
// family under the caret. Families without a per-view toggle stay unmapped.
var concealToggles = map[string]Item{
	concealfilter.TimestampDecoding:     {Title: "Toggle Timestamp Decoding", Kind: "view", CommandID: "view.toggleTimestampDecoding"},
	concealfilter.UnicodeEscapeDecoding: {Title: "Toggle Unicode Escape Decoding", Kind: "view", CommandID: "view.toggleUnicodeEscapeDecoding"},
	concealfilter.EntityDecoding:        {Title: "Toggle Entity Decoding", Kind: "view", CommandID: "view.toggleEntityDecoding"},
	concealfilter.Base64Decoding:        {Title: "Toggle Base64 Decoding", Kind: "view", CommandID: "view.toggleBase64Decoding"},
	concealfilter.CronHints:             {Title: "Toggle Cron Hints", Kind: "view", CommandID: "view.toggleCronHints"},
	concealfilter.PemSummary:            {Title: "Toggle PEM Summary", Kind: "view", CommandID: "view.togglePemSummary"},
	concealfilter.ByteSizeHints:         {Title: "Toggle Byte Size Hints", Kind: "view", CommandID: "view.toggleByteSizeHints"},
	concealfilter.DurationHints:         {Title: "Toggle Duration Hints", Kind: "view", CommandID: "view.toggleDurationHints"},
	concealfilter.DigitGrouping:         {Title: "Toggle Digit Grouping", Kind: "view", CommandID: "view.toggleDigitGrouping"},
	concealfilter.RadixHints:            {Title: "Toggle Radix Hints", Kind: "view", CommandID: "view.toggleRadixHints"},
	concealfilter.PermissionHints:       {Title: "Toggle Permission Hints", Kind: "view", CommandID: "view.togglePermissionHints"},
	concealfilter.CIDRHints:             {Title: "Toggle CIDR Hints", Kind: "view", CommandID: "view.toggleCIDRHints"},
	concealfilter.IDNHints:              {Title: "Toggle IDN Hints", Kind: "view", CommandID: "view.toggleIDNHints"},
	concealfilter.SecretMasking:         {Title: "Toggle Secret Masking", Kind: "view", CommandID: "view.toggleSecretMasking"},
	concealfilter.MarkdownRendering:     {Title: "Toggle Markdown Rendering", Kind: "view", CommandID: "view.toggleMarkdownRendering"},
	concealfilter.LogRendering:          {Title: "Toggle Log Rendering", Kind: "view", CommandID: "view.toggleLogRendering"},
}

// concealProvider offers the explain popover (#1998) for a value the
// explainer resolves at the caret, plus the per-view toggle of the family
// concealing it.
func concealProvider() Provider {
	return Provider{
		ID: "app.conceal",
		Items: func(cx Context) []Item {
			if !cx.ConcealValue {
				return nil
			}
			items := []Item{{Title: "Explain Concealed Value", Kind: "view", CommandID: "editor.explainConceal"}}
			if t, ok := concealToggles[cx.ConcealFamily]; ok {
				items = append(items, t)
			}
			return items
		},
	}
}

// diagnosticProvider offers the ignore-rule entry (#1259) when a diagnostic
// sits on the caret line.
func diagnosticProvider() Provider {
	return Provider{
		ID: "app.diagnostic",
		Items: func(cx Context) []Item {
			if !cx.DiagnosticAtCaret {
				return nil
			}
			return []Item{{Title: "Ignore Diagnostic Under Caret", Kind: "quick fix", CommandID: "lsp.ignoreDiagnostic"}}
		},
	}
}

// vcsProvider offers the change actions: revert for the hunk under the caret
// (#555), the conflict-block accepts (#1149), and — for any tracked file —
// blame plus, with a selection, its history (#1020).
func vcsProvider() Provider {
	return Provider{
		ID: "app.vcs",
		Items: func(cx Context) []Item {
			var items []Item
			if cx.HunkAtCaret {
				items = append(items, Item{Title: "Revert Hunk Under Caret", Kind: "vcs", CommandID: "vcs.revertHunk"})
			}
			if cx.ConflictAtCaret {
				items = append(items,
					Item{Title: "Accept Ours", Kind: "vcs", CommandID: "merge.acceptOurs"},
					Item{Title: "Accept Theirs", Kind: "vcs", CommandID: "merge.acceptTheirs"},
					Item{Title: "Accept Both", Kind: "vcs", CommandID: "merge.acceptBoth"},
				)
			}
			if cx.InRepo {
				items = append(items, Item{Title: "Toggle Inline Blame", Kind: "vcs", CommandID: "vcs.blameLine"})
				if cx.HasSelection {
					items = append(items, Item{Title: "Show History for Selection", Kind: "vcs", CommandID: "vcs.historyForSelection"})
				}
			}
			return items
		},
	}
}

// testProvider offers run/debug for the test the caret sits in (#1085's
// nearest-test gate, precomputed into Context).
func testProvider() Provider {
	return Provider{
		ID: "app.test",
		Items: func(cx Context) []Item {
			if !cx.TestAtCaret {
				return nil
			}
			return []Item{
				{Title: "Run Test at Cursor", Kind: "test", CommandID: "run.testAtCursor"},
				{Title: "Debug Test at Cursor", Kind: "test", CommandID: "debug.testAtCursor"},
			}
		},
	}
}

// editProvider offers the general editor intentions: the value toggle when
// the caret word has a counterpart (#1658), and the clipboard diff (#1477)
// over a selection.
func editProvider() Provider {
	return Provider{
		ID: "app.edit",
		Items: func(cx Context) []Item {
			var items []Item
			if cx.CanToggleValue {
				items = append(items, Item{Title: "Toggle Value Under Caret", Kind: "edit", CommandID: "editor.toggleValue"})
			}
			if cx.HasSelection {
				items = append(items, Item{Title: "Compare Selection with Clipboard", Kind: "diff", CommandID: "diff.compareWithClipboard"})
			}
			return items
		},
	}
}

// bufferLangProvider offers the buffer-level language pick (#2033) — the only
// entry that is about the *buffer* rather than the caret, which is why it
// lists last. It appears in a buffer with no file only: a saved file is
// classified by its name, so offering the pick there would advertise a choice
// the editor deliberately refuses. The current type rides in the title, so the
// popup both shows what the buffer is treated as and changes it.
func bufferLangProvider() Provider {
	return Provider{
		ID: "app.bufferlang",
		Items: func(cx Context) []Item {
			if !cx.Fileless {
				return nil
			}
			title := "Treat Buffer as…"
			if cx.LangID != "" {
				title += " (now " + cx.LangID + ")"
			}
			return []Item{{Title: title, Kind: "buffer", CommandID: "editor.setBufferLanguage"}}
		},
	}
}
