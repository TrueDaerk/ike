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
// sits on a value: the three copy flavours, and the playground seeded with the
// caret's path — the jq one over a JSON buffer, the yq one over a YAML buffer
// (#2039). The two playgrounds are the same mode over two decoders, so the
// entry is the same entry; only the buffer's language decides which command it
// dispatches.
//
// The playground needs the *whole* buffer as its input: against a selection
// the caret's path indexes the file and would name a location the input does
// not contain, so the …AtPath commands silently fall back to the identity
// program there (#2026) — an entry promising "at Cursor Path" that does not
// go to the cursor's path is not offered.
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
			if !cx.HasSelection {
				if docpath.IsYAML(cx.LangID) {
					items = append(items, Item{Title: "yq Playground at Cursor Path", Kind: "yq", CommandID: "yaml.yqPlaygroundAtPath"})
				} else {
					items = append(items, Item{Title: "jq Playground at Cursor Path", Kind: "jq", CommandID: "json.jqPlaygroundAtPath"})
				}
			}
			return items
		},
	}
}

// httpProvider offers the request actions when the caret sits inside an
// .http request block (#1131 and friends); the app precomputes the block
// probe (httpfile.RequestAt) into Context.
//
// Only the first two act on the caret's block. The copy and re-send entries
// act on the *shown response* and the environment picker on the env file
// next to the buffer, so each rides its own precomputed fact (#2026) — the
// caret being inside a request says nothing about a response ever having
// arrived.
func httpProvider() Provider {
	return Provider{
		ID: "app.http",
		Items: func(cx Context) []Item {
			if !cx.HTTPRequest {
				return nil
			}
			items := []Item{
				{Title: "Run Request", Kind: "http", CommandID: "http.run"},
				{Title: "Copy as curl", Kind: "http", CommandID: "http.copyAsCurl"},
			}
			if cx.HTTPResponseBody {
				items = append(items, Item{Title: "Copy Response Body", Kind: "http", CommandID: "http.copyBody"})
			}
			if cx.HTTPResponseHeaders {
				items = append(items, Item{Title: "Copy Response Headers", Kind: "http", CommandID: "http.copyHeaders"})
			}
			if cx.HTTPResendable {
				items = append(items, Item{Title: "Re-send Stored Request", Kind: "http", CommandID: "http.resend"})
			}
			if cx.HTTPEnvironments {
				items = append(items, Item{Title: "Select Environment", Kind: "http", CommandID: "http.selectEnvironment"})
			}
			return items
		},
	}
}

// curlProvider offers the caret line as an .http request when it is a curl
// command (#1994's parser), in any buffer — the shell-history paste becomes
// a runnable block.
//
// The gate parses the command the conversion would parse — the caret line
// plus its backslash continuations — rather than only recognizing the "curl "
// prefix (#2026): a truncated or malformed command (`curl -H` with no value,
// a command with no URL at all) used to be offered and then answered with the
// parser's error. ParseCurl is pure string work, so the check stays a cheap
// caret probe.
func curlProvider() Provider {
	return Provider{
		ID: "app.curl",
		Items: func(cx Context) []Item {
			cmd, _, ok := httpfile.CurlCommandAt(cx.lineAt, cx.lineCount(), cx.Line)
			if !ok {
				return nil
			}
			if _, err := httpfile.ParseCurl(cmd); err != nil {
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

// concealProvider offers the explain popover (#1998) for the conceal
// stand-in under the caret, plus the per-view toggle of the family drawing
// it. The command itself also answers "why is this *not* masked" for any
// plain value (#1930), but as an intention that read was pure noise (#2026):
// on `getConfig` in a Python import the popup offered "Explain Concealed
// Value" and the popover then said nothing conceals it. The palette entry and
// `g?` keep the plain-value reading; the popup only offers what is concealed.
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
//
// The rewriting entries — the revert and the three accepts — also need a
// writable buffer (#2026): in a read-only preview the edit would be dropped
// by the recorder lock without a word.
func vcsProvider() Provider {
	return Provider{
		ID: "app.vcs",
		Items: func(cx Context) []Item {
			var items []Item
			if cx.HunkAtCaret && cx.InRepo && !cx.ReadOnly {
				items = append(items, Item{Title: "Revert Hunk Under Caret", Kind: "vcs", CommandID: "vcs.revertHunk"})
			}
			if cx.ConflictAtCaret && !cx.ReadOnly {
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
// nearest-test gate, precomputed into Context). Debugging needs more than a
// test: a language with a debug adapter and no session already running
// (#2026), both of which the launch would otherwise refuse after the pick.
func testProvider() Provider {
	return Provider{
		ID: "app.test",
		Items: func(cx Context) []Item {
			if !cx.TestAtCaret {
				return nil
			}
			items := []Item{{Title: "Run Test at Cursor", Kind: "test", CommandID: "run.testAtCursor"}}
			if cx.CanDebug {
				items = append(items, Item{Title: "Debug Test at Cursor", Kind: "test", CommandID: "debug.testAtCursor"})
			}
			return items
		},
	}
}

// editProvider offers the general editor intentions: the value toggle when
// the caret word has a counterpart (#1658) and the buffer takes edits, and
// the clipboard diff (#1477) over a selection — which needs something on the
// clipboard to compare against, or it only reports "clipboard is empty"
// (#2026).
func editProvider() Provider {
	return Provider{
		ID: "app.edit",
		Items: func(cx Context) []Item {
			var items []Item
			if cx.CanToggleValue && !cx.ReadOnly {
				items = append(items, Item{Title: "Toggle Value Under Caret", Kind: "edit", CommandID: "editor.toggleValue"})
			}
			if cx.HasSelection && cx.HasClipboard {
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
