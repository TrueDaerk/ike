package jqplay

import (
	"strconv"
	"strings"
	"testing"
)

// xmqcomplete_test.go covers the xmq query line's XPath completion (#2790):
// the path walk over XML and HTML inputs, the attribute and predicate
// contexts, the silence outside a path argument and the node budget.

const xmqSampleXML = `<?xml version="1.0"?>
<report xmlns:x="urn:x">
  <meta page="1" total="2"/>
  <users>
    <user id="1" active="true"><name>Ada</name><x:tag/></user>
    <user id="2" role="admin"><name>Grace</name><mail/></user>
  </users>
</report>`

const xmqSampleHTML = `<!DOCTYPE html>
<html><head><title>t</title></head>
<body class="main"><div id="a"><p>one</p></div><div lang="en"><span>two</span></div><footer></footer></body></html>`

func xmqComplete(t *testing.T, doc, program string) ([]Candidate, int) {
	t.Helper()
	in, err := DialectXMQ.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	return Complete(program, len([]rune(program)), in, false)
}

func TestXMQPathCompletion(t *testing.T) {
	cases := []struct {
		doc, program, want string
		start              int
	}{
		{xmqSampleXML, "select ", "/report //", 7},
		{xmqSampleXML, "select /", "report", 8},
		{xmqSampleXML, "select /report/", "meta users", 15},
		{xmqSampleXML, "select /report/u", "users", 15},
		{xmqSampleXML, "select /report/users/user/", "name x:tag mail", 26},
		{xmqSampleXML, "select /report/users/user[2]/", "name mail", 29},
		// A value predicate keeps the superset: the tree holds no values.
		{xmqSampleXML, "select /report/users/user[@id='2']/", "name x:tag mail", 35},
		{xmqSampleXML, "select //user/@", "@id @active @role", 14},
		{xmqSampleXML, "select //user[@", "@id @active @role", 14},
		{xmqSampleXML, "select //user[@r", "@role", 14},
		{xmqSampleXML, "select /report/meta/@", "@page @total", 20},
		{xmqSampleXML, "select //", "report meta users user name x:tag mail", 9},
		{xmqSampleXML, "select //n", "name", 9},
		{xmqSampleXML, "delete '//users/", "user", 16},
		{xmqSampleXML, "for-each /report/users/../", "meta users", 26},
		{xmqSampleXML, "select /report/@", "", 0},
		{xmqSampleHTML, "select /html/body/", "div footer", 18},
		{xmqSampleHTML, "select /html/body/@", "@class", 18},
		{xmqSampleHTML, "select /html/body/div[1]/", "p", 25},
		{xmqSampleHTML, "select //div/@", "@id @lang", 13},
		{xmqSampleHTML, "select //div[", "p span", 13},
	}
	for _, c := range cases {
		items, start := xmqComplete(t, c.doc, c.program)
		if got := strings.Join(labels(items), " "); got != c.want {
			t.Errorf("%q offered %q, want %q", c.program, got, c.want)
			continue
		}
		if c.want != "" && start != c.start {
			t.Errorf("%q start = %d, want %d", c.program, start, c.start)
		}
	}
}

// TestXMQPathCompletionSilent: outside a path argument, and on steps the
// walk does not model, nothing opens on its own.
func TestXMQPathCompletionSilent(t *testing.T) {
	for _, program := range []string{
		"to-json ",
		"select //user to-json ",
		"select //user[@id='1'] ",
		"select count(/",
		"select //user/child::",
		"select /report/meta.",
	} {
		if items, _ := xmqComplete(t, xmqSampleXML, program); len(items) != 0 {
			t.Errorf("%q offered %v, want nothing", program, labels(items))
		}
	}
	// The command word itself still completes as a command.
	if items, _ := xmqComplete(t, xmqSampleXML, "sel"); strings.Join(labels(items), " ") != "select" {
		t.Errorf("sel offered %v", labels(items))
	}
}

// TestXMQPathCompletionMidLine: the candidate replaces only the partial, so
// the rest of the line survives an accept.
func TestXMQPathCompletionMidLine(t *testing.T) {
	in, err := DialectXMQ.Parse(xmqSampleXML)
	if err != nil {
		t.Fatal(err)
	}
	program := "select /report/us to-json"
	items, start := Complete(program, len("select /report/us"), in, false)
	if len(items) != 1 || items[0].Insert != "users" || start != len("select /report/") {
		t.Fatalf("mid-line completion: %v at %d", labels(items), start)
	}
}

// TestXMQPathCompletionBudget: a document larger than the node budget
// answers with what the capped walk saw, within the item cap.
func TestXMQPathCompletionBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("<r>")
	for i := range completionNodeBudget * 2 {
		n := strconv.Itoa(i)
		b.WriteString("<e" + n + " a" + n + `="1"/>`)
	}
	b.WriteString("</r>")
	for _, program := range []string{"select /r/", "select //", "select //*/@"} {
		items, _ := xmqComplete(t, b.String(), program)
		if len(items) == 0 {
			t.Errorf("%q: a capped walk should still offer what it saw", program)
		}
		if len(items) > MaxCompletionItems {
			t.Errorf("%q: items = %d, over the cap", program, len(items))
		}
	}
	in, err := DialectXMQ.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	budget := completionNodeBudget
	if got := descendantsOrSelf([]*xmqNode{in.xmqTree}, &budget); len(got) != completionNodeBudget || budget != 0 {
		t.Errorf("the descendant walk must stop at the budget: visited %d, left %d", len(got), budget)
	}
}
