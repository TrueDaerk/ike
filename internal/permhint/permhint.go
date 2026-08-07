// Package permhint decodes octal file-permission literals into the symbolic
// form a reader actually knows (#1656). `755` says nothing until it is spelled
// out as `rwxr-xr-x`, and the four-digit forms hide the bits that matter most:
// `4755` is setuid (`rwsr-xr-x`), `1777` is the sticky bit (`rwxrwxrwt`). The
// editor draws the symbolic form after the literal as a conceal-style stand-in,
// display-only and revealed under the caret like every other hint family
// (#1585, #1594).
//
// It is a leaf package — pure Go over internal/lang's span type — so every
// context that carries a permission literal can call it without a dependency
// cycle. Which run of digits *is* a permission literal is the harder half and
// lives in spans.go: a bare three-digit number is a port or a year far more
// often than a mode, so only the carrying contexts (`chmod`, `--chmod=`,
// `os.Chmod`, a `mode:` key) produce a hint.
package permhint

import "strings"

// Capture is the highlight capture carried by a hint span. The editor treats it
// as a conceal family of its own — gated by editor.permission_hints /
// view.togglePermissionHints — independent of the decode families (#1618,
// #1620) and of the other hint families (#1624, #1627, #1653).
const Capture = "perm.mode"

// Gap is the separator drawn between the literal and its hint inside the
// stand-in — the same two spaces the cron, number and network hints use.
const Gap = "  "

// Describe renders an octal permission literal as its symbolic form: `755` as
// "rwxr-xr-x", `0644` as "rw-r--r--", `2775` as "rwxrwsr-x". The Go/Python
// `0o` prefix and leading zeros are accepted; it reports false when text is not
// three or four octal digits.
//
// A fourth (leading) digit is the special-bit field: 4 setuid, 2 setgid,
// 1 sticky. Each replaces the execute character of its triad — `x` becomes `s`
// (`t` for sticky), and with the execute bit clear the capital form `S` (`T`)
// says the special bit is set without it, which is what `ls` prints.
func Describe(text string) (string, bool) {
	digits, ok := octalDigits(text)
	if !ok {
		return "", false
	}
	special := 0
	if len(digits) == 4 {
		special, digits = digits[0], digits[1:]
	}
	var b strings.Builder
	b.Grow(9)
	for i, d := range digits {
		b.WriteByte(flag(d&4 != 0, 'r'))
		b.WriteByte(flag(d&2 != 0, 'w'))
		b.WriteByte(execChar(d&1 != 0, special, i))
	}
	return b.String(), true
}

// HasOctalPrefix reports whether text is written in a form that marks it as
// octal on its own — a leading `0` or the `0o` prefix. The code and YAML
// contexts require it: in Go and Python a bare `755` is decimal, and in YAML
// `mode: 644` is the decimal 644 the number hints already read as `= 01204`
// (#1627), so a permission hint there would state the opposite of the truth.
// The shell contexts do not: `chmod 755` is octal by definition.
func HasOctalPrefix(text string) bool {
	s := strings.TrimSpace(text)
	return len(s) >= 4 && s[0] == '0'
}

// octalDigits normalizes a literal to its octal digits: the `0o` prefix and any
// leading zeros past four digits are dropped. It reports false when what is
// left is not three or four octal digits.
func octalDigits(text string) ([]int, bool) {
	s := strings.TrimSpace(text)
	if len(s) > 2 && s[0] == '0' && (s[1] == 'o' || s[1] == 'O') {
		s = s[2:]
	}
	for len(s) > 4 && s[0] == '0' {
		s = s[1:]
	}
	if len(s) != 3 && len(s) != 4 {
		return nil, false
	}
	out := make([]int, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '7' {
			return nil, false
		}
		out[i] = int(s[i] - '0')
	}
	return out, true
}

// flag renders one plain permission bit.
func flag(set bool, c byte) byte {
	if set {
		return c
	}
	return '-'
}

// specialBits are the special-bit masks in triad order: setuid on the user
// triad, setgid on the group one, sticky on the other one.
var specialBits = [3]int{4, 2, 1}

// execChar renders the execute character of triad i, folding in the special bit
// that triad carries.
func execChar(x bool, special, i int) byte {
	if special&specialBits[i] == 0 {
		return flag(x, 'x')
	}
	set, clear := byte('s'), byte('S')
	if i == 2 {
		set, clear = 't', 'T' // the sticky bit prints as t, not s
	}
	if x {
		return set
	}
	return clear
}
