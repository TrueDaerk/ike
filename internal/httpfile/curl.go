package httpfile

// curl.go converts between curl command lines and request blocks (#1994).
// Requests reach a developer as curl commands — browser devtools' "Copy as
// cURL", API documentation, shell history — and leave IKE the same way when
// they are handed to a colleague or run on a server. Both directions used to
// be manual transcription; ParseCurl and ExportCurl do them mechanically.
//
// ParseCurl tokenizes the command with POSIX-shell quoting rules (single
// quotes, double quotes with escapes, backslash line continuations), maps the
// flags it understands onto the request model — method, target, headers, body,
// basic auth, form/multipart — and *names* everything it had to drop in
// Ignored, so a flag never disappears silently. ExportCurl renders a request
// back, turning an `Authorization: Basic` header into `-u` and an inline
// multipart body back into `-F` parts, so the two conversions round-trip.
// Placeholders are none of this file's business: the caller resolves them
// (Request.ResolveVars) before exporting, exactly as dispatch does.

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

// CurlBoundary is the multipart boundary a `-F` import writes. A fixed value
// keeps an import reproducible (two imports of the same command produce the
// same block); it is long and distinctive enough not to collide with payload
// text, and the dispatcher rewrites nothing about it.
const CurlBoundary = "----IKEFormBoundary1994"

// CurlImport is one parsed curl command.
type CurlImport struct {
	// Request is the equivalent request block. Index/Line/Block fields stay
	// zero — the request was never part of a file.
	Request *Request
	// Ignored names the flags that could not be represented, in command
	// order, spelled as they were written ("--location", "-k"). The UI shows
	// them; nothing is dropped in silence.
	Ignored []string
}

// IsCurlCommand reports whether s looks like a curl command line — the test a
// paste-time offer to import applies before touching anything.
func IsCurlCommand(s string) bool {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "$ ")
	t = strings.TrimSpace(t)
	if !strings.HasPrefix(t, "curl") {
		return false
	}
	rest := t[len("curl"):]
	return rest != "" && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\\')
}

// valueShort lists the short options that consume a value, so "-X POST" and
// "-XPOST" both parse and a combined "-sS" is still split into flags.
var valueShort = map[byte]string{
	'A': "user-agent", 'b': "cookie", 'c': "cookie-jar", 'C': "continue-at",
	'D': "dump-header", 'd': "data", 'E': "cert", 'e': "referer",
	'F': "form", 'H': "header", 'K': "config", 'm': "max-time",
	'o': "output", 'P': "ftp-port", 'r': "range", 'T': "upload-file",
	'u': "user", 'U': "proxy-user", 'w': "write-out", 'X': "request",
	'x': "proxy",
	'y': "speed-time", 'Y': "speed-limit", 'z': "time-cond",
}

// boolShort maps the value-less short options onto their long names, so the
// ignored list reads the way curl's manual does.
var boolShort = map[byte]string{
	'G': "get", 'I': "head", 'i': "include", 'k': "insecure", 'L': "location",
	'N': "no-buffer", 'O': "remote-name", 's': "silent", 'S': "show-error",
	'v': "verbose", 'f': "fail", 'g': "globoff", 'j': "junk-session-cookies",
	'l': "list-only", 'n': "netrc", 'p': "proxytunnel", 'q': "disable",
	'R': "remote-time", 'Z': "parallel", '#': "progress-bar",
	'4': "ipv4", '6': "ipv6",
}

// valueLong lists the long options that consume a value. An option outside
// this set is assumed value-less and reported as ignored — guessing "takes a
// value" would swallow the URL.
var valueLong = map[string]bool{
	"request": true, "header": true, "data": true, "data-raw": true,
	"data-ascii": true, "data-binary": true, "data-urlencode": true,
	"json": true, "form": true, "form-string": true, "user": true,
	"url": true, "user-agent": true, "referer": true, "cookie": true,
	"cookie-jar": true, "oauth2-bearer": true, "output": true,
	"upload-file": true, "max-time": true, "connect-timeout": true,
	"proxy": true, "proxy-user": true, "cert": true, "key": true,
	"cacert": true, "capath": true, "resolve": true, "retry": true,
	"write-out": true, "range": true, "config": true, "dump-header": true,
	"interface": true, "limit-rate": true, "local-port": true,
	"max-redirs": true, "netrc-file": true, "unix-socket": true,
	"aws-sigv4": true, "ciphers": true, "http-version": true,
}

