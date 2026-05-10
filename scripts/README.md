# scripts/

macOS code-signing and notarization helpers used both for local development
builds and by the release workflow.

- `sign-macos-binary.sh` — Developer ID + hardened runtime + secure timestamp.
- `notarize-macos-binary.sh` — wraps the binary in a temp zip and submits to
  Apple's notary service via `xcrun notarytool`.

Both scripts are macOS-only at runtime. Operator documentation (Apple
credentials, GitHub secrets contract, release behavior) lives at
`docs/lifecycle/macos-release-signing.md` (added in AUR-325).

Three signing-toolchain entry points — `codesign`, `xcrun`, and `ditto` — can
be overridden via the `CODESIGN`, `XCRUN`, and `DITTO` env vars. Production
callers leave them unset and pick up `/usr/bin/codesign`, `/usr/bin/xcrun`,
and `/usr/bin/ditto`. The override seam exists for the test harness in
`scripts/test/` to stub the tools so the argv and JSON-status parsing logic
can be exercised on Linux CI runners. Other tools the scripts invoke
(`mktemp`, `basename`, `sed`, `head`, `tee`, `cat`, `grep`) use absolute
paths and are not overridable; they are present at `/usr/bin/*` (and
`/bin/cat`) on every supported macOS and Linux host.

## Tests

Run the bash test suite via:

```sh
make test-scripts
```

It is intentionally separate from `make check` so the Go health gate stays a
Go-only contract.
