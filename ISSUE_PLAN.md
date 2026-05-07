# ISSUE_PLAN — AUR-264 Split foreground `daemon run`

## Goal

Promote `daemon run` from an alias of `daemon start` to its own
subcommand. `daemon run` is the explicit foreground daemon loop. It
inherits the run-lock + identity-sidecar behavior introduced in AUR-263,
so it refuses to start when a compatible daemon already owns the
configured socket. `daemon start` keeps its current foreground behavior
in this commit; AUR-265 turns it into the explicit background launcher.

This intermediate landing keeps `make check` green: both `daemon start`
and `daemon run` are foreground commands invoking the same handler, so
existing testscripts that background `daemon start &` continue to work.
The interim user-visible difference is only "`run` is no longer an alias
of `start` and shows up in `--help`."

## Files

Edit:

- `internal/app/commands.go`:
  * Rename `runDaemonStart` → `runDaemonForeground` (handler shared by
    `daemon run` and, until AUR-265, `daemon start`).
  * Drop `Aliases: []string{"run"}` from the `start` cobra command.
  * Add a sibling `run` cobra command with its own help text:

    ```text
    run     Run the daemon in the foreground listening on the socket.
    ```

  * Update the parent `daemon` command's `Long` description to
    distinguish `start` (still foreground in this commit; backgrounded
    in AUR-265) from `run` (foreground).
- `internal/app/help_test.go` — the existing assertion already mentions
  the daemon subcommands; refresh expected help text.
- `internal/app/root_test.go` — the existing test that `run` resolves to
  `start` via the alias inverts: `run` is now its own subcommand.

## Tests

* New `daemon run` subcommand exists and prints distinct help.
* `daemon run` no longer appears as an alias on `daemon start`.
* `daemon run` invokes the foreground handler and refuses to start when
  a compatible daemon already owns the socket (covered by the AUR-263
  run-lock test path, just exercised through `daemon run` instead of
  `daemon start`).

## Out of scope

* Background-mode `daemon start` (AUR-265).
* The shared lifecycle launcher (AUR-265).
* Testscript migrations (AUR-269).