// ParseCurl turns a curl command line into a request block. A command that is
// not curl, or that names no URL, is an error; everything else parses as far
// as it can and reports what it could not represent.
func ParseCurl(cmd string) (*CurlImport, error) {
	tokens, err := splitShell(cmd)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	if base := path.Base(tokens[0]); base != "curl" && base != "curl.exe" {
		return nil, fmt.Errorf("not a curl command: %q", tokens[0])
	}
	p := &curlParser{imp: &CurlImport{}}
	if err := p.run(tokens[1:]); err != nil {
		return nil, err
	}
	return p.imp, nil
}

// curlData is one --data-* value together with the spelling that produced it:
// the spelling decides whether "@file" reads a file and whether the value is
// percent-encoded.
type curlData struct {
	value string
	flag  string // long option name, e.g. "data-raw"
}

// curlParser accumulates the pieces of one command before they are assembled
// into a request — order matters (headers keep theirs, data items concatenate)
// while the method depends on what was seen everywhere.
type curlParser struct {
	imp *CurlImport

	target   string
	method   string
	headers  []Header
	data     []curlData
	forms    []curlForm
	user     string
	get      bool // -G: the data goes into the query
	head     bool
	extraURL []string
}

func (p *curlParser) ignore(flag string) {
	for _, f := range p.imp.Ignored {
		if f == flag {
			return
		}
	}
	p.imp.Ignored = append(p.imp.Ignored, flag)
}

func (p *curlParser) header(name, value string) {
	p.headers = append(p.headers, Header{Name: name, Value: value})
}

// hasHeader reports whether the command already set a header itself.
func (p *curlParser) hasHeader(name string) bool {
	for _, h := range p.headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

func (p *curlParser) run(args []string) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			for _, rest := range args[i+1:] {
				p.operand(rest)
			}
			return p.finish()
		case strings.HasPrefix(arg, "--"):
			name := strings.TrimPrefix(arg, "--")
			value, hasValue := "", false
			if eq := strings.Index(name, "="); eq > 0 {
				name, value, hasValue = name[:eq], name[eq+1:], true
			}
			if !hasValue && valueLong[name] {
				if i+1 >= len(args) {
					return fmt.Errorf("--%s: missing value", name)
				}
				i++
				value, hasValue = args[i], true
			}
			p.option(name, value, hasValue, "--"+name)
		case len(arg) > 1 && arg[0] == '-':
			for j := 1; j < len(arg); j++ {
				c := arg[j]
				long, takesValue := valueShort[c]
				if !takesValue {
					name, known := boolShort[c]
					if !known {
						p.ignore("-" + string(c))
						continue
					}
					p.option(name, "", false, "-"+string(c))
					continue
				}
				value := arg[j+1:]
				if value == "" {
					if i+1 >= len(args) {
						return fmt.Errorf("-%s: missing value", string(c))
					}
					i++
					value = args[i]
				}
				p.option(long, value, true, "-"+string(c))
				break // the rest of the token was the value
			}
		default:
			p.operand(arg)
		}
	}
	return p.finish()
}

// operand takes a non-flag argument: the first one is the URL, further ones
// are additional transfers curl would run — a request block holds one.
func (p *curlParser) operand(arg string) {
	if p.target == "" {
		p.target = arg
		return
	}
	p.extraURL = append(p.extraURL, arg)
}

