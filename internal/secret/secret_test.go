package secret

import "testing"

func TestSuspectKeys(t *testing.T) {
	secrets := []string{
		"API_KEY", "api_key", "STRIPE_SECRET_KEY", "GITHUB_TOKEN", "DB_PASSWORD",
		"PASSWD", "PASSPHRASE", "CLIENT_SECRET", "AWS_ACCESS_KEY_ID_SECRET",
		"GOOGLE_CREDENTIALS", "JWT_SIGNING_KEY", "SESSION_SALT", "TLS_CERT",
		"DATABASE_DSN", "AUTH_TOKEN", "KEY", "PWD", "SERVICE_PWD", "dbPassword",
	}
	for _, k := range secrets {
		if !Suspect(k) {
			t.Errorf("Suspect(%q) = false, want true", k)
		}
	}
}

func TestSuspectPlainKeys(t *testing.T) {
	plain := []string{
		"", "API_HOST", "DEBUG", "PORT", "LOG_LEVEL", "NODE_ENV", "TIMEOUT",
		"MONKEY", "FWD", "KEYWORD", "AUTHOR", "PUBLIC_KEY", "SSH_KEY_PATH",
		"API_KEY_ID", "TOKEN_URL", "TOKEN_EXPIRY", "KEYCLOAK_URL",
	}
	for _, k := range plain {
		if Suspect(k) {
			t.Errorf("Suspect(%q) = true, want false", k)
		}
	}
}

// TestSuspectTrimsAndFoldsCase: the producer hands over the raw key text, so
// surrounding whitespace and casing must not decide the answer.
func TestSuspectTrimsAndFoldsCase(t *testing.T) {
	if !Suspect("  db_password  ") {
		t.Error("a padded lower-case key must still be suspect")
	}
}

// TestMaskHidesLength: the mask is fixed width, so a short password does not
// advertise how short it is.
func TestMaskHidesLength(t *testing.T) {
	if Mask == "" {
		t.Fatal("Mask must not be empty")
	}
	if len([]rune(Mask)) != 4 {
		t.Errorf("Mask = %q, want a fixed four-glyph stand-in", Mask)
	}
}
