; Markdown block injections (#880), adapted from
; tree-sitter-grammars/tree-sitter-markdown v0.5.3 (MIT) to ike's
; capture-name-driven fragment scheme (internal/highlight/fragment.go):
;
;  - every (inline) node is parsed with the markdown_inline grammar
;  - fenced code blocks inject the language named by their info string —
;    the dynamic @fragment.language / @fragment.content pair, resolved as a
;    language id first, then as a file extension
;  - YAML/TOML front matter injects the yaml/toml grammars
;  - html_block stays declared (harmless while html has no grammar)

(fenced_code_block
  (info_string
    (language) @fragment.language)
  (code_fence_content) @fragment.content)

((inline) @fragment.markdown_inline)

((minus_metadata) @fragment.yaml)

((plus_metadata) @fragment.toml)

((html_block) @fragment.html)
