; Embedded-language injections for template literals (#1625). Capture names
; follow the ike convention: fragment.<lang>[.guess] — .guess means the
; Go-side content heuristic decides. The text chunks around ${…}
; substitutions are separate string_fragment nodes; the Go side groups the
; chunks of one template (same capture name, same parent) and judges their
; joined text, so on a hit each chunk injects while the substitution
; expressions keep their TypeScript highlighting.
(template_string (string_fragment) @fragment.html.guess)
(template_string (string_fragment) @fragment.sql.guess)
