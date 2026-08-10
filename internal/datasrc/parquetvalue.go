package datasrc

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

// parquetvalue.go turns the flat, level-encoded values parquet-go hands back
// into displayable cells (#1766).
//
// Two steps, deliberately separate:
//
//  1. *Assembly* rebuilds a row's nested shape from the repetition and
//     definition levels (Dremel striping) — lists, maps and structs come back
//     as []any / map[string]any so a nested cell can render as compact JSON.
//  2. *Rendering* maps one leaf value through its logical type: timestamps to
//     ISO 8601, decimals with the scale applied, byte arrays to text when they
//     are valid UTF-8 and to a `<bytes N>` placeholder when they are not.
//
// Rendering happens at the leaf, so a timestamp nested three levels down inside
// a list of structs reads the same as a top-level one.

// columnLevels tracks the repetition and definition depth reached while
// walking into a schema node, exactly as the format's striping defines them.
type columnLevels struct {
	repetitionDepth int
	definitionLevel int
}

// assembleValue rebuilds the value of one schema node from the leaf columns it
// spans. cols holds one slice of values per leaf column in the node's range,
// in leaf order.
func assembleValue(c *parquet.Column, lv columnLevels, cols [][]parquet.Value) any {
	switch {
	case c.Repeated():
		return assembleRepeated(c, lv, cols)
	case c.Optional():
		lv.definitionLevel++
		if parquetAbsent(cols, lv.definitionLevel) {
			return nil
		}
		return assembleRequired(c, lv, cols)
	default:
		return assembleRequired(c, lv, cols)
	}
}

// assembleRepeated splits the node's values into one run per repetition and
// assembles each. An absent or empty repetition yields the empty list, which
// is how the format spells both.
func assembleRepeated(c *parquet.Column, lv columnLevels, cols [][]parquet.Value) any {
	lv.repetitionDepth++
	lv.definitionLevel++
	if parquetAbsent(cols, lv.definitionLevel) {
		return []any{}
	}
	// The first leaf column's repetition levels delimit the runs; every other
	// leaf in the range carries the same run structure.
	n := 0
	for i := 0; i < len(cols[0]); n++ {
		i++
		for i < len(cols[0]) && cols[0][i].RepetitionLevel() > lv.repetitionDepth {
			i++
		}
	}
	rest := make([][]parquet.Value, len(cols))
	copy(rest, cols)
	out := make([]any, 0, n)
	for r := 0; r < n; r++ {
		run := make([][]parquet.Value, len(rest))
		for j, col := range rest {
			if len(col) == 0 {
				continue
			}
			k := 1
			for k < len(col) && col[k].RepetitionLevel() > lv.repetitionDepth {
				k++
			}
			run[j], rest[j] = col[:k], col[k:]
		}
		out = append(out, assembleRequired(c, lv, run))
	}
	return out
}

// assembleRequired assembles a node whose presence is already settled: a leaf
// renders its single value, a group recurses into its fields.
func assembleRequired(c *parquet.Column, lv columnLevels, cols [][]parquet.Value) any {
	if c.Leaf() {
		if len(cols) == 0 || len(cols[0]) == 0 {
			return nil
		}
		return renderLeaf(c.Type(), cols[0][0])
	}
	if lt := c.Type().LogicalType(); lt != nil {
		switch {
		case lt.List != nil:
			return assembleList(c, lv, cols)
		case lt.Map != nil:
			return assembleMap(c, lv, cols)
		}
	}
	return assembleGroup(c, lv, cols)
}

// assembleGroup builds one object from the node's fields, each taking the leaf
// columns of its own sub-range.
func assembleGroup(c *parquet.Column, lv columnLevels, cols [][]parquet.Value) any {
	first, _ := parquetLeafRange(c)
	out := make(map[string]any, len(c.Columns()))
	for _, sub := range c.Columns() {
		sf, se := parquetLeafRange(sub)
		if sf < 0 {
			continue
		}
		lo, hi := sf-first, se-first
		if lo < 0 || hi > len(cols) || lo >= hi {
			out[sub.Name()] = nil
			continue
		}
		out[sub.Name()] = assembleValue(sub, lv, cols[lo:hi])
	}
	return out
}

// assembleList unwraps the LIST annotation: the three-level form
// `group L (LIST) { repeated group list { element } }` renders as the plain
// array of elements rather than as the wrapper objects the format stores. The
// legacy two-level form (a bare repeated field) already assembles to an array.
func assembleList(c *parquet.Column, lv columnLevels, cols [][]parquet.Value) any {
	inner := c.Columns()
	if len(inner) != 1 {
		return assembleGroup(c, lv, cols)
	}
	items, ok := assembleValue(inner[0], lv, cols).([]any)
	if !ok {
		return assembleGroup(c, lv, cols)
	}
	// Three-level form: each item is the repeated group, holding the element
	// as its only field.
	if elem := inner[0].Columns(); len(elem) == 1 {
		name := elem[0].Name()
		for i, it := range items {
			if m, ok := it.(map[string]any); ok && len(m) == 1 {
				items[i] = m[name]
			}
		}
	}
	return items
}