// option applies one recognised option. spelling is how it was written, so an
// ignored flag is reported the way the user typed it.
func (p *curlParser) option(name, value string, hasValue bool, spelling string) {
	switch name {
	case "request":
		p.method = strings.ToUpper(value)
	case "url":
		if p.target == "" {
			p.target = value
		} else {
			p.extraURL = append(p.extraURL, value)
		}
	case "header":
		nm, v, ok := splitCurlHeader(value)
		if !ok {
			p.ignore(spelling + " " + value)
			return
		}
		p.header(nm, v)
	case "data", "data-raw", "data-ascii", "data-binary", "data-urlencode":
		p.data = append(p.data, curlData{value: value, flag: name})
	case "json":
		p.data = append(p.data, curlData{value: value, flag: "data-raw"})
		if !p.hasHeader("Content-Type") {
			p.header("Content-Type", "application/json")
		}
		if !p.hasHeader("Accept") {
			p.header("Accept", "application/json")
		}
	case "form", "form-string":
		f, ok := parseCurlForm(value, name == "form-string")
		if !ok {
			p.ignore(spelling + " " + value)
			return
		}
		p.forms = append(p.forms, f)
	case "user":
		p.user = value
	case "oauth2-bearer":
		p.header("Authorization", "Bearer "+value)
	case "user-agent":
		p.header("User-Agent", value)
	case "referer":
		p.header("Referer", value)
	case "cookie":
		if !strings.Contains(value, "=") {
			p.ignore(spelling + " " + value) // a cookie *file*, not a value
			return
		}
		p.header("Cookie", value)
	case "compressed":
		if !p.hasHeader("Accept-Encoding") {
			p.header("Accept-Encoding", "gzip, deflate")
		}
	case "head":
		p.head = true
	case "get":
		p.get = true
	default:
		if hasValue {
			p.ignore(spelling + " " + value)
			return
		}
		p.ignore(spelling)
	}
}

// finish assembles the parsed pieces into the request.
func (p *curlParser) finish() error {
	if p.target == "" {
		return fmt.Errorf("no URL in the command")
	}
	for _, u := range p.extraURL {
		p.ignore("extra URL " + u)
	}
	req := &Request{Proto: DefaultProto, Target: normalizeCurlURL(p.target)}

	if p.user != "" {
		p.header("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.user)))
	}

	body, contentType := p.buildBody(req)
	req.Headers = p.headers
	if contentType != "" && !p.hasHeader("Content-Type") {
		req.Headers = append(req.Headers, Header{Name: "Content-Type", Value: contentType})
	}
	if p.get && body != "" {
		// -G moves the data into the query string and sends no body; the
		// Content-Type curl would have set for a payload goes with it.
		req.Target = appendRawQuery(req.Target, body)
		body = ""
		req.Headers = dropHeader(req.Headers, "Content-Type", contentType)
	}
	req.Body = body

	switch {
	case p.method != "":
		req.Method = p.method
	case p.head:
		req.Method = "HEAD"
	case p.get:
		req.Method = "GET"
	case body != "" || req.BodyFile != "" || len(p.forms) > 0:
		req.Method = "POST"
	default:
		req.Method = "GET"
	}
	p.imp.Request = req
	return nil
}

// dropHeader removes the first header with the given name *and* value — the
// one this file added itself, never one the command spelled out.
func dropHeader(headers []Header, name, value string) []Header {
	for i, h := range headers {
		if strings.EqualFold(h.Name, name) && h.Value == value {
			out := append(headers[:i:i], headers[i+1:]...)
			if len(out) == 0 {
				return nil // a request without headers has none, not zero
			}
			return out
		}
	}
	return headers
}

// buildBody renders the collected --data-*/-F values as the block's body and
// returns the Content-Type curl would have set for them ("" when the command
// carries no payload). An external-body directive is written to req directly.
func (p *curlParser) buildBody(req *Request) (body, contentType string) {
	if len(p.forms) > 0 {
		if len(p.data) > 0 {
			p.ignore("--data together with --form")
		}
		return buildFormBody(p.forms), "multipart/form-data; boundary=" + CurlBoundary
	}
	if len(p.data) == 0 {
		return "", ""
	}
	// A single "@file" payload becomes the external-body directive (#1305);
	// mixed with further data items there is nothing to splice it into, so it
	// is reported instead of silently inlined.
	if len(p.data) == 1 && p.data[0].flag != "data-raw" && strings.HasPrefix(p.data[0].value, "@") {
		file := strings.TrimPrefix(p.data[0].value, "@")
		if file == "-" {
			p.ignore("--" + p.data[0].flag + " @- (stdin)")
			return "", ""
		}
		req.BodyFile = file
		return "", "application/x-www-form-urlencoded"
	}
	parts := make([]string, 0, len(p.data))
	for _, d := range p.data {
		if d.flag != "data-raw" && strings.HasPrefix(d.value, "@") {
			p.ignore("--" + d.flag + " " + d.value)
			continue
		}
		if d.flag == "data-urlencode" {
			parts = append(parts, urlencodeData(d.value))
			continue
		}
		parts = append(parts, d.value)
	}
	return strings.Join(parts, "&"), "application/x-www-form-urlencoded"
}

// urlencodeData applies --data-urlencode's spellings: "content" and
// "=content" encode everything, "name=content" encodes the content only, and
// the file forms ("name@file") cannot be read here and keep their text.
func urlencodeData(v string) string {
	if strings.HasPrefix(v, "=") {
		return url.QueryEscape(v[1:])
	}
	if name, content, ok := strings.Cut(v, "="); ok && name != "" && !strings.Contains(name, "@") {
		return name + "=" + url.QueryEscape(content)
	}
	return url.QueryEscape(v)
}

// splitCurlHeader splits a -H value. "Name: value" is the usual spelling,
// "Name;" is curl's way of sending an empty header.
func splitCurlHeader(v string) (name, value string, ok bool) {
	if n, val, found := strings.Cut(v, ":"); found {
		n = strings.TrimSpace(n)
		if !ValidToken(n) {
			return "", "", false
		}
		return n, strings.TrimSpace(val), true
	}
	if n := strings.TrimSpace(strings.TrimSuffix(v, ";")); n != strings.TrimSpace(v) && ValidToken(n) {
		return n, "", true
	}
	return "", "", false
}

// normalizeCurlURL adds the scheme curl would guess for a bare host. curl
// itself defaults to http://; IKE writes https:// instead, so an imported
// block does not quietly downgrade a request that carries credentials.
func normalizeCurlURL(u string) string {
	if strings.Contains(u, "://") || strings.HasPrefix(u, "{{") || strings.HasPrefix(u, "$") {
		return u
	}
	return "https://" + u
}

// appendRawQuery attaches an already-encoded query string to a target (-G).
func appendRawQuery(target, query string) string {
	if query == "" {
		return target
	}
	switch {
	case strings.HasSuffix(target, "?"), strings.HasSuffix(target, "&"):
		return target + query
	case strings.Contains(target, "?"):
		return target + "&" + query
	default:
		return target + "?" + query
	}
}

// curlForm is one -F part.
type curlForm struct {
	Name        string
	Value       string // literal content, or the path when File is set
	File        bool
	FileName    string
	ContentType string
}

// parseCurlForm parses one -F / --form-string value: "name=value",
// "name=@path", "name=<path", plus the ";type=" and ";filename="
// parameters. --form-string is always literal and never reads a file.
func parseCurlForm(v string, literal bool) (curlForm, bool) {
	name, rest, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return curlForm{}, false
	}
	f := curlForm{Name: name}
	if literal {
		f.Value = rest
		return f, true
	}
	// Parameters follow the content, separated by ";". A literal value may
	// contain ";" itself, so only a file reference (@ or <) takes the full
	// parameter list — that is curl's own rule.
	if strings.HasPrefix(rest, "@") || strings.HasPrefix(rest, "<") {
		f.File = true
		fields := strings.Split(rest[1:], ";")
		f.Value = fields[0]
		for _, param := range fields[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "type":
				f.ContentType = strings.TrimSpace(value)
			case "filename":
				f.FileName = strings.TrimSpace(value)
			}
		}
		return f, true
	}
	if idx := strings.Index(rest, ";type="); idx >= 0 {
		f.Value, f.ContentType = rest[:idx], rest[idx+len(";type="):]
		return f, true
	}
	f.Value = rest
	return f, true
}

