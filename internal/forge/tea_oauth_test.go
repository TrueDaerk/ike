package forge

// tea_oauth_test.go covers the `tea api` transport (#2118): the binding a
// tea login gets when config.yml holds no plaintext token for it, because
// the login authenticates by OAuth and tea keeps the access token in its own
// credential store. The stub `tea` on PATH stands in for that store.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestTeaAPIArgs(t *testing.T) {
	q := url.Values{}
	q.Set("state", "open")
	q.Set("page", "1")
	args := teaAPIArgs("gregory", http.MethodGet, "/repos/org/repo/issues", q, false)
	want := []string{"api", "--login", "gregory", "--include", "--method", "GET",
		"/repos/org/repo/issues?page=1&state=open"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	// A request body never becomes an argv element — it travels on stdin.
	args = teaAPIArgs("gregory", http.MethodPatch, "/repos/org/repo/issues/42", nil, true)
	want = []string{"api", "--login", "gregory", "--include", "--method", "PATCH",
		"--data", "@-", "/repos/org/repo/issues/42"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestTeaStatusLine(t *testing.T) {
	code, reason := teaStatusLine([]byte("HTTP/2.0 404 Not Found\r\nContent-Type: application/json\r\n\r\n"))
	if code != 404 || reason != "404 Not Found" {
		t.Fatalf("status = (%d, %q)", code, reason)
	}
	// tea may warn before the status line; the parser skips to it.
	code, _ = teaStatusLine([]byte("Warning: failed to refresh OAuth token\nHTTP/1.1 200 OK\n"))
	if code != 200 {
		t.Fatalf("code = %d, want 200", code)
	}
	if code, _ := teaStatusLine([]byte("something else entirely\n")); code != 0 {
		t.Fatalf("code = %d, want 0 for a missing status line", code)
	}
	if code, _ := teaStatusLine(nil); code != 0 {
		t.Fatalf("code = %d, want 0 for empty stderr", code)
	}
}

// fakeTea puts a `tea` on PATH that answers any call with one canned
// response — the status line on stderr, as --include prints it, and the body
// on stdout. It records the argv and the piped stdin for the assertions.
func fakeTea(t *testing.T, status, body string) (argvLog, stdinLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub tea is a shell script")
	}
	dir := t.TempDir()
	argvLog = filepath.Join(dir, "argv")
	stdinLog = filepath.Join(dir, "stdin")
	bodyFile := filepath.Join(dir, "body")
	if err := os.WriteFile(bodyFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + argvLog + "'\n" +
		"cat > '" + stdinLog + "'\n" +
		"printf 'HTTP/1.1 " + status + "\\r\\n\\r\\n' >&2\n" +
		"cat '" + bodyFile + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "tea"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvLog, stdinLog
}

// argvOf reads one recorded command line back as a slice.
func argvOf(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// oauthForge is a binding for a login without a plaintext token — the OAuth
// case, where every call has to go through `tea api`.
func oauthForge(dir string) *teaForge {
	return &teaForge{dir: dir, baseURL: "https://goofy.007ac9.net",
		owner: "org", repo: "repo", name: "gregory", user: "gregory"}
}

func TestTeaOAuthListingRunsThroughTeaAPI(t *testing.T) {
	argvLog, _ := fakeTea(t, "200 OK", giteaIssuesFixture)
	f := oauthForge(t.TempDir())
	issues, err := f.Issues(IssuesOpen)
	if err != nil {
		t.Fatal(err)
	}
	// The raw REST document reaches the ordinary parser, colors and all —
	// that is the whole point of `tea api` over `tea issues list --output json`.
	if len(issues) != 2 || issues[0].Number != 42 {
		t.Fatalf("issues = %+v", issues)
	}
	if len(issues[0].Labels) != 2 || issues[0].Labels[0].Color != "ee0701" {
		t.Fatalf("label colors must survive the CLI transport: %+v", issues[0].Labels)
	}
	argv := argvOf(t, argvLog)
	if argv[0] != "api" {
		t.Fatalf("argv = %v", argv)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{"--login gregory", "--include", "--method GET",
		"/repos/org/repo/issues?"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q misses %q", joined, want)
		}
	}
}

func TestTeaOAuthWriteSendsBodyOnStdin(t *testing.T) {
	argvLog, stdinLog := fakeTea(t, "201 Created", `{"id": 7}`)
	f := oauthForge(t.TempDir())
	if err := f.CreateComment(42, "a body with 'quotes' and $shell chars"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argvOf(t, argvLog), " ")
	for _, want := range []string{"--method POST", "--data @-", "/repos/org/repo/issues/42/comments"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q misses %q", joined, want)
		}
	}
	raw, err := os.ReadFile(stdinLog)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("stdin %q is not the JSON payload: %v", raw, err)
	}
	if payload["body"] != "a body with 'quotes' and $shell chars" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestTeaOAuthSurfacesTheForgesReason(t *testing.T) {
	// tea api exits 0 on a 4xx, so the status line on stderr is what turns
	// the error document into an error instead of a parse failure.
	fakeTea(t, "403 Forbidden", `{"message":"token does not have at least one of required scope(s)"}`)
	f := oauthForge(t.TempDir())
	_, err := f.Issues(IssuesOpen)
	if err == nil {
		t.Fatal("a 403 must not parse as an empty listing")
	}
	if !strings.Contains(err.Error(), "required scope") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want the forge's message and the status", err)
	}
}

func TestTeaOAuthErrorDocumentWithoutAStatusLine(t *testing.T) {
	// An older tea whose --include prints nothing leaves the error document
	// as the only signal; it must still beat "parse the body as a listing".
	if runtime.GOOS == "windows" {
		t.Skip("the stub tea is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tea"),
		[]byte("#!/bin/sh\nprintf '%s' '{\"message\":\"repository does not exist\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := oauthForge(t.TempDir())
	_, err := f.Issues(IssuesOpen)
	if err == nil || !strings.Contains(err.Error(), "repository does not exist") {
		t.Fatalf("err = %v, want the forge's message", err)
	}
}

func TestTeaTokenLoginKeepsTheRESTTransport(t *testing.T) {
	// A login with a token in config.yml must not gain a subprocess: the
	// stub tea would answer, but it is never called — #2118 changes nothing
	// for token logins. The REST call fails against the closed port, which
	// is exactly the proof.
	argvLog, _ := fakeTea(t, "200 OK", giteaIssuesFixture)
	f := oauthForge(t.TempDir())
	f.token = "token-123"
	f.baseURL = "http://127.0.0.1:1"
	if _, err := f.Issues(IssuesOpen); err == nil {
		t.Fatal("the REST transport must have been used and failed")
	}
	if _, err := os.Stat(argvLog); !os.IsNotExist(err) {
		t.Fatalf("tea was invoked for a token login (%v)", err)
	}
}

// detectTea puts a `tea` on PATH that answers the two calls detection makes:
// the login listing, and the `api --help` probe for the passthrough command.
// apiSupported false stands for a tea older than 0.12.
func detectTea(t *testing.T, apiSupported bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub tea is a shell script")
	}
	dir := t.TempDir()
	apiExit := "0"
	if !apiSupported {
		apiExit = "3"
	}
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  logins) printf '%s' '[{\"login\":\"gregory\",\"url\":\"https://goofy.007ac9.net\",\"user\":\"gregory\"}]' ;;\n" +
		"  api) exit " + apiExit + " ;;\n" +
		"  *) exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "tea"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// tea's config is read from the user config dir; an empty HOME keeps a
	// real login on this machine out of the test.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

// teaRepo is a git repository whose origin points at the stub login's host.
func teaRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	gitIn(t, dir, "remote", "add", "origin", "https://goofy.007ac9.net/org/repo.git")
	t.Cleanup(func() { ResetDetection(dir) })
	ResetDetection(dir)
	return dir
}

func TestDetectOAuthLoginBindsTheCLITransport(t *testing.T) {
	// The acceptance case of #2118: a login with no token in config.yml is a
	// working backend, not the old "no token for the tea login …" setup wall.
	detectTea(t, true)
	dir := teaRepo(t)
	f, setup := detect(dir)
	if f == nil {
		t.Fatalf("detect refused an OAuth login: %q", setup)
	}
	tf, ok := f.(*teaForge)
	if !ok {
		t.Fatalf("backend = %T, want the tea binding", f)
	}
	if tf.token != "" {
		t.Fatalf("token = %q, want none", tf.token)
	}
	if tf.name != "gregory" || tf.owner != "org" || tf.repo != "repo" {
		t.Fatalf("binding = %+v", tf)
	}
}

func TestDetectOAuthLoginWithoutTheAPICommand(t *testing.T) {
	// A tea too old for `tea api` is the one case left with no way in — the
	// message must say why and how to get a usable login.
	detectTea(t, false)
	f, setup := detect(teaRepo(t))
	if f != nil {
		t.Fatalf("backend = %T, want none", f)
	}
	for _, want := range []string{"gregory", "credential store", "tea api", "tea login add --token"} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup %q misses %q", setup, want)
		}
	}
}
