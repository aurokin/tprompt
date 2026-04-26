# Prompt Store

Prompts are markdown files on disk.

## Discovery rules

Current discovery behavior:

- recurse through the configured prompts directory
- accept `.md` files only
- ignore hidden implementation files unless intentionally supported later

## ID derivation

ID is derived from the filename stem.

Examples:

- `/prompts/code-review.md` → `code-review`
- `/prompts/review/bug-hunt.md` → `bug-hunt`

Directories do not contribute to the ID.

## Duplicate detection

Prompt discovery must detect duplicate stems.

This is a hard error, not a warning.

## Frontmatter

Optional YAML frontmatter may define metadata.

Supported keys:

- `title` — short human-readable name
- `description` — one-line explanation, shown in the TUI row (soft-truncated with ellipsis)
- `tags` — list of strings, searchable
- `mode` — delivery default (`paste` | `type`)
- `enter` — delivery default (bool)
- `key` — single printable character for the TUI keybind board

Unsupported keys are ignored.
Invalid `mode` values are a hard error at load time.

### Empty-value tolerance

Any supported field present with an empty value is treated as if the field
were absent:

- `key:` (no value), `key: null`, and `key: ""` all behave as if no `key:`
  line existed — the prompt is auto-assigned a board key.
- `mode:` and `mode: ""` behave as if no `mode:` line existed — the
  config-level delivery default applies. Non-empty values still validate
  against `{paste, type}` (anything else remains a hard error).
- `tags:` (no value) and `tags: []` both decode to an empty list.
- `enter:` (no value) decodes to nil — config-level default applies.
- `title: ""` and `description: ""` are still accepted as legal display
  values; behaviour is unchanged.

This rule is a strict relaxation: nothing previously valid becomes invalid.

## `key:` validation

`key` accepts **a single printable character**. The following are hard errors at load time:

- **Duplicate across prompts.** Two prompts declaring the same `key:` value (case-insensitive). Surfaced as `DuplicateKeybind`. `tprompt doctor`, `list`, `send`, and `tui` all fail.
- **Reserved key collision.** A prompt declaring a key that is currently reserved (defaults: `P`, `/`, `Esc`, `Enter`; configurable).
- **Malformed value.** Multi-character string, non-printable character, or symbolic forms like `ctrl+x` / `alt-p`. (Empty/null values are treated as absent — see "Empty-value tolerance" above.)

Case sensitivity: `key: c` and `key: C` are the **same** key. The system normalizes to lower-case internally.

Keys outside the auto-assign pool (`1 2 3 4 5 q e r f g t z x c`) are allowed in frontmatter. A user may pin `key: m` and it takes a board slot using the character `m`.

## Keybind assignment

Two-stage process, deterministic given the same prompt set:

1. **Frontmatter-declared keys** take their declared character.
2. **Auto-assigned** prompts (no `key:` in frontmatter) scan alphabetically by `id` and receive the next available character from the pool `1 2 3 4 5 q e r f g t z x c`, skipping any character already taken by a frontmatter declaration.
3. Prompts that cannot receive a pool character (pool exhausted) are **overflow** and are reachable only via `/`-search in the TUI.

## Injected content

Only the markdown body is injected.

Frontmatter is never injected.

## Example

```markdown
---
title: Code Review
description: Deep review prompt focused on correctness, risk, tests
tags: [review, code]
key: c
mode: paste
enter: false
---

Review this code for correctness, risk, and missing tests.
```

Injected text:

```text
Review this code for correctness, risk, and missing tests.
```

Board row rendering:

```text
[c]  code-review      Deep review prompt focused on correctness, risk, tests
```

## Reloading strategy

The implementation may re-scan the prompt directory on each command if implementation simplicity is better than caching.

That is acceptable unless performance becomes meaningfully bad.
