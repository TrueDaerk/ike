package lsp

import "strings"

// undefmember.go classifies Intelephense's undefined-member diagnostics
// (0520, #2669). Inside a trait body the server resolves `$this` as the
// trait itself, so every member that only exists on the trait's consumers is
// reported undefined. The app's trait suppression pass needs two things off
// such a diagnostic — which kind of member is missing and what it is called —
// and this is where the server's vocabulary is kept: one code table plus the
// message shape as a secondary check, so a code reused for something else by
// a future server version cannot silently widen the pass.

// UndefinedMemberKind is the kind of member an undefined-member diagnostic
// reports missing.
type UndefinedMemberKind int

const (
	// UndefinedMethod is `$this->abc()` with no `abc` method in scope.
	UndefinedMethod UndefinedMemberKind = iota
	// UndefinedProperty is `$this->abc` with no `$abc` property in scope.
	UndefinedProperty
	// UndefinedClassConst is `self::ABC` with no `ABC` constant in scope.
	UndefinedClassConst
)

// word is the noun the server's message uses for the kind — the secondary
// check on top of the code.
func (k UndefinedMemberKind) word() string {
	switch k {
	case UndefinedProperty:
		return "property"
	case UndefinedClassConst:
		return "constant"
	}
	return "method"
}

// IntelephenseSource is the diagnostic source Intelephense publishes under.
const IntelephenseSource = "intelephense"

// undefinedMemberCodes maps Intelephense's undefined-member diagnostic codes
// to the member kind they report. The codes are published as "P1013" and
// friends; normUndefCode strips the prefix, so the table is keyed by the
// number alone and an unprefixed variant matches too.
var undefinedMemberCodes = map[string]UndefinedMemberKind{
	"1012": UndefinedClassConst, // Undefined class constant 'K'.
	"1013": UndefinedMethod,     // Undefined method 'abc'.
	"1014": UndefinedProperty,   // Undefined property '$abc'.
}

// UndefinedMember reports whether d is an Intelephense undefined-member
// diagnostic, and returns the kind of member and the name the message quotes.
// A property name keeps its `$`. All three must line up — the source, a code
// from the table and a message naming that kind of member in quotes — so an
// unrelated diagnostic sharing a code is never matched.
func UndefinedMember(d Diagnostic) (kind UndefinedMemberKind, name string, ok bool) {
	if d.Source != "" && !strings.EqualFold(d.Source, IntelephenseSource) {
		return 0, "", false
	}
	kind, ok = undefinedMemberCodes[normUndefCode(d.Code)]
	if !ok {
		return 0, "", false
	}
	if !strings.Contains(strings.ToLower(d.Message), kind.word()) {
		return 0, "", false
	}
	if name = quotedName(d.Message); name == "" {
		return 0, "", false
	}
	return kind, name, true
}

// normUndefCode reduces a published code to its number: "P1013" → "1013".
func normUndefCode(code string) string {
	c := strings.TrimSpace(code)
	if len(c) > 1 && (c[0] == 'P' || c[0] == 'p') {
		c = c[1:]
	}
	return c
}

// quotedName returns the first single-quoted run of a message, "" when the
// message quotes nothing. Intelephense always names the missing member that
// way ("Undefined method 'abc'.").
func quotedName(msg string) string {
	i := strings.IndexByte(msg, '\'')
	if i < 0 {
		return ""
	}
	rest := msg[i+1:]
	j := strings.IndexByte(rest, '\'')
	if j <= 0 {
		return ""
	}
	return rest[:j]
}
