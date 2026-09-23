// Package vaultinline recognises inline Ansible Vault values in YAML text
// (#2712): a `!vault |` tagged block scalar whose lines are the hex-armored
// `$ANSIBLE_VAULT;` envelope, the shape `ansible-vault encrypt_string`
// prints. Whole-file vaults are internal/ansiblevault's business (the buffer
// holds plaintext there); an inline value keeps its ciphertext in the buffer,
// and this package is the leaf the YAML span producer, the editor's stand-in
// layer and the intention probes share: one scanner decides what a vault
// block is, so the rendered row, the explain popover and the edit actions
// can never disagree about its extent.
//
// Detection reads structure only — the tag, the header line, the run of hex
// lines at the block's indentation. The hex body is not decoded here; the
// envelope is handed to ansiblevault.Decrypt only when the user asks for the
// value, and the plaintext never comes back through this package.
package vaultinline

import (
	"strconv"
	"strings"

	"ike/internal/ansiblevault"
	"ike/internal/lang"
)

// Capture is the highlight capture of the stand-in span on a block's header
// line; BodyCapture marks each hex line of the same block. Both ride the
// editor's decode channel, under the conceal family Family.
const (
	Capture     = "vault.value"
	BodyCapture = "vault.body"
	Family      = "vault"
)

// Tag is the YAML tag that marks an inline vault value.
const Tag = "!vault"

// Block is one inline vault value. Key is the line carrying the `!vault` tag
// (a mapping key or a sequence item), Head the `$ANSIBLE_VAULT;` header line,
// End the last hex line — every line index is 0-based and inclusive. Indent
// is the header's rune column, TagCol the tag's column on the key line.
type Block struct {
	Key, Head, End int
	Indent, TagCol int
	Version        string // "1.1" or "1.2"
	Cipher         string // "AES256"
	Label          string // the vault id of a 1.2 header, "" otherwise
}

// Lines is the number of hex lines in the body.
func (b Block) Lines() int { return b.End - b.Head }

// Contains reports whether line lies inside the block, key line included.
func (b Block) Contains(line int) bool { return line >= b.Key && line <= b.End }

// StandIn is the one-row reading of the block: the cipher, the vault id when
// the header names one, and the number of hex lines the row replaces.
func (b Block) StandIn() string {
	var sb strings.Builder
	sb.WriteString("⟨vault ")
	sb.WriteString(b.Cipher)
	if b.Label != "" {
		sb.WriteString(" · id: ")
		sb.WriteString(b.Label)
	}
	sb.WriteString(" · ")
	sb.WriteString(strconv.Itoa(b.Lines()))
	if b.Lines() == 1 {
		sb.WriteString(" line⟩")
	} else {
		sb.WriteString(" lines⟩")
	}
	return sb.String()
}

// Scan returns every inline vault block of lines, in buffer order.
func Scan(lines []string) []Block {
	return ScanLines(len(lines), func(i int) string { return lines[i] })
}

// ScanLines is Scan over a line accessor: n lines, at(i) the i-th.
func ScanLines(n int, at func(int) string) []Block {
	var out []Block
	for i := 0; i < n; i++ {
		if b, ok := fromKey(n, at, i); ok {
			out = append(out, b)
			i = b.End
		}
	}
	return out
}

// At returns the block containing line (key line through last hex line) of
// a buffer read through at, n lines long.
func At(n int, at func(int) string, line int) (Block, bool) {
	if line < 0 || line >= n {
		return Block{}, false
	}
	// A block is at most a handful of lines; walk up to the nearest tag
	// line rather than scanning the whole buffer for a caret probe.
	for k := line; k >= 0 && line-k <= maxBlockLines; k-- {
		if _, ok := tagAt(at(k)); !ok {
			continue
		}
		if b, ok := fromKey(n, at, k); ok && b.Contains(line) {
			return b, true
		}
		return Block{}, false
	}
	return Block{}, false
}

// maxBlockLines bounds the upward walk of At: an envelope of that many
// 80-column hex lines holds a value far larger than a vaulted variable.
const maxBlockLines = 4096

// FromHead returns the block whose `$ANSIBLE_VAULT;` header sits on line
// head — the editor's way back from a header stand-in span to its block: the
// tag line is at most an indicator line and a blank run above.
func FromHead(n int, at func(int) string, head int) (Block, bool) {
	for k := head - 1; k >= 0 && head-k <= maxHeadDistance; k-- {
		if _, ok := tagAt(at(k)); !ok {
			continue
		}
		if b, ok := fromKey(n, at, k); ok && b.Head == head {
			return b, true
		}
		return Block{}, false
	}
	return Block{}, false
}

// maxHeadDistance bounds FromHead's upward walk: the indicator line and a
// few blank lines are all that may separate a tag line from its header.
const maxHeadDistance = 8

