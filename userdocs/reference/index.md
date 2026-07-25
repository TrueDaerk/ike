# Reference

Complete, exhaustive tables — the pages you search rather than read.

These are **generated from the source** by `cmd/docgen` and re-generated in CI,
so they describe the current code rather than what the documentation happened
to say when it was written.

- **[Keybindings](keybindings.md)** — every default binding, grouped by
  context, with the chord on macOS and on Linux/Windows.
- **[Settings](settings.md)** — every configuration key, its type, default,
  allowed values, and which file a change is written to.
- **[Commands](commands.md)** — everything the command palette can run,
  including the vim keys and ex-commands that do the same job in the editor.

In the running IDE the same information is a keypress away: ++f1++ opens the
help overlay with the live bindings for your build, and the palette lists every
registered command.

!!! tip "Found a mistake?"
    A wrong entry here means the code and the generator disagree — worth
    [an issue](https://github.com/TrueDaerk/ike/issues/new/choose). Do not edit
    the generated pages directly; the next CI run overwrites them.
