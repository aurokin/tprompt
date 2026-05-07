# Daemon lifecycle and TUI auto-start

This document covers the daemon lifecycle implementation: the file-system primitives, the launcher's seams, the macOS trust gate, and the operator escape hatches. For the locked decision summary see [DECISIONS.md §33](../../DECISIONS.md). For user-visible contracts see [EXPECTATIONS.md](../../EXPECTATIONS.md).

## Modes

- `tprompt daemon run` — foreground. SIGINT, SIGTERM, or the `Stop` RPC unwind through `Server.Close`.
- `tprompt daemon start` — non-blocking. Spawns `daemon run` detached via the launcher, polls `Status` until ready, prints `tprompt daemon started on <socket>` (or `already running on <socket>`).
- TUI auto-start — default-on. The same launcher fires when `tprompt tui` (or bare `tprompt` dispatching to TUI inside tmux+tty) finds the socket unreachable.
- `tprompt daemon stop` — mode-agnostic. Dials the socket, issues the `Stop` RPC, waits (bounded) for the socket to disappear.

A *compatible daemon* is one reachable at the configured socket whose `Status` RPC succeeds. The launcher refuses to spawn over a *reachable-but-broken* socket — one bound by a process that cannot answer `Status` — and reports a manual-recovery message.

## File-system primitives

All four live next to the configured socket path, e.g. `~/.local/state/tprompt/daemon.sock.lock`:

| Path | Purpose |
|------|---------|
| `<socket>.lock` | Run lock. `flock(LOCK_EX \| LOCK_NB)` with `O_CLOEXEC`. Held by the live daemon for its full lifetime. |
| `<socket>.start.lock` | Start lock. Blocking `flock(LOCK_EX)`. Serializes concurrent cold starts. |
| `<socket>.identity.json` | Identity sidecar. `(pid, start_time, version)`. Atomic write via tmp+rename. Removed on graceful shutdown only when the live daemon still owns it (defends against PID reuse). |
| `<socket>.start.cooldown` | Cooldown marker. Recorded after an *implicit* (TUI) start failure; gates subsequent implicit starts for 10 s. Explicit starts always bypass and clear the marker on success. |

## Launcher seams

`internal/app/lifecycle/launcher.go` defines three injectable interfaces:

- `StatusProber.Probe(ctx) → (ProbeResult, error)`. Three outcomes: `ProbeOK`, `ProbeUnreachable`, `ProbeReachableBroken`. The launcher refuses to spawn over `ProbeReachableBroken`.
- `Spawner.Spawn(ctx, exec, args, logPath) → (SpawnHandle, error)`. The handle carries a PID so the readiness loop can detect child early-exit via `kill(pid, 0)` and report `ReasonChildExitedEarly` instead of timing out.
- `TrustAssessor.Assess(intent, exec) → AssessResult`. Runs only for `IntentImplicitTUI`. Non-darwin uses a no-op assessor; darwin uses `darwinAssessor`.

The launcher also takes a `Now` clock and a `LogPreSpawn` callback so unit tests can substitute time and capture diagnostics.

## Start intents

`StartIntent` distinguishes why the launcher was invoked:

- `IntentExplicitStart` — `tprompt daemon start`. Trust gate bypassed. Cooldown bypassed.
- `IntentExplicitRun` — `tprompt daemon run`. Doesn't actually drive the launcher (run is foreground), but the enum exists so pre-spawn diagnostics and the trust gate hook can match on it consistently.
- `IntentImplicitTUI` — TUI default-on auto-start. Trust gate active on darwin. Failure records a cooldown.

## Pre-spawn diagnostic

Before the detached child is spawned, the launcher appends a single logfmt line to the daemon log:

```
time=2026-05-07T02:18:34.012Z outcome=lifecycle_pre_spawn parent_pid=12345 intent=implicit_tui exec="/usr/local/bin/tprompt" socket="…" runlock="…" startlock="…" identity="…" cooldown="…" log="…" config="…" trust=allow
```

When the trust gate denies, `trust=denied reason="…"` replaces `trust=allow`. When the operator bypasses with `TPROMPT_UNSAFE_SKIP_TRUST_GATE`, `trust=allow_override reason="TPROMPT_UNSAFE_SKIP_TRUST_GATE=1 (debug override)"` is recorded. This makes spawn-time failures and bypasses visible in post-mortem.

## Readiness wait

The readiness budget is 5 s by default. The launcher polls `Status` at 50 ms intervals; on each iteration it also runs `kill(pid, 0)` against the spawn handle's PID. If the child has exited, the launcher returns `ReasonChildExitedEarly` immediately rather than burning the full budget on a dead PID.

Failure detail always includes the daemon log path so operators know where to look.

## macOS executable-trust gate

Implementation: `internal/app/lifecycle/trust_darwin.go`. Active only when `intent == IntentImplicitTUI`.

The assessor consults two macOS tools at absolute paths (`/usr/bin/codesign`, `/usr/sbin/spctl`) — both ship with the macOS base system on every supported version, and pinning the path means PATH manipulation cannot bypass the gate.

