package testdata

import (
	"bytes"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"
)

// allKindsSpec is one field per catalog kind, each with its documented sample
// parameter — the spec the catalog tests generate from.
func allKindsSpec(format Format, rows int, seed uint64) Spec {
	spec := Spec{Format: format, Rows: rows, Seed: seed, Table: "records"}
	for _, info := range Catalog() {
		spec.Fields = append(spec.Fields, Field{
			Name:  string(info.Kind),
			Kind:  info.Kind,
			Param: sampleParam(info.Kind),
		})
	}
	return spec
}

// TestSeedDeterminism is the acceptance criterion "same seed + same spec →
// byte-identical output", checked for every format and including the log
// writer, whose timestamps and levels are drawn from the same faker.
func TestSeedDeterminism(t *testing.T) {
	for _, f := range Formats() {
		t.Run(string(f), func(t *testing.T) {
			spec := allKindsSpec(f, 25, 4242)
			a, err := Render(spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			b, err := Render(spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("two runs of seed %d differ", spec.Seed)
			}
			spec.Seed = 4243
			c, err := Render(spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if bytes.Equal(a, c) {
				t.Fatal("a different seed produced identical output")
			}
		})
	}
}

// TestZeroSeedVaries pins the documented meaning of seed 0: a fresh random
// seed per run, so an unseeded generation is not the same file every time.
func TestZeroSeedVaries(t *testing.T) {
	spec := Spec{Format: FormatCSV, Rows: 20, Seed: 0, Fields: []Field{{Name: "u", Kind: KindUUID}}}
	a, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("seed 0 produced identical output twice; it must draw a fresh seed")
	}
}

// TestGeneratorInterleavingIsSeeded proves the generator uses an instance
// faker, not the package global: two generators alive at once must not steal
// each other's draws.
func TestGeneratorInterleavingIsSeeded(t *testing.T) {
	spec := Spec{Format: FormatCSV, Rows: 3, Seed: 7, Fields: []Field{{Name: "n", Kind: KindFullName}}}
	solo, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	var want []string
	for i := 0; i < 3; i++ {
		want = append(want, solo.Row(i)[0].(string))
	}

	a, _ := NewGenerator(spec)
	b, _ := NewGenerator(spec)
	var got []string
	for i := 0; i < 3; i++ {
		b.Row(i) // interleaved traffic on a second generator
		got = append(got, a.Row(i)[0].(string))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("interleaved generation changed the values: %v vs %v", got, want)
	}
}

// TestKindValues checks each catalog kind produces a value of the right shape:
// the typed kinds by Go type, the string kinds by not being empty, and the
// constrained ones by their constraint.
func TestKindValues(t *testing.T) {
	const rows = 40
	spec := allKindsSpec(FormatCSV, rows, 99)
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	from, to, _ := parseDateRange(sampleParam(KindDate))
	for r := 0; r < rows; r++ {
		vals := g.Row(r)
		for i, f := range spec.Fields {
			v := vals[i]
			switch f.Kind {
			case KindID:
				if v.(int64) != int64(r+1) {
					t.Fatalf("id row %d = %v, want %d", r, v, r+1)
				}
			case KindInt:
				n := v.(int64)
				if n < -5 || n > 5 {
					t.Fatalf("int = %d, outside -5..5", n)
				}
			case KindFloat:
				x := v.(float64)
				if x < 1.5 || x > 2.5 {
					t.Fatalf("float = %v, outside 1.5..2.5", x)
				}
			case KindBool:
				_ = v.(bool)
			case KindDate:
				d := v.(time.Time)
				if d.Before(from) || d.After(to) {
					t.Fatalf("date %s outside %s..%s", d, from, to)
				}
			case KindURL:
				if !strings.HasPrefix(v.(string), "https://example.com/") {
					t.Fatalf("url %q not constrained to example.com", v)
				}
			case KindHostname:
				if !strings.HasSuffix(v.(string), ".example.com") {
					t.Fatalf("hostname %q not constrained to example.com", v)
				}
			case KindEmail:
				if !strings.HasSuffix(v.(string), "@example.com") {
					t.Fatalf("email %q not constrained to example.com", v)
				}
			case KindIPv4:
				ip := net.ParseIP(v.(string))
				if ip == nil || ip.To4() == nil {
					t.Fatalf("ipv4 %q does not parse as IPv4", v)
				}
			case KindIPv6:
				if net.ParseIP(v.(string)) == nil {
					t.Fatalf("ipv6 %q does not parse", v)
				}
			case KindMAC:
				if _, err := net.ParseMAC(v.(string)); err != nil {
					t.Fatalf("mac %q does not parse: %v", v, err)
				}
			case KindUUID:
				if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(v.(string)) {
					t.Fatalf("uuid %q malformed", v)
				}
			case KindHexColor:
				if !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(v.(string)) {
					t.Fatalf("hex_color %q malformed", v)
				}
			case KindDomain:
				if !validDomain(v.(string)) {
					t.Fatalf("domain %q malformed", v)
				}
			case KindFromList:
				if s := v.(string); s != "red" && s != "green" && s != "blue" {
					t.Fatalf("from_list %q not in the list", s)
				}
			default:
				if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
					t.Fatalf("kind %s produced %#v, want a non-empty string", f.Kind, v)
				}
			}
		}
	}
}