// fromKey reads the block whose key line is k.
func fromKey(n int, at func(int) string, k int) (Block, bool) {
	keyLine := at(k)
	tagCol, ok := tagAt(keyLine)
	if !ok {
		return Block{}, false
	}
	keyIndent := indentWidth(keyLine)
	rest := strings.TrimSpace(strings.TrimPrefix(string([]rune(keyLine)[tagCol:]), Tag))
	rest, _, _ = strings.Cut(rest, "#")
	rest = strings.TrimSpace(rest)
	// The block indicator sits on the tag line (`!vault |`, `!vault |-`) or,
	// with a bare `!vault`, alone on the next non-blank line.
	head := k + 1
	if rest == "" {
		if head >= n {
			return Block{}, false
		}
		if isIndicator(strings.TrimSpace(at(head))) {
			head++
		}
	} else if !isIndicator(rest) {
		return Block{}, false
	}
	for head < n && strings.TrimSpace(at(head)) == "" {
		head++
	}
	if head >= n {
		return Block{}, false
	}
	indent := indentWidth(at(head))
	header := strings.TrimSpace(at(head))
	if indent <= keyIndent || !ansiblevault.IsVault([]byte(header)) {
		return Block{}, false
	}
	parts := strings.Split(header, ";")
	if len(parts) < 3 {
		return Block{}, false
	}
	b := Block{
		Key: k, Head: head, End: head, Indent: indent, TagCol: tagCol,
		Version: parts[1], Cipher: parts[2], Label: ansiblevault.Label([]byte(header)),
	}
	for j := head + 1; j < n; j++ {
		if line := at(j); indentWidth(line) != indent || !isHex(strings.TrimSpace(line)) {
			break
		}
		b.End = j
	}
	if b.End == b.Head {
		return Block{}, false // a header without a body decrypts to nothing
	}
	return b, true
}

// tagAt returns the column of the `!vault` tag in a value position of line —
// after `key:` or after a sequence dash — and false for every other line.
func tagAt(line string) (int, bool) {
	runes := []rune(line)
	i := skipSpace(runes, 0)
	if i >= len(runes) || runes[i] == '#' {
		return 0, false
	}
	// Sequence item(s): `- !vault |`, `- - !vault |`.
	for i < len(runes) && runes[i] == '-' && (i+1 >= len(runes) || isSpace(runes[i+1])) {
		i = skipSpace(runes, i+1)
	}
	if hasTag(runes, i) {
		return i, true
	}
	// Mapping pair: the value after the first `: ` (or a trailing `:`).
	for j := i; j < len(runes); j++ {
		if runes[j] == ':' && (j+1 >= len(runes) || isSpace(runes[j+1])) {
			v := skipSpace(runes, j+1)
			if hasTag(runes, v) {
				return v, true
			}
			return 0, false
		}
	}
	return 0, false
}

// hasTag reports the `!vault` tag at column i, followed by a space or the
// end of the line (so `!vault_secret` or `!vaulted` never match).
func hasTag(runes []rune, i int) bool {
	tag := []rune(Tag)
	if i+len(tag) > len(runes) {
		return false
	}
	for k, r := range tag {
		if runes[i+k] != r {
			return false
		}
	}
	return i+len(tag) == len(runes) || isSpace(runes[i+len(tag)])
}

// isIndicator reports a literal block scalar indicator: `|` with optional
// chomping and indentation indicators (`|-`, `|+`, `|2`, `|-2`).
func isIndicator(s string) bool {
	if s == "" || s[0] != '|' {
		return false
	}
	for _, r := range s[1:] {
		if r != '-' && r != '+' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// isHex reports a non-empty lowercase/uppercase hex string.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// Spans emits the stand-in spans of every block: the header line carries the
// one-row reading under Capture, every hex line a BodyCapture span the editor
// uses to fold the body away (its Replace never renders — the row is hidden
// or, with the caret inside, drawn raw).
func Spans(lines []string) []lang.Span {
	var out []lang.Span
	for _, b := range Scan(lines) {
		repl := b.StandIn()
		out = append(out, lang.Span{
			Line: b.Head, StartCol: b.Indent, EndCol: len([]rune(lines[b.Head])),
			Capture: Capture, Replace: repl,
		})
		for l := b.Head + 1; l <= b.End; l++ {
			out = append(out, lang.Span{
				Line: l, StartCol: b.Indent, EndCol: len([]rune(lines[l])),
				Capture: BodyCapture, Replace: repl,
			})
		}
	}
	return out
}

// Envelope reassembles the `$ANSIBLE_VAULT;` envelope of a block from the
// buffer, indentation stripped, in the form ansiblevault.Decrypt reads.
func Envelope(at func(int) string, b Block) []byte {
	var sb strings.Builder
	for l := b.Head; l <= b.End; l++ {
		sb.WriteString(strings.TrimSpace(at(l)))
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

// Decrypt returns the plaintext of the block under password.
func Decrypt(at func(int) string, b Block, password string) (string, error) {
	plain, err := ansiblevault.Decrypt(Envelope(at, b), password)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Wrap indents an envelope (ansiblevault.Encrypt's output: header plus
// 80-column hex lines) to the block indentation of a `!vault |` value. The
// result has no trailing newline; each line is one buffer line.
func Wrap(envelope []byte, indent int) string {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(strings.TrimRight(string(envelope), "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// Encrypt encrypts plaintext under password (with the 1.2 vault id label
// when non-empty) and returns the block body — header line and hex lines —
// indented to indent, ready to replace a block's Head..End lines.
func Encrypt(plaintext, password, label string, indent int) (string, error) {
	env, err := ansiblevault.Encrypt([]byte(plaintext), password, label)
	if err != nil {
		return "", err
	}
	return Wrap(env, indent), nil
}

// BlockIndent is the indentation `ansible-vault encrypt_string` prints the
// envelope at, relative to the key line — the shape a fresh block takes.
const BlockIndent = 10

func indentWidth(line string) int {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return len(line)
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

func skipSpace(runes []rune, i int) int {
	for i < len(runes) && isSpace(runes[i]) {
		i++
	}
	return i
}
