package settings

import (
	"encoding/base64"
	"html"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"ike/internal/concealfilter"
	"ike/internal/cronhint"
	"ike/internal/epochtime"
	"ike/internal/idcolor"
	"ike/internal/nethint"
	"ike/internal/numhint"
	"ike/internal/permhint"
	"ike/internal/secret"
	"ike/internal/theme"
)

// conceal_preview.go builds the Conceal & Hints page's live preview (#2133):
// one sample buffer line per family, drawn twice — as the raw bytes and as the
// family draws them — so a toggle is read as a picture rather than as a
// sentence. The stand-ins come from the very packages the editor renders
// through (epochtime, numhint, cronhint, permhint, nethint, secret), so a
// preview cannot drift from what the buffer will show; only the layers with no
// single-line reading (Markdown, CSV and log rendering, the PEM summary) carry
// a written sample.

// concealSample is one family's preview pair: the bytes in the file and what
// the family draws instead.
type concealSample struct {
	// Raw is the sample source line.
	Raw string
	// Shown is the same line as the family renders it.
	Shown string
	// Paint renders Shown in the family's own colors, for the coloring layers
	// (rainbow brackets, color preview, identifier colors) where the whole
	// difference *is* the color and the columns are identical.
	Paint func(p *theme.Palette, text string) string
}

// concealSampleFor returns the preview pair for one config key, or ok=false
// for the keys that decorate no single line (the list editors, the identifier
// length stepper — those preview through the file-rule and list previews).
func concealSampleFor(key string) (concealSample, bool) {
	switch key {
	case "editor.markdown_rendering":
		return concealSample{
			Raw:   "**bold** text and a [link](https://ike.dev)",
			Shown: "bold text and a link",
		}, true
	case "editor.csv_rendering":
		return concealSample{
			Raw:   "id,name,size",
			Shown: "id │ name │ size",
		}, true
	case "editor.log_rendering":
		// The escape bytes are what the layer conceals; ^[ is how a terminal
		// prints ESC, which is what the raw line looks like without it.
		return concealSample{
			Raw:   "^[[31mERROR^[[0m disk full",
			Shown: "ERROR disk full",
		}, true
	case "editor.timestamp_decoding":
		if out, ok := epochtime.Decode("1735689600"); ok {
			return concealSample{Raw: `"created_at": 1735689600`, Shown: `"created_at": ` + out}, true
		}
	case "editor.unicode_escape_decoding":
		if out, err := strconv.Unquote(`"café"`); err == nil {
			return concealSample{Raw: `"note": "café"`, Shown: `"note": "` + out + `"`}, true
		}
	case "editor.entity_decoding":
		const raw = "caf&eacute; &amp; cr&#xE8;me"
		return concealSample{Raw: "<p>" + raw + "</p>", Shown: "<p>" + html.UnescapeString(raw) + "</p>"}, true
	case "editor.base64_decoding":
		const enc = "c3VwZXItc2VjcmV0"
		if out, err := base64.StdEncoding.DecodeString(enc); err == nil {
			return concealSample{Raw: "  password: " + enc, Shown: "  password: " + string(out)}, true
		}
	case "editor.cron_hints":
		if out, ok := cronhint.Describe("*/5 * * * *"); ok {
			return concealSample{Raw: `schedule: "*/5 * * * *"`, Shown: `schedule: "*/5 * * * *"  ` + out}, true
		}
	case "editor.pem_summary":
		return concealSample{
			Raw:   "-----BEGIN CERTIFICATE----- (+18 base64 lines)",
			Shown: "-----BEGIN CERTIFICATE-----  CN=example.com, until 2027-04-01, RSA 2048",
		}, true
	case "editor.byte_size_hints":
		if out, ok := numhint.FormatBytes(10485760); ok {
			return concealSample{Raw: `"max_size": 10485760`, Shown: `"max_size": 10485760  ` + out}, true
		}
	case "editor.duration_hints":
		if out, ok := numhint.FormatDuration(86400000, time.Millisecond); ok {
			return concealSample{Raw: `"timeout_ms": 86400000`, Shown: `"timeout_ms": 86400000  ` + out}, true
		}
	case "editor.digit_grouping":
		if out, ok := numhint.Group("1000000"); ok {
			return concealSample{Raw: "total = 1000000", Shown: "total = " + out}, true
		}
	case "editor.radix_hints":
		if out, ok := numhint.DecimalOf("1F4"); ok {
			return concealSample{Raw: "mask = 0x1F4", Shown: "mask = 0x1F4  = " + out}, true
		}
	case "editor.permission_hints":
		if out, ok := permhint.Describe("0644"); ok {
			return concealSample{Raw: "chmod 0644 app.conf", Shown: "chmod 0644  " + out + " app.conf"}, true
		}
	case "editor.cidr_hints":
		if out, ok := nethint.DescribeCIDR("10.0.0.0/8"); ok {
			return concealSample{Raw: `allow = "10.0.0.0/8"`, Shown: `allow = "10.0.0.0/8"  ` + out}, true
		}
	case "editor.idn_hints":
		if out, _, ok := nethint.DecodeIDN("xn--mnchen-3ya.de"); ok {
			return concealSample{Raw: `host = "xn--mnchen-3ya.de"`, Shown: `host = "xn--mnchen-3ya.de"  ` + out}, true
		}
	case "editor.secret_masking", "editor.secret_masking_keys":
		return secretSample(), true
	case "editor.rainbow_brackets":
		return concealSample{Raw: "f(g(h(x)))", Shown: "f(g(h(x)))", Paint: paintRainbow}, true
	case "editor.color_preview":
		return concealSample{Raw: `accent = "#7aa2f7"`, Shown: `accent = "#7aa2f7"`, Paint: paintColorLiteral}, true
	case "editor.id_colors", "editor.id_color_min_length":
		return concealSample{Raw: "trace_id=9f2c1ab34de5f6a7", Shown: "trace_id=9f2c1ab34de5f6a7", Paint: paintIdentifier}, true
	}
	return concealSample{}, false
}

