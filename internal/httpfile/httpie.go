package httpfile

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"path"
	"strings"
)

// httpie.go is the second export format of a request block (#2384), next to
// ExportCurl. It is not a translation of the curl flags: httpie spells the
// same request in its own item syntax — a header is "Name:Value", a query
// parameter "name==value", a JSON field "name=value" (or "name:=raw" for a
// non-string), an upload "--form name@path" — and a command built by
// rewriting "-H" into ":" would not be an httpie command at all.
//
// The two exporters share this package's preprocessing (basicAuthUser,
// multipartForms, shellQuote) so both formats agree on what a request *is*;
// they differ only in how they spell it.
//
// Nothing is masked: an Authorization header is exported as it was written,
// the same deliberate line ExportCurl draws.

// httpieCommand is the httpie executable an exported command calls.
const httpieCommand = "http"

// ExportHTTPie renders a request as a runnable httpie command. Like
// ExportCurl it resolves nothing — the caller substitutes first, so the
// command carries the values a dispatch would send.
//
// How far the translation goes, per body kind:
//
//   - A multipart body becomes "--form" plus one item per part, files as
//     "name@path" — httpie writes its own boundary, so the request's
//     Content-Type is dropped exactly as in the curl export.
//   - An urlencoded body becomes "--form" plus one "name=value" per pair,
//     which is what httpie's form mode sends.
//   - A JSON object body becomes field items: "name=value" for a string,
//     "name:=raw" for anything else (number, bool, null, array, object),
//     with the raw JSON preserved verbatim. This is the readable spelling
//     httpie exists for, and it is only used when the body is an object
//     whose keys survive the item syntax.
//   - Anything else — a JSON array, a non-JSON text body, a body whose keys
//     would need escaping — goes out as "--raw", which sends the bytes
//     unchanged. Correct always, idiomatic sometimes; the field syntax is
//     preferred wherever it is faithful.
//   - An external body file is redirected on stdin ("< path"), which httpie
//     reads as the raw body.
//
// The item order is the request's own (headers as written, query parameters
// as spelled in the target, fields in body order), so the same request always
// exports the same command.
func ExportHTTPie(r *Request) string {
	flags, items := []string{}, []string{}

	forms, isForm := multipartForms(r)
	urlencoded, isURLEncoded := urlencodedForms(r)
	if isForm || isURLEncoded {
		flags = append(flags, "--form")
	}
	for _, h := range r.Headers {
		if user, ok := basicAuthUser(h); ok {
			flags = append(flags, "-a", shellQuote(user))
			continue
		}
		if (isForm || isURLEncoded) && strings.EqualFold(h.Name, "Content-Type") {
			continue // httpie writes the form content type itself
		}
		items = append(items, shellQuote(httpieHeaderItem(h)))
	}

	target, query := httpieTarget(r.Target)
	items = append(items, query...)

	redirect := ""
	switch {
	case isForm:
		for _, f := range forms {
			items = append(items, shellQuote(httpieFormItem(f)))
		}
	case isURLEncoded:
		for _, f := range urlencoded {
			items = append(items, shellQuote(f.Name+"="+f.Value))
		}
	case r.BodyFile != "":
		redirect = " < " + shellQuote(r.BodyFile)
	case r.Body != "":
		if fields, ok := httpieJSONFields(r); ok {
			items = append(items, fields...)
		} else {
			flags = append(flags, "--raw", shellQuote(r.Body))
		}
	}

	args := []string{httpieCommand}
	args = append(args, flags...)
	args = append(args, httpieMethod(r.Method), shellQuote(target))
	args = append(args, items...)
	return strings.Join(args, " ") + redirect
}

// httpieMethod spells the method the way httpie wants it: uppercase and
// always explicit. httpie infers GET or POST from whether there is a body,
// and an exported command should never depend on that guess.
func httpieMethod(m string) string {
	if m = strings.ToUpper(strings.TrimSpace(m)); m == "" {
		return "GET"
	}
	return m
}

// httpieHeaderItem renders one header as an httpie item. An empty value is
// "Name;", which sends the header empty — "Name:" is httpie's spelling for
// *unsetting* a header it would otherwise add, which is a different request.
func httpieHeaderItem(h Header) string {
	if h.Value == "" {
		return h.Name + ";"
	}
	return h.Name + ":" + h.Value
}

