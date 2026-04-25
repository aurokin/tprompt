# Phase 6 Coverage Audit

This page maps the Phase 6 hardening checklist to current proof. Keep it as a
release-gate index: when behavior changes, update the mapping alongside tests.

## Coverage Matrix

| Checklist item | Proof | Status |
| --- | --- | --- |
| Prompt discovery and duplicate IDs | `internal/store/store_test.go`: recursive discovery, duplicate ID errors, hidden-file ignores, overflow summaries, rediscovery failure cache clearing. `cmd/tprompt/testdata/script/doctor_duplicate_ids.txtar` verifies user-facing duplicate reporting. | Covered |
| Config validation | `internal/config/config_test.go`: default values, TOML decoding, unknown keys, invalid modes, invalid sanitizer values, reserved-key parsing, argv parsing, paste-specific validation. `cmd/tprompt/testdata/script/config_*.txtar` verifies CLI errors. | Covered |
| Keybind resolution | `internal/keybind/keybind_test.go`: frontmatter precedence, alphabetical auto-assignment, overflow, duplicate keys, reserved collisions, malformed values. `internal/store/store_test.go` verifies those errors surface through store discovery. | Covered |
| Body and frontmatter parsing | `internal/promptmeta/promptmeta_test.go`: frontmatter/body split, no-frontmatter body, CRLF, UTF-8 BOM, unknown fields, malformed mapping rejection, non-frontmatter fences, single newline trim. Store tests verify delivered body and prompt defaults. | Covered |
| `key:` frontmatter behavior | `internal/promptmeta/promptmeta_test.go` verifies declared key metadata. `internal/store/store_test.go` and `internal/keybind/keybind_test.go` verify normalized keys, empty/null keys, duplicate/reserved/malformed failures, and explicit-key precedence. | Covered |
| Sanitizer modes `off`, `safe`, `strict` | `internal/sanitize/sanitize_test.go`: identity behavior, dangerous OSC/DCS/CSI/keypad stripping in safe mode, cosmetic preservation in safe mode, strict rejection with class and byte offset, UTF-8 boundaries, malformed nested escapes. App and testscript coverage verifies strict rejection at CLI boundaries. | Covered |
| Clipboard reader detection, overrides, validation errors | `internal/clipboard/clipboard_test.go`: Wayland/X11/macOS detection seams, no-reader guidance, command stdout/stderr handling, empty/non-UTF-8/oversize validation. `internal/config/config_test.go` verifies override argv parsing. `internal/app/paste_test.go` and paste testscript files verify command behavior. | Covered |
| CLI exit codes, including cancellation success | `internal/app/exit_test.go`: typed error to exit-code mapping. `internal/app/commands_test.go` and picker tests verify `pick` cancellation returns nil. TUI model/app tests verify cancel returns success; testscript includes black-box `pick` and TUI cancellation paths. | Covered |
| Tmux paste/type command construction | `internal/tmux/adapter_test.go`: `load-buffer`, `paste-buffer -d -p`, `send-keys -l --`, Enter separation, rune-safe chunking, pane probes, message scoping, cleanup on failure. | Covered |
| TUI model/view regressions | `internal/tui/model_test.go`, `internal/tui/model_search_test.go`, `internal/tui/search_index_test.go`: board rendering, resolved reserved keys, empty store, cancellation, prompt selection, clipboard flow, inline errors, search ranking, overflow reachability, scrolling, highlight anchoring, pending-state behavior. `internal/app/tui_test.go` covers app preflight and state construction. | Covered |
| Known limitations documentation | `README.md`, `EXPECTATIONS.md`, `docs/roadmap/future-phases.md`, and command docs describe same-host clipboard, single-character keybinds, no live clipboard preview, no remote delivery, and no templating. AUR-113 owns the final docs refresh after Phase 6/7 behavior lands. | Covered for current MVP; final refresh deferred to AUR-113 |
| Release gate | `go test ./...` is the fast local gate. `make check` remains the full health gate when pinned tools are installed. | Covered |

## Meaningful Phase 6 Additions

The audit added explicit coverage where the old proof was present but too
indirect:

- Store-level tests now verify duplicate, reserved, malformed, empty, and null
  `key:` errors surface through prompt discovery.
- Testscript now includes black-box cancellation success for `tprompt pick`.
- Testscript now includes black-box TUI cancellation through
  `TPROMPT_TEST_RENDERER=cancel`.

## Deferred To Sibling Issues

The following are milestone work, but not AUR-105 coverage-audit scope:

- Expanded `doctor` diagnostics: AUR-106.
- Resolved keybind display in `list` and `show`: AUR-107.
- Help text polish: AUR-108.
- Daemon status and log readability: AUR-109.
- Opt-in daemon auto-start: AUR-110.
- `daemon stop`: AUR-111.
- Warning-only post-injection verification: AUR-112.
- Final user-facing documentation pass: AUR-113.
- Final full release gate: AUR-114.
