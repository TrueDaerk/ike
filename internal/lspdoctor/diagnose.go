package lspdoctor

import "strings"

// diagnose.go maps captured evidence to one diagnosis class with a concrete
// fix (#2164). The feasibility result: from the evidence the check chain
// captures — resolution outcome per directory family, exec bit, shebang +
// interpreter version, --version output, spawn error text and the decisive
// stderr line — these classes are reliably distinguishable: binary missing,
// PATH-of-GUI-process mismatch, not executable, wrong architecture, runtime
// (node) mismatch, crash-on-initialize, bad workspace root. Not reliably
// distinguishable from a single run: config/settings the server rejects
// silently, and slow-but-healthy servers vs hangs (the probe times out and
// reports the timeout as crash evidence instead of guessing).

// archSignatures mark a binary built for the wrong CPU (the Rosetta trap,
// #1614).
var archSignatures = []string{
	"exec format error",
	"bad CPU type",
	"cannot execute binary file",
}

// runtimeSignatures mark a node runtime too old or incompatible for the
// server script.
var runtimeSignatures = []string{
	"Unsupported engine",
	"requires Node",
	"node: bad option",
	"Unexpected token",
	"ERR_REQUIRE_ESM",
	"NODE_MODULE_VERSION",
	"ERR_OSSL",
	"GLIBC_",
}

// knownLaunchFailures maps recognizable crash complaints to advice better
// than "reinstall" — the launch-advice table (#1065) grown for the doctor.
var knownLaunchFailures = []struct{ command, needle, advice string }{
	// Homebrew builds taplo without the lsp feature; the npm/cargo builds
	// carry it (the TOML case that motivated #2164).
	{"taplo", "not part of this build", "this taplo was built without the LSP — install an LSP-capable build: npm install -g @taplo/cli (or cargo install taplo-cli --features lsp)"},
}

// classify turns one server's evidence into its Result.
func classify(ev evidence) Result {
	res := Result{Lang: ev.srv.Lang, Command: ev.srv.Command, Path: ev.path}
	install := installFix(ev.srv)

	// Resolution check row.
	switch {
	case ev.onPath:
		res.Checks = append(res.Checks, Check{Name: "binary", Status: StatusOK, Detail: "found on PATH at " + ev.path})
	case ev.fallbackDir != "":
		res.Checks = append(res.Checks, Check{Name: "binary", Status: StatusWarn, Detail: "not on PATH, but IKE resolves it from " + ev.fallbackDir})
	case ev.strandedDir != "":
		res.Checks = append(res.Checks, Check{Name: "binary", Status: StatusFail, Detail: "exists at " + ev.path + ", but that directory is not on IKE's PATH"})
	default:
		res.Checks = append(res.Checks, Check{Name: "binary", Status: StatusFail, Detail: "not found — searched PATH, toolchain install dirs and common install dirs"})
	}

	if ev.path == "" && !ev.notExecutable {
		res.Class = ClassMissing
		res.Diagnosis = ev.srv.Command + " is not installed (searched PATH and the usual install directories)"
		res.Fix = install
		return finish(res, ev)
	}

	if ev.notExecutable {
		res.Checks = append(res.Checks, Check{Name: "executable", Status: StatusFail, Detail: ev.path + " exists but lacks the execute bit"})
		res.Class = ClassNotExecutable
		res.Diagnosis = ev.path + " is not executable"
		res.Fix = "chmod +x " + ev.path
		return finish(res, ev)
	}

	if ev.strandedDir != "" {
		res.Class = ClassPathMismatch
		res.Diagnosis = ev.srv.Command + " is installed at " + ev.path + ", but " + ev.strandedDir +
			" is not on the PATH IKE inherited — typical when IKE was launched from a GUI app whose PATH skips your shell profile (#1614)"
		res.Fix = "add " + ev.strandedDir + " to PATH in your shell profile and relaunch IKE from a terminal (or the ike-gui launcher), or set [lsp.servers." + ev.srv.Lang + "] command = \"" + ev.path + "\""
		return finish(res, ev)
	}

	// Runtime check row (node scripts).
	if ev.runtime == "node" {
		if ev.runtimeVersion == "" {
			res.Checks = append(res.Checks, Check{Name: "runtime", Status: StatusFail, Detail: "script needs node (" + ev.shebang + "), but node is not on IKE's PATH"})
			res.Class = ClassRuntimeMismatch
			res.Diagnosis = "the server is a node script but node itself is not available to IKE"
			res.Fix = "install node (e.g. brew install node) or fix PATH so IKE sees it, then re-run the doctor"
			return finish(res, ev)
		}
		res.Checks = append(res.Checks, Check{Name: "runtime", Status: StatusOK, Detail: "node script, node " + ev.runtimeVersion + " available"})
	}

	// Version check row (evidence only — many servers ship no --version).
	switch {
	case ev.versionErr == "" && ev.versionOut != "":
		res.Checks = append(res.Checks, Check{Name: "version", Status: StatusOK, Detail: firstLine(ev.versionOut)})
	case ev.versionErr != "":
		detail := "--version failed: " + ev.versionErr
		if ev.versionOut != "" {
			detail += " (" + firstLine(ev.versionOut) + ")"
		}
		res.Checks = append(res.Checks, Check{Name: "version", Status: StatusWarn, Detail: detail})
	}

	if ev.rootErr != "" {
		res.Checks = append(res.Checks, Check{Name: "workspace root", Status: StatusFail, Detail: ev.rootErr})
		res.Class = ClassBadRoot
		res.Diagnosis = "the workspace root the server would initialize against is unusable: " + ev.rootErr
		res.Fix = "open IKE inside the project directory the server should analyze"
		return finish(res, ev)
	}

	// Spawn + initialize — the decisive check.
	if ev.spawnRan {
		if ev.spawn.Err == "" {
			detail := "spawned and answered initialize"
			if ev.spawn.ServerName != "" {
				detail += " (" + ev.spawn.ServerName + ")"
			}
			res.Checks = append(res.Checks, Check{Name: "initialize", Status: StatusOK, Detail: detail})
			res.Class = ClassOK
			return finish(res, ev)
		}
		combined := ev.spawn.Err + "\n" + ev.spawn.Stderr + "\n" + ev.versionOut + "\n" + ev.versionErr
		detail := ev.spawn.Err
		if ev.spawn.Stderr != "" {
			detail += " — stderr: " + ev.spawn.Stderr
		}
		res.Checks = append(res.Checks, Check{Name: "initialize", Status: StatusFail, Detail: detail})

		if sig := match(combined, archSignatures); sig != "" {
			res.Class = ClassArchMismatch
			res.Diagnosis = ev.path + " is built for the wrong CPU architecture (" + sig + ") — often an x86_64 install under Rosetta on an arm64 Mac (#1614)"
			res.Fix = reinstallFix(install, "for this machine's architecture")
			return finish(res, ev)
		}
		if sig := match(combined, runtimeSignatures); sig != "" {
			res.Class = ClassRuntimeMismatch
			res.Diagnosis = "the server's runtime rejects it (" + sig + ")"
			if ev.runtimeVersion != "" {
				res.Diagnosis += " — node " + ev.runtimeVersion + " is what IKE sees"
			}
			res.Fix = reinstallFix("upgrade node (e.g. brew upgrade node)", "then reinstall the server: "+install)
			return finish(res, ev)
		}
		res.Class = ClassCrashInit
		res.Diagnosis = "the server spawns but fails the initialize handshake"
		if ev.spawn.Stderr != "" {
			res.Diagnosis += " — stderr says: " + ev.spawn.Stderr
		}
		res.Fix = crashFix(ev, install)
		return finish(res, ev)
	}

	res.Class = ClassOK
	return finish(res, ev)
}

