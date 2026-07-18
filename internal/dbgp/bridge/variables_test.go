package bridge

import (
	"encoding/base64"
	"strings"
	"testing"

	"ike/internal/dap"
)

// bootToBreak drives a fresh session to its first breakpoint stop.
func bootToBreak(t *testing.T) (*dap.Session, chan dap.Event, *fakeXdebug) {
	t.Helper()
	s, events, engines := testClient(t)
	if err := s.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	launchDone := s.LaunchAsync(map[string]any{"request": "launch", "program": "/proj/test.php", "cwd": "/proj"})
	engine := <-engines
	engine.serveFeatureSets()
	if err := <-launchDone; err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitEvent(t, events, "initialized")
	go func() {
		name, tid, _ := engine.next()
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="` + name + `" transaction_id="` + tid + `" status="break" reason="ok"/>`)
	}()
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}
	waitEvent(t, events, "stopped")
	return s, events, engine
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestScopesVariablesAndSetVariable(t *testing.T) {
	s, _, engine := bootToBreak(t)

	// Scopes of the top frame (id 1 → depth 0).
	go func() {
		name, tid, line := engine.next()
		if name != "context_names" || !strings.Contains(line, "-d 0") {
			t.Errorf("unexpected command: %q", line)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="context_names" transaction_id="` + tid + `">` +
			`<context name="Locals" id="0"/><context name="Superglobals" id="1"/></response>`)
	}()
	scopes, err := s.Scopes(1)
	if err != nil {
		t.Fatalf("scopes: %v", err)
	}
	if len(scopes) != 2 || scopes[0].Name != "Locals" || scopes[0].VariablesReference == 0 {
		t.Fatalf("unexpected scopes: %+v", scopes)
	}
	if scopes[0].Expensive || !scopes[1].Expensive {
		t.Fatalf("expensive flags wrong: %+v", scopes)
	}

	// Locals: a clipped string, an int, and an array with children.
	go func() {
		name, tid, line := engine.next()
		if name != "context_get" || !strings.Contains(line, "-c 0") {
			t.Errorf("unexpected command: %q", line)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="context_get" transaction_id="` + tid + `">` +
			`<property name="$s" fullname="$s" type="string" size="10" encoding="base64">` + b64("hello") + `</property>` +
			`<property name="$n" fullname="$n" type="int">7</property>` +
			`<property name="$arr" fullname="$arr" type="array" children="1" numchildren="2" pagesize="32"/>` +
			`</response>`)
	}()
	vars, err := s.Variables(scopes[0].VariablesReference)
	if err != nil {
		t.Fatalf("variables: %v", err)
	}
	if len(vars) != 3 {
		t.Fatalf("want 3 variables: %+v", vars)
	}
	if vars[0].Value != `"hello…"` || vars[0].Type != "string(10)" {
		t.Errorf("clipped string rendering: %+v", vars[0])
	}
	if vars[1].Value != "7" || vars[1].Type != "int" || vars[1].VariablesReference != 0 {
		t.Errorf("int rendering: %+v", vars[1])
	}
	arr := vars[2]
	if arr.Value != "array(2)" || arr.VariablesReference == 0 {
		t.Fatalf("array rendering: %+v", arr)
	}

	// Expanding the array goes through property_get by fullname.
	go func() {
		name, tid, line := engine.next()
		if name != "property_get" || !strings.Contains(line, `-n $arr`) {
			t.Errorf("unexpected command: %q", line)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="property_get" transaction_id="` + tid + `">` +
			`<property name="$arr" fullname="$arr" type="array" children="1" numchildren="2" pagesize="32">` +
			`<property name="0" fullname="$arr[0]" type="int">1</property>` +
			`<property name="1" fullname="$arr[1]" type="null"/>` +
			`</property></response>`)
	}()
	kids, err := s.Variables(arr.VariablesReference)
	if err != nil {
		t.Fatalf("child variables: %v", err)
	}
	if len(kids) != 2 || kids[0].Value != "1" || kids[1].Value != "null" {
		t.Fatalf("unexpected children: %+v", kids)
	}

	// setVariable on a local: property_set + echo via property_get.
	go func() {
		name, tid, line := engine.next()
		if name != "property_set" || !strings.Contains(line, "-n $n") ||
			!strings.Contains(line, "-- "+b64("42")) {
			t.Errorf("unexpected command: %q", line)
		}
		engine.ack(name, tid, `success="1"`)
		name, tid, _ = engine.next()
		if name != "property_get" {
			t.Errorf("expected echo property_get, got %q", name)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="property_get" transaction_id="` + tid + `">` +
			`<property name="$n" fullname="$n" type="int">42</property></response>`)
	}()
	if !s.SupportsSetVariable() {
		t.Fatal("bridge must advertise supportsSetVariable")
	}
	v, err := s.SetVariable(scopes[0].VariablesReference, "$n", "42")
	if err != nil {
		t.Fatalf("setVariable: %v", err)
	}
	if v.Value != "42" || v.Type != "int" {
		t.Fatalf("unexpected echo: %+v", v)
	}
}

func TestVariableRefsDieOnResume(t *testing.T) {
	s, events, engine := bootToBreak(t)

	go func() {
		_, tid, _ := engine.next()
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="context_names" transaction_id="` + tid + `">` +
			`<context name="Locals" id="0"/></response>`)
	}()
	scopes, err := s.Scopes(1)
	if err != nil || len(scopes) != 1 {
		t.Fatalf("scopes: %v %+v", err, scopes)
	}
	ref := scopes[0].VariablesReference

	// Resume, break again: the old reference must now be stale-empty.
	go func() {
		name, tid, _ := engine.next()
		if name != "run" {
			t.Errorf("expected run, got %q", name)
		}
		engine.send(`<response xmlns="urn:debugger_protocol_v1" command="run" transaction_id="` + tid + `" status="break" reason="ok"/>`)
	}()
	if err := s.Continue(1); err != nil {
		t.Fatalf("continue: %v", err)
	}
	waitEvent(t, events, "stopped")
	vars, err := s.Variables(ref)
	if err != nil {
		t.Fatalf("stale variables call: %v", err)
	}
	if len(vars) != 0 {
		t.Fatalf("stale reference served data: %+v", vars)
	}
}
