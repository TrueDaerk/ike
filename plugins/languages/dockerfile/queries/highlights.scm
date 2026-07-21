; Dockerfile highlights, adapted from camdencheek/tree-sitter-dockerfile v0.2.0 (MIT).
; Capture names remapped to ike's theme captures (see internal/theme/builtins.go):
;   punctuation.special -> punctuation, @none dropped. ike additions: image
;   names/aliases -> type, ENV/ARG/LABEL keys -> property, expose ports -> number.

[
  "FROM"
  "AS"
  "RUN"
  "CMD"
  "LABEL"
  "EXPOSE"
  "ENV"
  "ADD"
  "COPY"
  "ENTRYPOINT"
  "VOLUME"
  "USER"
  "WORKDIR"
  "ARG"
  "ONBUILD"
  "STOPSIGNAL"
  "HEALTHCHECK"
  "SHELL"
  "MAINTAINER"
  "CROSS_BUILD"
  (heredoc_marker)
  (heredoc_end)
] @keyword

[
  ":"
  "@"
] @operator

(comment) @comment

(image_spec
  name: (image_name) @type)

(image_alias) @type

(env_pair
  name: (unquoted_string) @property)

(arg_instruction
  name: (unquoted_string) @property)

(label_pair
  key: (_) @property)

(expose_port) @number

[
  (double_quoted_string)
  (single_quoted_string)
  (json_string)
  (heredoc_line)
] @string

(escape_sequence) @escape

(expansion
  [
    "$"
    "{"
    "}"
  ] @punctuation)

(variable) @variable

((variable) @constant
  (#match? @constant "^[A-Z][A-Z_0-9]*$"))
