# Agent Instructions

## Toolchain

Go module project (`go.mod`). Pinned via `mise.toml`: Go 1.26.2, `golangci-lint v2.1.6`, `gofumpt v0.7.0`, `goimports v0.26.0`. `mise install` or `make tools` to bootstrap.

## File-Scoped Commands

| Task | Command |
|------|---------|
| Build | `go build ./cmd/tprompt` |
| Test (one package) | `go test ./internal/<pkg>/` |
| Test (one func) | `go test -run TestName ./internal/<pkg>/` |
| Lint (one package) | `golangci-lint run ./internal/<pkg>/...` |
| Format (one file) | `golangci-lint fmt internal/<pkg>/file.go` |

## Health Gate

`make check` (fmt-check + lint + race-enabled tests) must pass before merge. See [docs/testing/harness.md](docs/testing/harness.md). Never bypass with `--no-verify` or `--no-gpg-sign`.

## Contracts that must not regress

- Exit-code mapping: `internal/app/exit.go` + `exit_test.go`. Documented in [docs/commands/cli.md](docs/commands/cli.md).
- No prompt body or clipboard content in logs. Enforced by `internal/daemon/executor_test.go`.
- Frontmatter rules: see [docs/storage/prompt-store.md](docs/storage/prompt-store.md).
- User-visible behavior contract: [EXPECTATIONS.md](EXPECTATIONS.md).
- Locked product/engineering decisions: [DECISIONS.md](DECISIONS.md).

## Test isolation

Testscripts in `cmd/tprompt/testdata/script/*.txtar` exec real `tmux`. Confirm with the user before running `go test ./...` if the surrounding shell has tmux state that matters.

## Workflow

- Pick up a Linear issue (project Tprompt, team Aurokin). Branch name matches the issue's `gitBranchName` (e.g., `aur-144-empty-value-frontmatter-tolerance`).
- One vertical slice per PR; tests accompany code.
- Open PR → comment `@codex please review` → address findings → merge with merge commit, delete remote branch.
- After merging, mark the Linear issue Done.
- Planning artifacts (PRDs, issue breakdowns) live in the Linear milestone description, not in the repo.

## Git safety

- Never force-push to `master`.
- Never run `git reset --hard`, `git checkout --`, `git restore .`, or `git clean -f` without explicit user approval.
- Create new commits rather than amending published commits.
- Confirm with the user before pushing or opening PRs.

## Where to look next

- [docs/README.md](docs/README.md) — progressive-disclosure index ("I want to change X" → narrowest doc).
- [docs/architecture/overview.md](docs/architecture/overview.md) — system shape and data flow.
- [docs/implementation/interfaces.md](docs/implementation/interfaces.md) — module seams.
- [docs/testing/harness.md](docs/testing/harness.md) — proof surfaces per subsystem.
- [docs/roadmap/future-phases.md](docs/roadmap/future-phases.md) — intentionally deferred ideas.
