; Makefile injections (#1136), following the HTML <script>/<style> pattern
; (#925) in ike's capture-name-driven fragment scheme: recipe bodies and
; $(shell …) / != command text parse with the shell grammar (#894, official
; tree-sitter-bash — sh/zsh highlight best-effort under it). Make constructs
; inside a recipe line ($(VAR), automatic variables) are sibling nodes of the
; (shell_text), so they keep their Makefile colouring between the injected
; shell tokens.

((shell_text) @fragment.shell)

((shell_command) @fragment.shell)
