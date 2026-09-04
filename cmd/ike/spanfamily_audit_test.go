package main

import (
	"sort"
	"strings"
	"testing"

	"ike/internal/lang"
)

// spanfamily_audit_test.go is the standing ledger of the language span-
// capability audit (#2337), the sibling of the unbound-command audit in
// keybind_audit_test.go (#2305). Language plugins wire their stand-in and hint
// families — unicode-escape decoding, entity decoding, base64 decoding, secret
// masking, network/permission/cron/number hints — by hand in their
// lang.Language.Spans hook, and nothing used to say which language *should*
// offer which family: the unicode decoding of #1620 reached Python, PHP, YAML
// and TOML only years later, in #2334, because nobody noticed the hole.
//
// This test closes that hole. Every registered language is checked against
// every audited family: it either offers the family, or carries an entry here
// saying why it does not. A newly registered language therefore fails the
// build until someone decides — wire or justification. The ledger is kept
// honest in both directions: an entry for a language that has since wired the
// family, or that no longer exists, is stale and fails too.
//
// "Offers" is decided behaviourally, not declaratively: each family carries a
// probe buffer covering every producer shape the family has (per-dialect
// unicode forms, the k8s Secret document, chmod in shell and in code, …), and
// a language offers the family exactly when its Spans hook emits a span with
// the family's capture on that probe. That keeps the audit free of production
// code and immune to a capability list that lies — but it also means the
// probes are load-bearing: a new producer shape (a new unicode dialect, a new
// mode-carrying key) must be added to the probe when it is added to a family,
// or the wired language will read as unwired.

// The audited families: the capture prefixes their spans carry, and the probe
// buffers that make every known producer fire. A family fires when any probe
// buffer yields a span whose capture starts with one of the prefixes.
type spanFamily struct {
	name     string
	captures []string
	probes   [][]string
}

// The family names, as the ledger and the failure messages spell them.
const (
	famUnicode = "unicode-escapes" // \uXXXX etc. decode (internal/escapes)
	famEntity  = "entities"        // &amp; etc. decode (internal/escapes)
	famBase64  = "base64"          // conventional base64 decodes (internal/escapes)
	famSecret  = "secret-masking"  // suspect values mask (internal/secret)
	famNet     = "net-hints"       // CIDR/IDN hints (internal/nethint)
	famPerm    = "perm-hints"      // file-mode hints (internal/permhint)
	famCron    = "cron-hints"      // cron-schedule hints (internal/cronhint)
	famNumber  = "number-hints"    // numeric readability hints (internal/numhint, internal/consthint)
)