// buildFormBody renders -F parts as the hand-written multipart body the
// dispatcher assembles (#1707): one delimiter per part, a Content-Disposition
// header, and — for a file part — the "< path" directive as its content.
func buildFormBody(forms []curlForm) string {
	b := &strings.Builder{}
	for _, f := range forms {
		b.WriteString("--" + CurlBoundary + "\n")
		disp := `Content-Disposition: form-data; name="` + f.Name + `"`
		if f.File {
			name := f.FileName
			if name == "" {
				name = path.Base(f.Value)
			}
			disp += `; filename="` + name + `"`
		}
		b.WriteString(disp + "\n")
		if f.ContentType != "" {
			b.WriteString("Content-Type: " + f.ContentType + "\n")
		}
		b.WriteString("\n")
		if f.File {
			b.WriteString("< " + f.Value + "\n")
			continue
		}
		b.WriteString(f.Value + "\n")
	}
	b.WriteString("--" + CurlBoundary + "--")
	return b.String()
}

// FormatRequest renders a request as the text of a `.http` block — the
// spelling the parser reads back and the built-in reformatter (#1602) leaves
// alone. name, when set, opens the block with its "###" separator line. The
// result ends with a newline.
func FormatRequest(r *Request, name string) string {
	b := &strings.Builder{}
	if name != "" {
		b.WriteString(separator + " " + name + "\n")
	}
	line := r.Method + " " + r.Target
	if r.Proto != "" && r.Proto != DefaultProto {
		line += " " + r.Proto
	}
	b.WriteString(line + "\n")
	for _, h := range r.Headers {
		b.WriteString(h.Name + ": " + h.Value + "\n")
	}
	switch {
	case r.BodyFile != "":
		directive := "< "
		if r.BodyFileSubstitute {
			directive = "<@ "
		}
		b.WriteString("\n" + directive + r.BodyFile + "\n")
	case r.Body != "":
		b.WriteString("\n" + r.Body + "\n")
	}
	return b.String()
}