// assembleMap unwraps the MAP annotation into a JSON object. Keys are rendered
// to strings — JSON has no other kind — so an integer-keyed map still reads.
func assembleMap(c *parquet.Column, lv columnLevels, cols [][]parquet.Value) any {
	inner := c.Columns()
	if len(inner) != 1 || len(inner[0].Columns()) != 2 {
		return assembleGroup(c, lv, cols)
	}
	entries, ok := assembleValue(inner[0], lv, cols).([]any)
	if !ok {
		return assembleGroup(c, lv, cols)
	}
	keyName := inner[0].Columns()[0].Name()
	valName := inner[0].Columns()[1].Name()
	out := make(map[string]any, len(entries))
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		out[scalarKey(m[keyName])] = m[valName]
	}
	return out
}

// parquetAbsent reports whether the node's values are missing at the given
// definition level — the format's way of spelling NULL and the empty list.
func parquetAbsent(cols [][]parquet.Value, definitionLevel int) bool {
	return len(cols) == 0 || len(cols[0]) == 0 || cols[0][0].DefinitionLevel() < definitionLevel
}

// scalarKey renders a map key as a string.
func scalarKey(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// renderLeaf maps one stored value through its logical type. The result is a
// JSON-marshalable Go value: a string for anything with a textual reading
// (timestamps, decimals, UUIDs, text), a number or bool otherwise, nil for
// NULL.
func renderLeaf(t parquet.Type, v parquet.Value) any {
	if v.IsNull() {
		return nil
	}
	if lt := t.LogicalType(); lt != nil {
		switch {
		case lt.Timestamp != nil:
			return renderTimestamp(v.Int64(), lt.Timestamp)
		case lt.Date != nil:
			return time.Unix(int64(v.Int32())*86400, 0).UTC().Format("2006-01-02")
		case lt.Time != nil:
			return renderTime(v, lt.Time)
		case lt.Decimal != nil:
			return renderDecimal(v, int(lt.Decimal.Scale))
		case lt.UUID != nil:
			return renderUUID(v.ByteArray())
		case lt.Float16 != nil:
			return renderFloat16(v.ByteArray())
		case lt.Integer != nil && !lt.Integer.IsSigned:
			return renderUnsigned(v)
		case lt.UTF8 != nil, lt.Enum != nil, lt.Json != nil, lt.Bson != nil:
			return string(v.ByteArray())
		}
	}
	switch v.Kind() {
	case parquet.Boolean:
		return v.Boolean()
	case parquet.Int32:
		return int64(v.Int32())
	case parquet.Int64:
		return v.Int64()
	case parquet.Int96:
		return renderInt96(v.Int96())
	case parquet.Float:
		return finiteFloat(float64(v.Float()))
	case parquet.Double:
		return finiteFloat(v.Double())
	default:
		return renderBytes(v.ByteArray())
	}
}

// renderTimestamp formats a stored instant as ISO 8601. A timestamp adjusted
// to UTC keeps its `Z`; a local (unzoned) one is rendered without an offset,
// because the file states none and inventing the reader's would be a lie.
func renderTimestamp(n int64, ts *format.TimestampType) string {
	t := time.Unix(0, n*timeUnitNanos(ts.Unit)).UTC()
	if ts.IsAdjustedToUTC {
		return t.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return t.Format("2006-01-02T15:04:05.999999999")
}

// renderTime formats a time of day. MILLIS is stored as INT32, the finer units
// as INT64.
func renderTime(v parquet.Value, tt *format.TimeType) string {
	n := v.Int64()
	if v.Kind() == parquet.Int32 {
		n = int64(v.Int32())
	}
	d := time.Duration(n * timeUnitNanos(tt.Unit))
	return time.Unix(0, 0).UTC().Add(d).Format("15:04:05.999999999")
}

// timeUnitNanos is one tick of the stored unit in nanoseconds.
func timeUnitNanos(u format.TimeUnit) int64 {
	switch {
	case u.Micros != nil:
		return int64(time.Microsecond)
	case u.Nanos != nil:
		return 1
	default: // Millis, and the unset union as the format's default
		return int64(time.Millisecond)
	}
}

// renderInt96 formats the deprecated INT96 timestamp older writers (Impala,
// Hive, Spark before 3.0) still emit: nanoseconds within the day, then the
// Julian day number.
func renderInt96(i deprecated.Int96) string {
	nanos := int64(i[0]) | int64(i[1])<<32
	// 2440588 is the Julian day of 1970-01-01.
	days := int64(int32(i[2])) - 2440588
	return time.Unix(days*86400, nanos).UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
}

// renderDecimal applies the column's scale to the stored unscaled integer. The
// unscaled value rides in an INT32, an INT64 or a big-endian two's complement
// byte array, depending on the precision the writer needed.
func renderDecimal(v parquet.Value, scale int) string {
	var unscaled *big.Int
	switch v.Kind() {
	case parquet.Int32:
		unscaled = big.NewInt(int64(v.Int32()))
	case parquet.Int64:
		unscaled = big.NewInt(v.Int64())
	default:
		unscaled = bigIntFromTwosComplement(v.ByteArray())
	}
	return scaleDecimal(unscaled, scale)
}

// bigIntFromTwosComplement decodes a big-endian, signed two's complement byte
// array — the format's wire form for wide decimals.
func bigIntFromTwosComplement(b []byte) *big.Int {
	n := new(big.Int).SetBytes(b)
	if len(b) > 0 && b[0]&0x80 != 0 {
		// Negative: subtract 2^(8*len) to reinterpret the magnitude as signed.
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(8*len(b))))
	}
	return n
}

