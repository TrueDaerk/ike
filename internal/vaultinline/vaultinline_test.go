package vaultinline

import (
	"strings"
	"testing"

	"ike/internal/ansiblevault"
)

const testPassword = "test-vault-password"

// fixture builds a YAML document with one encrypted mapping value at the
// given key indentation, returning its lines.
func fixture(t *testing.T, plain, label string, keyIndent int) []string {
	t.Helper()
	body, err := Encrypt(plain, testPassword, label, keyIndent+BlockIndent)
	if err != nil {
		t.Fatal(err)
	}
	pad := strings.Repeat(" ", keyIndent)
	doc := "db:\n" + pad + "password: !vault |\n" + body + "\n" + pad + "host: db.example.com\n"
	return strings.Split(strings.TrimSuffix(doc, "\n"), "\n")
}

func TestScanFindsEncryptedMappingValue(t *testing.T) {
	lines := fixture(t, "hunter2", "", 2)
	blocks := Scan(lines)
	if len(blocks) != 1 {
		t.Fatalf("Scan = %+v, want one block", blocks)
	}
	b := blocks[0]
	if b.Key != 1 || b.Head != 2 || b.Indent != 12 || b.TagCol != 12 {
		t.Errorf("block = %+v, want key 1, head 2, indent 12, tag col 12", b)
	}
	if b.End != len(lines)-2 || b.Lines() < 3 {
		t.Errorf("block end = %d (%d lines), want the last hex line %d", b.End, b.Lines(), len(lines)-2)
	}
	if b.Cipher != "AES256" || b.Version != "1.1" || b.Label != "" {
		t.Errorf("header = %+v, want AES256 1.1 without label", b)
	}
	if got := b.StandIn(); !strings.HasPrefix(got, "⟨vault AES256 · ") || !strings.HasSuffix(got, " lines⟩") {
		t.Errorf("StandIn = %q", got)
	}
}

func TestScanCarriesVaultID(t *testing.T) {
	lines := fixture(t, "hunter2", "prod", 0)
	blocks := Scan(lines)
	if len(blocks) != 1 || blocks[0].Label != "prod" || blocks[0].Version != "1.2" {
		t.Fatalf("Scan = %+v, want a 1.2 block labelled prod", blocks)
	}
	if got := blocks[0].StandIn(); !strings.Contains(got, "· id: prod ·") {
		t.Errorf("StandIn = %q, want the id", got)
	}
}

func TestScanIssueExample(t *testing.T) {
	lines := []string{
		"vault_mysql_ai_prompt_password: !vault |",
		"          $ANSIBLE_VAULT;1.1;AES256",
		"          64626536313436653262653964393739303331343431313339383331383466333162393761636563",
		"          3263393165366233333632366132343231616465333362310a333636336162336163356638363334",
		"          63633461633032313265386263653634346135646661376430366533636531333934656366393630",
		"          3234633431333039610a373464623061306366633234373738366539303137336138336233646263",
		"          62323938343762356432306564363631346539396639346437323136343333363130643166386134",
		"          3137363931323733346662303131616332383061643634656433",
		"other: value",
	}
	blocks := Scan(lines)
	if len(blocks) != 1 {
		t.Fatalf("Scan = %+v, want one block", blocks)
	}
	if b := blocks[0]; b.Key != 0 || b.Head != 1 || b.End != 7 || b.StandIn() != "⟨vault AES256 · 6 lines⟩" {
		t.Errorf("block = %+v, stand-in %q", b, b.StandIn())
	}
	spans := Spans(lines)
	if len(spans) != 7 || spans[0].Capture != Capture || spans[0].Line != 1 || spans[0].StartCol != 10 {
		t.Fatalf("Spans = %+v", spans)
	}
	for _, s := range spans[1:] {
		if s.Capture != BodyCapture {
			t.Errorf("body span %+v has capture %q", s, s.Capture)
		}
	}
}

func TestScanShapes(t *testing.T) {
	hex := "          $ANSIBLE_VAULT;1.1;AES256\n          6162636465"
	cases := []struct {
		name string
		doc  string
		want int
	}{
		{"strip chomping", "k: !vault |-\n" + hex, 1},
		{"keep chomping", "k: !vault |+\n" + hex, 1},
		{"indicator on next line", "k: !vault\n  |\n" + hex, 1},
		{"sequence item", "- !vault |\n" + hex, 1},
		{"nested sequence mapping", "- k: !vault |\n" + hex, 1},
		{"trailing comment", "k: !vault | # secret\n" + hex, 1},
		{"folded indicator", "k: !vault >\n" + hex, 0},
		{"no header", "k: !vault |\n          not a vault\n", 0},
		{"header without body", "k: !vault |\n          $ANSIBLE_VAULT;1.1;AES256\nnext: 1", 0},
		{"tag in comment", "# k: !vault |\n" + hex, 0},
		{"similar tag", "k: !vaulted |\n" + hex, 0},
		{"header not indented", "k: !vault |\n$ANSIBLE_VAULT;1.1;AES256\n6162", 0},
		{"plain value", "k: value", 0},
	}
	for _, c := range cases {
		lines := strings.Split(c.doc, "\n")
		if got := len(Scan(lines)); got != c.want {
			t.Errorf("%s: Scan found %d blocks, want %d", c.name, got, c.want)
		}
	}
}