var spanFamilies = []spanFamily{
	// \uXXXX works inside a double-quoted literal in every dialect the
	// scanner knows (internal/escapes.UnicodeDialect), so three key/value
	// shapes cover code, JSON and YAML/TOML alike. The prefix-less CSS
	// dialect and shell's ANSI-C quoting (#2345) need probes of their own.
	{famUnicode, []string{"escape.unicode"}, [][]string{
		{
			`x = "caf\u00e9"`,
			`"key": "caf\u00e9",`,
			`a: "caf\u00e9"`,
		},
		{`content: "\00e9";`},
		{`echo $'caf\u00e9'`},
	}},
	// &amp; and &#65; decode in both the HTML and the XML entity set.
	{famEntity, []string{"escape.entity"}, [][]string{{
		`<i>&amp; &#65;</i>`,
	}}},
	// The contexts where base64 is the convention: the data: block of a
	// Kubernetes Secret document ("c2VjcmV0dmFsdWU=" is "secretvalue") — in
	// YAML and, since #2345, as a JSON manifest — and the Basic credential of
	// an Authorization header ("dXNlcjpwYXNz" is "user:pass").
	{famBase64, []string{"escape.base64"}, [][]string{
		{
			`kind: Secret`,
			`data:`,
			`  password: c2VjcmV0dmFsdWU=`,
		},
		{`{"kind": "Secret", "data": {"password": "c2VjcmV0dmFsdWU="}}`},
		{
			`GET https://example.com/api HTTP/1.1`,
			`Authorization: Basic dXNlcjpwYXNz`,
		},
	}},
	// A secret-suspect key in every masking producer's shape: dotenv/shell/
	// crontab assignment, code assignment, JSON member, YAML/ini pair — plus
	// the Dockerfile ENV operand and a request's credential header (#2345),
	// whose producers key on their own carrying line.
	{famSecret, []string{"secret.value"}, [][]string{
		{
			`DB_PASSWORD=hunter2secret`,
			`DB_PASSWORD = "hunter2secret"`,
			`"db_password": "hunter2secret",`,
			`db_password: hunter2secret`,
		},
		{`ENV DB_PASSWORD=hunter2secret`},
		{
			`GET https://example.com/api HTTP/1.1`,
			`Authorization: Bearer hunter2secret`,
		},
	}},
	// A CIDR prefix quoted (the source-code producers scan string literals
	// only) and bare (the config-format producers scan whole lines).
	{famNet, []string{"net."}, [][]string{{
		`addr = "10.0.0.0/8"`,
		`network: 10.0.0.0/8`,
		`allow 10.0.0.0/8`,
	}}},
	// A file mode in every producer's shape: chmod in shell and in a RUN
	// line, --chmod in Dockerfile COPY, the Go and Python mode APIs, and a
	// YAML mode: key with the octal prefix the producer requires.
	{famPerm, []string{"perm.mode"}, [][]string{{
		`chmod 755 /tmp/f`,
		`COPY --chmod=755 src dst`,
		`RUN chmod 755 /tmp/f`,
		`os.Chmod("f", 0o755)`,
		`os.chmod("f", 0o755)`,
		`mode: "0755"`,
	}}},
	// A schedule in every producer's shape: a crontab line, a YAML
	// schedule: key, and quoted scalars for the key-less config formats.
	{famCron, []string{"cron.hint"}, [][]string{{
		`*/5 * * * * /bin/task`,
		`schedule: "*/5 * * * *"`,
		`cron = "*/5 * * * *"`,
		`"schedule": "*/5 * * * *",`,
	}}},
	// 10485760 = 10 MiB, under a name that says bytes, in the constant and
	// config shapes — plus an HTTP request of its own, because the .http
	// producer only hints inside a parsed request's value positions.
	{famNumber, []string{"number."}, [][]string{
		{
			`MAX_BYTES = 10485760`,
			`const MaxBytes = 10485760`,
			`define('MAX_BYTES', 10485760);`,
			`max_bytes: 10485760`,
			`max_bytes = 10485760`,
			`MAX_BYTES=10485760`,
		},
		{
			`GET https://example.com/api?max_bytes=10485760 HTTP/1.1`,
			`X-Max-Bytes: 10485760`,
		},
	}},
}

// The reasons a language may record for not offering a family, grouped so the
// ledger reads as an audit rather than as an opt-out list.
const (
	// The language's syntax simply has no form this family could decode:
	// dotenv values process no backslash escapes, TOML has no entities.
	reasonNoSyntax = "the language has no syntax for this family's literals"
	// The family needs a convention saying where such a value sits (a mode:
	// key, a Secret document, a key=value shape), and this format has none —
	// matching on shape alone would misfire.
	reasonNoConvention = "no convention marks such a value in this format"
	// The buffer's content is foreign data the editor must render verbatim:
	// CSV cells, patch hunks, log output. Reinterpreting it would rewrite
	// the user's payload.
	reasonForeignData = "the buffer holds foreign data, rendered verbatim"
	// An injection helper grammar (markdown_inline): no buffer is ever of
	// this language, so its Spans seam has nothing to decode.
	reasonInjection = "injection helper grammar; no buffer is ever of this language"
	// A real gap surfaced by filling in this ledger — tracked in an issue,
	// not excused. When a gap closes, its cell moves to offeredSpanFamilies.
	// The initial set (#2345) is closed; the reason stays for the next gap a
	// new family or language surfaces.
	reasonGap = "genuine gap, wiring tracked in an open issue"
)

