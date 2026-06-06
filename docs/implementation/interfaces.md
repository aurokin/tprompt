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
- CapturePaneTail(ctx context.Context, paneID string, lines int) -> string, error
- Paste(target TargetContext, body string, pressEnter bool) -> error
- Type(target TargetContext, body string, pressEnter bool) -> error
- DisplayMessage(target MessageTarget, message string) -> error
```

The shared `TmuxAdapter` interface stays lean. A read-only query needed by a
single consumer (e.g. `ListKeys` for `doctor`'s popup-binding check, or
`ListPanes` for the TUI's direct-mode pane gate) is implemented on the concrete
`*tmux.Exec` and consumed through a narrow consumer-side interface recovered by
type assertion — rather than widening `TmuxAdapter` and forcing every fake
(paste/send/handoff/tmux/delivery tests) to grow a method only that one check
uses.

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

## Import TUI

`internal/importtui` is a separate Bubble Tea seam for `import wispr -i` — a
checkbox picker that surfaces import conflicts and per-item overwrite. It is a
deliberate sibling of `internal/tui` (neither imports the other): the board selects
a prompt to submit; the import picker selects ids to write (and which to overwrite).
It depends on no store, clipboard, or submitter — only the items the command layer
hands it.

```text
ImportRenderer
- Run(state ImportState) -> ImportResult | error

ImportState {
  items: []ImportItem              // one conflict-classified snippet per row
}

ImportItem {
  id: string                       // disambiguated prompt id (the id the writer uses)
  title: string                    // snippet phrase, shown as the row label
  conflict: none | exact-target | cross-path   // glyph + selectability
  blocker: string                  // conflicting path (cross-path rows only)
  armed: bool                      // pre-arm an exact-target (a CLI --overwrite refresh)
  tags: []string                   // provenance tags (search corpus only; never rendered)
}

ImportResult {
  action: "confirm" | "cancel"
  selected_ids: []string           // every checked id (writes); confirm only, item order
  overwrite_ids: []string          // checked exact-targets armed to overwrite (⊆ selected_ids)
}
```

The command layer builds the write-free plan (`dryRunPlan`), projects fresh /
exact-target / cross-path rows to `Run`, and on confirm re-runs the writer over the
same snippets slice — importing `selected_ids` and folding `overwrite_ids` into a
per-item effective overwrite. Per-item overwrite is exact-target-only and routes
through the single `writePromptContent` overwrite path, so the writer still refuses a
cross-path duplicate (exit 3); the picker surfaces policy, it cannot weaken it. The
small pure scroll/viewport helpers are copied from `internal/tui` (noted in the
package doc), and the renderer runs with alt-screen so the picker is torn down before
the command prints created-path lines to stdout. Its `/`-search delegates to the
shared `internal/searchindex` core (below) over the row's id, title, and tags — so the
picker gains fuzzy filtering without importing `internal/tui`, keeping its dependency
isolation intact.

## Search index (shared fuzzy core)

```text
searchindex.Index[T]
- New[T](items []T, fields func(T) Fields, tieKey func(T) string) -> *Index[T]
- (*Index[T]) Query(q string) -> []Match[T]   // ""→catalog in tieKey order; else ranked

Fields { id, title, description string; tags []string }   // four weighted corpuses
Match[T] { item T; score float64 }
```

A dependency-free, generic fuzzy scorer (only `sahilm/fuzzy` + stdlib) shared by the
board (`internal/tui`) and the import picker (`internal/importtui`). A caller adapts its
row to the four weighted corpuses (`Fields`) and supplies a stable `tieKey`; the core
ranks by best-matched-field priority, then weighted score, then `tieKey`, and returns
the caller's own item type back in `Match`. It knows nothing about the board's clip row
or Scope, or the picker's conflicts — those domain concerns stay in the callers'
adapters (`tui` reproduces its exact `(PromptID, Scope)` ordering by passing
`tieKey = PromptID+"\x00"+Scope`). This is the seam that lets two renderers share
scoring without sharing dependencies.

## Keybind resolver

```text
KeybindResolver
- Resolve(prompts []Prompt, reserved map<char, action>, pool []char) -> KeybindAssignment | error
```

Pure function. Errors on duplicate / reserved / malformed `key:` values.

## Delivery handoff

```text
DeliveryClient
- Submit(job DeferredJob) -> JobSubmitResult | error

HandoffWorker
- RunJob(job_path) -> error
```

## Verification engine

```text
VerificationEngine
- WaitUntilReady(target TargetContext, policy VerificationPolicy) -> VerificationResult
```

## Wispr import reader

```text
Reader
- Snippets() -> []Snippet, error
```

The `import wispr` command depends only on this one-method seam, injected through
`Deps.NewWisprReader(dbPath) Reader`. Production wires `wispr.NewReader(path)` (a
lazily-opened, read-only `flow.sqlite` reader); tests inject a fake returning fixed
snippets or a typed error, so the whole import command — flag handling, id minting,
collision policy, summary, exit-code mapping — is provable without a real SQLite
database. `Snippets()` returns the typed DB-error taxonomy
(`DB{NotFound,PathRequired,Open}Error`) that `app.ExitCode` maps to exit codes.
Snippet→prompt mapping is a pure method (`Snippet.ToPrompt(tag)`), tested directly.
A snippet's tag set is itself a pure method (`Snippet.Tags(tag)` → `[tag]` plus
`starred`), the single source of truth `ToPrompt` and the import picker's search
corpus both consume, so frontmatter tags and search-matchable tags cannot drift.

## Why interfaces matter

They keep tmux process execution, clipboard reading, sanitization, keybind resolution, and UI rendering mockable for tests.
