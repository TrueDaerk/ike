// associations.go resolves the user-configured file associations (#1365):
// the `[files.associations]` slot map in the config maps a file pattern —
// `"*.mytool" = "toml"`, `"Jenkinsfile" = "groovy"` — to a registered
// language id. Explicit user intent beats detection, so ByPath consults this
// layer before its own indexes, and the editor's open-time sniff skips
// content sniffing for an associated file.

package lang

import (
	"path/filepath"
	"sort"

	"ike/internal/config"
)

// ByAssociation resolves path through the user's [files.associations] map.
// An exact base-name key wins over glob patterns; glob patterns are tried in
// sorted key order (deterministic when several match) via filepath.Match
// against the base name. A pattern whose target id names no registered
// language never matches — the file falls through to the built-in detection
// and, failing that, plain text (fail soft; InvalidAssociations reports it).
func ByAssociation(path string) (Language, bool) {
	assoc := config.Get().Files.Associations
	if len(assoc) == 0 {
		return Language{}, false
	}
	base := filepath.Base(path)
	if id, ok := assoc[base]; ok {
		if l, found := ByID(id); found {
			return l, true
		}
	}
	patterns := make([]string, 0, len(assoc))
	for p := range assoc {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)
	for _, p := range patterns {
		if p == base {
			continue // exact match already tried
		}
		if ok, err := filepath.Match(p, base); err == nil && ok {
			if l, found := ByID(assoc[p]); found {
				return l, true
			}
		}
	}
	return Language{}, false
}

// InvalidAssociation is one [files.associations] entry whose target names no
// registered language.
type InvalidAssociation struct {
	Pattern string
	Lang    string
}

// InvalidAssociations lists the configured associations that cannot resolve
// because their language id is not registered, sorted by pattern. The app
// surfaces them as warnings on config load/reload (#1365); the files
// themselves fall back to plain text.
func InvalidAssociations() []InvalidAssociation {
	var out []InvalidAssociation
	for p, id := range config.Get().Files.Associations {
		if _, ok := ByID(id); !ok {
			out = append(out, InvalidAssociation{Pattern: p, Lang: id})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pattern < out[j].Pattern })
	return out
}
