// Package ansiblevault implements the Ansible Vault 1.1/1.2 payload format
// (AES-256-CTR, PBKDF2-HMAC-SHA256, HMAC-SHA256 integrity) so vault files can
// be decrypted and re-encrypted without the ansible-vault CLI.
package ansiblevault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Header is the marker every Ansible Vault file starts with.
const Header = "$ANSIBLE_VAULT;"

const (
	cipherName    = "AES256"
	kdfIterations = 10000
	saltLen       = 32
	// PBKDF2 output: 32 bytes AES key + 32 bytes HMAC key + 16 bytes IV.
	derivedKeyLen = 80
	// Vault body lines are wrapped at 80 hex characters.
	wrapWidth = 80
)

// ErrWrongPassword is returned when the HMAC check fails, which in practice
// means the password does not match the file.
var ErrWrongPassword = errors.New("ansible-vault: HMAC mismatch (wrong password or corrupted file)")

// ErrNotVault is returned when the input does not carry the vault header.
var ErrNotVault = errors.New("ansible-vault: input is not an Ansible Vault file")

// IsVault reports whether data starts with the Ansible Vault header.
func IsVault(data []byte) bool {
	return bytes.HasPrefix(data, []byte(Header))
}

// Label extracts the vault id label from a 1.2 header, or "" for 1.1 files.
func Label(data []byte) string {
	line, _, _ := strings.Cut(string(data), "\n")
	parts := strings.Split(strings.TrimSpace(line), ";")
	if len(parts) >= 4 && parts[0] == "$ANSIBLE_VAULT" {
		return parts[3]
	}
	return ""
}

// Decrypt decrypts an Ansible Vault 1.1/1.2 envelope with the given password.
func Decrypt(data []byte, password string) ([]byte, error) {
	if !IsVault(data) {
		return nil, ErrNotVault
	}
	headerLine, body, _ := strings.Cut(string(data), "\n")
	parts := strings.Split(strings.TrimSpace(headerLine), ";")
	if len(parts) < 3 {
		return nil, fmt.Errorf("ansible-vault: malformed header %q", headerLine)
	}
	if version := parts[1]; version != "1.1" && version != "1.2" {
		return nil, fmt.Errorf("ansible-vault: unsupported format version %q", version)
	}
	if parts[2] != cipherName {
		return nil, fmt.Errorf("ansible-vault: unsupported cipher %q", parts[2])
	}

	inner, err := hex.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return nil, fmt.Errorf("ansible-vault: malformed body: %w", err)
	}
	segments := bytes.SplitN(inner, []byte("\n"), 3)
	if len(segments) != 3 {
		return nil, errors.New("ansible-vault: malformed payload: expected salt, HMAC and ciphertext")
	}
	salt, err := hex.DecodeString(string(segments[0]))
	if err != nil {
		return nil, fmt.Errorf("ansible-vault: malformed salt: %w", err)
	}
	expectedMAC, err := hex.DecodeString(string(segments[1]))
	if err != nil {
		return nil, fmt.Errorf("ansible-vault: malformed HMAC: %w", err)
	}
	ciphertext, err := hex.DecodeString(string(segments[2]))
	if err != nil {
		return nil, fmt.Errorf("ansible-vault: malformed ciphertext: %w", err)
	}

	aesKey, hmacKey, iv, err := deriveKeys(password, salt)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(ciphertext)
	if !hmac.Equal(mac.Sum(nil), expectedMAC) {
		return nil, ErrWrongPassword
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCTR(block, iv).XORKeyStream(plaintext, ciphertext)
	return stripPKCS7(plaintext)
}

// Encrypt encrypts plaintext into an Ansible Vault envelope with the given
// password. A non-empty label produces a 1.2 header carrying the vault id,
// otherwise a 1.1 header is written. The result ends with a newline, matching
// ansible-vault output.
func Encrypt(plaintext []byte, password, label string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encryptWithSalt(plaintext, password, label, salt)
}

func encryptWithSalt(plaintext []byte, password, label string, salt []byte) ([]byte, error) {
	aesKey, hmacKey, iv, err := deriveKeys(password, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	padded := padPKCS7(plaintext)
	ciphertext := make([]byte, len(padded))
	cipher.NewCTR(block, iv).XORKeyStream(ciphertext, padded)
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(ciphertext)

	inner := strings.Join([]string{
		hex.EncodeToString(salt),
		hex.EncodeToString(mac.Sum(nil)),
		hex.EncodeToString(ciphertext),
	}, "\n")
	body := hex.EncodeToString([]byte(inner))

	header := "$ANSIBLE_VAULT;1.1;" + cipherName
	if label != "" {
		header = "$ANSIBLE_VAULT;1.2;" + cipherName + ";" + label
	}
	var out strings.Builder
	out.WriteString(header)
	for i := 0; i < len(body); i += wrapWidth {
		out.WriteByte('\n')
		out.WriteString(body[i:min(i+wrapWidth, len(body))])
	}
	out.WriteByte('\n')
	return []byte(out.String()), nil
}

func deriveKeys(password string, salt []byte) (aesKey, hmacKey, iv []byte, err error) {
	derived, err := pbkdf2.Key(sha256.New, password, salt, kdfIterations, derivedKeyLen)
	if err != nil {
		return nil, nil, nil, err
	}
	return derived[:32], derived[32:64], derived[64:80], nil
}

func padPKCS7(data []byte) []byte {
	pad := aes.BlockSize - len(data)%aes.BlockSize
	return append(append([]byte{}, data...), bytes.Repeat([]byte{byte(pad)}, pad)...)
}

func stripPKCS7(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("ansible-vault: invalid padded plaintext length")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, errors.New("ansible-vault: invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("ansible-vault: invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}
