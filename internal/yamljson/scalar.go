// Package yamljson converts a decoded YAML scalar node into the Go value
// shapes a JSON-oriented consumer expects, shared by the jq path finder
// (internal/jqpath) and the yq playground (internal/jqplay) so both decode a
// YAML document identically.
package yamljson

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Scalar resolves one scalar to a Go value by its tag. Ints and floats
// become json.Number wherever their decimal spelling is one — `0x1f` and
// `1_000` are re-spelled, a 20-digit id keeps its digits — so arithmetic in
// the program sees numbers and the output round-trips. Everything the core
// schema does not cover (a timestamp, a `!Ref`, a binary blob) stays the
// string it was written as: a query language has nothing better to do with a
// value it cannot compute on, and dropping to a string keeps it visible.
func Scalar(n *yaml.Node) any {
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err == nil {
			return b
		}
		return n.Value
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return json.Number(strconv.FormatInt(i, 10))
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return json.Number(strconv.FormatUint(u, 10))
		}
		if _, ok := new(big.Int).SetString(n.Value, 10); ok {
			return json.Number(n.Value) // wider than int64: keep every digit
		}
		return n.Value
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return n.Value
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return f // `.inf` / `.nan`: no decimal spelling, but gojq has them
		}
		if IsDecimal(n.Value) {
			return json.Number(n.Value) // keep `1.50` as written
		}
		return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return n.Value
}

// IsDecimal reports whether s is already a JSON number literal, in which case
// it can be carried as json.Number unchanged.
func IsDecimal(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9') && r != '-' && r != '+' && r != '.' && r != 'e' && r != 'E' {
			return false
		}
	}
	return true
}