// offeredSpanFamilies lists, per language, the families its Spans hook
// produces. Every entry is verified against the probes: a listed family whose
// probe stays silent fails, so a removed wiring (or a rotted probe) cannot
// hide here.
var offeredSpanFamilies = map[string][]string{
	"ansible":    {famBase64, famCron, famNet, famNumber, famPerm, famSecret, famUnicode},
	"crontab":    {famCron, famSecret},
	"css":        {famUnicode},
	"dockerfile": {famPerm, famSecret},
	"dotenv":     {famNet, famNumber, famSecret},
	"go":         {famCron, famNet, famNumber, famPerm, famSecret, famUnicode},
	"html":       {famEntity},
	"http":       {famBase64, famNet, famNumber, famSecret},
	"ini":        {famNet, famNumber, famSecret},
	"json":       {famBase64, famCron, famNet, famNumber, famSecret, famUnicode},
	"log":        {famNumber},
	"markdown":   {famEntity},
	"ndjson":     {famBase64, famCron, famNet, famNumber, famSecret, famUnicode},
	"php":        {famCron, famEntity, famNet, famNumber, famPerm, famSecret, famUnicode},
	"python":     {famCron, famNet, famNumber, famPerm, famSecret, famUnicode},
	"shell":      {famPerm, famSecret, famUnicode},
	"toml":       {famCron, famNet, famNumber, famSecret, famUnicode},
	"typescript": {famCron, famEntity, famNet, famNumber, famPerm, famSecret, famUnicode},
	"xml":        {famEntity},
	"yaml":       {famBase64, famCron, famNet, famNumber, famPerm, famSecret, famUnicode},
}

// notOfferedSpanFamilies records why a language does not offer a family. A
// "*" entry covers every family the language neither offers nor lists
// specifically; a specific entry wins over the wildcard. Every entry is
// verified against the probes in the stale direction: a cell whose probe
// fires means the family has since been wired, and the entry must go.
var notOfferedSpanFamilies = []struct{ lang, family, reason string }{
	{"ansible", famEntity, reasonNoSyntax},
	{"crontab", famUnicode, reasonNoSyntax},
	{"crontab", famEntity, reasonNoSyntax},
	{"crontab", "*", reasonNoConvention},
	{"css", "*", reasonNoConvention},
	{"csv", "*", reasonForeignData},
	{"diff", "*", reasonForeignData},
	{"dockerfile", famUnicode, reasonNoSyntax},
	{"dockerfile", famEntity, reasonNoSyntax},
	{"dockerfile", "*", reasonNoConvention},
	{"dotenv", famUnicode, reasonNoSyntax}, // dotenv values process no escapes
	{"dotenv", famEntity, reasonNoSyntax},
	{"dotenv", "*", reasonNoConvention},
	{"go", famEntity, reasonNoSyntax},
	{"go", famBase64, reasonNoConvention},
	{"go.mod", "*", reasonNoConvention},
	{"go.sum", "*", reasonForeignData}, // checksum lines
	{"go.work", "*", reasonNoConvention},
	{"html", famUnicode, reasonNoSyntax}, // markup has no \u; scripts are typescript's
	{"html", "*", reasonNoConvention},
	{"http", famUnicode, reasonNoSyntax}, // percent-encoding is its own family
	{"http", famEntity, reasonNoSyntax},
	{"http", "*", reasonNoConvention},
	{"ini", famUnicode, reasonNoSyntax},
	{"ini", famEntity, reasonNoSyntax},
	{"ini", "*", reasonNoConvention},
	{"json", famEntity, reasonNoSyntax},
	{"json", famPerm, reasonNoConvention},
	{"log", "*", reasonForeignData},
	{"make", famUnicode, reasonNoSyntax},
	{"make", famEntity, reasonNoSyntax},
	{"make", "*", reasonNoConvention},
	{"markdown", "*", reasonNoConvention},
	{"markdown_inline", "*", reasonInjection},
	{"ndjson", famEntity, reasonNoSyntax},
	{"ndjson", famPerm, reasonNoConvention},
	{"php", famBase64, reasonNoConvention},
	{"psv", "*", reasonForeignData},
	{"python", famEntity, reasonNoSyntax},
	{"python", famBase64, reasonNoConvention},
	{"shell", famEntity, reasonNoSyntax},
	{"shell", "*", reasonNoConvention},
	{"sql", famUnicode, reasonNoSyntax}, // standard SQL; dialect U&'…' is niche
	{"sql", famEntity, reasonNoSyntax},
	{"sql", "*", reasonNoConvention},
	{"toml", famEntity, reasonNoSyntax},
	{"toml", famBase64, reasonNoConvention},
	{"toml", famPerm, reasonNoConvention},
	{"tsv", "*", reasonForeignData},
	{"typescript", famBase64, reasonNoConvention},
	{"xml", famUnicode, reasonNoSyntax},
	{"xml", "*", reasonNoConvention},
	{"yaml", famEntity, reasonNoSyntax},
}