// ExportCurl renders a request as a runnable curl command. Placeholders are
// *not* resolved here — the caller substitutes first (Request.ResolveVars),
// so the command carries the same values the dispatch would send.
//
// It is the inverse of ParseCurl wherever curl has its own spelling for
// something the block expresses as a header: an `Authorization: Basic`
// header becomes `-u user:pass`, and an inline multipart body becomes one
// `-F` per part (with its Content-Type header dropped, since curl generates
// the boundary itself).
func ExportCurl(r *Request) string {
	args := []string{"curl", shellQuote(r.Target)}
	if m := strings.ToUpper(r.Method); m != "" && m != "GET" {
		args = append(args, "-X", m)
	}

	forms, isForm := multipartForms(r)
	for _, h := range r.Headers {
		if user, ok := basicAuthUser(h); ok {
			args = append(args, "-u", shellQuote(user))
			continue
		}
		if isForm && strings.EqualFold(h.Name, "Content-Type") {
			continue // curl writes its own boundary
		}
		args = append(args, "-H", shellQuote(h.Name+": "+h.Value))
	}
	switch {
	case isForm:
		for _, f := range forms {
			args = append(args, "-F", shellQuote(formArg(f)))
		}
	case r.BodyFile != "":
		args = append(args, "--data-binary", shellQuote("@"+r.BodyFile))
	case r.Body != "":
		args = append(args, "--data-raw", shellQuote(r.Body))
	}
	return strings.Join(args, " ")
}

// formArg renders one part as a -F value.
func formArg(f curlForm) string {
	out := f.Name + "="
	if f.File {
		out += "@" + f.Value
	} else {
		out += f.Value
	}
	if f.ContentType != "" {
		out += ";type=" + f.ContentType
	}
	if f.File && f.FileName != "" && f.FileName != path.Base(f.Value) {
		out += ";filename=" + f.FileName
	}
	return out
}

