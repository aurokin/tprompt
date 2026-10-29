# Agent Instructions

## Toolchain

Go module project (`go.mod`). Pinned via `mise.toml`: Go 1.26.3, `golangci-lint v2.1.6`, `gofumpt v0.7.0`, `goimports v0.26.0`. `mise install` or `make tools` to bootstrap.

## File-Scoped Commands

| Task | Command |
|------|---------|
| Build (quick compile) | `go build ./cmd/tprompt` |
| Build (release-stamped) | `make build` → writes `bin/tprompt` |
| Test (one package) | `go test ./internal/<pkg>/` |
| Test (one func) | `go test -run TestName ./internal/<pkg>/` |
| Lint (one package) | `golangci-lint run ./internal/<pkg>/...` |
| Format (one file) | `golangci-lint fmt internal/<pkg>/file.go` |

## Health Gate

`make check` (fmt-check + lint + race-enabled tests) must pass before merge. See [docs/testing/harness.md](docs/testing/harness.md). Never bypass hooks with `--no-verify` or `--no-gpg-sign`.

## Contracts that must not regress

- Exit-code mapping: `internal/app/exit.go` + `exit_test.go`. Documented in [docs/commands/cli.md](docs/commands/cli.md).
- No prompt body or clipboard content in logs. Enforced by `internal/delivery/executor_test.go`.
- Frontmatter rules: see [docs/storage/prompt-store.md](docs/storage/prompt-store.md).
- User-visible behavior contract: [EXPECTATIONS.md](EXPECTATIONS.md).
- Locked product/engineering decisions: [DECISIONS.md](DECISIONS.md).

## Test isolation

Testscripts in `cmd/tprompt/testdata/script/*.txtar` exec real `tmux`. Confirm with the user before running `go test ./...` if the surrounding shell has tmux state that matters.

## Workflow

- Track work in GitHub issues; deferred product ideas live in [docs/roadmap/future-phases.md](docs/roadmap/future-phases.md).
- Use a short descriptive branch name (e.g., `fix/paste-size-cap`). When the work has a GitHub issue, reference it in the PR body (`Closes #N`) so merging closes it.
- One vertical slice per PR; tests accompany code.
- Scope larger work in the GitHub issue (problem, decisions, slices). Settled outcomes belong in `DECISIONS.md`, `EXPECTATIONS.md`, and the subsystem docs, not in planning files. Past milestone PRDs are archived in [docs/archive/](docs/archive/README.md) for history.
- Durable repo docs are contracts, decisions, architecture, interfaces, and proof surfaces. Keep them progressively disclosed from `README.md` → `docs/README.md` → narrow subsystem docs.

## Git safety

- Never force-push to `main`.
- Never run `git reset --hard`, `git checkout --`, `git restore .`, or `git clean -f` without explicit user approval.
- Create new commits rather than amending published commits.

## Where to look next

- [README.md](README.md) — product overview, command map, and documentation entrypoint.
- [docs/README.md](docs/README.md) — progressive-disclosure index ("I want to change X" → narrowest doc).
- [docs/architecture/overview.md](docs/architecture/overview.md) — system shape and data flow.
- [docs/implementation/interfaces.md](docs/implementation/interfaces.md) — module seams.
- [docs/testing/harness.md](docs/testing/harness.md) — proof surfaces per subsystem.
- [DECISIONS.md](DECISIONS.md) / [EXPECTATIONS.md](EXPECTATIONS.md) — locked decisions and user-visible contract.