// crashFix picks the best advice for a crash-on-initialize: a recognized
// complaint's advice first, then the shadowed-copy hint (a broken PATH binary
// hiding a working install — the "npm install did not help" trap), then the
// plain reinstall.
func crashFix(ev evidence, install string) string {
	for _, k := range knownLaunchFailures {
		if k.command == ev.srv.Command && (strings.Contains(ev.spawn.Stderr, k.needle) || strings.Contains(ev.spawn.Err, k.needle)) {
			if len(ev.otherCopies) > 0 {
				return k.advice + " — note: another copy already exists at " + ev.otherCopies[0] +
					", but the failing " + ev.path + " shadows it on PATH; put its directory first on PATH or set [lsp.servers." + ev.srv.Lang + "] command = \"" + ev.otherCopies[0] + "\""
			}
			return k.advice
		}
	}
	if len(ev.otherCopies) > 0 {
		return "another copy exists at " + ev.otherCopies[0] + " — the failing " + ev.path +
			" shadows it on PATH; try [lsp.servers." + ev.srv.Lang + "] command = \"" + ev.otherCopies[0] + "\" or reorder PATH"
	}
	fix := "check the server log (\"LSP: Show Server Log\")"
	if install != "" {
		fix += "; reinstalling may help: " + install
	}
	return fix
}

// installFix words the spec's install recipe as a runnable command.
func installFix(srv Server) string {
	if len(srv.Install) == 0 {
		return "install " + srv.Command + " manually (the " + srv.Lang + " plugin ships no install recipe)"
	}
	return strings.Join(srv.Install, " ")
}

// reinstallFix joins a primary action with a qualifier.
func reinstallFix(action, qualifier string) string {
	if qualifier == "" {
		return action
	}
	return action + " " + qualifier
}

// match returns the first signature contained in text, or "".
func match(text string, signatures []string) string {
	for _, s := range signatures {
		if strings.Contains(text, s) {
			return s
		}
	}
	return ""
}

// firstLine trims output to its first non-empty line.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}

// finish attaches shared evidence rows and returns the result.
func finish(res Result, ev evidence) Result {
	if len(ev.otherCopies) > 0 && res.Class != ClassOK {
		res.Checks = append(res.Checks, Check{Name: "other copies", Status: StatusWarn, Detail: strings.Join(ev.otherCopies, ", ")})
	}
	return res
}
