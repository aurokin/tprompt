# Testing Harness

`tprompt` is a tmux-first tool, but most behavior should be provable without a
live tmux session. The harness is built around small seams: store, config,
keybind resolution, sanitizer, clipboard reader, tmux runner, daemon client,
daemon queue, submitter, and TUI model.

Good tests assert public behavior and failure contracts. Avoid tests that only
mirror private helper structure.

## Health Gates

Fast local gate:

```bash
go test ./...
```

Full project gate when tools are installed:

```bash
make check
```

`make check` runs formatting, linting, and the race-enabled test target from
the `Makefile`.

## Proof Surface By Subsystem

### Prompt Store

Proof surface: unit tests over temporary prompt directories and fixture files.

Assert:

- recursive discovery of markdown files
- filename-stem ID derivation
- duplicate stem detection with useful paths
- body extraction with and without frontmatter
- unsupported file extensions ignored
- supported frontmatter fields parsed
- invalid prompt delivery defaults rejected
- metadata escape stripping preserves body bytes
- resolved prompt and list data are returned as cloned values

### Config

Proof surface: unit tests over config structs and temporary config files.

Assert:

- defaults match documented behavior
- config files decode expected TOML shape
- unknown or invalid fields fail clearly
- `reserved_keys` accepts printable and symbolic values
- disabled reserved keys are respected
- `keybind_pool` is deduplicated and filtered against reserved keys
- clipboard and picker command strings parse into argv
- delivery precedence is flags, frontmatter, config, defaults

### Keybind Resolver

Proof surface: pure unit tests.

Assert:

- frontmatter keys take precedence over auto-assignment
- auto-assignment scans prompts alphabetically by ID
- the configured pool is consumed in order
- prompts beyond the pool become overflow
- duplicate keys are case-insensitive errors
- reserved-key collisions are errors
- malformed keys are errors

### Clipboard Reader

Proof surface: unit tests with injected environment and look-path seams, plus
command-reader tests with controlled commands.

Assert:

- macOS selects `pbpaste`
- Wayland selects `wl-paste` when available
- X11 prefers `xclip` and falls back to `xsel`
- missing reader reports install guidance
- override commands are used verbatim
- non-zero command exits surface stderr
- empty, non-UTF-8, and oversized content is rejected before delivery

### Sanitizer

Proof surface: unit tests over byte fixtures.

Fixture corpus should include:

- OSC title and clipboard-write sequences
- DCS sequences
- CSI private mode toggles
- application keypad mode
- cosmetic SGR colors
- cursor movement and erase sequences
- malformed escape-adjacent input
- multi-byte UTF-8 adjacent to escape sequences
- content without escape sequences

Assert every relevant class across `off`, `safe`, and `strict`:

- `off` is identity
- `safe` strips dangerous classes and preserves cosmetic classes
- `strict` rejects any escape sequence with class and byte offset

### Tmux Adapter

Proof surface: fake `Runner` tests. Live tmux is not required for command
construction.

Assert:

- paste mode constructs `load-buffer` and `paste-buffer -d -p`
- paste mode with `--enter` sends Enter after `paste-buffer`
- type mode uses `send-keys -l -- <chunk>`
- type chunks split on rune boundaries
- pane-exists and selected-pane probes map tmux output correctly
- `display-message` uses client scope when available and target fallback otherwise
- tmux runner failures map into the correct error taxonomy
- size-cap rejection happens before tmux commands are invoked

### Daemon

Proof surface: unit tests for queue, executor, verifier, logger, and validation;
Unix-socket integration tests for server/client round trips.

Assert:

- job validation rejects invalid shape and preserves valid fields
- `source = clipboard` carries captured bytes and no prompt ID
- same-pane replacement cancels or drops the older pending job as specified
- different-pane jobs can proceed independently
- verification waits on tmux selection state and respects timeout/cancellation
- executor checks size, sanitizes, and delivers in the documented order
- failures are logged without prompt bodies or clipboard bytes
- socket permissions and stale-socket behavior are correct
- status responses expose pid, socket, log, uptime, pending jobs, and version

### TUI

Proof surface: pure model/update/view tests. Avoid brittle terminal snapshot
tests unless a specific rendering regression demands them.

Assert:

- board rows render key, id, and display description
- description falls back to title, then blank
- descriptions truncate to terminal width without wrapping
- clipboard row appears first when enabled
- reserved keys render in footer hints
- `/` enters search
- search covers board and overflow prompts, but not body content
- search ranks ID above title, title above description, and description above tags
- cursor and scroll offsets stay in bounds
- `Esc` and configured cancel keys exit with cancel result
- prompt keypresses submit the correct prompt
- clipboard keypress reads and validates clipboard content asynchronously
- inline errors persist or clear according to user-visible action rules

### CLI And App Layer

Proof surface: app-level tests with fake deps plus `testscript` for black-box
command behavior.

Assert:

- command registration and help surface are stable
- config load failures map to usage/config errors
- prompt discovery failures map to prompt errors
- `list`, `show`, `send`, `paste`, `pick`, `tui`, `doctor`, and daemon commands
  expose the documented stdout/stderr behavior
- cancellation exits with status 0
- direct sends do not require daemon state
- TUI preflight checks happen in the documented order

## Optional Live Tmux Checks

Live tmux tests are valuable but should stay opt-in unless the project promotes
them into the main gate. They are best suited for final confidence checks:

- create a disposable tmux pane
- submit a deferred job
- verify prompt text arrives after returning focus to the target pane
- verify bracketed paste preserves multiline content
- verify `--enter` sends exactly one Enter after paste
- intentionally close the target pane and confirm the failure path

## Manual Smoke Checklist

- Run `tprompt doctor`.
- Start the daemon.
- Open a tmux pane with a shell prompt.
- Launch the TUI through the documented popup binding.
- Select a prompt and confirm it lands after the TUI closes.
- Repeat with `tprompt paste`.
- Repeat with paste mode and type mode.
- Close the target pane before delivery and confirm failure is surfaced.
- Confirm success remains silent by default.
- Confirm TUI cancellation exits 0.
