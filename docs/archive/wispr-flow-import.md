# Wispr Flow Snippet Import (archived PRD)

Historical. Live contract: DECISIONS §34, EXPECTATIONS (Import),
`docs/commands/cli.md#tprompt-import-wispr`.

## Problem

Users of Wispr Flow (voice dictation) have prompt-shaped snippets (trigger
phrase to expansion text) with no way out, so adopting tprompt meant retyping
them.

## Feasibility research (2026-05)

- No usable public API: Wispr's API is gated and transcription-only, with no
  read or export endpoints.
- No in-app export: Wispr's docs say there is no built-in export; bulk import is
  one-way and account egress is a manual data request.
- An undocumented cloud API exists but its terms prohibit reverse engineering
  and scraping, and it is brittle. Out of scope.
- Viable path: Wispr's local, app-unencrypted SQLite database `flow.sqlite`
  (macOS `~/Library/Application Support/Wispr Flow/`, Windows
  `%APPDATA%\Wispr Flow\`), opened read-only with no auth or network, the same
  path other community tools use. Snippets are `Dictionary` rows with
  `isSnippet = 1 AND isDeleted = 0`; `phrase` is the name and `replacement` the
  body.

## Mapping

| Wispr | tprompt |
|---|---|
| `phrase` | `title` (verbatim) and slug source for the ID |
| `replacement` | body |
| (none) | `tags: [wispr]`, plus `starred` when `isStarred = 1` |
| `id` (UUID) | collision disambiguator only |
| timestamps | dropped |
| key / mode / enter | omitted (config defaults; never auto-pin a key) |

Frontmatter is YAML-marshaled, never string-templated, because phrases can
contain `:` or quotes.

Slugging: lowercase, whitespace and `_` to `-`, drop characters outside
`[a-z0-9-]`, collapse and trim dashes; an empty result falls back to
`wispr-<uuid prefix>`; intra-batch collisions append a short UUID suffix
(deterministic across re-runs); collisions with existing prompts follow the
conflict policy (skip by default, `--overwrite` to replace).

## Command and design

`tprompt import wispr [--db-path] [--project] [--dry-run] [--overwrite] [--tag]`.
Created paths on stdout (scriptable), tty-gated summary on stderr. The SQLite
driver (`modernc.org/sqlite`, pure Go, no CGO) is confined to `internal/wispr`,
injected via `Deps`. The collision-detection and atomic-write core was extracted
from `new` so both commands share one TOCTOU-safe writer.

Errors: missing DB path is a usage error (2); a DB that cannot be opened (locked
while Wispr writes, Full Disk Access denied) is a general error (1) naming the
fix; write failures use the prompt-store taxonomy (3). Null or empty
replacements are skipped and counted. Team snippets are imported.

## Out of scope then

Wispr custom transforms / custom prompts (storage location unconfirmed, needs a
spike), Wispr history and transcripts (privacy-sensitive, never read), a generic
import framework, a Linux default path (`--db-path` required).
