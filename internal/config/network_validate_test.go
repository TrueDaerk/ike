package config

import (
	"strings"
	"testing"
)

// TestNetworkPortClamps: a port outside 1..65535 falls back to the default
// with a diagnostic naming the field.
func TestNetworkPortClamps(t *testing.T) {
	for _, bad := range []int{0, -1, 65536} {
		c := defaults()
		c.Network.Port = bad
		diags := validate(c)
		if c.Network.Port != NetworkDefaultPort {
			t.Fatalf("port %d: got %d, want %d", bad, c.Network.Port, NetworkDefaultPort)
		}
		if !hasDiag(diags, "network.port") {
			t.Fatalf("port %d: no diagnostic in %+v", bad, diags)
		}
	}
	c := defaults()
	c.Network.Port = 8080
	if diags := validate(c); c.Network.Port != 8080 || hasDiag(diags, "network.port") {
		t.Fatalf("a valid port must stand: %d %+v", c.Network.Port, diags)
	}
}

// TestNetworkBindValidates: IP literals and the empty value pass; host names
// and garbage are reset to the default with a diagnostic.
func TestNetworkBindValidates(t *testing.T) {
	for _, ok := range []string{"", "0.0.0.0", "127.0.0.1", "::1", "[::]", " 10.0.0.5 "} {
		if msg := NetworkBindError(ok); msg != "" {
			t.Errorf("%q must be accepted, got %q", ok, msg)
		}
	}
	for _, bad := range []string{"localhost", "example.com:4530", "0.0.0.0:4530", "not an ip"} {
		if msg := NetworkBindError(bad); msg == "" {
			t.Errorf("%q must be refused", bad)
		}
		c := defaults()
		c.Network.Bind = bad
		diags := validate(c)
		if c.Network.Bind != NetworkDefaultBind || !hasDiag(diags, "network.bind") {
			t.Errorf("%q: bind %q diags %+v", bad, c.Network.Bind, diags)
		}
	}
}

// TestNetworkNameValidates: the empty name and printable single labels
// pass; dots, control characters and over-long names are reset to empty
// (the host name) with a diagnostic.
func TestNetworkNameValidates(t *testing.T) {
	for _, ok := range []string{"", "geants-mac", " ike on the desk ", "Büro"} {
		if msg := NetworkNameError(ok); msg != "" {
			t.Errorf("%q must be accepted, got %q", ok, msg)
		}
	}
	for _, bad := range []string{"a.b", "tab\there", strings.Repeat("x", NetworkNameMaxBytes+1)} {
		if msg := NetworkNameError(bad); msg == "" {
			t.Errorf("%q must be refused", bad)
		}
		c := defaults()
		c.Network.Name = bad
		diags := validate(c)
		if c.Network.Name != "" || !hasDiag(diags, "network.name") {
			t.Errorf("%q: name %q diags %+v", bad, c.Network.Name, diags)
		}
	}
	c := defaults()
	c.Network.Name = "desk"
	if diags := validate(c); c.Network.Name != "desk" || hasDiag(diags, "network.name") {
		t.Fatalf("a valid name must stand: %q %+v", c.Network.Name, diags)
	}
}

// TestNetworkDefaultsOff: the endpoint is opt-in.
func TestNetworkDefaultsOff(t *testing.T) {
	c := defaults()
	if c.Network.Enabled || c.Network.Port != NetworkDefaultPort || c.Network.Bind != NetworkDefaultBind || !c.Network.MDNS || c.Network.Name != "" {
		t.Fatalf("defaults %+v", c.Network)
	}
	flat := c.Flat()
	for _, k := range []string{"network.enabled", "network.port", "network.bind", "network.mdns", "network.name"} {
		if _, ok := flat[k]; !ok {
			t.Errorf("Flat lacks %s", k)
		}
	}
}

func hasDiag(diags []Diagnostic, field string) bool {
	for _, d := range diags {
		if d.Field == field && strings.TrimSpace(d.Message) != "" {
			return true
		}
	}
	return false
}
