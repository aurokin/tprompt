# Data Model

## Prompt record

Suggested logical model:

```text
Prompt {
  id: string            // filename stem only
  path: string          // absolute or canonical path
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

- `source = "clipboard"` means the TUI already captured the bytes before exiting; the daemon does not re-read the clipboard.
- `sanitize_mode` is resolved at request construction (flag > config > default) so the daemon does not need to re-resolve config.
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

The require-style behavior is baked into the daemon rather than expressed as wire fields: verify target pane existence, wait for the submitter process to exit when `submitter_pid` is present, then verify pane selection before delivery. Post-injection capture-pane verification is deferred.

## Replacement semantics

When a new `DeferredJob` arrives with the same `request.pane_id` as a pending job, the pending job is **discarded**. Only the newer job is executed once verification passes.

## Notes

- Keep the in-memory model straightforward.
- Persisted job queues are not part of the current contract.
- If the daemon restarts, in-flight TUI-submitted jobs may be lost.
- Clipboard bytes embedded in a `DeliveryRequest.body` are transient — they live only for the lifetime of the job and must not be written to logs.
