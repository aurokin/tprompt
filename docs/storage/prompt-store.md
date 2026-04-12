# Prompt Store

Prompts are markdown files on disk.

## Discovery rules

For MVP:

- recurse through the configured prompts directory
- accept `.md` files only
- ignore hidden implementation files unless intentionally supported later

## ID derivation

ID is derived from the filename stem.

Examples:

- `/prompts/code-review.md` -> `code-review`
- `/prompts/review/bug-hunt.md` -> `bug-hunt`

Directories do not contribute to the ID.

## Duplicate detection

Prompt discovery must detect duplicate stems.

This is a hard error, not a warning.

## Frontmatter

Optional YAML frontmatter may define metadata.

Suggested supported keys for MVP:

- `title`
- `description`
- `tags`
- `mode`
- `enter`

If unsupported keys exist, ignoring them is acceptable for MVP.

## Injected content

Only the markdown body is injected.

Frontmatter is never injected.

## Example

```markdown
---
title: Code Review
description: General deep review prompt
tags: [review, code]
mode: paste
enter: false
---

Review this code for correctness, risk, and missing tests.
```

Injected text:

```text
Review this code for correctness, risk, and missing tests.
```

## Reloading strategy

MVP may re-scan the prompt directory on each command if implementation simplicity is better than caching.

That is acceptable unless performance becomes meaningfully bad.
