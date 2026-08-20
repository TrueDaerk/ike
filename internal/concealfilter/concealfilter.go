// Package concealfilter decides where the conceal families may draw (#1704).
//
// Every conceal family already has an on/off switch — a config default plus a
// per-view toggle — but the switch is global: masking a dotenv value is
// welcome in a checked-in `.env.example` and unwelcome in a fixture whose
// point is the literal bytes. This package adds the missing dimension: file
// patterns, matched against the buffer's path, that gate the families
// independently of their toggles.
//
// Two levels compose. The global level (editor.conceal_include /
// editor.conceal_exclude) covers every family at once. The per-family level
// (editor.conceal_file_rules, entries written `family=pattern`) overrides it
// for one family. Within a level exclude beats include, and a family that
// decides a path on its own level never consults the global one — that is what
// makes it an override rather than a second opinion.
//
// The gate composes with, and never replaces, the toggles: a stand-in draws
// only if its family is on *and* the path passes the filter. The editor folds
// the verdict into the family's resolved config default (editor.applyConfig),
// so an explicit per-view toggle still wins — a filter states a default, not a
// prohibition.
package concealfilter

import (
	"sort"
	"strings"

	"ike/internal/pathglob"
)

// The conceal families, named by the suffix of their editor.* toggle key so a
// rule reads like the setting it overrides ("secret_masking=-*.log"). Only
// families that hide or annotate source text are gateable; colour swatches and
// identifier colours change no columns and are deliberately not here.
const (
	MarkdownRendering     = "markdown_rendering"
	CSVRendering          = "csv_rendering"
	LogRendering          = "log_rendering"
	TimestampDecoding     = "timestamp_decoding"
	UnicodeEscapeDecoding = "unicode_escape_decoding"
	EntityDecoding        = "entity_decoding"
	Base64Decoding        = "base64_decoding"
	CronHints             = "cron_hints"
	PemSummary            = "pem_summary"
	ByteSizeHints         = "byte_size_hints"
	DurationHints         = "duration_hints"
	DigitGrouping         = "digit_grouping"
	RadixHints            = "radix_hints"
	PermissionHints       = "permission_hints"
	CIDRHints             = "cidr_hints"
	IDNHints              = "idn_hints"
	SecretMasking         = "secret_masking"
)

// IsFamily reports whether name is a registered conceal family — the
// validation the rule parser applies, exported for the intention catalog's
// family→toggle map (#2020).
func IsFamily(name string) bool { return families[name] }

// families is the set a rule may name, for validation and for the settings
// description.
var families = map[string]bool{
	MarkdownRendering:     true,
	CSVRendering:          true,
	LogRendering:          true,
	TimestampDecoding:     true,
	UnicodeEscapeDecoding: true,
	EntityDecoding:        true,
	Base64Decoding:        true,
	CronHints:             true,
	PemSummary:            true,
	ByteSizeHints:         true,
	DurationHints:         true,
	DigitGrouping:         true,
	RadixHints:            true,
	PermissionHints:       true,
	CIDRHints:             true,
	IDNHints:              true,
	SecretMasking:         true,
}

