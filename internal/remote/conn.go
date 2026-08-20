package remote

// conn.go is the real Conn: the sftp subsystem of the user's own ssh binary,
// spoken over the subprocess's stdio via github.com/pkg/sftp. Everything about
// reaching the host — HostName/User/Port resolution, identity files, the
// agent, ProxyJump, known_hosts — is ssh reading its own config for the alias,
// exactly like the SSH terminal (#1938). BatchMode forbids interactive
// prompts, so a host that would need a password fails the dial with ssh's own
// stderr message instead of hanging.

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// sshArgv assembles the dial command: the alias verbatim (ssh resolves the
// config the picker listed it from), the sftp subsystem on its stdio, no
// forwardings, and a bounded connect wait so a dead host errors out instead
// of stalling the pane forever.
func sshArgv(alias string) []string {
	return []string{
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "ClearAllForwardings=yes",
		"-o", "ConnectTimeout=15",
		"-s", alias, "sftp",
	}
}

// stderrCap bounds the captured ssh stderr: enough for any auth banner, never
// unbounded for a host that streams garbage.
const stderrCap = 4 << 10

// boundedBuf is a concurrency-safe stderr sink capped at stderrCap bytes.
type boundedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *boundedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := stderrCap - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

// tail renders the captured stderr as one trimmed line block.
func (b *boundedBuf) tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

// sshConn is a live session over one ssh subprocess.
type sshConn struct {
	cmd    *exec.Cmd
	client *sftp.Client
	stderr *boundedBuf

	closeOnce sync.Once
	closeErr  error
}

// Dial connects to alias by spawning `ssh -s <alias> sftp` and completing the
// SFTP handshake over its pipes. A failure — unreachable host, refused key,
// missing agent, no sftp subsystem — is reported with ssh's own stderr tail,
// so the message names the actual cause ("Permission denied (publickey)").
func Dial(alias string) (Conn, error) {
	argv := sshArgv(alias)
	cmd := exec.Command(argv[0], argv[1:]...)
	errBuf := &boundedBuf{}
	cmd.Stderr = errBuf
	// Wait must not hang on a stuck ssh holding its pipes after Kill.
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot run ssh: %w", err)
	}
	client, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, dialError(alias, err, errBuf.tail())
	}
	return &sshConn{cmd: cmd, client: client, stderr: errBuf}, nil
}

// dialError composes the actionable connect failure: ssh's own stderr wins
// (it names the cause), the handshake error is the fallback, and a refused
// authentication adds the one hint that matters here — BatchMode means no
// interactive prompt could have saved the dial.
func dialError(alias string, err error, stderr string) error {
	msg := stderr
	if msg == "" {
		msg = err.Error()
	}
	if strings.Contains(msg, "Permission denied") || strings.Contains(msg, "Host key verification failed") {
		msg += " — interactive auth is not available here; set up a key or agent (or connect once via a terminal)"
	}
	return fmt.Errorf("ssh %s: %s", alias, msg)
}

// Home implements Conn via the server's resolution of the session start
// directory.
func (c *sshConn) Home() (string, error) { return c.client.Getwd() }

// ReadDir implements Conn.
func (c *sshConn) ReadDir(path string) ([]Entry, error) {
	infos, err := c.client.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		out = append(out, Entry{Name: fi.Name(), Size: fi.Size(), Mode: fi.Mode(), ModTime: fi.ModTime()})
	}
	return out, nil
}

// Fetch implements Conn: a whole-file read stopped one byte past max, so the
// cap fires without buffering what it refuses.
func (c *sshConn) Fetch(path string, max int64) ([]byte, error) {
	f, err := c.client.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Close implements Conn: end the SFTP session, kill the transport, reap the
// subprocess. Idempotent — the pane closes it once from releaseContent and a
// Discard on an unroutable connect result may close it again.
func (c *sshConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.client.Close()
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	})
	return c.closeErr
}
