# Tech Stack

Locked by DECISIONS.md §30. This doc expands on the choices and how they wire together.

## Language

**Go 1.26.**

Why:

- single static binary, trivial cross-compilation for Linux/macOS
- low process startup latency — matters because `tprompt tui` fires from a tmux keybind into a popup; users expect instant
- idiomatic subprocess handling (needed everywhere: tmux commands, clipboard readers)
- mature TUI ecosystem (Bubble Tea)
- small runtime footprint for the long-lived daemon

Rust is explicitly rejected for v1. Equally good binary story, but the async/sync split and borrow-checker friction around the daemon's queue and target-pane state cost more than Go does, for no safety benefit that matters in a local-only CLI.

## Module layout

```
cmd/tprompt/          entry point (main package)
internal/app/         cobra root + subcommand wiring
internal/config/      TOML loading, defaults, validation
internal/store/       prompt discovery + ID resolution
internal/promptmeta/  frontmatter + body extraction
internal/keybind/     keybind resolver (pool + frontmatter merge)
internal/tmux/        tmux command construction and verification
internal/daemon/      IPC server, job queue, replace-same-target
internal/clipboard/   clipboard reader (auto-detect + override)
internal/sanitize/    off / safe / strict sanitizer
internal/tui/         Bubble Tea TUI (board + search + clipboard row)
internal/picker/      external-picker wrapper for `tprompt pick`
testdata/prompts/     fixture prompts used across packages
```

All app code is under `internal/` so downstream consumers cannot import it.

## Libraries

| Purpose | Library |
|---|---|
| CLI | `github.com/spf13/cobra` |
| TUI | `github.com/charmbracelet/bubbletea` + `github.com/charmbracelet/lipgloss` |
| Config (TOML) | `github.com/BurntSushi/toml` |
| Frontmatter (YAML) | `gopkg.in/yaml.v3` |
| Test diffs | `github.com/google/go-cmp` |
| CLI black-box tests | `github.com/rogpeppe/go-internal/testscript` |

No ORM, no web framework, no logger library — stdlib `log/slog` is sufficient for daemon logs.

## Format

- **`gofumpt`** — stricter superset of `gofmt`. All code must pass it.
- **`goimports`** — import grouping/sorting.

Both are bundled by `golangci-lint v2 fmt`. Running `make fmt` runs both through that entrypoint.

## Lint

**`golangci-lint v2`** (config format v2; runs faster than v1 and has cleaner config).

Enabled linters (curated; aggressive but not noisy):

- `govet` — stdlib static checks
- `staticcheck` — the big one; catches bugs, simplifications, deprecations
- `errcheck` — unhandled errors
- `revive` — style successor to deprecated `golint`
- `ineffassign` — ineffectual assignments
- `unused` — dead code
- `gosec` — security issues (subprocess, file permissions)
- `misspell` — obvious typos
- `nolintlint` — enforces that `//nolint` directives are well-formed and justified

Explicitly not enabled: `gocyclo` (superseded by `gocognit`), `gochecknoglobals` (too noisy for CLI code with cobra), `wsl` / `wrapcheck` / `goconst` (style bikeshed).

## Complexity

- **`gocognit`** — cognitive complexity metric; threshold `15`. Chosen over `gocyclo` because it tracks nested-condition pain better.
- **`funlen`** — max 80 lines / 40 statements per function.

Both run inside `golangci-lint`.

## Testing

- **stdlib `testing`** — table-driven tests are idiomatic and enough.
- **`google/go-cmp`** — structural diffing (`cmp.Diff`) for readable failure output.
- **`rogpeppe/go-internal/testscript`** — CLI black-box tests. Each `tprompt` subcommand (`list`, `show`, `send`, `paste`, `doctor`) gets a `.txtar` script asserting stdout/stderr/exit code against a fixture prompt directory. This is the primary integration-test surface.
- Coverage via `go test -covermode=atomic ./...`.
- Real-tmux smoke tests live behind the `tmuxsmoke` build tag so CI only runs them where tmux is installed.

Rejected: `testify`. Its `assert.Equal` style is fine, but stdlib `testing` + `go-cmp` has a smaller dep surface and ages better. If anyone is tempted to add testify later: add `require.*` only, not `assert.*`, and only if the motivation is real rather than familiarity.

## Build & run

- `make build` → `go build -o bin/tprompt ./cmd/tprompt`
- `make fmt` → `golangci-lint fmt`
- `make lint` → `golangci-lint run`
- `make test` → `go test -race -covermode=atomic ./...`
- `make check` → `fmt-check && lint && test` (what CI runs)
- `mise install` → installs the pinned project-local Go/tool versions from `mise.toml`
- `make tools` → installs pinned versions of `golangci-lint`, `gofumpt`, `goimports` into `$(go env GOPATH)/bin`

Release tooling (`goreleaser`, signed builds, Homebrew tap) is deferred to post-MVP.

## Version pinning

Tooling versions are pinned in `Makefile` and mirrored in `mise.toml` — not via `tools.go` (that pattern is legacy; Go 1.24+ prefers `go.mod` tool directives, but pinning outside `go.mod` keeps the module dep graph lean).
