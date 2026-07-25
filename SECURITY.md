# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/TrueDaerk/ike/security/advisories/new)
form. That opens a draft advisory only the maintainer can see.

Include what you would want in a bug report — what you did, what happened,
what you expected — plus the impact as you see it and, if you have one, a
minimal reproduction.

## What to expect

IKE is a personal project maintained by one person in their spare time (see
[CONTRIBUTING.md](CONTRIBUTING.md)), so there is no guaranteed response time.
Reports are read and taken seriously; fixes land when they can. Only the
latest commit on `main` is supported — there are no maintained release
branches to backport to.

## Scope

Worth reporting: anything that lets a *file you merely open* — or a project
you merely browse — run code, exfiltrate data, or escape a sandbox. Concretely
that includes the WASM plugin sandbox and its host ABI, the language-server
and debug-adapter processes IKE spawns, the integrated terminal, editorconfig
and settings parsing, and any path handling that could escape the project
root.

Out of scope: IKE runs the shell, language servers, debuggers and tool panes
you configure it to run, with your privileges. A setting that runs a command
you asked it to run is the feature, not a vulnerability.
