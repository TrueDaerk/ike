# Command line

`ike` opens the current directory as the project. Anything you pass beyond
that is a file to open.

## Opening files

```sh
ike                               # just the project
ike main.go                       # one tab
ike main.go README.md             # two tabs, the first one focused
```

Every path becomes an editor tab. A path that does not exist yet opens as a
new unsaved buffer, so `ike newfile.go` is how you start one.

## Jumping to a position

Append `:line` or `:line:column`:

```sh
ike internal/app/app.go:725       # open at line 725
ike main.go:10:4                  # line 10, column 4
```

The vim-style prefix works too, which matters when you are pasting from
something that emits it:

```sh
ike +42 main.go
```

Combining them is fine — each target is resolved independently:

```sh
ike main.go:10:4 README.md        # two tabs, the first focused at 10:4
```

This is the form most tools emit, so `grep -n`, compiler errors and stack
traces paste straight in.

## Reading from a pipe

`-` reads standard input to EOF and drops it into a scratch buffer:

```sh
git log | ike -
go test ./... 2>&1 | ike -
```

The buffer is not attached to a file, so nothing is written unless you save it
somewhere. Because stdin is the pipe, IKE takes its keyboard from
`/dev/tty` — which means `-` only works when you actually have a terminal;
`echo x | ike - < /dev/null` has nowhere to read keys from and errors out.

## Which project gets opened

The working directory is the project root. Passing a file does **not** change
that — `ike ~/other/thing.go` from inside `~/src/my-project` opens the file as
a tab in the *current* project.

Passing any target also suppresses the restore-last-project behaviour: an
explicit file means you meant this directory, not your most recent one.

## Environment variables

| Variable | Effect |
|---|---|
| `IKE_CONFIG_DIR` | Directory holding your user `settings.toml`, replacing `~/.ike` |
| `IKE_PPROF` | Address to serve `net/http/pprof` on, for profiling |
| `IKE_PPROF_DIR` | Where `SIGUSR1` dumps goroutine and heap profiles (default: the temp directory) |