// Families lists the family names a rule may name, sorted.
func Families() []string {
	out := make([]string, 0, len(families))
	for f := range families {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// patterns is one level's include/exclude pair.
type patterns struct {
	include []string
	exclude []string
}

// Rules is a compiled filter. The zero value allows every family everywhere,
// which is what an unconfigured IKE wants.
type Rules struct {
	global   patterns
	byFamily map[string]patterns
}

// Compile builds the filter from the three raw settings: the global include
// and exclude lists and the per-family rules (`family=pattern`, the pattern
// prefixed `-` or `!` for an exclude, bare or `+` for an include). Blank
// entries and rules naming no known family are dropped — a typo silences one
// rule, never the whole filter; Invalid reports them for the config warnings.
func Compile(include, exclude, rules []string) Rules {
	r := Rules{global: patterns{include: clean(include), exclude: clean(exclude)}}
	for _, raw := range rules {
		fam, pat, exclude, ok := parseRule(raw)
		if !ok {
			continue
		}
		if r.byFamily == nil {
			r.byFamily = map[string]patterns{}
		}
		p := r.byFamily[fam]
		if exclude {
			p.exclude = append(p.exclude, pat)
		} else {
			p.include = append(p.include, pat)
		}
		r.byFamily[fam] = p
	}
	return r
}

// Empty reports whether the filter constrains nothing, so callers can skip it.
func (r Rules) Empty() bool {
	return len(r.global.include) == 0 && len(r.global.exclude) == 0 && len(r.byFamily) == 0
}

// Allows reports whether the conceal family may draw in the file at path.
//
// Precedence, per level, is exclude > include > default-allow. The family's
// own level is consulted first and, when it decides — an exclude match, or a
// non-empty include list — it is final; only a family with nothing to say
// falls through to the global level.
//
// A buffer with no path (an untitled scratch) has no name to match and is
// always allowed: an include list names files that exist, and letting it hide
// every stand-in in a buffer the user just opened would read as a bug.
func (r Rules) Allows(family, path string) bool {
	if path == "" {
		return true
	}
	if p, ok := r.byFamily[family]; ok {
		if verdict, decided := p.verdict(path); decided {
			return verdict
		}
	}
	if verdict, decided := r.global.verdict(path); decided {
		return verdict
	}
	return true
}

// verdict applies one level's rules; decided is false when the level says
// nothing about path.
func (p patterns) verdict(path string) (allow, decided bool) {
	for _, pat := range p.exclude {
		if matchPath(pat, path) {
			return false, true
		}
	}
	if len(p.include) == 0 {
		return false, false
	}
	for _, pat := range p.include {
		if matchPath(pat, path) {
			return true, true
		}
	}
	return false, true
}

// Invalid lists the conceal_file_rules entries that name no known family or
// carry no pattern, in input order — the app surfaces them as config warnings
// (a dropped rule is otherwise invisible).
func Invalid(rules []string) []string {
	var out []string
	for _, raw := range rules {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if _, _, _, ok := parseRule(raw); !ok {
			out = append(out, strings.TrimSpace(raw))
		}
	}
	return out
}

// parseRule splits one `family=pattern` entry. The family may be written bare
// (`secret_masking`) or with the setting's prefix (`editor.secret_masking`),
// case-insensitively; a `-` or `!` in front of the pattern makes it an
// exclude, `+` or nothing an include.
func parseRule(raw string) (family, pattern string, exclude, ok bool) {
	s := strings.TrimSpace(raw)
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return "", "", false, false
	}
	family = strings.ToLower(strings.TrimSpace(s[:i]))
	family = strings.TrimPrefix(family, "editor.")
	pattern = strings.TrimSpace(s[i+1:])
	switch {
	case strings.HasPrefix(pattern, "-"), strings.HasPrefix(pattern, "!"):
		pattern, exclude = strings.TrimSpace(pattern[1:]), true
	case strings.HasPrefix(pattern, "+"):
		pattern = strings.TrimSpace(pattern[1:])
	}
	if !families[family] || pattern == "" {
		return "", "", false, false
	}
	return family, pattern, exclude, true
}

// clean drops blank entries from a raw pattern list.
func clean(pats []string) []string {
	var out []string
	for _, p := range pats {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchPath matches one pattern against a buffer path, case-insensitively
// (the filesystems IKE runs on mostly are, and `*.PY` surprising nobody is
// worth more than the purity).
//
// A pattern without a separator matches the base name — `*.py`, `Makefile` —
// which is what a per-filetype rule wants. A pattern with one matches the
// whole path, anchored at any segment boundary unless it starts with `/` or
// `**`, so `vendor/**`, `**/vendor/**` and `/etc/**` all mean what they read
// as.
func matchPath(pattern, path string) bool {
	// Backslashes are replaced outright rather than via filepath.ToSlash: the
	// path may have been typed with Windows separators on any host, and a
	// glob never contains one.
	p := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	pat := strings.ToLower(pattern)
	if !strings.Contains(pat, "/") {
		return pathglob.Match(pat, pathBase(p))
	}
	if strings.HasPrefix(pat, "/") || strings.HasPrefix(pat, "**") {
		return pathglob.Match(pat, p)
	}
	return pathglob.Match(pat, p) || pathglob.Match("**/"+pat, p)
}

// pathBase is path.Base over an already-slashed path, without the "." and "/"
// special cases path.Base carries (a buffer path is a file name).
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
