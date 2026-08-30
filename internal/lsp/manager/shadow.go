package manager

import (
	"hash/fnv"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	langreg "ike/internal/lang"
	"ike/internal/lsp"
)

// shadow.go implements the per-language shadow documents of an EmbeddedShadow
// host (#2330), VS Code virtual-document style: all embedded regions of one
// language (every <script> body of an HTML buffer) merge into a single
// virtual document that spans the whole host buffer, with every rune outside
// the regions blanked to a space (newlines preserved). Positions map 1:1
// between host and shadow — the shadow fragment starts at 0:0, so the
// ordinary hostToFrag/fragToHost mapping degenerates to identity — and the
// regions of one language share one scope, matching how a browser executes a
// page's script tags. Hosts without the flag keep the per-region fragment
// documents of fragments.go (an SQL string in Python is a standalone
// statement; merging separate strings would produce parse errors).

// detectedFragment is one virtual document to reconcile: the fragment
// mirrored into the document, plus — for shadow documents — the embedded
// regions that carry real code. Request routing only enters those regions;
// the blanked filler around them belongs to no language. A nil regions slice
// (the per-region case) routes by the fragment's own range.
type detectedFragment struct {
	frag    highlight.Fragment
	regions []highlight.Fragment
}

// plainDetected wraps the detector output 1:1 — the non-shadow default.
func plainDetected(found []highlight.Fragment) []detectedFragment {
	out := make([]detectedFragment, len(found))
	for i, fr := range found {
		out[i] = detectedFragment{frag: fr}
	}
	return out
}

// hostShadow reports whether the host language opted into shadow documents.
func hostShadow(hostLang string) bool {
	l, ok := langreg.ByID(hostLang)
	return ok && l.EmbeddedShadow
}

// shadowDetected merges the detected fragments per embedded language into one
// whole-buffer shadow fragment each. Slots follow the sorted language ids, so
// a re-detection with the same language set continues the same server-side
// documents; a language appearing or vanishing may shift the others' slots
// once (close + reopen), which reconciliation handles like any slot change.
func shadowDetected(lines []string, found []detectedFragment) []detectedFragment {
	byLang := map[string][]highlight.Fragment{}
	var langs []string
	for _, det := range found {
		if _, ok := byLang[det.frag.Lang]; !ok {
			langs = append(langs, det.frag.Lang)
		}
		byLang[det.frag.Lang] = append(byLang[det.frag.Lang], det.frag)
	}
	sort.Strings(langs)

	out := make([]detectedFragment, 0, len(langs))
	for _, lang := range langs {
		regions := byLang[lang]
		endLine := len(lines) - 1
		if endLine < 0 {
			continue
		}
		out = append(out, detectedFragment{
			frag: highlight.Fragment{
				Lang:      lang,
				StartLine: 0, StartCol: 0,
				EndLine: endLine, EndCol: len([]rune(lines[endLine])),
				Lines: shadowLines(lines, regions),
			},
			regions: regions,
		})
	}
	return out
}

// shadowLines returns the host lines with every rune outside the regions
// replaced by a space. Line count and per-line rune count are preserved, so
// editor positions are valid in both documents and the region text is
// byte-identical between host and shadow. Regions reaching outside the buffer
// are clamped.
func shadowLines(lines []string, regions []highlight.Fragment) []string {
	blanked := make([][]rune, len(lines))
	for i, l := range lines {
		src := []rune(l)
		b := make([]rune, len(src))
		for j := range b {
			b[j] = ' '
		}
		blanked[i] = b
	}
	for _, reg := range regions {
		for ln := max(reg.StartLine, 0); ln <= reg.EndLine && ln < len(lines); ln++ {
			src := []rune(lines[ln])
			from, to := 0, len(src)
			if ln == reg.StartLine && reg.StartCol > from {
				from = reg.StartCol
			}
			if ln == reg.EndLine && reg.EndCol < to {
				to = reg.EndCol
			}
			if from > len(src) {
				continue
			}
			copy(blanked[ln][from:to], src[from:to])
		}
	}
	out := make([]string, len(lines))
	for i, b := range blanked {
		out[i] = string(b)
	}
	return out
}

// contains reports whether a host position routes into this fragment
// document: inside one of the shadow regions when set, inside the fragment's
// own range otherwise. Caller holds m.mu (the fields are mutated under it).
func (fd *fragmentDoc) contains(pos buffer.Position) bool {
	if len(fd.regions) > 0 {
		for _, r := range fd.regions {
			if fragContains(r, pos) {
				return true
			}
		}
		return false
	}
	return fragContains(fd.frag, pos)
}

// fragmentPathMarker tags scheme-based fragment URIs so isFragmentURI can
// recognize them regardless of the scheme in front.
const fragmentPathMarker = "ike-embedded"

// fragmentURIFor builds the virtual-document URI for a fragment: the default
// ike-fragment scheme, unless the fragment server's spec declares a
// FragmentScheme (#2330) — vtsls only serves schemes on the VS Code
// TypeScript extension's supported list, so it declares "untitled".
func fragmentURIFor(spec lsp.ServerSpec, hostPath string, slot int, fragLang string) string {
	if spec.FragmentScheme == "" {
		return fragmentURI(hostPath, slot)
	}
	return schemeFragmentURI(spec.FragmentScheme, hostPath, slot, fragmentExt(fragLang))
}

// schemeFragmentURI shapes a fragment URI on a server-declared scheme as a
// plausible file path, e.g. untitled:/ike-embedded/a1b2c3d4/0/index.ts. The
// host path is folded into a hash instead of embedded raw: the URI must
// survive the server's parse→serialize round trip byte-identically (published
// diagnostics are matched back by exact string), and path runes outside the
// unreserved set would be percent-encoded on the way back. The extension lets
// the server infer the script kind.
func schemeFragmentURI(scheme, hostPath string, slot int, ext string) string {
	h := fnv.New32a()
	h.Write([]byte(hostPath))
	base := sanitizeURIPart(strings.TrimSuffix(filepath.Base(hostPath), filepath.Ext(hostPath)))
	return scheme + ":/" + fragmentPathMarker + "/" + strconv.FormatUint(uint64(h.Sum32()), 16) +
		"/" + strconv.Itoa(slot) + "/" + base + "." + ext
}

// fragmentExt is the file extension for a fragment language's virtual
// document: the language's first registered extension, the id itself as a
// fallback.
func fragmentExt(fragLang string) string {
	if l, ok := langreg.ByID(fragLang); ok && len(l.Extensions) > 0 {
		return strings.TrimPrefix(l.Extensions[0], ".")
	}
	return fragLang
}

// sanitizeURIPart reduces one path segment to URI-safe runes (letters,
// digits, ".", "_", "-"); everything else becomes "_" so the URI round-trips
// through the server without percent-encoding drift.
func sanitizeURIPart(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "doc"
	}
	return string(out)
}
