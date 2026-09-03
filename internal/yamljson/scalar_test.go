package yamljson

import (
	"encoding/json"
	"math"
	"testing"

	"gopkg.in/yaml.v3"
)

// decode parses a single YAML scalar into its Node for Scalar to convert.
func decode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal(%q): %v", src, err)
	}
	return doc.Content[0]
}

func TestScalar(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want any
	}{
		{"null", "null", nil},
		{"bool true", "true", true},
		{"bool false", "false", false},
		{"small int", "42", json.Number("42")},
		{"negative int", "-7", json.Number("-7")},
		{"big int beyond int64", "99999999999999999999", json.Number("99999999999999999999")},
		{"decimal float kept as written", "1.50", json.Number("1.50")},
		{"decimal float in exponent form kept as written", "1e10", json.Number("1e10")},
		{"float re-spelled when underscores make it non-decimal", "1_0.5", json.Number("10.5")},
		{"plain string", "hello", "hello"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Scalar(decode(t, c.src))
			if got != c.want {
				t.Errorf("Scalar(%q) = %#v (%T), want %#v (%T)", c.src, got, got, c.want, c.want)
			}
		})
	}
}

// TestScalarInfNaN covers the two tags with no decimal spelling: they carry
// through as plain float64, not json.Number.
func TestScalarInfNaN(t *testing.T) {
	inf := Scalar(decode(t, ".inf"))
	f, ok := inf.(float64)
	if !ok || !math.IsInf(f, 1) {
		t.Errorf("Scalar(.inf) = %#v, want +Inf float64", inf)
	}

	nan := Scalar(decode(t, ".nan"))
	f, ok = nan.(float64)
	if !ok || !math.IsNaN(f) {
		t.Errorf("Scalar(.nan) = %#v, want NaN float64", nan)
	}
}

func TestIsDecimal(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"1.50", true},
		{"-3", true},
		{"1e10", true},
		{"0x1f", false},
		{"1_000", false},
		{"abc", false},
	}
	for _, c := range cases {
		if got := IsDecimal(c.in); got != c.want {
			t.Errorf("IsDecimal(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
