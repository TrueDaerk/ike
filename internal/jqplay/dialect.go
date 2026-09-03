package jqplay

// dialect.go is the seam that lets one playground serve two document
// languages (#2039). The jq playground (#1936) and the yq playground are the
// *same* mode over the same engine: the query line, the completion, the
// result buffer, the history and the saved-filter library know nothing about
// JSON or YAML. Exactly three things differ, and they all live here:
//
//   - **how the buffer is read** — a JSON stream vs. a YAML document stream,
//   - **how a value is written back out** — pretty JSON vs. block YAML,
//   - **how the result folds** — delimiter counting vs. indentation.
//
// Everything else — Run, History, Library, Tokens, Complete, Wrap — is shared
// verbatim, which is why the engine stays gojq for both: yq's expression
// language *is* jq's for the part anyone types into a query line, so decoding
// YAML into the same value shapes gojq already runs over buys the whole
// language for the price of a decoder (the trade `gojq --yaml-input
// --yaml-output` makes, and mikefarah's yq for its common subset). A second
// engine would have meant a second completion list, a second scanner and a
// second set of error spellings for no user-visible gain.
//
// A Dialect is carried by the Input it parsed and by the Result that came out
// of it, so a caller that holds either never has to remember which language
// it was looking at. The zero value is DialectJQ: a Result built by hand
// (`Result{Err: …}` for a failure the host phrases itself) stays jq-shaped.

// Dialect names one of the playground's two document languages.
type Dialect int

// The three dialects. DialectJQ is the zero value — jq is the original mode
// and the one every hand-built Result belongs to. DialectXMQ (#2414) is the
// odd one out: its engine is the external `xmq` binary rather than gojq, but
// it lives behind the same seam so the host stays dialect-agnostic.
const (
	DialectJQ Dialect = iota
	DialectYQ
	DialectXMQ
)

// Name is the dialect's command-line name — the query line's label, the
// command ids and every message that says which language failed.
func (d Dialect) Name() string {
	switch d {
	case DialectYQ:
		return "yq"
	case DialectXMQ:
		return "xmq"
	}
	return "jq"
}

// Format names the document language the dialect reads and writes, for the
// messages that talk about the *input* rather than about the program.
func (d Dialect) Format() string {
	switch d {
	case DialectYQ:
		return "YAML"
	case DialectXMQ:
		return "XML/HTML"
	}
	return "JSON"
}

// Ext is the file extension the result is written under: the scratch buffer
// the open-as-scratch action creates, and the extension in the result
// buffer's display path that resolves its highlighting. It is the dialect's
// *default* — an xmq result names its own extension per command (to-json
// writes JSON, to-html HTML), which Result.Ext answers.
func (d Dialect) Ext() string {
	switch d {
	case DialectYQ:
		return "yaml"
	case DialectXMQ:
		return "xmq"
	}
	return "json"
}

// ResultPath is the display path of the substitute result editor. Like an
// archive entry's virtual path (#1762) it is never written to; it exists so
// the language sniff resolves the result's highlighting.
func (d Dialect) ResultPath() string { return d.Name() + " result." + d.Ext() }

// Parse reads text as the dialect's document stream — one value for an
// ordinary document, several for a `.jsonl` export or a `---`-separated YAML
// file. An empty or malformed text is an error the playground shows on its
// input line; it is never a crash.
func (d Dialect) Parse(text string) (*Input, error) {
	switch d {
	case DialectYQ:
		return parseYAML(text)
	case DialectXMQ:
		return parseXMQ(text)
	}
	return parseJSON(text)
}

// Folds returns the foldable nodes of a result the dialect produced (#2029):
// the multi-line objects and arrays of pretty JSON, the indented blocks of
// YAML. Both scans read text the dialect's own encoder wrote, so structure is
// a matter of counting rather than of re-decoding.
func (d Dialect) Folds(text string) []Fold {
	switch d {
	case DialectYQ:
		return yamlFolds(text)
	case DialectXMQ:
		// The xmq result is the CLI's stdout — xmq text, XML, HTML or JSON per
		// command — which the structural scans of this package do not read.
		// The result buffer still folds by grammar where one applies;
		// Result.Folds answers the JSON case (`to-json`) that the scan can.
		return nil
	}
	return jsonFolds(text)
}

// encode renders one output value in the dialect's spelling.
func (d Dialect) encode(v any) string {
	if d == DialectYQ {
		return encodeYAML(v)
	}
	return encodeJSON(v)
}

// separator joins two consecutive outputs in the result buffer. jq's stdout
// puts one value per line; a YAML stream needs the document marker, or two
// mappings printed back to back would read as one.
func (d Dialect) separator() string {
	if d == DialectYQ {
		return "\n---\n"
	}
	return "\n"
}

// emptyInput is the message for a buffer that holds no document at all — the
// one error that is phrased before any parsing happens.
func (d Dialect) emptyInput() string {
	return "no " + d.Format() + " input — the buffer is empty"
}
