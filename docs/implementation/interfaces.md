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
  rows: []TUIRow                   // board rows; printable clipboard row, when present, is first
  overflow: []TUIRow               // prompts hidden from board; reachable via search
  reserved: ReservedKeys           // clipboard, search, cancel, select
  clipboard_available: bool        // true when clipboard can still appear in search
}

TUIRow {
  key: char?                       // absent for overflow/search-only rows
  prompt_id: string?               // absent for clipboard row
  title: string?
  description: string?
  tags: []string
}

ReservedKeys {
  clipboard: ReservedBinding
  search: ReservedBinding
  cancel: ReservedBinding
  select: ReservedBinding
}

ReservedBinding {
  printable: char?
  symbolic: string?                // e.g. Esc, Enter, Tab, Space
  disabled: bool
}

TUIResult {
  action: "prompt" | "clipboard" | "cancel"
  prompt_id: string?               // when action == "prompt"
  clipboard_body: []byte?          // when action == "clipboard"
}

TUIModelDeps {
  submitter: Submitter             // invoked by the Model via tea.Cmd
  clipboard_reader: ClipboardReader?
  prompt_store: PromptStore
  max_paste_bytes: integer
}
```

The production TUI model owns recoverable selection handling. It resolves prompt bodies, reads clipboard content, validates `max_paste_bytes`, and invokes the injected `Submitter` via a Bubble Tea command. `Renderer.Run` returns the final `TUIResult` plus any submit error so the command layer can apply normal exit-code mapping.

```text
Submitter
- Submit(result TUIResult) -> error
```

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
- Status() -> DaemonStatus | error
- Stop() -> DaemonStopResult | error

DaemonServer
- Start() -> error
- Stop request handler triggers graceful shutdown
```

## Verification engine

```text
VerificationEngine
- WaitUntilReady(target TargetContext, policy VerificationPolicy) -> VerificationResult
```

## Why interfaces matter

They keep tmux process execution, clipboard reading, sanitization, keybind resolution, and UI rendering mockable for tests.
