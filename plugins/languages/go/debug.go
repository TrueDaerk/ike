package langgo

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ike/internal/lang"
	"ike/internal/lsp/transport"
)

// Debug-adapter contribution (#1914): Go debugs through delve's DAP server.
// `dlv dap` speaks DAP only over a socket — there is no stdio mode — so Go
// rides the in-process connect seam (lang.DebugAdapterInProcess, the PHP
// bridge's path): DebugAdapterConnect spawns `dlv dap --listen=127.0.0.1:0`,
// waits for the listen banner on stdout, dials the port and returns a
// connection whose Close also stops the dlv process. Past construction the
// session is indistinguishable from a stdio adapter.

var (
	_ lang.DebugAdapterProvider  = toolchain{}
	_ lang.DebugAdapterInProcess = toolchain{}
	_ lang.DebugAdapterInstaller = toolchain{}
)

// dlvResolve and dlvListenTimeout are seams for tests.
var (
	dlvResolve       = transport.Resolve
	dlvListenTimeout = 10 * time.Second
)

// dlvBanner precedes the listen address on dlv's stdout.
const dlvBanner = "DAP server listening at: "

// DebugAdapter implements lang.DebugAdapterProvider. The connect seam below
// is preferred by the debug manager; the argv form exists only to satisfy the
// provider interface and would hand dlv a stdio it cannot speak DAP on.
func (toolchain) DebugAdapter(_ string, _ string) ([]string, bool) {
	dlv, err := dlvResolve("dlv")
	if err != nil {
		return nil, false
	}
	return []string{dlv, "dap"}, true
}

// dlvConn is the TCP connection to a spawned `dlv dap`, tying the process
// lifetime to the connection: Close kills dlv (a safety net — delve exits on
// disconnect by itself) and reaps it.
type dlvConn struct {
	net.Conn
	cmd *exec.Cmd
}

func (c *dlvConn) Close() error {
	err := c.Conn.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	return err
}

// DebugAdapterConnect implements lang.DebugAdapterInProcess: spawn dlv, wait
// for its listen banner, dial. Resolution goes beyond PATH (transport.Resolve)
// because `go install` drops dlv into GOBIN/GOPATH/bin, which a GUI-launched
// process typically misses.
func (toolchain) DebugAdapterConnect(root, _ string) (io.ReadWriteCloser, error) {
	dlv, err := dlvResolve("dlv")
	if err != nil {
		return nil, errors.New("dlv (delve) is not installed")
	}
	cmd := exec.Command(dlv, "dap", "--listen=127.0.0.1:0")
	cmd.Dir = root
	// Delve's children (the debuggee, macOS's debugserver) inherit the stderr
	// pipe; without a bound, Wait would block on the stderr copy until the
	// last of them exits — killing dlv alone must be enough to reap it.
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if addr, ok := strings.CutPrefix(sc.Text(), dlvBanner); ok {
				addrCh <- strings.TrimSpace(addr)
				break
			}
		}
		// Keep draining so dlv never blocks on a full stdout pipe; EOF ends
		// the goroutine with the process.
		_, _ = io.Copy(io.Discard, stdout)
	}()
	fail := func(err error) (io.ReadWriteCloser, error) {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		if tail := strings.TrimSpace(stderr.String()); tail != "" {
			return nil, fmt.Errorf("%v — dlv: %s", err, tail)
		}
		return nil, err
	}
	select {
	case addr := <-addrCh:
		conn, err := net.DialTimeout("tcp", addr, dlvListenTimeout)
		if err != nil {
			return fail(err)
		}
		return &dlvConn{Conn: conn, cmd: cmd}, nil
	case <-time.After(dlvListenTimeout):
		return fail(errors.New("dlv did not announce its DAP port"))
	}
}

// DebugAdapterMissing implements lang.DebugAdapterInstaller (#589): the
// adapter is the dlv binary itself; resolution beyond PATH covers a freshly
// `go install`ed delve.
func (toolchain) DebugAdapterMissing(_ string, _ string) (bool, string) {
	if _, err := dlvResolve("dlv"); err != nil {
		return true, "dlv (delve) is not installed"
	}
	return false, ""
}

// DebugAdapterInstall implements lang.DebugAdapterInstaller. `go install`
// with the resolved go binary first — it works everywhere the project builds
// and lands in GOBIN, where dlvResolve finds it; Homebrew is the fallback for
// machines that keep tools out of GOPATH.
func (toolchain) DebugAdapterInstall(_ string, interpreter string) [][]string {
	if interpreter == "" {
		interpreter = "go"
	}
	return [][]string{
		{interpreter, "install", "github.com/go-delve/delve/cmd/dlv@latest"},
		{"brew", "install", "delve"},
	}
}

// DebugLaunchArgs implements lang.DebugAdapterProvider: delve builds the
// target itself — mode "debug" for a program, mode "test" for a test-scope
// configuration (#1150), where the program is the file's package directory
// and the selection travels as `-test.run`/`-test.bench` binary flags.
// integratedTerminal launches the debuggee via runInTerminal into the
// debuggee terminal pane (#625/#1370), so interactive and TUI programs get a
// real tty.
func (toolchain) DebugLaunchArgs(_ string, spec lang.RunSpec, cwd string, env map[string]string) map[string]any {
	args := map[string]any{
		"request": "launch",
		"console": "integratedTerminal",
		"cwd":     cwd,
	}
	if spec.Tests {
		args["mode"] = "test"
		args["program"] = filepath.Dir(spec.File)
		var flags []string
		if spec.TestName != "" {
			// TestKind carries the declaration's kind group (Test/Benchmark/
			// Fuzz); names are Go identifiers, so anchoring needs no quoting.
			switch spec.TestKind {
			case "Benchmark":
				flags = []string{"-test.bench", "^" + spec.TestName + "$", "-test.run", "^$"}
			default:
				flags = []string{"-test.run", "^" + spec.TestName + "$"}
			}
		}
		flags = append(flags, spec.Args...)
		if len(flags) > 0 {
			args["args"] = flags
		}
	} else {
		args["mode"] = "debug"
		args["program"] = spec.File
		if len(spec.Args) > 0 {
			args["args"] = spec.Args
		}
	}
	if len(env) > 0 {
		args["env"] = env
	}
	return args
}
