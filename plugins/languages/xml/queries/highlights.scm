; XML highlights, adapted from tree-sitter-grammars/tree-sitter-xml
; (master @ 5000ae8, MIT — see grammar/LICENSE) to ike's theme captures (see
; internal/theme/builtins.go): upstream's markup.* / string.special.symbol /
; @error captures have no theme entry here, so they are remapped onto the
; concrete captures (string, comment, punctuation, …). Dotted captures such
; as constant.builtin and punctuation.delimiter resolve via the theme's
; dotted fallback. Specialised patterns come before their general fallback —
; CaptureAt is first-covering-wins.

;; XML declaration (<?xml version="1.0" encoding="UTF-8"?>).

"xml" @keyword

[ "version" "encoding" "standalone" ] @property

(EncName) @string

(VersionNum) @number

[ "yes" "no" ] @boolean

;; Processing instructions.

(PI) @embedded

(PI (PITarget) @keyword)

(XmlModelPI "xml-model" @keyword)

(StyleSheetPI "xml-stylesheet" @keyword)

(PseudoAtt (Name) @property)

(PseudoAtt (PseudoAttValue) @string)

;; Doctype and the internal subset (element / attlist / entity / notation
;; declarations parse under the xml grammar even though the standalone DTD
;; grammar is not vendored).

(doctypedecl "DOCTYPE" @keyword)

(doctypedecl (Name) @type)

(elementdecl
  "ELEMENT" @keyword
  (Name) @tag)

(contentspec
  (_ (Name) @property))

"#PCDATA" @type

[ "EMPTY" "ANY" ] @type

(GEDecl
  "ENTITY" @keyword
  (Name) @constant)

(GEDecl (EntityValue) @string)

(NDataDecl
  "NDATA" @keyword
  (Name) @label)

(PEDecl
  "ENTITY" @keyword
  "%" @operator
  (Name) @constant)

(PEDecl (EntityValue) @string)

(NotationDecl
  "NOTATION" @keyword
  (Name) @constant)

(NotationDecl
  (ExternalID
    (SystemLiteral (URI) @string)))

(AttlistDecl
  "ATTLIST" @keyword
  (Name) @tag)

(AttDef (Name) @property)

(AttDef (Enumeration (Nmtoken) @string))

(DefaultDecl (AttValue) @string)

[
  (StringType)
  (TokenizedType)
] @type

(NotationType "NOTATION" @type)

[
  "#REQUIRED"
  "#IMPLIED"
  "#FIXED"
] @attribute

;; Entity and character references. The five predefined entities are
;; builtins; everything else is a plain constant.

((EntityRef) @constant.builtin
 (#any-of? @constant.builtin
   "&amp;" "&lt;" "&gt;" "&quot;" "&apos;"))

(EntityRef) @constant

(CharRef) @constant

(PEReference) @constant

;; External references.

[ "PUBLIC" "SYSTEM" ] @keyword

(PubidLiteral) @string

(SystemLiteral (URI) @string)

;; Tags.

(STag (Name) @tag)

(ETag (Name) @tag)

(EmptyElemTag (Name) @tag)

;; Attributes.

(Attribute (Name) @attribute)

(Attribute (AttValue) @string)

;; CDATA: the <![CDATA[ / ]]> fences read as punctuation, the payload as a
;; raw string.

(CDSect
  (CDStart) @punctuation
  (CData) @string
  "]]>" @punctuation)

;; Comments.

(Comment) @comment

;; Delimiters, brackets and operators. These come last: a more specific
;; capture above must win over the generic punctuation sweep.

[
 "<?" "?>"
 "<!" "]]>"
 "<" ">"
 "</" "/>"
] @punctuation.delimiter

[ "(" ")" "[" "]" ] @punctuation.bracket

[ "\"" "'" ] @punctuation.delimiter

[ "," "|" "=" ] @operator

[ "*" "?" "+" ] @operator
