// Package secret decides which configuration keys name a value nobody should
// have to read off a shared screen (#1623): API keys, tokens, passwords,
// credentials. It is the key-pattern half of the dotenv masking feature — the
// producer (plugins/languages/env) turns a Suspect key into a stand-in span
// carrying Mask, and the editor renders that mask everywhere the caret is not
// (#1594's positional reveal).
//
// A leaf package with no IKE dependencies: the language plugin, the editor and
// any future consumer (a .properties or shell-export mode) share one table, so
// "what looks like a secret" is answered in exactly one place.
package secret

import "strings"

// Capture is the highlight capture carried by a masked value's span. The
// editor treats it as a conceal family of its own — gated by
// editor.secret_masking / view.toggleSecretMasking, independent of the decode
// families (#1620).
const Capture = "secret.value"

// Mask is the stand-in drawn instead of a suspect value. Fixed width on
// purpose: a mask sized to the value would leak its length, which for a short
// password is most of the secret.
const Mask = "••••"

// strong are the substrings that name a secret outright, whatever else the key
// says: `AWS_ACCESS_KEY_ID_SECRET` is a secret even though it also contains
// `KEY_ID`. Matched on the upper-cased key, so `db_password`, `DB_PASSWORD`
// and `dbPassword` all hit. The lists are deliberately blunt — a false
// positive costs one keystroke of toggling, a false negative costs a leaked
// credential on a screen share.
var strong = []string{
	"SECRET",
	"PASSWORD",
	"PASSWD",
	"PASSPHRASE",
	"CREDENTIAL",
	"PRIVATE_KEY",
}

// markers are the substrings that name a secret unless a publicMarker clears
// the key first: a `TOKEN` is one, a `TOKEN_URL` is not.
var markers = []string{
	"TOKEN",
	"APIKEY",
	"API_KEY",
	"ACCESS_KEY",
	"AUTH",
	"SALT",
	"SIGNING",
	"SIGNATURE",
	"CERT",
	"DSN",
}

// suffixes are the key endings that name a secret without a marker word
// appearing inside them: `..._KEY`, `..._PWD`. The underscore is required so a
// word merely ending in the letters ("MONKEY", "FWD") stays untouched; the
// bare names are matched exactly by exact below.
var suffixes = []string{"_KEY", "_PWD"}

// exact are the whole key names that are secrets on their own.
var exact = []string{"KEY", "PWD", "PASS"}

// publicMarkers are the substrings that clear an otherwise-suspect key: a
// public key, a key id, a key name or a token *expiry* are not the secret
// itself, and masking them only trains people to switch masking off.
var publicMarkers = []string{
	"PUBLIC",
	"KEY_ID",
	"KEYID",
	"KEY_NAME",
	"KEY_PATH",
	"KEY_FILE",
	"TOKEN_URL",
	"TOKEN_EXPIRY",
	"TOKEN_TTL",
	"AUTH_URL",
	"AUTH_TYPE",
	"AUTH_METHOD",
	"AUTHOR",
	"KEYCLOAK",
	"KEYWORD",
	"CERTAIN",
}

// Suspect reports whether key names a value that should render masked. An
// empty key is never suspect.
//
// The user's own patterns (editor.secret_masking_keys, #1712) are consulted
// first and decide on their own: a positive entry masks a key the tables below
// never heard of, a negative one exempts a key they would otherwise mask. Only
// a key no configured pattern matches reaches the built-ins.
func Suspect(key string) bool {
	up := strings.ToUpper(strings.TrimSpace(key))
	if up == "" {
		return false
	}
	if mask, matched := keyVerdict(up); matched {
		return mask
	}
	for _, s := range strong {
		if strings.Contains(up, s) {
			return true
		}
	}
	for _, p := range publicMarkers {
		if strings.Contains(up, p) {
			return false
		}
	}
	for _, m := range markers {
		if strings.Contains(up, m) {
			return true
		}
	}
	for _, s := range suffixes {
		if strings.HasSuffix(up, s) {
			return true
		}
	}
	for _, e := range exact {
		if up == e {
			return true
		}
	}
	return false
}