// offersByProbe reports whether l's Spans hook produces a span of family f on
// f's probe buffers — the behavioural definition of "offers".
func offersByProbe(l lang.Language, f spanFamily) bool {
	if l.Spans == nil {
		return false
	}
	for _, probe := range f.probes {
		for _, s := range l.Spans(probe) {
			for _, c := range f.captures {
				if strings.HasPrefix(s.Capture, c) {
					return true
				}
			}
		}
	}
	return false
}

// ledgerOffered reports whether the ledger lists id as offering family.
func ledgerOffered(id, family string) bool {
	for _, f := range offeredSpanFamilies[id] {
		if f == family {
			return true
		}
	}
	return false
}

// specificReason returns the non-wildcard justification recorded for id not
// offering family, or "".
func specificReason(id, family string) string {
	for _, e := range notOfferedSpanFamilies {
		if e.lang == id && e.family == family {
			return e.reason
		}
	}
	return ""
}

// ledgerReason returns the recorded justification for id not offering family,
// or "" when there is none. A specific entry wins over the "*" wildcard.
func ledgerReason(id, family string) string {
	wildcard := ""
	for _, e := range notOfferedSpanFamilies {
		if e.lang != id {
			continue
		}
		if e.family == family {
			return e.reason
		}
		if e.family == "*" {
			wildcard = e.reason
		}
	}
	return wildcard
}

// TestEverySpanFamilyIsWiredOrJustified is the audit's guardrail: every
// registered language either offers each audited family (verified by probe)
// or records why it does not.
func TestEverySpanFamilyIsWiredOrJustified(t *testing.T) {
	for _, l := range lang.All() {
		for _, f := range spanFamilies {
			offered := ledgerOffered(l.ID, f.name)
			reason := ledgerReason(l.ID, f.name)
			fires := offersByProbe(l, f)
			switch {
			case offered && specificReason(l.ID, f.name) != "":
				t.Errorf("%s / %s is listed both as offered and with a reason: keep one (#2337)",
					l.ID, f.name)
			case offered && !fires:
				t.Errorf("ledger says %s offers %s, but the probe produced no %v span: "+
					"the wiring is gone, or the probe in spanFamilies rotted — fix the "+
					"Spans hook or the probe (#2337)", l.ID, f.name, f.captures)
			case offered:
				// wired and verified.
			case fires:
				t.Errorf("stale ledger entry for %s / %s: the language now offers the "+
					"family — delete its notOfferedSpanFamilies entry and add the family "+
					"to offeredSpanFamilies (#2337)", l.ID, f.name)
			case reason == "":
				t.Errorf("%s has no ledger entry for span family %s: wire the family in "+
					"its Spans hook (plugins/languages/%s) and list it in "+
					"offeredSpanFamilies, or record a reason in notOfferedSpanFamilies "+
					"(#2337)", l.ID, f.name, l.ID)
			}
		}
	}
}

// TestSpanFamilyLedgerIsCurrent keeps the ledger itself honest: every entry
// must name a registered language and a known family, so a renamed or removed
// language cannot leave a dead justification behind.
func TestSpanFamilyLedgerIsCurrent(t *testing.T) {
	known := map[string]bool{}
	for _, f := range spanFamilies {
		known[f.name] = true
	}
	registered := map[string]bool{}
	for _, l := range lang.All() {
		registered[l.ID] = true
	}
	ids := make([]string, 0, len(offeredSpanFamilies))
	for id := range offeredSpanFamilies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !registered[id] {
			t.Errorf("stale offeredSpanFamilies entry %q: no registered language has this id", id)
		}
		for _, f := range offeredSpanFamilies[id] {
			if !known[f] {
				t.Errorf("offeredSpanFamilies[%q] names unknown family %q", id, f)
			}
		}
	}
	for _, e := range notOfferedSpanFamilies {
		if !registered[e.lang] {
			t.Errorf("stale notOfferedSpanFamilies entry %s / %s: no registered language has this id",
				e.lang, e.family)
		}
		if e.family != "*" && !known[e.family] {
			t.Errorf("notOfferedSpanFamilies entry %s names unknown family %q", e.lang, e.family)
		}
	}
}
