package settings

import "testing"

// TestSSHHostValidate verifies the terminal.ssh_hosts element rule (#1938):
// a bare alias is accepted, an empty or whitespace-carrying one rejected with
// a message that says why.
func TestSSHHostValidate(t *testing.T) {
	for _, ok := range []string{"build01", "ops@10.0.0.5", " padded "} {
		if msg := sshHostValidate(nil, ok); msg != "" {
			t.Fatalf("%q rejected: %s", ok, msg)
		}
	}
	for _, bad := range []string{"", "   ", "two hosts", "tab\there"} {
		if msg := sshHostValidate(nil, bad); msg == "" {
			t.Fatalf("%q accepted, want a rejection message", bad)
		}
	}
}

// TestSSHHostsEntryInSchema verifies the setting is reachable in the Settings
// UI as a validated list (a config-file-only setting would be a regression).
func TestSSHHostsEntryInSchema(t *testing.T) {
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key != "terminal.ssh_hosts" {
				continue
			}
			if e.Type != List || e.ValidateEntry == nil {
				t.Fatalf("entry = %+v, want a List with element validation", e)
			}
			return
		}
	}
	t.Fatal("terminal.ssh_hosts missing from the settings schema")
}
