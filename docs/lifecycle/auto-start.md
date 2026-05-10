# Daemon lifecycle and TUI auto-start

This document covers the daemon lifecycle implementation: the file-system primitives, the launcher's seams, the macOS implicit-disabled policy, and the operator escape hatches. For the locked decision summary see [DECISIONS.md §33](../../DECISIONS.md). For user-visible contracts see [EXPECTATIONS.md](../../EXPECTATIONS.md).

## Modes

- `tprompt daemon run` — foreground. SIGINT, SIGTERM, or the `Stop` RPC unwind through `Server.Close`.
- `tprompt daemon start` — non-blocking. Spawns `daemon run` detached via the launcher, polls `Status` until ready, prints `tprompt daemon started on <socket>` (or `already running on <socket>`).
- TUI auto-start — default-on on Linux. On macOS, hardcoded off (see [macOS implicit-disabled policy](#macos-implicit-disabled-policy) below). On Linux, the same launcher fires when `tprompt tui` (or bare `tprompt` dispatching to TUI inside tmux+tty) finds the socket unreachable.
- `tprompt daemon stop` — mode-agnostic. Dials the socket, issues the `Stop` RPC, waits (bounded) for the socket to disappear.

A *compatible daemon* is one reachable at the configured socket whose `Status` RPC succeeds. The launcher refuses to spawn over a *reachable-but-broken* socket — one bound by a process that cannot answer `Status` — and reports a manual-recovery message.

## File-system primitives

All four live next to the configured socket path, e.g. `~/.local/state/tprompt/daemon.sock.lock`:

| Path | Purpose |
|------|---------|
| `<socket>.lock` | Run lock. `flock(LOCK_EX \| LOCK_NB)` with `O_CLOEXEC`. Held by the live daemon for its full lifetime. |
| `<socket>.start.lock` | Start lock. Blocking `flock(LOCK_EX)`. Serializes concurrent cold starts. |
| `<socket>.identity.json` | Identity sidecar. `(pid, start_time, version)`. Atomic write via tmp+rename. Removed on graceful shutdown only when the live daemon still owns it (defends against PID reuse). |
| `<socket>.start.cooldown` | Cooldown marker. Recorded after an *implicit* (TUI) start failure on platforms where implicit auto-start is permitted; gates subsequent implicit starts for 10 s. Explicit starts always bypass and clear the marker on success. Unused on macOS because implicit auto-start is hardcoded off. |

## Launcher seams

`internal/app/lifecycle/launcher.go` defines three injectable interfaces plus a platform-policy seam:

- `StatusProber.Probe(ctx) → (ProbeResult, error)`. Three outcomes: `ProbeOK`, `ProbeUnreachable`, `ProbeReachableBroken`. The launcher refuses to spawn over `ProbeReachableBroken`.
- `Spawner.Spawn(ctx, exec, args, logPath) → (SpawnHandle, error)`. The handle carries a PID so the readiness loop can detect child early-exit via `kill(pid, 0)` and report `ReasonChildExitedEarly` instead of timing out.
- `TrustAssessor.Assess(intent, exec) → AssessResult`. Reserved for future explicit-start preflight (AUR-327). After AUR-326 it is not invoked on the implicit path; the macOS implicit path is refused before the launcher reaches the assessor.
- `MacOSImplicitAutoStartDisabled(intent)` — build-tagged platform policy. Returns `(true, reason)` on darwin for `IntentImplicitTUI`; returns `(false, "")` otherwise. The launcher short-circuits before the cooldown, start lock, trust assessor, and spawn path when this returns true.

The launcher also takes a `Now` clock and a `LogPreSpawn` callback so unit tests can substitute time and capture diagnostics.

## Start intents

`StartIntent` distinguishes why the launcher was invoked:

- `IntentExplicitStart` — `tprompt daemon start`. Platform policy bypassed. Cooldown bypassed.
- `IntentExplicitRun` — `tprompt daemon run`. Doesn't actually drive the launcher (run is foreground), but the enum value exists so pre-spawn diagnostics and policy/preflight hooks can match on it consistently.
- `IntentImplicitTUI` — TUI default-on auto-start (Linux). On darwin this intent is refused by `MacOSImplicitAutoStartDisabled` before the launcher reaches the spawn path.

## Pre-spawn diagnostic

Before the detached child is spawned, the launcher appends a single logfmt line to the daemon log:

```
time=2026-05-07T02:18:34.012Z outcome=lifecycle_pre_spawn parent_pid=12345 intent=explicit_start exec="/usr/local/bin/tprompt" socket="…" runlock="…" startlock="…" identity="…" cooldown="…" log="…" config="…" trust=allow
```

When the macOS implicit-disabled policy refuses, the launcher emits a distinct logfmt line so the refusal is visible to operators reviewing the daemon log:

```
outcome=lifecycle_implicit_disabled parent_pid=12345 intent=implicit_tui exec="/usr/local/bin/tprompt" socket="…" runlock="…" startlock="…" identity="…" cooldown="…" log="…" reason="implicit daemon auto-start is disabled on macOS; run 'tprompt daemon start' (background) or 'tprompt daemon run' (foreground) to start the daemon explicitly"
```

## Readiness wait

The readiness budget is 5 s by default. The launcher polls `Status` at 50 ms intervals; on each iteration it also runs `kill(pid, 0)` against the spawn handle's PID. If the child has exited, the launcher returns `ReasonChildExitedEarly` immediately rather than burning the full budget on a dead PID.

Failure detail always includes the daemon log path so operators know where to look.

## macOS implicit-disabled policy

Implementation: `internal/app/lifecycle/policy_darwin.go`. The policy is hardcoded; there is no config or environment override that re-enables the implicit path.

Behavior:

- `IntentImplicitTUI` on darwin → `OutcomeFailed` with `ReasonPolicyDisabled`. The launcher's failure detail names both recovery commands: `tprompt daemon start` (background) and `tprompt daemon run` (foreground).
- `IntentExplicitStart` and `IntentExplicitRun` are not affected; explicit intents always reach the spawn path.
- The TUI command path (`internal/app/tui.go`'s `autoStartTUIDaemon`) mirrors the refusal before constructing the launcher so the test seams can assert `NewLauncher` is not invoked on darwin.

Rationale: macOS launch evaluation triggered repeated kernel panics in `AppleSystemPolicy` / `AMFI` / `syspolicyd` during implicit auto-start of real release binaries. Diagnosing the root cause requires kernel-level cooperation we do not have; until then, implicit auto-start is refused on darwin so a TUI invocation never feeds the launchd path that panicked the kernel. Explicit `daemon start` and `daemon run` exercise a different code path that has not exhibited the panic.

The previous AUR-314 trust gate (`internal/app/lifecycle/trust_darwin.go`) was active on `IntentImplicitTUI` only. With AUR-326 the implicit path is refused before the assessor is consulted, so the trust gate code is dormant. AUR-327 plans to re-wire the assessor as a preflight on the explicit start path; the implementation in `trust_darwin.go` is retained for that purpose.

## Recovery paths

When the macOS implicit policy refuses, or when a non-darwin implicit auto-start is refused (cooldown, reachable-but-broken socket), the user has two escape hatches:

1. **`tprompt daemon start`** — bypasses the macOS implicit-disabled policy and any cooldown. Same idempotent success behavior as if the daemon was already running.
2. **`tprompt daemon run`** — also bypasses. Useful when the user wants to keep the daemon in the foreground for debugging.

The TUI failure banner only suggests options the TUI command path accepts (i.e., it doesn't tell the user to retry `tprompt tui --some-flag` that doesn't exist). It mentions the daemon log path for post-mortem and points at the explicit-recovery commands by name.

## Signing/notarization expectation for releases

Although implicit auto-start is currently disabled on macOS, the release artifacts that ship `tprompt` on macOS must still be:

- **Validly signed** with a Developer ID Application certificate (or equivalent trusted by Gatekeeper).
- **Notarized** with Apple if distributed outside the App Store.

This is for two reasons: (1) Gatekeeper still inspects the binary on first launch even for explicit invocations, and (2) AUR-327's planned explicit-start preflight will reuse the existing `trust_darwin.go` assessor, at which point signing/notarization status will be re-evaluated on explicit invocations.

For development builds (`go build`, `make build`) the user is expected to run `tprompt daemon start` explicitly.

The local signing scripts and the GitHub Actions release pipeline that produces signed/notarized artifacts are documented in [macos-release-signing.md](macos-release-signing.md).

## See also

- [DECISIONS.md §33](../../DECISIONS.md) — locked decision summary.
- [EXPECTATIONS.md](../../EXPECTATIONS.md) — user-visible contract.
- [docs/commands/daemon.md](../commands/daemon.md) — `daemon start`/`run`/`status`/`stop` reference.
- [docs/commands/tui-flow.md](../commands/tui-flow.md) — TUI flow reference.
- [internal/app/lifecycle/launcher.go](../../internal/app/lifecycle/launcher.go) — launcher implementation.
- [internal/app/lifecycle/policy_darwin.go](../../internal/app/lifecycle/policy_darwin.go) — macOS implicit-disabled policy.
- [internal/app/lifecycle/trust_darwin.go](../../internal/app/lifecycle/trust_darwin.go) — trust assessor (currently dormant, AUR-327 will re-wire on explicit).
- [docs/lifecycle/macos-release-signing.md](macos-release-signing.md) — signing/notarization scripts and release pipeline.
- [internal/daemon/lifecycle/](../../internal/daemon/lifecycle/) — primitives.
