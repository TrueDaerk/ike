package netlink

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// tokens.go is the store of paired clients. A pairing hands the client a
// random 256-bit token; only its SHA-256 is written to disk, so the file
// leaking never yields a usable credential. The store is a small JSON file
// under the user state dir, rewritten atomically on every change.

// Client is one paired device as recorded on disk.
type Client struct {
	// ID is a short random handle shown in lists and used to revoke one client.
	ID string `json:"id"`
	// Name is what the client called itself when pairing ("phone", "laptop").
	Name string `json:"name"`
	// Hash is hex(sha256(token)).
	Hash string `json:"hash"`
	// Addr is the remote address the pairing came from — informational.
	Addr     string    `json:"addr"`
	Paired   time.Time `json:"paired"`
	LastSeen time.Time `json:"last_seen"`
}

// Store holds the paired clients, mirrored to a JSON file.
type Store struct {
	mu      sync.Mutex
	path    string
	clients []Client
}

// DefaultStorePath is the clients file: $IKE_CONFIG_DIR/netlink-clients.json
// when the override is set, else ~/.ike/netlink-clients.json.
func DefaultStorePath() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "netlink-clients.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "netlink-clients.json")
}

// OpenStore loads the clients file at path; a missing file is an empty
// store. path "" keeps the store in memory only.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Clients []Client `json:"clients"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	s.clients = file.Clients
	return s, nil
}

// Issue creates a client entry for name/addr and returns the plaintext token
// — the only time it exists outside the client.
func (s *Store) Issue(name, addr string, now time.Time) (token string, c Client, err error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", Client{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw[:])
	var idRaw [4]byte
	_, _ = rand.Read(idRaw[:])
	c = Client{
		ID:       hex.EncodeToString(idRaw[:]),
		Name:     name,
		Hash:     hashToken(token),
		Addr:     addr,
		Paired:   now.UTC(),
		LastSeen: now.UTC(),
	}
	s.mu.Lock()
	s.clients = append(s.clients, c)
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return "", Client{}, err
	}
	return token, c, nil
}

// Verify reports whether token belongs to a paired client and, when it does,
// stamps the client's last-seen time. The comparison is constant-time over
// the hashes; every stored hash is compared so the answer time is
// independent of the match position.
func (s *Store) Verify(token string, now time.Time) (Client, bool) {
	if token == "" {
		return Client{}, false
	}
	want := []byte(hashToken(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	hit := -1
	for i := range s.clients {
		if subtle.ConstantTimeCompare([]byte(s.clients[i].Hash), want) == 1 {
			hit = i
		}
	}
	if hit < 0 {
		return Client{}, false
	}
	// Coarse stamp (minute resolution) so a chatty client does not rewrite
	// the file on every request.
	if now.Sub(s.clients[hit].LastSeen) >= time.Minute {
		s.clients[hit].LastSeen = now.UTC()
		_ = s.saveLocked()
	}
	return s.clients[hit], true
}

// Clients lists the paired clients, most recently paired first.
func (s *Store) Clients() []Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Client(nil), s.clients...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Paired.After(out[j].Paired) })
	return out
}

// Revoke forgets one client by ID; ok reports whether it existed.
func (s *Store) Revoke(id string) (ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.clients[:0]
	for _, c := range s.clients {
		if c.ID == id {
			ok = true
			continue
		}
		kept = append(kept, c)
	}
	s.clients = kept
	if !ok {
		return false, nil
	}
	return true, s.saveLocked()
}

// RevokeAll forgets every paired client; each one has to pair again.
func (s *Store) RevokeAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = nil
	return s.saveLocked()
}

// saveLocked writes the file atomically (temp + rename), 0600. Called with
// mu held. An in-memory store is a no-op.
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Clients []Client `json:"clients"`
	}{Clients: s.clients}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// hashToken is hex(sha256(token)).
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