// scaleDecimal renders unscaled/10^scale in plain decimal notation, without
// the exponent a float conversion would introduce and without losing digits a
// float could not hold.
func scaleDecimal(unscaled *big.Int, scale int) string {
	if scale <= 0 {
		if scale < 0 {
			return unscaled.String() + strings.Repeat("0", -scale)
		}
		return unscaled.String()
	}
	sign := ""
	digits := new(big.Int).Abs(unscaled).String()
	if unscaled.Sign() < 0 {
		sign = "-"
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	cut := len(digits) - scale
	return sign + digits[:cut] + "." + digits[cut:]
}

// renderUUID formats the 16 stored bytes canonically.
func renderUUID(b []byte) string {
	if len(b) != 16 {
		return renderBytes(b)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// renderFloat16 widens an IEEE 754 half-precision value stored little-endian.
func renderFloat16(b []byte) any {
	if len(b) != 2 {
		return renderBytes(b)
	}
	bits := uint16(b[0]) | uint16(b[1])<<8
	sign := uint32(bits>>15) << 31
	exp := int32(bits>>10) & 0x1f
	frac := uint32(bits) & 0x3ff
	switch exp {
	case 0x1f: // Inf or NaN
		return finiteFloat(math.Float32frombits(sign | 0x7f800000 | frac<<13))
	case 0:
		if frac == 0 {
			return finiteFloat(math.Float32frombits(sign))
		}
		// Subnormal: renormalize into the wider exponent range.
		e := int32(-1)
		for frac&0x400 == 0 {
			frac <<= 1
			e--
		}
		frac &= 0x3ff
		return finiteFloat(math.Float32frombits(sign | uint32(e+112+15)<<23 | frac<<13))
	default:
		return finiteFloat(math.Float32frombits(sign | uint32(exp+112)<<23 | frac<<13))
	}
}

// renderUnsigned reads an unsigned integer column, whose bits are stored in a
// signed INT32/INT64 and would otherwise render negative past the midpoint.
func renderUnsigned(v parquet.Value) any {
	if v.Kind() == parquet.Int32 {
		return uint64(uint32(v.Int32()))
	}
	return uint64(v.Int64())
}

// renderBytes shows a byte array as text when it is valid UTF-8 — an untagged
// BYTE_ARRAY is usually a string — and as a size placeholder when it is not,
// because raw bytes in a grid help no one.
func renderBytes(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return fmt.Sprintf("<bytes %d>", len(b))
}

// finiteFloat keeps NaN and the infinities out of the JSON encoder, which
// cannot represent them, by handing them over as their textual form.
func finiteFloat[F float32 | float64](f F) any {
	v := float64(f)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return v
}

// parquetCell renders one assembled value as a grid cell: scalars directly,
// anything nested as compact one-line JSON.
func parquetCell(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{Null: true}
	case string:
		return Cell{Text: x}
	case bool:
		return Cell{Text: strconv.FormatBool(x)}
	case int64:
		return Cell{Text: strconv.FormatInt(x, 10)}
	case uint64:
		return Cell{Text: strconv.FormatUint(x, 10)}
	case float64:
		return Cell{Text: strconv.FormatFloat(x, 'g', -1, 64)}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return Cell{Text: fmt.Sprint(v)}
	}
	return Cell{Text: string(b)}
}