// httpieTarget splits a request target into the URL httpie is called with and
// the "name==value" items for its query string. The split only happens when
// every component is a decodable "name=value" pair — a bare flag parameter or
// an undecodable escape has no item spelling, and the whole query then stays
// in the URL where it is byte-exact.
func httpieTarget(target string) (string, []string) {
	base, raw, ok := strings.Cut(target, "?")
	if !ok || raw == "" {
		return target, nil
	}
	fragment := ""
	if q, f, ok := strings.Cut(raw, "#"); ok {
		raw, fragment = q, "#"+f
	}
	items := make([]string, 0, strings.Count(raw, "&")+1)
	for _, part := range strings.Split(raw, "&") {
		name, value, ok := strings.Cut(part, "=")
		if !ok || name == "" {
			return target, nil
		}
		dn, err := url.QueryUnescape(name)
		if err != nil {
			return target, nil
		}
		dv, err := url.QueryUnescape(value)
		if err != nil {
			return target, nil
		}
		items = append(items, shellQuote(dn+"=="+dv))
	}
	return base + fragment, items
}

// httpieFormItem renders one multipart part as an httpie form item: a literal
// part is "name=value", an upload "name@path", with the ";type=" and
// ";filename=" parameters httpie accepts on an upload.
func httpieFormItem(f curlForm) string {
	if !f.File {
		return f.Name + "=" + f.Value
	}
	out := f.Name + "@" + f.Value
	if f.ContentType != "" {
		out += ";type=" + f.ContentType
	}
	if f.FileName != "" && f.FileName != path.Base(f.Value) {
		out += ";filename=" + f.FileName
	}
	return out
}

// urlencodedForms reads an "application/x-www-form-urlencoded" body back into
// its pairs, so httpie can send it as a form instead of as raw bytes. It
// reports false for any other content type, and for a body whose escapes do
// not decode — that one is exported raw rather than guessed at.
func urlencodedForms(r *Request) ([]curlForm, bool) {
	ct, ok := r.Header("Content-Type")
	if !ok || r.Body == "" {
		return nil, false
	}
	mediaType, _, _ := strings.Cut(ct, ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/x-www-form-urlencoded") {
		return nil, false
	}
	var forms []curlForm
	for _, part := range strings.Split(strings.TrimSpace(r.Body), "&") {
		name, value, ok := strings.Cut(part, "=")
		if !ok || name == "" {
			return nil, false
		}
		dn, err := url.QueryUnescape(name)
		if err != nil {
			return nil, false
		}
		dv, err := url.QueryUnescape(value)
		if err != nil {
			return nil, false
		}
		forms = append(forms, curlForm{Name: dn, Value: dv})
	}
	return forms, len(forms) > 0
}

// httpieJSONFields renders a JSON object body as httpie field items, in the
// body's own key order — the decoder is walked token by token precisely so
// the order survives, since a map would randomize it.
//
// It declines (false) for anything that is not a JSON object, for a body with
// trailing content, and for a key that the item syntax cannot carry
// unambiguously — httpie reads "=", ":=", "==", "@" and ":" as separators, so
// a key containing one would have to be escaped and is left to --raw instead.
func httpieJSONFields(r *Request) ([]string, bool) {
	if ct, ok := r.Header("Content-Type"); ok {
		if mediaType, _, _ := strings.Cut(ct, ";"); !isJSONMediaType(strings.TrimSpace(mediaType)) {
			return nil, false
		}
	}
	dec := json.NewDecoder(strings.NewReader(r.Body))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var items []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, ok := key.(string)
		if !ok || name == "" || strings.ContainsAny(name, "=:@\\") {
			return nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		items = append(items, shellQuote(httpieField(name, raw)))
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false // trailing content: not a single object
	}
	return items, len(items) > 0
}

// httpieField renders one JSON member: a string is the plain "name=value"
// item, everything else keeps its JSON spelling behind ":=", which is what
// makes numbers stay numbers and nested structures stay structures.
func httpieField(name string, raw json.RawMessage) string {
	// Only a JSON *string* gets the plain "=" item; the test is on the raw
	// spelling rather than on Unmarshal, which happily reads null into a
	// string and would export a null as an empty field.
	if trimmed := strings.TrimSpace(string(raw)); strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return name + "=" + s
		}
	}
	return name + ":=" + compactJSON(raw)
}

// compactJSON strips the insignificant whitespace of a raw value, so a
// pretty-printed body does not smuggle newlines into a shell word.
func compactJSON(raw json.RawMessage) string {
	b := &bytes.Buffer{}
	if err := json.Compact(b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

// isJSONMediaType reports whether a media type carries JSON — "application/
// json" and the "+json" suffix types (application/problem+json and friends).
func isJSONMediaType(mediaType string) bool {
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || mediaType == "text/json" ||
		strings.HasSuffix(mediaType, "+json")
}
