# Data Model

## Prompt record

Suggested logical model:

```text
Prompt {
  id: string            // filename stem only
  path: string          // absolute or canonical path
  scope: "global" | "project"  // source tier this prompt was discovered in
  shadowed: bool        // true when a cross-tier counterpart won prompt_priority
  shadowed_by: string?  // id of the winning counterpart (set on the loser)
  shadow_path: string?  // absolute path of the winning counterpart (set on the loser)
  title: string?        // optional frontmatter
  description: string?  // optional frontmatter
  tags: []string        // optional frontmatter
  key: char?            // optional frontmatter; single printable char; case-insensitive
  body: string          // markdown body only
  defaults: {
    mode: "paste" | "type" | null
    enter: bool | null
  }
}
```

`key` is validated at load time:

- duplicate across prompts → hard error
- collision with a reserved key → hard error
- malformed (multi-char, empty, non-printable) → hard error

## Wispr snippet (import input)

The input model for `tprompt import wispr` — one live row read from a Wispr Flow
`flow.sqlite`. It is mapped to a `Prompt` file by `Snippet.ToPrompt(tag)`; it is not
persisted or carried on any wire.

```text
Snippet {
  id: string            // Wispr UUID; used only to disambiguate id collisions
  phrase: string        // -> title (verbatim) and the slug source for the filename id
  replacement: string   // -> markdown body; empty/NULL means not importable (skipped)
  starred: bool         // isStarred -> appends the "starred" tag
}
```

Mapping is deterministic and one-way: the slug is the prompt id (with a
`wispr-<uuid>` fallback for unsluggable phrases and a uuid suffix for intra-batch
collisions), the phrase is always preserved verbatim as the title, and the
provenance tag (`wispr` by default) plus an optional `starred` tag are written to
frontmatter. Imported prompts carry the full `tprompt new` frontmatter field set
(`title, description, tags, key, mode, enter`); only `title`/`tags` are populated
and the rest are empty stubs, so an imported prompt is as editable as a
scaffolded one. See `DECISIONS.md` §34.

## Source scope and shadow markers

Prompts are discovered through an ordered list of sources (see
`docs/storage/config.md`). Each source carries a tier:

- **global** — primary `prompts_dir` plus any `additional_prompts_dirs`.
- **project** — the nearest project `tprompt/` overlay walked up from cwd.

Within a single tier, duplicate IDs are a hard error. Across tiers,
`prompt_priority` (`global` by default, `project` opt-in) selects the winner.
The losing prompt is **shadowed**: it remains in the in-memory model with
`shadowed = true` and a `shadowed_by` / `shadow_path` pointer at the winner,
so `tprompt list` can mark it, `tprompt show <id>` (on the winner) can report
the shadowed counterpart, and the TUI can keep it reachable through search.
Shadowed prompts receive no board key — overflow rules apply.

## Duplicate prompt record

Used for diagnostics.

```text
DuplicatePromptID {
  id: string
  paths: []string
}

DuplicateKeybind {
  key: char
  prompt_ids: []string
}
```

## Keybind assignment result

Produced by the keybind resolver; consumed by the TUI.

```text
KeybindAssignment {
  bindings: map<char, prompt_id>       // key -> prompt
  overflow: []prompt_id                // prompts with no board slot (search-only)
  reserved: map<char, reserved_action> // clipboard, search, cancel, select
}
```

## Origin context

```text
OriginContext {
  session_id: string?
  window_id: string?
  client_tty: string?
}
```

## Delivery request

```text
DeliveryRequest {
  source: "prompt" | "clipboard"
  prompt_id: string?            // set when source = "prompt"
  source_path: string?          // set when source = "prompt"
  body: string                  // resolved content; already captured by the TUI when source = "clipboard"
  mode: "paste" | "type"
  press_enter: bool
  sanitize_mode: "off" | "safe" | "strict"
  pane_id: string
  origin: OriginContext?
}
```

Notes:

- `source = "clipboard"` means the TUI already captured the bytes before exiting; the handoff worker does not re-read the clipboard.
- `sanitize_mode` is resolved at request construction (flag > config > default) so the handoff worker does not need to re-resolve config.
- `body` is the post-resolution content but **pre-sanitization**. The sanitizer runs in the delivery path immediately before the tmux adapter.

## Deferred job

```text
DeferredJob {
  job_id: string
  created_at: timestamp
  submitter_pid: integer?
  request: DeliveryRequest
  verification_policy: VerificationPolicy
}
```

## Verification policy

```text
VerificationPolicy {
  timeout_ms: integer
  poll_interval_ms: integer
}
```

The require-style behavior is baked into the handoff worker rather than expressed as wire fields: verify target pane existence, wait for the submitter process to exit when `submitter_pid` is present, then verify pane selection before delivery. Post-injection capture-pane comparison is an opt-in delivery diagnostic controlled by config, and remains warning-only.

## Replacement semantics

When a new `DeferredJob` arrives with the same `request.pane_id` as a pending job, the pending job is **discarded**. Only the newer job is executed once verification passes.

## Notes

- Keep the in-memory model straightforward.
- Persisted job queues are not part of the current contract.
- If the handoff worker exits before delivery, the TUI-submitted job may be lost.
- Clipboard bytes embedded in a `DeliveryRequest.body` are transient — they live only for the lifetime of the job and must not be written to logs.
