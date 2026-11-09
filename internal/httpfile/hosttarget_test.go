package httpfile

import (
	"strings"
	"testing"
)

func TestAbsoluteTarget(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		headers []Header
		vars    *Vars
		want    string
		wantErr string
	}{
		{name: "file host", target: "/a", vars: &Vars{File: map[string]string{"host": "http://x.com"}}, want: "http://x.com/a"},
		{name: "trailing slash", target: "/api", vars: &Vars{File: map[string]string{"host": "http://x.com/"}}, want: "http://x.com/api"},
		{name: "no scheme", target: "/a", vars: &Vars{File: map[string]string{"host": "my-host.com"}}, want: "http://my-host.com/a"},
		{name: "nested", target: "/a", vars: &Vars{File: map[string]string{"host": "{{base}}"}, Env: map[string]string{"base": "https://e.com"}}, want: "https://e.com/a"},
		{name: "captured wins", target: "/a", vars: &Vars{Captured: map[string]string{"host": "http://c"}, File: map[string]string{"host": "http://f"}}, want: "http://c/a"},
		{name: "process env", target: "/a", vars: &Vars{Lookup: func(n string) (string, bool) { return "http://p", n == "host" }}, want: "http://p/a"},
		{name: "host header", target: "/a", headers: []Header{{Name: "host", Value: "h.com:8080"}}, vars: &Vars{}, want: "http://h.com:8080/a"},
		{name: "host header with scheme", target: "/a", headers: []Header{{Name: "Host", Value: "https://h.com"}}, vars: &Vars{}, want: "https://h.com/a"},
		{name: "variable beats header", target: "/a", headers: []Header{{Name: "Host", Value: "h.com"}}, vars: &Vars{File: map[string]string{"host": "http://v"}}, want: "http://v/a"},
		{name: "absolute untouched", target: "http://abs/a", vars: &Vars{File: map[string]string{"host": "http://x"}}, want: "http://abs/a"},
		{name: "protocol relative untouched", target: "//abs/a", vars: &Vars{File: map[string]string{"host": "http://x"}}, want: "//abs/a"},
		{name: "missing", target: "/do/stuff", vars: &Vars{}, wantErr: `relative target "/do/stuff" needs a @host variable or Host header`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Request{Method: "GET", Target: tc.target, Headers: tc.headers}
			got, err := AbsoluteTarget(r, r, tc.vars)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAbsoluteTargetPlaceholderFrontNotRelative(t *testing.T) {
	r := &Request{Method: "GET", Target: "{{path}}"}
	resolved := &Request{Method: "GET", Target: "/resolved"}
	got, err := AbsoluteTarget(r, resolved, &Vars{File: map[string]string{"host": "http://x"}})
	if err != nil || got != "/resolved" {
		t.Errorf("got %q, %v", got, err)
	}
}
