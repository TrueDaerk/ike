; HTML injections (#925), adapted from tree-sitter/tree-sitter-html v0.23.2
; (MIT) to ike's capture-name-driven fragment scheme: <script> bodies parse
; with the typescript grammar (the TSX superset, which covers plain JS) and
; <style> bodies with the css grammar.

((script_element
  (raw_text) @fragment.typescript))

((style_element
  (raw_text) @fragment.css))

; Inline code in attributes (#2329). Only the inner attribute_value is
; captured, so the surrounding quotes keep their HTML colour, and the
; injection is gated on the attribute *name* through a text predicate — the
; go-tree-sitter binding evaluates #match? while iterating matches, so no
; extra Go machinery is needed to make the rule name-conditional.
;
; Both values are snippets rather than documents, hence the .partial mode:
; the Go side parses a partial CSS fragment inside a synthetic `*{…}` rule
; (a bare declaration list otherwise reads as a selector), and skips partial
; fragments on the LSP virtual-document seam.

; Event-handler attributes: onclick, onload, oninput, … Matching the shape
; rather than enumerating the ~90 standard handlers keeps the rule free of
; maintenance, and the bounds make it exact in practice: names are
; case-insensitive in HTML, an unbroken run of letters excludes hyphenated
; framework attributes (data-on-x, hx-on:click), and requiring at least three
; letters after "on" excludes the non-handler attributes `once` and `only`
; while admitting every standard handler (the shortest is `oncut`).
((attribute
  (attribute_name) @_event
  (quoted_attribute_value (attribute_value) @fragment.typescript.partial))
 (#match? @_event "(?i)^on[a-z]{3,}$"))

; style="…" — a declaration list without selector or braces.
((attribute
  (attribute_name) @_style
  (quoted_attribute_value (attribute_value) @fragment.css.partial))
 (#match? @_style "(?i)^style$"))
