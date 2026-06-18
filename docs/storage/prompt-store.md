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
- `variables` — ordered list of string template inputs

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
- `variables:` (no value) and `variables: []` both decode to an empty list.
- `title: ""` and `description: ""` are still accepted as legal display
  values; behaviour is unchanged.

This rule is a strict relaxation: nothing previously valid becomes invalid.

### Imported prompt frontmatter parity

`tprompt import wispr` writes the **same** frontmatter field set that `tprompt
new` scaffolds (`title`, `description`, `tags`, `key`, `mode`, `enter`,
`variables`, in that order): only `title`/`tags` are populated from the snippet
and the rest are emitted as empty stubs, so an imported prompt is as editable as
a scaffolded one.
The snippet-controlled fields are YAML-marshaled (never string-templated), and the
body passes the same [Body trimming](#body-trimming) contract — imported bodies
are byte-for-byte *through* that trim, not raw verbatim bytes. The `new`↔import
field set is held in lockstep by a parity test (not a shared abstraction); see
[DECISIONS.md §34](../../DECISIONS.md#34-wispr-flow-snippet-import) for the locked
decision and its rationale.

## `key:` validation

`key` accepts **a single printable character**. The following are hard errors at load time:

- **Duplicate across prompts.** Two prompts declaring the same `key:` value (case-insensitive). Surfaced as `DuplicateKeybind`. `tprompt doctor`, `list`, `send`, and `tui` all fail.
- **Reserved key collision.** A prompt declaring a key that is currently reserved (defaults: `P`, `/`, `Esc`, `Enter`; configurable).
- **Malformed value.** Multi-character string, non-printable character, or symbolic forms like `ctrl+x` / `alt-p`. (Empty/null values are treated as absent — see "Empty-value tolerance" above.)

Case sensitivity: `key: c` and `key: C` are the **same** key. The system normalizes to lower-case internally.

Keys outside the auto-assign pool (`1 2 3 4 5 q e r f g t z x c`) are allowed in frontmatter. A user may pin `key: m` and it takes a board slot using the character `m`.

## Template variables

`variables` declares ordered string inputs for the prompt body. A prompt becomes
templated only when this list is non-empty; prompts without variables may contain
literal `{{...}}` text without special handling.

Example:

```yaml
variables:
  - name: issue-id
    label: Issue
    description: Linear issue identifier
    default: AUR-123
    required: true
  - name: focus
    default: correctness and tests
```

Variable fields:

- `name` — required; lowercase kebab-case (`issue`, `issue-id`, `p0-context`)
- `label` — optional TUI display label; falls back to `name`
- `description` — optional TUI helper text
- `default` — optional string value used when the CLI/TUI provides no override
- `required` — optional bool; when true, the final value must not be empty or whitespace

Validation rules:

- Variable names must be unique within the prompt.
- Variable names must not collide with built-in `send` or root flags:
  `mode`, `enter`, `target-pane`, `sanitize`, `config`, `help`, `h`, or
  `version`.
- Placeholder syntax is `{{name}}`; spaces inside the braces are tolerated
  (`{{ name }}`).
- `\{{name}}` renders as the literal text `{{name}}`.
- A placeholder in a templated prompt must name a declared variable. Unknown,
  malformed, empty, or unclosed placeholders are prompt-store errors.
- Every declared variable must be used by at least one unescaped placeholder in
  the body. Unused declarations are prompt-store errors.

Rendering happens before `max_paste_bytes` validation and before sanitization.
The rendered markdown body is the content delivered by `send` or written into a
TUI handoff job.

## Keybind assignment

Two-stage process, deterministic given the same prompt set:

1. **Frontmatter-declared keys** take their declared character.
2. **Auto-assigned** prompts (no `key:` in frontmatter) scan alphabetically by `id` and receive the next available character from the pool `1 2 3 4 5 q e r f g t z x c`, skipping any character already taken by a frontmatter declaration.
3. Prompts that cannot receive a pool character (pool exhausted) are **overflow** and are reachable only via `/`-search in the TUI.

## Injected content

Only the rendered markdown body is injected.

Frontmatter is never injected. For non-template prompts, "rendered" means the
trimmed body unchanged. For templated prompts, it means the body after
frontmatter-declared variables have been substituted.

### Body trimming

The parser strips **one** trailing line break (`\n` or `\r\n`) from the body.
Most editors enforce a POSIX end-of-file newline; without trimming, that
newline would land in the target as an extra blank line at paste time. A
single trailing line break is treated as a file convention, not as content.

The trim is one-only — `body\n\n` becomes `body\n` so authors who deliberately
end a prompt with a blank line keep that blank line.

A single leading line break immediately after the closing `---` fence is also
stripped, so the canonical layout

```markdown
---
title: Foo
---

Body text.
```

yields exactly `Body text.` (no leading blank line, no trailing newline).

## Example

```markdown
---
title: Code Review
description: Deep review prompt focused on correctness, risk, tests
tags: [review, code]
key: c
mode: paste
enter: false
variables:
  - name: focus
    default: correctness, risk, and missing tests
---

Review this code for {{focus}}.
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
