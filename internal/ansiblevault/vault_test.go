package ansiblevault

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Fixtures generated with ansible-core's ansible-vault CLI (password
// "test-vault-password", plaintext "SECRET_TOKEN=hunter2\nDB_PASSWORD=s3cr3t\n").
const (
	fixturePassword  = "test-vault-password"
	fixturePlaintext = "SECRET_TOKEN=hunter2\nDB_PASSWORD=s3cr3t\n"

	fixture11 = `$ANSIBLE_VAULT;1.1;AES256
35613633323062616165333632656363373837376332306363323936666465313061633637333038
6435313930663834346661323064373863613832376431340a616565623538666234313231343164
33623461383763353362616365656665656439356366653638316136396536373936396636353165
3364336466663061310a333335333131376666366361326535383065643161373031623634343666
31326436383666356566346434313165366664376533616639346136626363393231346238323663
6266326436636565386438666330366332373937656339656430
`

	fixture12 = `$ANSIBLE_VAULT;1.2;AES256;myid
38666635303464656631343936393836343530363636633835636665363665646361313835663362
3662383337613431356162653230316639623166323237620a353135656364613863663536626164
30323732326237393136663530303062386330623632626139653135386664616631323135623735
3165663335333465640a613630383166333336636462383965373030393930383132613039363465
65383433653265636337356139386236393231623332393662343130646362316564336632346463
3561373965323834633361613261356434666363336465616331
`
)

func TestDecryptFixture11(t *testing.T) {
	got, err := Decrypt([]byte(fixture11), fixturePassword)
	if err != nil {
		t.Fatalf("Decrypt 1.1 fixture: %v", err)
	}
	if string(got) != fixturePlaintext {
		t.Errorf("plaintext = %q, want %q", got, fixturePlaintext)
	}
}

func TestDecryptFixture12(t *testing.T) {
	got, err := Decrypt([]byte(fixture12), fixturePassword)
	if err != nil {
		t.Fatalf("Decrypt 1.2 fixture: %v", err)
	}
	if string(got) != fixturePlaintext {
		t.Errorf("plaintext = %q, want %q", got, fixturePlaintext)
	}
}

func TestDecryptWrongPassword(t *testing.T) {
	_, err := Decrypt([]byte(fixture11), "not-the-password")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
}

func TestDecryptNotVault(t *testing.T) {
	_, err := Decrypt([]byte("just some text"), fixturePassword)
	if !errors.Is(err, ErrNotVault) {
		t.Fatalf("err = %v, want ErrNotVault", err)
	}
}

func TestDecryptMalformed(t *testing.T) {
	for name, input := range map[string]string{
		"header only":         "$ANSIBLE_VAULT;1.1;AES256\n",
		"bad hex body":        "$ANSIBLE_VAULT;1.1;AES256\nzzzz\n",
		"unsupported version": "$ANSIBLE_VAULT;9.9;AES256\nabcd\n",
		"unsupported cipher":  "$ANSIBLE_VAULT;1.1;ROT13\nabcd\n",
		"truncated header":    "$ANSIBLE_VAULT;\nabcd\n",
	} {
		if _, err := Decrypt([]byte(input), fixturePassword); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestEncryptRoundtrip(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		plaintext   string
	}{
		{"1.1 plain", "", "hello vault\n"},
		{"1.2 labeled", "prod", "hello vault\n"},
		{"empty plaintext", "", ""},
		{"exact block size", "", strings.Repeat("x", 32)},
		{"binary-ish", "", "line1\nline2\x00\xff\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := Encrypt([]byte(tc.plaintext), fixturePassword, tc.label)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if !IsVault(enc) {
				t.Fatalf("encrypted output missing vault header: %q", enc[:20])
			}
			if Label(enc) != tc.label {
				t.Errorf("Label = %q, want %q", Label(enc), tc.label)
			}
			dec, err := Decrypt(enc, fixturePassword)
			if err != nil {
				t.Fatalf("Decrypt roundtrip: %v", err)
			}
			if !bytes.Equal(dec, []byte(tc.plaintext)) {
				t.Errorf("roundtrip = %q, want %q", dec, tc.plaintext)
			}
		})
	}
}

func TestEncryptDeterministicSaltMatchesFormat(t *testing.T) {
	enc, err := Encrypt([]byte("data\n"), "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(enc), "\n"), "\n")
	if lines[0] != "$ANSIBLE_VAULT;1.1;AES256" {
		t.Errorf("header = %q", lines[0])
	}
	for i, line := range lines[1:] {
		if len(line) > 80 {
			t.Errorf("body line %d longer than 80 chars: %d", i, len(line))
		}
	}
}

func TestEncryptUsesFreshSalt(t *testing.T) {
	a, err := Encrypt([]byte("data"), "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt([]byte("data"), "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("two encryptions produced identical output; salt not random")
	}
}

func TestIsVault(t *testing.T) {
	if !IsVault([]byte(fixture11)) || !IsVault([]byte(fixture12)) {
		t.Error("fixtures not recognized as vault")
	}
	if IsVault([]byte("plain text")) || IsVault(nil) {
		t.Error("non-vault input recognized as vault")
	}
}

func TestLabel(t *testing.T) {
	if l := Label([]byte(fixture11)); l != "" {
		t.Errorf("1.1 label = %q, want empty", l)
	}
	if l := Label([]byte(fixture12)); l != "myid" {
		t.Errorf("1.2 label = %q, want myid", l)
	}
}
