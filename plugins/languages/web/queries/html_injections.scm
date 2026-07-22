; HTML injections (#925), adapted from tree-sitter/tree-sitter-html v0.23.2
; (MIT) to ike's capture-name-driven fragment scheme: <script> bodies parse
; with the typescript grammar (the TSX superset, which covers plain JS) and
; <style> bodies with the css grammar.

((script_element
  (raw_text) @fragment.typescript))

((style_element
  (raw_text) @fragment.css))
