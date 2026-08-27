package httpfile

// refs.go answers the reverse of vars.go's question (#2158): not "what does
// {{name}} resolve to" but "which names does this file reference, and where".
// The editor needs the second one to warn about a reference no rung of the
// chain defines — a typo'd `{{hsot}}` used to surface only at request time,
// as a failed dispatch, which is late and far from the line that caused it.

import (
	"strings"
	"unicode/utf8"
)

// Reference is one `{{name}}` placeholder occurrence: the referenced name and
// the span it covers, counted the way the editor counts — a 1-based line and
// 0-based rune columns, the braces included on both ends.
type Reference struct {
	Name     string
	Line     int
	StartCol int
	EndCol   int
}

// References lists every user-defined `{{name}}` placeholder of src, in file
// order. Two kinds of text are deliberately left out:
//
//   - The process-environment forms (`{{$env NAME}}`, `${NAME}`). They resolve
//     against the environment of whoever runs IKE, which no file-local
//     knowledge can judge.
//   - Comment lines. A `# see {{host}}` is prose, and the `# @capture name`
//     directive is a definition rather than a reference — so is the `###`
//     separator's request name.
//
// A definition's own value is included (`@api = {{host}}/api`), because it is
// substituted like any other text and a typo in it fails exactly the same way.
func References(src string) []Reference {
	var out []Reference
	for i, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if isComment(line) {
			continue
		}
		for _, m := range placeholderRE.FindAllStringSubmatchIndex(line, -1) {
			// Submatch 3 is the user-defined form; a match of the other two
			// is one of the process-environment spellings.
			if m[6] < 0 {
				continue
			}
			out = append(out, Reference{
				Name:     line[m[6]:m[7]],
				Line:     i + 1,
				StartCol: utf8.RuneCountInString(line[:m[0]]),
				EndCol:   utf8.RuneCountInString(line[:m[1]]),
			})
		}
	}
	return out
}
