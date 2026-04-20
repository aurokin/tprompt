# Suggested Interfaces

These are conceptual interfaces, not language-specific mandates.

## Prompt store

```text
PromptStore
- Discover() -> PromptIndex | error
- Resolve(id string) -> Prompt | error
- List() -> []PromptSummary | error
```

## Tmux adapter

```text
TmuxAdapter
- CurrentContext() -> TargetContext | error
- PaneExists(paneID string) -> bool, error
- IsTargetSelected(target TargetContext) -> bool, error
- CapturePaneTail(paneID string, lines int) -> string, error
- Paste(target TargetContext, body string, pressEnter bool) -> error
- Type(target TargetContext, body string, pressEnter bool) -> error
- DisplayMessage(target MessageTarget, message string) -> error
```

## Clipboard reader

```text
ClipboardReader
- Read() -> []byte, error
```

Constructors:

```text
NewAutoDetect() -> ClipboardReader | error
NewCommand(argv []string) -> ClipboardReader
NewStatic(content []byte) -> ClipboardReader        // test helper
```

## Sanitizer

```text
Sanitizer
- Mode() -> "off" | "safe" | "strict"
- Process(content []byte) -> []byte, error
```

## Picker (external)

```text
Picker
- Select(prompts []PromptSummary) -> selectedID string, cancelled bool, error
```

Drives the optional `tprompt pick` command. Does not participate in the TUI flow.

## TUI

```text
TUIRenderer
- Run(state TUIState) -> TUIResult | error

TUIState {
  bindings: KeybindAssignment
  overflow: []PromptSummary        // reachable via search
  reserved: map<char, ReservedAction>
  pool: []char
}

TUIResult {
  action: "prompt" | "clipboard" | "cancel"
  prompt_id: string?               // when action == "prompt"
}
```

The TUI returns a result; the caller then resolves content (prompt body or clipboard bytes) and submits the daemon job.

## Keybind resolver

```text
KeybindResolver
- Resolve(prompts []Prompt, reserved map<char, action>, pool []char) -> KeybindAssignment | error
```

Pure function. Errors on duplicate / reserved / malformed `key:` values.

## Daemon

```text
DaemonClient
- Submit(job DeferredJob) -> JobSubmitResult | error

DaemonServer
- Start() -> error
- Stop() -> error
```

## Verification engine

```text
VerificationEngine
- WaitUntilReady(target TargetContext, policy VerificationPolicy) -> VerificationResult
```

## Why interfaces matter

They keep tmux process execution, clipboard reading, sanitization, keybind resolution, and UI rendering mockable for tests.
