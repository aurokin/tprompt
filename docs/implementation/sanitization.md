# Sanitization

Optional content-sanitization layer that runs between source (prompt body or clipboard) and tmux adapter. Controls whether terminal escape sequences inside the payload are forwarded, stripped, or cause the delivery to be rejected.

## Modes

Three modes, locked at `config.toml` or overridden per-invocation with `--sanitize`:

| Mode | Behavior |
|---|---|
| `off` (default) | No modification. Content is forwarded byte-for-byte. |
| `safe` | Strip a **denylist** of known-dangerous sequences; leave cosmetic sequences (SGR colors, cursor move) alone. |
| `strict` | Reject the delivery if **any** escape sequence is present. |

Both `safe` and `strict` require fixture-backed tests (see `docs/testing/harness.md`). `off` requires no logic beyond pass-through.

## Scope

The same mode applies to both `tprompt paste` and `tprompt send <id>`. There is **no** per-source override — one policy, one code path.

## Sequence classes

Classes the sanitizer recognizes. `safe` strips the dangerous set; `strict` rejects on any.

### Dangerous — stripped by `safe`, rejects `strict`

| Class | Example | Reason |
|---|---|---|
| OSC (Operating System Command) | `ESC]0;title BEL`, `ESC]52;c;<base64> BEL` | Can set title, copy to clipboard (OSC-52 write can clobber user clipboard), or trigger terminal-specific behavior |
| DCS (Device Control String) | `ESC P … ESC \` | Device-specific control, historically exploitable |
| CSI mode toggles | `ESC[?1000h` (enable mouse), `ESC[?1049h` (alt screen), `ESC[?2004h` (bracketed paste) | Alters terminal state persistently |
| Application keypad mode | `ESC=`, `ESC>` | Changes how keypad input is reported |
| Private mode sequences | `ESC[?<n>h`, `ESC[?<n>l` | Catch-all for DEC private modes |

### Cosmetic — left alone by `safe`, rejects `strict`

| Class | Example | Reason it's cosmetic |
|---|---|---|
| SGR (Select Graphic Rendition) | `ESC[31m` (red), `ESC[0m` (reset) | Color / bold / underline — harmless |
| Cursor movement | `ESC[2A`, `ESC[10;5H` | Moves cursor; no persistent state |
| Erase in line/display | `ESC[K`, `ESC[2J` | Visual only |

`strict` rejects even these because the conservative stance is "prompts should be pure text; anything else is suspicious."

### Always preserved (not sequences)

- printable ASCII and UTF-8 text
- newline (`\n`), tab (`\t`)
- carriage return (`\r`) — preserved, but note that naive CRLF in clipboard content may land as an extra blank line in some terminals

## Interface

```text
Sanitizer
- Mode() -> "off" | "safe" | "strict"
- Process(content []byte) -> (cleaned []byte, err error)
```

- `off` — returns `(content, nil)` unchanged
- `safe` — returns `(cleaned, nil)` where `cleaned` has dangerous sequences removed; never errors on content
- `strict` — returns `(nil, err)` when any escape sequence is detected; otherwise returns `(content, nil)`

### Malformed-sequence handling in `safe`

An OSC or DCS that lacks its terminator (BEL or ESC-backslash) is treated as
running to end-of-input and stripped in full. Rationale: a truncated control
string is unsafe by definition — the terminal would continue consuming bytes
looking for the terminator, so refusing to forward the tail is the
conservative choice. A CSI with a non-final trailing byte is likewise
treated as dangerous and stripped.

## Error shape (strict mode)

Concrete type: `sanitize.StrictRejectError{Class, Offset}`.

```text
content rejected by sanitizer (mode=strict): escape sequence detected at byte 142 (OSC)
```

The error includes:

- offending class (OSC / DCS / CSI / etc.)
- byte offset where the first offense was found — **0-based**, matching Go
  slice indexing; no translation between internal representation and the
  user-facing message

Exit code: **3** (`ExitPrompt`), treated as a content-validation error
parallel to clipboard validation failures. See `docs/commands/cli.md` and
`docs/implementation/error-handling.md`.

Callers surface this via the daemon's normal error-feedback channels (`tmux display-message` + log) or directly as CLI stderr.

## Failure snippet logging

When a delivery is rejected by `strict`, the daemon log records the error message and the offending **class/offset**, but **not** the raw content. This avoids accidentally persisting sensitive clipboard bytes to the log.

## Configuration

```toml
# ~/.config/tprompt/config.toml
sanitize = "off"         # default
# sanitize = "safe"      # recommended for heavy clipboard users
# sanitize = "strict"    # audited environments
```

Per-invocation flag:

```bash
tprompt send code-review --sanitize safe
tprompt paste --sanitize strict
```

Precedence: flag > config > built-in default (`off`).

## Ordering with `max_paste_bytes`

The `max_paste_bytes` cap is enforced **pre-sanitize**. Callers check the raw
body/clipboard length against the cap first, then hand the bytes to
`Sanitizer.Process`. Rationale:

- `safe` only shrinks, so a body that passes the pre-check also passes
  post-sanitize.
- `strict` rejects before delivery anyway, so post-check would only add a
  pointless second measurement.
- Checking pre-sanitize keeps a single, predictable size contract across
  `tprompt send`, `tprompt paste`, and the TUI-flow daemon job.

## Interaction with bracketed paste

Bracketed paste (default delivery mode) provides **some** protection — bracketed-paste-aware apps treat the wrapped content as literal input rather than interpreting in-band escape sequences. But:

- not all target apps respect the wrapper
- the terminal itself still sees the bytes, and some terminal-level sequences (title change, clipboard write) fire regardless of app behavior

Sanitization is therefore layered on top of bracketed paste, not a substitute for it.

## Non-goals

- per-prompt sanitize overrides via frontmatter — deferred
- content transformation beyond the dangerous-denylist (no URL allowlisting, no HTML escaping, etc.)
- auditing historical clipboard content

## Test expectations

See `docs/testing/harness.md`. At minimum, the sanitizer must be tested against a fixture corpus of:

- each dangerous class (one positive case each)
- each cosmetic class (confirmed preserved in `safe`, rejected in `strict`)
- multi-byte UTF-8 adjacent to escape sequences (boundary correctness)
- content with no escape sequences (identity in all modes)
