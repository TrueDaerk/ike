package config

import "testing"

// terminal.ssh_hosts validation (#1938): entries are trimmed, blanks dropped
// silently, and an alias carrying whitespace is dropped with a diagnostic —
// it could never be a single ssh argument.
func TestValidateSSHHosts(t *testing.T) {
	c := defaults()
	c.Terminal.SSHHosts = []string{" build01 ", "", "   ", "two hosts", "monitor"}
	diags := validate(c)

	want := []string{"build01", "monitor"}
	if len(c.Terminal.SSHHosts) != len(want) {
		t.Fatalf("hosts = %v, want %v", c.Terminal.SSHHosts, want)
	}
	for i, h := range want {
		if c.Terminal.SSHHosts[i] != h {
			t.Fatalf("hosts = %v, want %v", c.Terminal.SSHHosts, want)
		}
	}
	got := 0
	for _, d := range diags {
		if d.Field == "terminal.ssh_hosts" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("%d diagnostics, want exactly the whitespace one (%v)", got, diags)
	}
}