// secretSample previews masking through secret.Suspect, so a configured key
// pattern (editor.secret_masking_keys) shows up here the moment it is added:
// the sample key is the first candidate the live tables actually mask.
func secretSample() concealSample {
	candidates := []string{"API_TOKEN", "DB_PASSWORD", "SECRET"}
	for _, k := range candidates {
		if secret.Suspect(k) {
			return concealSample{Raw: k + "=s3cr3t-value", Shown: k + "=" + strings.Repeat("•", 8)}
		}
	}
	// Every candidate exempted by a configured pattern: the preview has to say
	// so rather than draw a mask that will not happen.
	key := candidates[0]
	return concealSample{Raw: key + "=s3cr3t-value", Shown: key + "=s3cr3t-value   (exempted by a configured pattern)"}
}

// paintRainbow colors the sample's brackets by nesting depth, the way the
// bracket layer does.
func paintRainbow(p *theme.Palette, text string) string {
	depth := 0
	var b strings.Builder
	for _, r := range text {
		switch r {
		case '(', '[', '{':
			b.WriteString(colored(rainbowColor(p, depth), string(r)))
			depth++
			continue
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
			b.WriteString(colored(rainbowColor(p, depth), string(r)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// rainbowColor picks one depth's color out of the palette's rainbow slots,
// returning "" on a palette that defines none — the preview then renders in
// the panel's own colors rather than in a color the theme never chose.
func rainbowColor(p *theme.Palette, depth int) string {
	var cycle []string
	for i := 0; i < 6; i++ {
		if c, ok := p.Captures["rainbow."+strconv.Itoa(i)]; ok && c != "" {
			cycle = append(cycle, c)
		}
	}
	if len(cycle) == 0 {
		return ""
	}
	if depth < 0 {
		depth = 0
	}
	return cycle[depth%len(cycle)]
}

// paintColorLiteral tints the sample's hex literal with its own color, the way
// the color-preview layer does.
func paintColorLiteral(_ *theme.Palette, text string) string {
	i := strings.Index(text, "#")
	if i < 0 {
		return text
	}
	end := i + 1
	for end < len(text) && isHexDigit(text[end]) {
		end++
	}
	lit := text[i:end]
	return text[:i] + lipgloss.NewStyle().Foreground(lipgloss.Color(lit)).Render(lit) + text[end:]
}

// isHexDigit reports whether b is a hex digit of a #rrggbb literal.
func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// paintIdentifier colors the sample's hex run by hashing it into the rainbow
// palette, the way the identifier-color layer does — same id, same color.
func paintIdentifier(p *theme.Palette, text string) string {
	i := strings.LastIndex(text, "=")
	if i < 0 || i+1 >= len(text) {
		return text
	}
	id := text[i+1:]
	return text[:i+1] + colored(rainbowColor(p, idcolor.Slot(id)), id)
}

// colored renders text in token, or plain when the palette supplied none.
func colored(token, text string) string {
	if token == "" {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(token)).Render(text)
}

// concealSamplePaths are the paths the file-rule preview classifies: one
// ordinary source file, one fixture and one log — the three cases the rules
// are usually written about.
var concealSamplePaths = []string{"src/app.go", "testdata/fixture.env", "build/run.log"}

// concealFilterPreview reports, per sample path, whether family may draw there
// under the live include/exclude/rule settings. An empty family stands for
// "any family with no rules of its own", which is what the global lists decide.
func concealFilterPreview(family string, include, exclude, rules []string) []string {
	r := concealfilter.Compile(include, exclude, rules)
	fam := family
	if fam == "" {
		// A name no rule can name: the verdict is then the global level's,
		// which is exactly what the global lists are being previewed for.
		fam = "\x00none"
	}
	out := make([]string, 0, len(concealSamplePaths))
	for _, path := range concealSamplePaths {
		verdict := "draws"
		if !r.Allows(fam, path) {
			verdict = "blocked"
		}
		out = append(out, pad(path, 24)+verdict)
	}
	return out
}
