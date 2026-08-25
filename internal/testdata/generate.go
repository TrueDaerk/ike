package testdata

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

// Generator turns a validated spec into rows. It owns one seeded faker
// instance and is single-threaded by construction: every value — including the
// log writer's timestamps and levels — is drawn from that one instance in a
// fixed order, which is what makes a seeded run byte-identical on repeat.
type Generator struct {
	spec Spec
	fake *gofakeit.Faker
	gens []func(row int) any
	// logAt is the log writer's running timestamp; it starts at logEpoch and
	// only ever moves forward, so generated log files are ordered like real
	// ones.
	logAt time.Time
}

// logEpoch anchors generated log timestamps. A fixed instant rather than
// time.Now(), because a seeded run must not depend on when it ran.
var logEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// defaultDateFrom / defaultDateTo bound the date kind when the field names no
// range — again fixed, not "now ± something".
var (
	defaultDateFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultDateTo   = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
)

// NewGenerator validates the spec and compiles each field into a closure, so
// the per-row loop costs no parameter parsing and an invalid parameter is
// reported once, up front.
func NewGenerator(spec Spec) (*Generator, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	spec = spec.Normalized()
	g := &Generator{spec: spec, fake: gofakeit.New(spec.Seed), logAt: logEpoch}
	g.gens = make([]func(row int) any, len(spec.Fields))
	for i, f := range spec.Fields {
		gen, err := g.compile(f)
		if err != nil {
			return nil, err
		}
		g.gens[i] = gen
	}
	return g, nil
}

// Spec returns the normalized spec the generator runs.
func (g *Generator) Spec() Spec { return g.spec }

// Row generates the values of row (0-based) in field order. Values are typed —
// string, int64, float64, bool or time.Time — so each writer can render them
// in its own idiom (a JSON number, a TOML datetime, a quoted SQL literal)
// instead of re-parsing pre-formatted strings.
func (g *Generator) Row(row int) []any {
	out := make([]any, len(g.gens))
	for i, gen := range g.gens {
		out[i] = gen(row)
	}
	return out
}

// compile builds one field's value closure, validating its parameter.
func (g *Generator) compile(f Field) (func(row int) any, error) {
	f.Param = strings.TrimSpace(f.Param)
	if err := checkParam(f); err != nil {
		return nil, fmt.Errorf("field %q: %w", f.Name, err)
	}
	fake := g.fake
	switch f.Kind {
	case KindID:
		return func(row int) any { return int64(row + 1) }, nil
	case KindUUID:
		return func(int) any { return fake.UUID() }, nil
	case KindFirstName:
		return func(int) any { return fake.FirstName() }, nil
	case KindLastName:
		return func(int) any { return fake.LastName() }, nil
	case KindFullName:
		return func(int) any { return fake.Name() }, nil
	case KindEmail:
		if f.Param == "" {
			return func(int) any { return fake.Email() }, nil
		}
		domain := strings.ToLower(f.Param)
		return func(int) any { return emailAt(fake, domain) }, nil
	case KindURL:
		if f.Param == "" {
			return func(int) any { return fake.URL() }, nil
		}
		domain := strings.ToLower(f.Param)
		return func(int) any { return urlAt(fake, domain) }, nil
	case KindHostname:
		domain := strings.ToLower(f.Param)
		return func(int) any {
			d := domain
			if d == "" {
				d = fake.DomainName()
			}
			return label(fake) + "." + d
		}, nil
	case KindDomain:
		return func(int) any { return fake.DomainName() }, nil
	case KindIPv4:
		return func(int) any { return fake.IPv4Address() }, nil
	case KindIPv6:
		return func(int) any { return fake.IPv6Address() }, nil
	case KindMAC:
		return func(int) any { return fake.MacAddress() }, nil
	case KindPhone:
		return func(int) any { return fake.PhoneFormatted() }, nil
	case KindStreet:
		return func(int) any { return fake.Street() }, nil
	case KindCity:
		return func(int) any { return fake.City() }, nil
	case KindCountry:
		return func(int) any { return fake.Country() }, nil
	case KindCompany:
		return func(int) any { return fake.Company() }, nil
	case KindJobTitle:
		return func(int) any { return fake.JobTitle() }, nil
	case KindSentence:
		return func(int) any { return fake.Sentence() }, nil
	case KindParagraph:
		return func(int) any { return fake.Paragraph() }, nil
	case KindBool:
		return func(int) any { return fake.Bool() }, nil
	case KindHexColor:
		return func(int) any { return fake.HexColor() }, nil
	case KindUserAgent:
		return func(int) any { return fake.UserAgent() }, nil
	case KindInt:
		lo, hi := 1, 1000
		if f.Param != "" {
			lo, hi, _ = parseIntRange(f.Param)
		}
		return func(int) any { return int64(fake.IntRange(lo, hi)) }, nil
	case KindFloat:
		lo, hi := 0.0, 1000.0
		if f.Param != "" {
			lo, hi, _ = parseFloatRange(f.Param)
		}
		return func(int) any { return round2(fake.Float64Range(lo, hi)) }, nil
	case KindDate:
		from, to := defaultDateFrom, defaultDateTo
		if f.Param != "" {
			from, to, _ = parseDateRange(f.Param)
		}
		return func(int) any { return fake.DateRange(from, to).UTC().Truncate(time.Second) }, nil
	}
	// Unreachable: Validate rejects unknown kinds before compile runs. Kept as
	// a guard so a kind added to the catalog without a case here fails loudly
	// instead of silently generating nothing.
	return nil, fmt.Errorf("field %q: kind %q has no generator", f.Name, string(f.Kind))
}

