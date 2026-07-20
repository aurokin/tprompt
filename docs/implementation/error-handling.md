# Error Handling

`tprompt` should prefer explicit operational errors over silent fallback behavior.

## Errors that must be clear

### Prompt store errors

- prompt ID not found
- duplicate prompt IDs found
- unreadable prompt file
- invalid frontmatter if parsing is strict enough to reject it
- **duplicate frontmatter `key:` across prompts** (case-insensitive)
- **frontmatter `key:` collides with a reserved key**
- **malformed frontmatter `key:`** (multi-char, empty, non-printable, symbolic like `ctrl+x`)
- **malformed frontmatter `variables` declaration** (missing/invalid/duplicate
  name, unused declaration, or a name colliding with a built-in flag)
- **malformed template placeholder** (unknown variable, invalid name, empty
  placeholder, or unclosed `{{`)

### Template value errors

- **required template value missing on `send`** — usage error (exit 2), naming
  the missing flag (for example `--issue is required`)
- **required template value empty in the TUI** — inline recoverable footer error
  naming the field (for example `Issue is required`)
- **rendered templated prompt exceeds `max_paste_bytes`** — rejected before
  handoff/delivery; the TUI keeps the input view open, while `send` exits with
  the normal oversize mapping

### Environment errors

- not inside tmux when a tmux target is required
- invalid target pane supplied
- configured picker command missing
- handoff worker unavailable
- **no clipboard reader available** (no auto-detected candidate, no override)
- **clipboard reader command fails** (non-zero exit; stderr surfaced)

### Clipboard content errors

- **clipboard is empty**
- **clipboard content is not valid UTF-8 text**
- **clipboard content exceeds `max_paste_bytes`** (include byte count and cap, e.g. `(N > max_paste_bytes)`)

### Sanitization errors

- **content rejected by sanitizer** in `strict` mode (include class + byte offset; never include raw content)

### Delivery errors

- target pane no longer exists
- verification timed out
- tmux command failed
- delivery mode invalid

### tmux error taxonomy (Phase 3)

Concrete error types the tmux adapter surfaces. `app.ExitCode` maps these to
the CLI exit codes documented in `docs/commands/cli.md`.

| Error type | Meaning | Exit code |
|---|---|---|
| `tmux.EnvError` | not inside tmux and no `--target-pane` supplied | 4 |
| `tmux.PaneMissingError` | resolved/supplied pane does not exist | 4 |
| `tmux.DeliveryError` | `load-buffer` / `paste-buffer` / `send-keys` returned non-zero, or body exceeds `max_paste_bytes` | 6 |

### Sanitizer error taxonomy (Phase 3.5)

| Error type | Meaning | Exit code |
|---|---|---|
| `sanitize.StrictRejectError` | content contained an escape sequence in `strict` mode (fields: `Class`, 0-based `Offset`) | 3 |

Treated as a content-validation error, parallel to clipboard validation
failures — the payload was rejected before any tmux command was issued, so
delivery-layer exit 6 is the wrong bucket.

## Behavioral guidance

- do not silently pick one duplicate ID
- do not silently remap a colliding keybind
- do not silently fall back to a random pane
- do not silently sleep and hope TUI/focus state fixed itself
- do not hide handoff failures behind generic "send failed" messages
- do not log raw clipboard or prompt content on sanitizer rejection

## Example good errors

```text
Unable to deliver prompt 'code-review': target pane %12 no longer exists
Duplicate keybind 'c' declared by:
- /prompts/agents/code-review.md
- /prompts/review/commit.md
Invalid frontmatter key 'ctrl+x' in /prompts/review/commit.md: must be a single printable character
No clipboard reader available; install pbpaste, wl-paste, xclip, or xsel, or set `clipboard_read_command`
Clipboard content exceeds max_paste_bytes (4823104 > 2097152)
Content rejected by sanitizer (mode=strict): escape sequence detected at byte 142 (OSC)
Missing required template value --issue
Invalid template placeholder "focus" at byte 42: not declared in frontmatter variables
```

## Example bad error

```text
Something went wrong
```

## Surfacing strategy

- **CLI commands** print errors to stderr and exit non-zero (see exit-code table in `docs/commands/cli.md`).
- **Handoff worker** writes metadata-only failures to the configured delivery log and runs `tmux display-message` on the originating client for user-visible failures.
- **TUI** shows inline errors in its footer for interactive recovery (e.g., empty clipboard), and exits non-zero for unrecoverable errors.
