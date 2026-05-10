# scripts/

macOS code-signing and notarization helpers used both for local development
builds and by the release workflow.

- `sign-macos-binary.sh` — Developer ID + hardened runtime + secure timestamp.
- `notarize-macos-binary.sh` — wraps the binary in a temp zip and submits to
  Apple's notary service via `xcrun notarytool`.

Both scripts are macOS-only at runtime. The test harness (`make
test-scripts`) is portable: every external it touches — `codesign`,
`xcrun`, `ditto`, `plutil` — is stubbed via the env-var seam below, so
it runs unchanged on Linux dev/CI hosts. Operator documentation (Apple
credentials, GitHub secrets contract, release behavior) lives at
`docs/lifecycle/macos-release-signing.md` (added in AUR-325).

Four signing-toolchain entry points — `codesign`, `xcrun`, `ditto`, and
`plutil` — can be overridden via the `CODESIGN`, `XCRUN`, `DITTO`, and
`PLUTIL` env vars. Production callers leave them unset and pick up
`/usr/bin/codesign`, `/usr/bin/xcrun`, `/usr/bin/ditto`, and
`/usr/bin/plutil`. The override seam exists for the test harness in
`scripts/test/` to stub the tools so the argv and JSON-status parsing logic
can be exercised under controlled fixtures. Other tools the scripts invoke
(`mktemp`, `basename`, `tee`, `cat`, `grep`) use absolute paths and are not
overridable; they are present at `/usr/bin/*` (and `/bin/cat`) on every
supported macOS host. JSON parsing is delegated to `plutil` so callers
don't have to assume any particular formatting from
`xcrun notarytool --output-format json`.

## Tests

Run the bash test suite via:

```sh
make test-scripts
```

It is intentionally separate from `make check` so the Go health gate stays a
Go-only contract.
