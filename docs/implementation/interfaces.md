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
```

## Picker

```text
Picker
- Select(prompts []PromptSummary) -> selectedID string, cancelled bool, error
```

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

They keep tmux process execution, filesystem scanning, and UI selection mockable for tests.
