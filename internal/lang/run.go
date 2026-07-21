package lang

// Run-command seam (0350, #575): a language plugin contributes how to run a
// file of its language. Run configurations store data (file, module form,
// args), and the command line is synthesized at launch through this seam, so
// interpreter changes (venv switch, explicit [lang.<id>] interpreter) apply
// to every later run automatically.

// RunSpec is the launch-relevant slice of a run configuration.
type RunSpec struct {
	// File is the absolute path of the file to run.
	File string
	// Module is the language's module spelling (Python's package.module for
	// `-m`); empty runs the file directly.
	Module string
	// Args are the program's own arguments, appended after the target.
	Args []string
	// Listen marks a listen-style debug session (#823): no process is
	// launched — the adapter waits for incoming connections instead (PHP's
	// "listen for Xdebug connections from php-fpm"). File/Module/Args are
	// empty then.
	Listen bool
}

// RunCommandProvider is an optional Toolchain extension: it turns a RunSpec
// into the argv to execute from root. interpreter is the resolved toolchain
// binary (lang.Interpreter; "" when nothing resolved — the provider picks its
// own fallback or reports ok=false).
type RunCommandProvider interface {
	RunCommand(root string, spec RunSpec, interpreter string) (argv []string, ok bool)
}

// ModuleResolver is an optional Toolchain extension reporting a file's module
// spelling (Python: dotted path when the file sits in a package). Default run
// configurations use it to prefer the `-m` form.
type ModuleResolver interface {
	Module(root, file string) (module string, ok bool)
}

// RunArgv synthesizes the command line to run spec for langID at root.
// explicit is the user's configured interpreter ([lang.<id>] interpreter),
// which wins over detection exactly like everywhere else. ok=false means the
// language contributes no run command.
func RunArgv(langID, root string, spec RunSpec, explicit string) (argv []string, ok bool) {
	l, found := ByID(langID)
	if !found || l.Toolchain == nil {
		return nil, false
	}
	p, providerOK := l.Toolchain.(RunCommandProvider)
	if !providerOK {
		return nil, false
	}
	interpreter, _ := Interpreter(langID, root, explicit)
	return p.RunCommand(root, spec, interpreter)
}

// ModuleFor reports the module spelling for file in langID's terms, "" when
// the language has no module concept or the file is not part of one.
func ModuleFor(langID, root, file string) string {
	l, found := ByID(langID)
	if !found || l.Toolchain == nil {
		return ""
	}
	if r, ok := l.Toolchain.(ModuleResolver); ok {
		if m, found := r.Module(root, file); found {
			return m
		}
	}
	return ""
}
