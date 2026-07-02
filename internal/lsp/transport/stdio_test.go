package transport

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStartNotFound(t *testing.T) {
	_, err := Start(Spec{Command: "definitely-not-a-real-binary-xyz"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestProcessRoundTripWithCat(t *testing.T) {
	// `cat` echoes stdin to stdout: a trivial duplex server for the transport.
	p, err := Start(Spec{Command: "cat"})
	if err != nil {
		t.Skipf("cat unavailable: %v", err)
	}
	defer p.Stop()

	conn := p.Conn()
	if _, err := io.WriteString(conn, "hello\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "hello" {
		t.Fatalf("echo = %q", line)
	}
}

func TestProcessExitSignalled(t *testing.T) {
	// `true` exits immediately.
	p, err := Start(Spec{Command: "true"})
	if err != nil {
		t.Skipf("true unavailable: %v", err)
	}
	select {
	case <-p.Exited():
		if err := p.WaitErr(); err != nil {
			t.Fatalf("clean exit reported error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process exit not signalled")
	}
}

func TestStderrCaptured(t *testing.T) {
	// Write to stderr then exit.
	p, err := Start(Spec{Command: "sh", Args: []string{"-c", "echo boom 1>&2"}})
	if err != nil {
		t.Skipf("sh unavailable: %v", err)
	}
	<-p.Exited()
	if !strings.Contains(p.Stderr(), "boom") {
		t.Fatalf("stderr = %q, want it to contain boom", p.Stderr())
	}
}
