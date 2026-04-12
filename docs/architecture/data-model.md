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
  body: string          // markdown body only
  defaults: {
    mode: "paste" | "type" | null
    enter: bool | null
  }
}
```

## Duplicate prompt record

Used for diagnostics.

```text
DuplicatePromptID {
  id: string
  paths: []string
}
```

## Delivery target

```text
TargetContext {
  pane_id: string
  session_id: string?
  window_id: string?
  client_tty: string?
}
```

## Delivery request

```text
DeliveryRequest {
  prompt_id: string
  source_path: string
  body: string
  mode: "paste" | "type"
  press_enter: bool
  target: TargetContext
}
```

## Deferred job

```text
DeferredJob {
  job_id: string
  created_at: timestamp
  request: DeliveryRequest
  verification_policy: VerificationPolicy
}
```

## Verification policy

```text
VerificationPolicy {
  require_target_pane_exists: true
  require_popup_process_exit: true
  require_selected_pane_match: bool
  require_post_injection_change_check: bool
  timeout_ms: integer
}
```

## Notes

- MVP should keep the in-memory model straightforward.
- Persisted job queues are not required for MVP.
- If the daemon restarts, in-flight popup jobs may be lost. That is acceptable for MVP if documented clearly.
