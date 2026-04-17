# Clipboard Reader

Clipboard content is a first-class delivery source via `tprompt paste` and the TUI's pinned clipboard row. This file describes how `tprompt` acquires clipboard content at runtime.

## Scope rule

The clipboard is always read on the host running `tprompt`. Since tmux runs on the same host, this is also the host where delivery lands. Cross-host clipboard (laptop → remote) is **not** an MVP feature.

If the user is SSH'd into a server and runs `tprompt paste`, they read **that server's** clipboard, not their laptop's. This is documented as a known limitation, not something the tool attempts to work around.

## Detection strategy

Default behavior is auto-detect, with a user-supplied override.

### Auto-detect order

1. Explicit config override (`clipboard_read_command`) if set — use it verbatim, skip detection.
2. macOS (`runtime.GOOS == "darwin"`) → `pbpaste`
3. Linux Wayland (`WAYLAND_DISPLAY` set) **and** `wl-paste` on `$PATH` → `wl-paste -n` (`-n` suppresses the trailing newline that `wl-paste` appends by default, matching `pbpaste`/`xclip`/`xsel`)
4. Linux X11 (`DISPLAY` set) → `xclip -selection clipboard -o`, then `xsel -b -o` as a secondary candidate. This step also runs for XWayland users whose `WAYLAND_DISPLAY` is set but who don't have `wl-paste` installed — step 3 falls through rather than failing.
5. Fallback: no reader available → error

### Error when no reader is found

```text
No clipboard reader available on this host.

Install one of:
  - pbpaste (macOS, built in)
  - wl-paste (part of wl-clipboard, Wayland)
  - xclip or xsel (X11)

Or set `clipboard_read_command` in your tprompt config.
```

## Override

```toml
# ~/.config/tprompt/config.toml
clipboard_read_command = "pbpaste"
```

The command is executed as-is. It must write the clipboard content to stdout and exit 0. Non-zero exit is surfaced verbatim as an error (stderr included).

## Interface

`ClipboardReader` (see `docs/implementation/interfaces.md`) has a single method:

```text
Read() -> bytes, error
```

Implementations:

- `NewAutoDetect(getenv, lookPath)` — encapsulates the detection logic above; the seams are injected so tests can fake platform env and `$PATH` without touching the real host
- `NewCommand(argv []string)` — used for the config override; argv[0] is resolved via `$PATH` by `os/exec`
- `NewStatic(bytes)` — test fake

## Validation

The reader returns raw bytes. Validation runs in a shared helper
`clipboard.Validate(content []byte, cap int64) error`, not in the reader:

- empty content → `clipboard is empty`
- not valid UTF-8 → `clipboard content is not valid UTF-8 text`
- exceeds `max_paste_bytes` → `clipboard content exceeds max_paste_bytes (N > max_paste_bytes)`

The helper is called by `tprompt paste` (Phase 5.5) and by the TUI clipboard
row (Phase 5b). It lives in `internal/clipboard` alongside the reader so the
error strings stay in one place.

## `doctor` checks

`tprompt doctor` reports:

- which reader strategy is active (auto-detected or overridden)
- the resolved command (e.g., `pbpaste` or the user's custom line)
- whether the command is installed / on `$PATH` (via `exec.LookPath`)

Doctor does **not** execute the reader. Tools like `xclip -o` and `xsel -b -o`
exit non-zero when the selection is empty, so a dry-run would false-fail on a
perfectly-configured host.

A missing reader is reported as a **warning**, not a failure. A user who only
uses `tprompt send` does not need a clipboard reader installed, so doctor's
overall exit code is not affected by the clipboard check. A missing override
command (user set `clipboard_read_command` to something that is not on
`$PATH`) is also a warning at doctor time — the hard error fires when
`tprompt paste` actually tries to read.

Example output:

```text
ok   clipboard reader: wl-paste (auto-detected, Wayland)
warn clipboard reader: none available (install pbpaste, wl-paste, xclip, or xsel)
```

## Security notes

- The reader command runs with the user's permissions; no privilege elevation.
- `clipboard_read_command` is **not** shell-expanded by `tprompt`. It is split with standard argv parsing. This prevents config-injection surprises.
- Clipboard content is never written to disk by the reader. It may hit the daemon log only if sanitization or validation fails and the failure message includes a snippet — the log policy for failure snippets is controlled by the sanitizer (see `docs/implementation/sanitization.md`).

## Non-goals

- OSC-52 read (cross-host clipboard) — deferred
- automatic clipboard polling / preview — deferred (see `docs/commands/tui.md`)
- writing back to the clipboard — out of scope for MVP
