package lang

// template.go renders a language's initial content for a newly created file
// (#170): `package <dir>` for Go, `<?php` for PHP, and so on. A template is
// registered on the Language (see Language.Template) and can be overridden per
// language by the user via `[lang.<id>] template = "..."` in the config — an
// explicitly empty override disables the template. Rendering substitutes the
// `${...}` variables documented on expandTemplate.
//
// This is the registry's one IKE import: internal/config, itself a leaf
// package that imports nothing in IKE, so the no-cycle guarantee for
// internal/highlight and internal/lsp still holds.

import (
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"ike/internal/config"
)

// TemplateFor returns the rendered initial content for a new file at path, or
// "" when no language matches the path, the matched language registers no
// template, or the user disabled it with an empty `[lang.<id>] template`.
func TemplateFor(path string) string {
	l, ok := ByPath(path)
	if !ok {
		return ""
	}
	tpl := l.Template
	if o, set := config.Get().Lang[l.ID]["template"]; set {
		tpl = o
	}
	return expandTemplate(tpl, path, time.Now())
}

// expandTemplate substitutes the template variables:
//
//	${FILENAME}  base name with extension       "main.go"
//	${NAME}      base name without extension    "main"
//	${DIR}       containing directory's name    "mypkg"
//	${PACKAGE}   ${DIR} sanitised to an identifier ("my-pkg" -> "mypkg")
//	${DATE}      now as YYYY-MM-DD
//	${YEAR}      now as YYYY
//
// path is made absolute first so a relative `:e foo.go` still resolves its
// directory name; on failure (unlikely) the path is used as given.
func expandTemplate(tpl, path string, now time.Time) string {
	if tpl == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	dir := filepath.Base(filepath.Dir(path))
	return strings.NewReplacer(
		"${FILENAME}", base,
		"${NAME}", name,
		"${DIR}", dir,
		"${PACKAGE}", identifier(dir),
		"${DATE}", now.Format("2006-01-02"),
		"${YEAR}", now.Format("2006"),
	).Replace(tpl)
}

// identifier lowercases s and strips everything that cannot appear in an
// identifier (keeping letters, digits and '_', with no leading digit), so
// `${PACKAGE}` yields a valid Go package name even for a directory like
// "my-project". An empty result falls back to "main".
func identifier(s string) string {
	var b []rune
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '_' || unicode.IsLetter(r):
			b = append(b, r)
		case unicode.IsDigit(r) && len(b) > 0:
			b = append(b, r)
		}
	}
	if len(b) == 0 {
		return "main"
	}
	return string(b)
}