// basicAuthUser recognises an "Authorization: Basic <base64>" header and
// returns the "user:password" it encodes, so an exported command uses curl's
// own -u spelling. Anything that does not decode to a credential pair stays a
// plain header.
func basicAuthUser(h Header) (string, bool) {
	if !strings.EqualFold(h.Name, "Authorization") {
		return "", false
	}
	rest, ok := cutPrefixFold(strings.TrimSpace(h.Value), "Basic ")
	if !ok {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
	if err != nil || !strings.Contains(string(raw), ":") || strings.ContainsAny(string(raw), "\r\n") {
		return "", false
	}
	return string(raw), true
}

// cutPrefixFold is strings.CutPrefix with a case-insensitive comparison —
// header values spell the scheme "Basic", "basic" and "BASIC" alike.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}

// multipartForms reads an inline multipart body back into its parts, the
// inverse of buildFormBody. It reports false when the request is not a
// multipart one or its body does not follow the declared boundary — such a
// body is exported as raw data instead of guessed at.
func multipartForms(r *Request) ([]curlForm, bool) {
	ct, ok := r.Header("Content-Type")
	if !ok || r.Body == "" {
		return nil, false
	}
	boundary, ok := MultipartBoundary(ct)
	if !ok {
		return nil, false
	}
	delim, closing := "--"+boundary, "--"+boundary+"--"
	lines := strings.Split(strings.ReplaceAll(r.Body, "\r\n", "\n"), "\n")
	var forms []curlForm
	i := 0
	for i < len(lines) && trimPadding(lines[i]) != delim {
		if trimPadding(lines[i]) == closing {
			return nil, false // nothing but a closing delimiter
		}
		i++ // preamble
	}
	for i < len(lines) && trimPadding(lines[i]) == delim {
		i++
		var headers []Header
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" &&
			trimPadding(lines[i]) != delim && trimPadding(lines[i]) != closing {
			if nm, v, found := strings.Cut(lines[i], ":"); found {
				headers = append(headers, Header{Name: strings.TrimSpace(nm), Value: strings.TrimSpace(v)})
			}
			i++
		}
		if i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		start := i
		for i < len(lines) && trimPadding(lines[i]) != delim && trimPadding(lines[i]) != closing {
			i++
		}
		f, ok := formFromPart(headers, lines[start:i])
		if !ok {
			return nil, false
		}
		forms = append(forms, f)
	}
	return forms, len(forms) > 0
}

// formFromPart turns one parsed multipart part into a -F argument. A part
// without a name is not expressible as -F, which drops the whole request back
// to a raw-body export.
func formFromPart(headers []Header, content []string) (curlForm, bool) {
	f := curlForm{}
	for _, h := range headers {
		switch {
		case strings.EqualFold(h.Name, "Content-Disposition"):
			_, params, err := mime.ParseMediaType(h.Value)
			if err != nil {
				return f, false
			}
			f.Name, f.FileName = params["name"], params["filename"]
		case strings.EqualFold(h.Name, "Content-Type"):
			f.ContentType = h.Value
		}
	}
	if f.Name == "" {
		return f, false
	}
	if p, _, ok := partFileDirective(content); ok {
		f.File, f.Value = true, p
		return f, true
	}
	for len(content) > 0 && strings.TrimSpace(content[len(content)-1]) == "" {
		content = content[:len(content)-1]
	}
	f.Value = strings.Join(content, "\n")
	return f, true
}

// shellSafeRE matches the values a POSIX shell passes through unquoted.
var shellSafeRE = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote renders v as one shell word: single quotes unless the value is
// plain enough to need none, with an embedded single quote spliced the only
// way a shell allows: close, escape, reopen.
func shellQuote(v string) string {
	if v != "" && shellSafeRE.MatchString(v) {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// splitShell tokenizes a command line with POSIX-shell quoting: single quotes
// are literal, double quotes honour backslash escapes, a bare backslash
// escapes the next character (backslash-newline being the line continuation
// devtools and documentation wrap long commands with), and unquoted newlines
// separate words like any other whitespace. An unterminated quote is an
// error — a half-pasted command must not import as a truncated request.
func splitShell(s string) ([]string, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "$ ")) // a copied shell prompt
	var (
		out   []string
		cur   strings.Builder
		begun bool
	)
	flush := func() {
		if begun {
			out = append(out, cur.String())
			cur.Reset()
			begun = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\n':
			flush()
		case '\\':
			if i+1 >= len(s) {
				return nil, fmt.Errorf("trailing backslash")
			}
			i++
			if s[i] == '\n' {
				continue // line continuation
			}
			begun = true
			cur.WriteByte(s[i])
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated ' quote")
			}
			begun = true
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
		case '"':
			begun = true
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					// Inside double quotes a backslash escapes only these.
					if next := s[i+1]; next == '"' || next == '\\' || next == '$' || next == '`' || next == '\n' {
						i++
						if s[i] == '\n' {
							continue // line continuation inside the quote
						}
					}
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, fmt.Errorf(`unterminated " quote`)
			}
		case '^':
			// The Windows devtools spelling wraps lines with "^": outside a
			// quote and at the end of a line it is a continuation, not text.
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
				continue
			}
			begun = true
			cur.WriteByte(c)
		default:
			begun = true
			cur.WriteByte(c)
		}
	}
	flush()
	return out, nil
}

// IgnoredSummary renders the ignored flags for a notice, sorted and capped so
// a command full of transport options cannot fill the screen.
func (c *CurlImport) IgnoredSummary() string {
	if len(c.Ignored) == 0 {
		return ""
	}
	const maxShown = 8
	sorted := append([]string(nil), c.Ignored...)
	sort.Strings(sorted)
	suffix := ""
	if len(sorted) > maxShown {
		suffix = fmt.Sprintf(" (+%d more)", len(sorted)-maxShown)
		sorted = sorted[:maxShown]
	}
	return strings.Join(sorted, ", ") + suffix
}
