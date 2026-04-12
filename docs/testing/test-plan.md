# Test Plan

## Goal

The MVP should be testable without requiring a full live tmux session for most logic.

## Unit tests

### Prompt store

- discovers markdown files recursively
- derives ID from filename stem
- detects duplicate stems
- extracts body correctly with and without frontmatter
- ignores unsupported file extensions

### Config

- loads defaults
- merges config + flags correctly
- validates invalid mode/path values

### Job validation

- rejects missing target pane
- rejects invalid mode
- preserves prompt body and metadata correctly

## Adapter tests

Use mocks/fakes for tmux command execution where possible.

Test:

- correct tmux command construction
- paste flow includes optional Enter when requested
- type flow handles multiline text reasonably
- pane-exists and selection checks map command output correctly

## CLI tests

- `list` success and duplicate-ID failure
- `show` missing ID
- `send` outside tmux without target pane
- `doctor` reports missing prompt directory

## Integration-ish tests

If practical, add a small set of opt-in tests using a real tmux session in CI or local development.

Potential cases:

- create pane
- submit deferred job
- switch into popup-like intermediate process
- verify prompt lands in target pane after return

These tests are valuable but should not block MVP if they are disproportionately brittle.

## Manual test checklist

- open tmux pane with shell prompt
- run popup flow
- choose prompt
- confirm prompt lands after popup closes
- repeat with paste mode and type mode
- repeat after intentionally closing target pane to confirm failure path
