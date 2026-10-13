package complexitycli

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/cli"
)

// serverGo holds Server's two methods (CCN 2 + 1, cog 1) and a free
// function; clientGo Client's two (CCN 5 + 1, cog 6).
const serverGo = `package p

type Server struct{}

func (s *Server) Handle(a int) int {
	if a > 0 {
		return 1
	}
	return 0
}

func (s Server) Close() {}

func helper() {}
`

const clientGo = `package p

type Client struct{}

func (c *Client) Send(a, b int) int {
	if a > 0 {
		if b > 0 {
			return 1
		}
	}
	for i := 0; i < a; i++ {
		if i == b {
			return i
		}
	}
	return 0
}

func (c *Client) Retry() {}
`

// groupFixture is two files, two classes and a free function.
func groupFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	write(t, dir, "server.go", serverGo)
	write(t, dir, "client.go", clientGo)
	return dir
}

// dataLines are the table rows between the header and the blank line.
func dataLines(out string) (header string, rows []string) {
	lines := strings.Split(out, "\n")
	for _, l := range lines[1:] {
		if l == "" {
			break
		}
		rows = append(rows, strings.Join(strings.Fields(l), " "))
	}
	return strings.Join(strings.Fields(lines[0]), " "), rows
}

func TestGroupByClassText(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{By: "class", Paths: []string{dir}})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	header, rows := dataLines(out)
	if want := "FILE CLASS FUNCS NLOC CCN CCN_AVG CCN_MAX COG COG_AVG COG_MAX CMT% OVER EXCESS"; header != want {
		t.Errorf("header %q, want %q", header, want)
	}
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "client.go Client 2 ") || !strings.HasPrefix(rows[1], "server.go Server 2 ") {
		t.Fatalf("rows = %q", rows)
	}
	// FUNCS NLOC CCN CCN_AVG CCN_MAX COG COG_AVG COG_MAX for Client.
	if f := strings.Fields(rows[0]); f[4] != "6" || f[5] != "3.0" || f[6] != "5" || f[7] != "6" || f[9] != "6" {
		t.Errorf("Client row %q", rows[0])
	}
	if strings.Contains(out, "helper") || !strings.Contains(out, "summary: 5 functions in 2 files · ΣCCN 10") {
		t.Errorf("output:\n%s", out)
	}
	if strings.Contains(out, "shown:") {
		t.Error("unfiltered report printed a shown line")
	}
}

func TestGroupByFileText(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	out, _, code := run(t, cli.ComplexityArgs{By: "file", Paths: []string{dir}})
	if code != ExitOK {
		t.Fatal(code)
	}
	header, rows := dataLines(out)
	if strings.Contains(header, "CLASS") || !strings.HasPrefix(header, "FILE FUNCS NLOC CCN") {
		t.Errorf("header %q", header)
	}
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "client.go 2 ") || !strings.HasPrefix(rows[1], "server.go 3 ") {
		t.Fatalf("rows = %q", rows)
	}
}

func TestGroupJSON(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	for _, by := range []string{"class", "file"} {
		out, _, code := run(t, cli.ComplexityArgs{By: by, JSON: true, Paths: []string{dir}})
		if code != ExitOK {
			t.Fatal(code)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatal(err)
		}
		if _, ok := doc["functions"]; ok {
			t.Errorf("--by %s: functions array present", by)
		}
		for _, k := range []string{"summary", "groups", "skipped"} {
			if _, ok := doc[k]; !ok {
				t.Errorf("--by %s: no %q key", by, k)
			}
		}
		var groups []map[string]any
		if err := json.Unmarshal(doc["groups"], &groups); err != nil {
			t.Fatal(err)
		}
		if len(groups) != 2 {
			t.Fatalf("--by %s: %d groups", by, len(groups))
		}
		g := groups[0]
		for _, k := range []string{"file", "functions", "nloc", "ccn_sum", "ccn_avg", "ccn_max", "cog_sum", "cog_avg", "cog_max",
			"over_threshold", "excess", "comment_lines", "comment_density"} {
			if _, ok := g[k]; !ok {
				t.Errorf("--by %s: group has no %q", by, k)
			}
		}
		_, hasClass := g["class"]
		if hasClass != (by == "class") {
			t.Errorf("--by %s: class key present = %v", by, hasClass)
		}
		if g["file"] != "client.go" || g["ccn_sum"] != 6.0 || g["ccn_max"] != 5.0 {
			t.Errorf("--by %s: first group %v", by, g)
		}
	}
}

func TestGroupFilters(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	cases := []struct {
		args  cli.ComplexityArgs
		rows  int
		shown string
	}{
		// ΣCCN: Client 6, Server 3; Σcog 6 and 1.
		{cli.ComplexityArgs{By: "class", Min: 5}, 1, "shown: 1 of 2 classes (ΣCCN or Σcog ≥ 5)"},
		{cli.ComplexityArgs{By: "class", Min: 3}, 2, "shown: 2 of 2 classes"},
		// Max CCN: Client 5, Server 2.
		{cli.ComplexityArgs{By: "class", CCN: 5}, 1, "shown: 1 of 2 classes (max CCN ≥ 5 or max cog ≥ 15"},
		{cli.ComplexityArgs{By: "file", Cog: 6}, 1, "shown: 1 of 2 files (max CCN ≥ 15 or max cog ≥ 6"},
		{cli.ComplexityArgs{By: "file", CCN: 50}, 0, "shown: 0 of 2 files"},
	}
	for _, c := range cases {
		c.args.Paths = []string{dir}
		out, _, code := run(t, c.args)
		if code != ExitOK {
			t.Fatal(code)
		}
		if _, rows := dataLines(out); len(rows) != c.rows {
			t.Errorf("%+v: %d rows, want %d\n%s", c.args, len(rows), c.rows, out)
		}
		if !strings.Contains(out, c.shown) {
			t.Errorf("%+v: no %q in\n%s", c.args, c.shown, out)
		}
	}
}

func TestGroupUsageErrors(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	_, stderr, code := run(t, cli.ComplexityArgs{By: "method", Paths: []string{dir}})
	if code != ExitError || !strings.Contains(stderr, "not class or file") {
		t.Errorf("--by method: exit %d, stderr %q", code, stderr)
	}
	_, stderr, code = run(t, cli.ComplexityArgs{By: "class", Diff: true, Paths: []string{dir}})
	if code != ExitError || !strings.Contains(stderr, "--by is not supported with --diff") {
		t.Errorf("--by with --diff: exit %d, stderr %q", code, stderr)
	}
}
