# Phase 5a: TUI Command Shell (archived PRD)

Historical. The daemon described here was removed (DECISIONS §33); the
`Submitter` seam now feeds the handoff worker. Live docs:
`docs/commands/tui.md`, `docs/commands/tui-flow.md`,
`docs/implementation/interfaces.md`.

## Problem

`tprompt tui` was a stub. There was no command shell, no capture of the
originating pane, no pre-flight validation, and no path from a TUI selection to
deferred delivery, so the signature popup workflow was unusable.

## Solution

- Flags: `--target-pane` (required at the time; later relaxed by direct mode),
  optional `--client-tty` and `--session-id` so failure banners reach the right
  client. No `--mode` / `--enter` / `--sanitize` overrides on the TUI.
- Pre-flight chain before drawing any UI, stopping at the first failure, ordered
  "most fundamental broken thing first": config (exit 2), prompt store (exit 3:
  duplicate ID, malformed key, reserved-key collision), daemon socket (exit 5),
  target pane exists (exit 4).
- A `Renderer` interface; 5a shipped a stub that always cancels, so 5b could
  drop the real UI into the same slot without touching the shell.
- A `Submitter` deep module: `Submit(result)` resolves the prompt, runs
  `ResolveDelivery(cfg, frontmatter, nil)` (no CLI overrides in the TUI flow),
  enforces `max_paste_bytes` with a typed `BodyTooLargeError`, and builds the
  submit request. Clipboard selections carry the bytes captured by the TUI, so
  content is pinned to the moment of intent. Cancel is a no-op.
- `tui.Result` carries `Action`, `PromptID`, and `ClipboardBody`.
- `Deps.NewRenderer` factory, overridable in tests to inject canned results.
- Store metadata escape stripping: OSC, DCS, and CSI sequences are removed from
  `title`, `description`, and `tags` at load (bodies untouched), protecting the
  TUI, `list`, `show`, and `doctor`.
- Exit codes: 0 success or cancel (Esc and Ctrl+C alike), 2 usage/config,
  3 prompt or content validation, 4 tmux environment, 5 delivery IPC.

## Key user-facing rules

- Forgetting the target fails loudly rather than delivering to the wrong pane.
- Oversized prompt or bad clipboard (empty, non-UTF-8, oversized) keeps the TUI
  open with an inline error instead of exiting.
- A submission that replaces a pending job on the same pane exits 0; the
  displaced-job notice is shown in the target pane.

## Testing approach

Assert externally visible behavior (output, exit code, submitted request), not
intermediate fields. Table-driven `Submitter` tests with a fake client;
fixture tests for metadata stripping (multi-byte UTF-8 next to escapes
survives); `runTUI` integration tests with injected fakes; testscripts for
missing target, unreachable backend, missing pane, cancel, happy path, and
oversize body.

## Out of scope then

The real Bubble Tea UI (5b), auto-start and `daemon stop` (later), post-injection
verification, per-prompt verification timeouts, retry on submit failure (fail
fast), cross-host delivery, modifier-key keybinds.