func TestScanStopsAtIndentChange(t *testing.T) {
	lines := []string{
		"a: !vault |",
		"    $ANSIBLE_VAULT;1.1;AES256",
		"    6162636465",
		"    6162636465",
		"  b: 6162",
		"c: !vault |",
		"    $ANSIBLE_VAULT;1.2;AES256;dev",
		"    6162636465",
	}
	blocks := Scan(lines)
	if len(blocks) != 2 {
		t.Fatalf("Scan = %+v, want two blocks", blocks)
	}
	if blocks[0].End != 3 || blocks[1].Key != 5 || blocks[1].End != 7 || blocks[1].Label != "dev" {
		t.Errorf("blocks = %+v", blocks)
	}
}

func TestAtAndFromHead(t *testing.T) {
	lines := fixture(t, "hunter2", "", 2)
	at := func(i int) string { return lines[i] }
	n := len(lines)
	for _, line := range []int{1, 2, 3, len(lines) - 2} {
		b, ok := At(n, at, line)
		if !ok || b.Key != 1 {
			t.Errorf("At(%d) = %+v, %v; want the block", line, b, ok)
		}
	}
	for _, line := range []int{0, len(lines) - 1} {
		if _, ok := At(n, at, line); ok {
			t.Errorf("At(%d) claimed a block", line)
		}
	}
	if b, ok := FromHead(n, at, 2); !ok || b.Key != 1 {
		t.Errorf("FromHead(2) = %+v, %v", b, ok)
	}
	if _, ok := FromHead(n, at, 3); ok {
		t.Error("FromHead on a hex line claimed a block")
	}
}

func TestDecryptRoundTrip(t *testing.T) {
	plain := "multi\nline value\n"
	lines := fixture(t, plain, "prod", 4)
	b := Scan(lines)[0]
	at := func(i int) string { return lines[i] }
	got, err := Decrypt(at, b, testPassword)
	if err != nil || got != plain {
		t.Fatalf("Decrypt = %q, %v; want %q", got, err, plain)
	}
	if _, err := Decrypt(at, b, "wrong"); err != ansiblevault.ErrWrongPassword {
		t.Errorf("wrong password error = %v, want ErrWrongPassword", err)
	}
}

func TestEncryptShape(t *testing.T) {
	body, err := Encrypt("hunter2", testPassword, "prod", 6)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(body, "\n")
	if lines[0] != "      $ANSIBLE_VAULT;1.2;AES256;prod" {
		t.Errorf("header = %q", lines[0])
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "      ") || len(l) > 86 || !isHex(strings.TrimSpace(l)) {
			t.Errorf("hex line %q: wrong indent, width or characters", l)
		}
	}
	if strings.HasSuffix(body, "\n") {
		t.Error("body must not end in a newline")
	}
}

func TestScalarAt(t *testing.T) {
	cases := []struct {
		line     string
		key, val string
		ok       bool
	}{
		{"password: hunter2", "password", "hunter2", true},
		{"  password: hunter2 # note", "password", "hunter2", true},
		{`- token: "a#b"`, "token", "a#b", true},
		{`token: 'it''s'`, "token", "it's", true},
		{`token: "tab\there"`, "token", "tab\there", true},
		{"password: !vault |", "", "", false},
		{"password: |", "", "", false},
		{"ref: *anchor", "", "", false},
		{"list: [a, b]", "", "", false},
		{"password:", "", "", false},
		{"# password: x", "", "", false},
		{"- plain item", "", "", false},
	}
	for _, c := range cases {
		s, ok := ScalarAt(c.line)
		if ok != c.ok || s.Key != c.key || s.Text != c.val {
			t.Errorf("ScalarAt(%q) = %+v, %v; want %q=%q, %v", c.line, s, ok, c.key, c.val, c.ok)
		}
	}
}

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"hunter2":     "hunter2",
		"with space":  "with space",
		"":            `""`,
		"true":        `"true"`,
		"123":         `"123"`,
		"a: b":        `"a: b"`,
		"a #b":        `"a #b"`,
		"*star":       `"*star"`,
		"two\nlines":  `"two\nlines"`,
		" padded":     `" padded"`,
		"it's":        "it's",
		"key=val@x.y": "key=val@x.y",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
		if Unquote(Quote(in)) != in {
			t.Errorf("Unquote(Quote(%q)) does not round-trip", in)
		}
	}
}