Order:

1. **`codesign --verify --strict <exec>`** — exit 0 means the signature verifies. Non-zero is `RejectInvalidSignature` (covers unsigned, tampered, signature-broken). `--deep` is intentionally not used; per `man codesign` it is for nested code in app bundles.
2. **`codesign -d -vv <exec>`** — describes the signing identity. Ad-hoc detection matches BOTH:
   - `Signature=adhoc` literal line
   - `flags=...adhoc...` substring on the `CodeDirectory` line (e.g., `flags=0x20002(adhoc,linker-signed)`)
   Either match → `RejectAdHoc`. We do NOT use "missing `Authority=`" as a fallback, because a developer-signed binary with a self-signed identity has Authority lines but is still validly signed.
3. **`spctl --assess --type execute -vv <exec>`** — Gatekeeper.
   - exit 0 → Allow.
   - exit non-zero AND stderr contains `the code is valid but does not seem to be an app` → Allow (CLI bypass; standard CLI binaries trigger this).
   - otherwise → `RejectGatekeeper`.

Quarantined and unnotarized binaries are caught by spctl as `RejectGatekeeper`. Invalid signatures are caught by step 1.

Failure detail format:

```
daemon executable "/path/to/tprompt" failed macOS trust check (<reason>). Run 'tprompt daemon start' to start the daemon explicitly, or set TPROMPT_UNSAFE_SKIP_TRUST_GATE=1 for local builds.
```

Where `<reason>` is one of: `ad-hoc signature`, `invalid signature: <stderr first line>`, `Gatekeeper rejected: <stderr first line>`, `trust tools unavailable: <which one>`.

If either tool is missing the gate fails closed with the "trust tools unavailable" reason. The macOS base system always ships these binaries; absence implies an exotic host where the user has opted out of standard tooling. Fail-closed is the right posture for a security gate; the user has an explicit recovery path through `tprompt daemon start`.

## Debug override

`TPROMPT_UNSAFE_SKIP_TRUST_GATE` short-circuits the assessor before any `codesign`/`spctl` invocation. It is honored only when set to a known-positive value (case-insensitive `1`, `true`, `yes`, `on`); other values (including `0`, `false`, `no`, `off`, garbage) leave the gate active so a typo cannot silently disable security.

When to use it:

- **Local development:** running `tprompt` built by `go build` from a working tree, where the binary is ad-hoc-signed by clang's linker.
- **Test harnesses:** the testscript suite uses it for the same reason.
- **Recovery:** when an operator knows their binary is trustworthy but the gate refuses (e.g., a release build distributed outside the App Store that hasn't been notarized yet).

When NOT to use it:

- **Normal release operation.** A signed and notarized release should sail through the gate without help.
- **Untrusted environments.** Setting this in a shared CI image effectively disables the gate for every implicit start on that image.

The launcher records the bypass in the pre-spawn diagnostic so operators reviewing the daemon log can see when and why the gate was bypassed.

## Recovery paths

When implicit auto-start is refused (trust gate, cooldown, reachable-but-broken socket), the user has two escape hatches:

1. **`tprompt daemon start`** — bypasses both the trust gate and the cooldown. Same idempotent success behavior as if the daemon was already running.
2. **`tprompt daemon run`** — also bypasses the gate. Useful when the user wants to keep the daemon in the foreground for debugging.

The TUI failure banner only suggests options the TUI command path accepts (i.e., it doesn't tell the user to retry `tprompt tui --some-flag` that doesn't exist). It mentions the daemon log path for post-mortem and points at the explicit-recovery commands by name.

## Signing/notarization expectation for releases

Release artifacts that support implicit daemon auto-start on macOS must be:

- **Validly signed** with a Developer ID Application certificate (or equivalent trusted by Gatekeeper).
- **Notarized** with Apple if distributed outside the App Store. Unnotarized release builds will hit `RejectGatekeeper` on Apple Silicon hosts.

CLI binaries (which we are) trigger the spctl "valid but not an app" CLI bypass when the signature is valid, so the gate's CLI-bypass branch is the expected happy path for our release artifacts.

For development builds (`go build`, `make build`) the user is expected to either:

- Run `tprompt daemon start` explicitly (gate bypassed).
- Set `TPROMPT_UNSAFE_SKIP_TRUST_GATE=1` for the session.

## See also

- [DECISIONS.md §33](../../DECISIONS.md) — locked decision summary.
- [EXPECTATIONS.md](../../EXPECTATIONS.md) — user-visible contract.
- [docs/commands/daemon.md](../commands/daemon.md) — `daemon start`/`run`/`status`/`stop` reference.
- [docs/commands/tui-flow.md](../commands/tui-flow.md) — TUI flow reference.
- [internal/app/lifecycle/launcher.go](../../internal/app/lifecycle/launcher.go) — launcher implementation.
- [internal/app/lifecycle/trust_darwin.go](../../internal/app/lifecycle/trust_darwin.go) — macOS trust gate.
- [internal/daemon/lifecycle/](../../internal/daemon/lifecycle/) — primitives.