// round2 trims a generated float to two decimals — the difference between a
// readable sample column and 17 digits of float noise.
func round2(v float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	if err != nil {
		return v
	}
	return r
}

// label draws one lowercase DNS label: a dictionary word reduced to
// [a-z0-9-], suffixed with a number so repeats stay distinguishable.
func label(f *gofakeit.Faker) string {
	w := sanitizeLabel(f.Word())
	if w == "" {
		w = "host"
	}
	return w + strconv.Itoa(f.IntRange(1, 99))
}

// sanitizeLabel keeps only characters legal in a DNS label.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// emailAt builds an address in the given domain: the local part comes from a
// name pair, so the column still reads like a people table.
func emailAt(f *gofakeit.Faker, domain string) string {
	local := sanitizeLabel(f.FirstName()) + "." + sanitizeLabel(f.LastName())
	return local + "@" + domain
}

// urlAt builds an https URL under the given domain — the point of the domain
// parameter is that *every* value stays inside it, so the scheme and host are
// fixed and only the path varies.
func urlAt(f *gofakeit.Faker, domain string) string {
	n := f.IntRange(1, 3)
	segs := make([]string, n)
	for i := range segs {
		s := sanitizeLabel(f.Word())
		if s == "" {
			s = "p" + strconv.Itoa(f.IntRange(1, 999))
		}
		segs[i] = s
	}
	return "https://" + domain + "/" + strings.Join(segs, "/")
}

// logLevels is the level distribution of generated log lines, as cumulative
// permille bounds: mostly info, a debug tail, few warnings, fewer errors, the
// occasional fatal — the shape a real service log has, so the log timeline and
// the severity colouring have something to show.
var logLevels = []struct {
	upTo  int // exclusive upper bound in permille
	level string
}{
	{550, "info"},
	{750, "debug"},
	{900, "warn"},
	{980, "error"},
	{1000, "fatal"},
}

// logEntry draws the next log line's timestamp, level and message. The
// timestamp advances by a random step so the file reads as a stream rather
// than a burst, and never goes backwards.
func (g *Generator) logEntry() (ts time.Time, level, msg string) {
	g.logAt = g.logAt.Add(time.Duration(g.fake.IntRange(1, 5000)) * time.Millisecond)
	pick := g.fake.IntRange(0, 999)
	level = "info"
	for _, l := range logLevels {
		if pick < l.upTo {
			level = l.level
			break
		}
	}
	msg = strings.TrimSuffix(g.fake.Sentence(), ".")
	return g.logAt, level, msg
}
