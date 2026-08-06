package langyaml

import (
	"regexp"
	"strings"

	"ike/internal/lang"
)

// regions.go detects shell fragments in CI-style YAML (#1625): the value of a
// `run:` key — a GitHub Actions / CircleCI step script — highlights with the
// shell grammar through the embedded-region seam (lang.Language.Regions,
// #1303). An injection query cannot express this: tree-sitter-yaml exposes the
// key and value as plain scalars and the decision needs the key text plus a
// buffer-level gate. Only buffers that look like a CI pipeline participate —
// a `steps:` line must exist somewhere — so arbitrary YAML carrying a run:
// key (or prose that happens to contain one) stays plain YAML.

// runKey matches a `run:` mapping key, plain or as a sequence item
// (`- run:`), capturing the leading indentation and the rest of the line.
var runKey = regexp.MustCompile(`^(\s*(?:-\s+)?)run:(\s*)(.*)$`)

// blockScalarHeader matches a YAML block-scalar introducer with optional
// indentation indicator and chomping modifier (|, >, |-, >2+, …), optionally
// followed by a comment.
var blockScalarHeader = regexp.MustCompile(`^[|>][0-9]*[+-]?[0-9]*(?:[ \t]+#.*)?$`)

// stepsLine gates the detector to CI-looking buffers.
var stepsLine = regexp.MustCompile(`^\s*steps:\s*(#.*)?$`)

// shellRegions is the lang.Language.Regions hook for YAML.
func shellRegions(lines []string) []lang.Region {
	ci := false
	for _, l := range lines {
		if stepsLine.MatchString(l) {
			ci = true
			break
		}
	}
	if !ci {
		return nil
	}
	var out []lang.Region
	for i := 0; i < len(lines); i++ {
		m := runKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		rest := m[3]
		if rest == "" {
			continue
		}
		if blockScalarHeader.MatchString(rest) {
			if r, next, ok := blockScalarRegion(lines, i, len(m[1])); ok {
				out = append(out, r)
				i = next
			}
			continue
		}
		if r, ok := inlineRegion(i, len(m[1])+len("run:")+len(m[2]), rest); ok {
			out = append(out, r)
		}
	}
	return out
}

// blockScalarRegion resolves the extent of a block scalar whose header sits on
// line i with the key indented by keyIndent columns. Content indentation is
// taken from the first non-empty following line and must exceed the key
// indent; the region runs while lines stay at least that indented (empty
// lines pass through), with trailing blanks trimmed. It returns the region
// and the last line consumed.
func blockScalarRegion(lines []string, i, keyIndent int) (lang.Region, int, bool) {
	content := -1
	end := i
	for j := i + 1; j < len(lines); j++ {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := len(l) - len(strings.TrimLeft(l, " \t"))
		if content < 0 {
			if ind <= keyIndent {
				break
			}
			content = ind
		}
		if ind < content {
			break
		}
		end = j
	}
	if content < 0 || end <= i {
		return lang.Region{}, i, false
	}
	return lang.Region{
		Lang:      "shell",
		StartLine: i + 1,
		StartCol:  0,
		EndLine:   end,
		EndCol:    len(lines[end]),
	}, end, true
}

// inlineRegion resolves a single-line `run: <command>` value starting at
// column col of line i. Matching single or double quotes are stripped by
// narrowing the region; a plain scalar is cut at YAML's inline comment
// (` #`).
func inlineRegion(i, col int, rest string) (lang.Region, bool) {
	end := col + len(rest)
	if len(rest) >= 2 && (rest[0] == '\'' || rest[0] == '"') && rest[len(rest)-1] == rest[0] {
		col++
		end--
	} else if cut := strings.Index(rest, " #"); cut >= 0 {
		end = col + cut
	}
	if end <= col {
		return lang.Region{}, false
	}
	return lang.Region{Lang: "shell", StartLine: i, StartCol: col, EndLine: i, EndCol: end}, true
}
