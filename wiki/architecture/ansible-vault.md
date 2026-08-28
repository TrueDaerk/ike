---
type: concept
title: Ansible Vault Editing
description: Transparent editing of $ANSIBLE_VAULT; files — decrypted into the buffer on open when a password source is configured, re-encrypted on save so the plaintext never lands on disk, with a "Treat as Vault File" intention for the files the automatic path did not cover.
resource: internal/ansiblevault
tags: [architecture, editor, vault, ansible, security, intentions, settings]
timestamp: 2026-08-28T00:00:00Z
---

# Ansible Vault Editing

Issue #2293. Env files and other secrets increasingly live as **Ansible
Vault** files, and the manual decrypt → edit → encrypt cycle just to check one
value is friction the editor can absorb: like the conceal families mask what a
buffer *shows*, vault editing unmasks what a file *is*. A `$ANSIBLE_VAULT;`
file opens as its decrypted plaintext in an ordinary editable buffer, and
saving re-encrypts on the way out — **the plaintext never touches the disk**:
not the file, not persistent undo, not crash-recovery backups.

## Structure

```
internal/ansiblevault/        the leaf package: Vault 1.1/1.2 format + password resolution
internal/editor/vault.go      per-document vault state, decrypt notes, TreatAsVault
internal/editor/editor.go     Load — the decrypt hook (raw bytes kept for diskHash)
internal/editor/actions.go    saveAs — the encrypt hook before os.WriteFile
internal/editor/reload.go     reloadFrom — external changes decrypt/adopt/drop the state
internal/app/vault.go         the vault.treatAsFile command handler
internal/intention/catalog.go vaultProvider — the "Treat as Vault File" entry
```

## The format, natively

`internal/ansiblevault` implements the Vault **1.1/1.2 envelope**
(AES-256-CTR, PBKDF2-HMAC-SHA256 at 10 000 iterations, HMAC-SHA256 integrity,
PKCS#7 padding, hex-armored body wrapped at 80 columns) on the Go 1.26
standard library alone — no dependency, and no `ansible-vault` CLI needed on
the machine. `Encrypt` writes a 1.1 header, or a 1.2 header when a vault-id
label is passed; `Label` extracts a 1.2 file's id so the round-trip preserves
it. The package's fixtures were generated with the real ansible-core CLI, and
interop is verified in both directions (its output decrypts here; our output
decrypts there). The HMAC check runs before any decryption, so a wrong
password is `ErrWrongPassword`, never garbage plaintext.

## Password sources

Resolution (`ansiblevault.ResolvePassword`) follows Ansible's own precedence —
environment first, then configuration:

1. `ANSIBLE_VAULT_PASSWORD` — the password itself,
2. `ANSIBLE_VAULT_PASSWORD_FILE` — Ansible's standard variable naming a file,
3. `ansible.vault_password_file` — IKE's setting: the **user-scope value is
   the global default, a project-scope value overrides it** (plain layer-merge
   semantics, [config](./config.md)), both editable on the Settings UI's
   **Ansible Vault** page as a `Path` entry (existence-checked in the form;
   the lenient config validator reports a missing or directory path as a
   diagnostic but keeps the value — the file may appear later).

A password file is read as text — its first line, without the line ending;
executable password scripts are not run. Without any source, vault files open
as ciphertext exactly as before, with the reason on the ex line rather than
silence; a wrong password likewise keeps the ciphertext and the file untouched
and names the failing source.

## The automatic round-trip

**Open** — `Load` recognizes the `$ANSIBLE_VAULT;` prefix on the raw bytes and
decrypts before the text decode. The password is captured on the document
(`vaultPass`), so a config change mid-session cannot strand an open buffer;
the 1.2 label rides along. `diskHash` hashes the **on-disk ciphertext**, not
the buffer — staleness detection and reconcile compare against what the file
actually holds.

**Save** — `saveAs` is the single write choke point every save flavor funnels
through (`:w`, save-as, Save All, focus/idle autosave, the format-on-save
chain), and the encrypt sits between `textenc.Encode` and `os.WriteFile`: what
reaches the disk is fresh ciphertext (a new random salt each save — vault
output is deliberately non-deterministic, like the CLI's).

**External changes** — `reloadFrom` decrypts an incoming rewrite with the
document's password, so an own-write that slipped past the watcher's
suppression still no-ops on the plaintext compare (#1406). A rewrite that no
longer decrypts (re-encrypted under a different password) keeps the buffer, an
externally *decrypted* file drops the vault state and follows the disk, and a
plaintext file encrypted outside IKE adopts the vault state like a fresh open
when a source is available.

## Where the plaintext must not go

The document flag gates every persistence path that would otherwise write
buffer text:

- **Persistent undo** (#148) — both directions: `PersistUndo` refuses vault
  documents and `restoreUndo` never adopts (`undopersist.go`), since the
  snapshots would carry plaintext fragments into the undo store as JSON.
- **Crash-recovery backups** (#167) — `snapshotDueBackups` and
  `backupFlushWorkspace` skip vault documents; the trade-off is explicit: a
  crash loses unsaved vault edits rather than leaking them.
- **Local History** and the TODO index re-read the *file* after save, so they
  see ciphertext by construction; session/layout stores paths only.

What deliberately still sees plaintext: the in-memory buffer, registers and
clipboard (user-driven), and the LSP didOpen/didChange stream — a language
server on the decrypted YAML is half the point of decrypting.

Vault state is a **document property** like line endings (#66): copied by
`ShareDocumentWith`, mirrored across views via `SyncMsg`, cleared by
`NewFile`/`ShowReadOnly`.

## Treat as Vault File (#2293)

The intention ([intention actions](./intention-actions.md)) covers the two
files the automatic path did not: `vaultProvider` offers **"Treat as Vault
File"** for a writable, file-backed, not-yet-vault buffer whenever a password
source is configured (`Context.VaultReady` — checked without reading the
file, the popup gate must stay cheap; `Context.VaultBuffer` suppresses it once
flipped). One command, `vault.treatAsFile`, two branches in
`editor.TreatAsVault`:

- **Buffer already holds ciphertext** (opened before the source existed): the
  buffer decrypts **in place** — through `RestoreContent`, so shared views see
  it — then history resets and the result is marked saved: the disk already
  holds exactly this content, encrypted. The file is not rewritten.
- **Buffer is plaintext**: the document flips to vault-backed and saves
  immediately, so the file on disk is **encrypted on the spot** and never
  stays clear once the user asked for a vault file.

Either way the same tab simply becomes the vault-backed buffer — nothing to
close, nothing new to focus — and every later save encrypts.

## Tests

`internal/ansiblevault` covers the format against real ansible-core fixtures
(1.1 and 1.2), wrong password, malformed envelopes, and round-trips;
`internal/editor/vault_test.go` covers the open/edit/save round-trip per
source, the no-source and wrong-password messages (file untouched), label
preservation, external-change reloads, both `TreatAsVault` branches, share
semantics, and that neither the saved file nor the undo store ever holds
plaintext; `internal/intention/vault_test.go` the offer gates;
`internal/config` the password-file diagnostics.