// TestUnconstrainedNetworkKinds checks the parameterless spelling of the
// domain-taking kinds still produces well-formed values.
func TestUnconstrainedNetworkKinds(t *testing.T) {
	spec := Spec{Format: FormatCSV, Rows: 20, Seed: 3, Fields: []Field{
		{Name: "url", Kind: KindURL},
		{Name: "host", Kind: KindHostname},
		{Name: "mail", Kind: KindEmail},
	}}
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	for i := 0; i < spec.Rows; i++ {
		vals := g.Row(i)
		if u := vals[0].(string); !strings.HasPrefix(u, "http") {
			t.Fatalf("url %q is not a URL", u)
		}
		if h := vals[1].(string); !strings.Contains(h, ".") {
			t.Fatalf("hostname %q has no domain part", h)
		}
		if e := vals[2].(string); !strings.Contains(e, "@") {
			t.Fatalf("email %q is not an address", e)
		}
	}
}

// TestLogEntriesAscend proves generated log timestamps move forward and the
// level distribution actually spreads across the severities.
func TestLogEntriesAscend(t *testing.T) {
	g, err := NewGenerator(Spec{Format: FormatLog, Rows: 500, Seed: 11, Fields: []Field{{Name: "id", Kind: KindID}}})
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	levels := map[string]int{}
	prev := time.Time{}
	for i := 0; i < 500; i++ {
		ts, level, msg := g.logEntry()
		if !ts.After(prev) {
			t.Fatalf("timestamp %s did not advance past %s", ts, prev)
		}
		if msg == "" {
			t.Fatal("empty log message")
		}
		prev = ts
		levels[level]++
	}
	for _, want := range []string{"info", "debug", "warn", "error"} {
		if levels[want] == 0 {
			t.Fatalf("level %q never appeared in 500 lines: %v", want, levels)
		}
	}
	if levels["info"] <= levels["error"] {
		t.Fatalf("info should dominate the distribution, got %v", levels)
	}
}

// TestGeneratorRejectsInvalidSpec keeps the guard in one place: the generator
// refuses what Validate refuses, so no caller can bypass it.
func TestGeneratorRejectsInvalidSpec(t *testing.T) {
	if _, err := NewGenerator(Spec{Format: FormatCSV, Rows: 0}); err == nil {
		t.Fatal("NewGenerator accepted a spec with no rows and no fields")
	}
	if _, err := Render(Spec{Format: FormatCSV, Rows: 1, Fields: []Field{{Name: "x", Kind: "nope"}}}); err == nil {
		t.Fatal("Render accepted an unknown kind")
	}
}
