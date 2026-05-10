# macOS release signing

`tprompt` macOS release binaries are Developer ID signed, hardened-runtime
enabled, secure-timestamped, and accepted by Apple's notary service before
publishing.

## Why this matters

Signing is part of the daemon lifecycle policy, not just release polish.
A signed and notarized release passes the macOS trust gate (see
[auto-start.md](auto-start.md#macos-executable-trust-gate)) so an
implicit TUI auto-start can spawn the daemon without prompting the user
to run `tprompt daemon start` explicitly. Ad-hoc and locally-built
binaries trip the trust gate by design — that's what the gate is for.

For local development, prefer foreground `tprompt daemon run` rather
than detached background startup. The trust gate refuses ad-hoc
binaries on the implicit auto-start path, which is the right behavior;
`daemon start` is the explicit-intent escape hatch.

## Local signing

Prerequisites:

- A valid `Developer ID Application` certificate with the private key in
  the login keychain.
- A notarytool keychain profile, created once. Generate an
  app-specific password at <https://appleid.apple.com> first:

```sh
xcrun notarytool store-credentials tprompt-notary \
  --apple-id "APPLE_ID_EMAIL" \
  --team-id "APPLE_TEAM_ID" \
  --password "APP_SPECIFIC_PASSWORD"
```

Omitting `--password` makes notarytool prompt interactively, which
silently hangs in non-interactive shells. The profile name
`tprompt-notary` is the script default; if you stash it under a
different name, pass `TPROMPT_NOTARY_PROFILE=<name>` to the notarize
script. The profile lands in your default login keychain unless you
also pass `--keychain` to `store-credentials`; if you do, set
`TPROMPT_NOTARY_KEYCHAIN=<path>` when running the notarize script.

Sign a local binary:

```sh
TPROMPT_CODESIGN_IDENTITY="Developer ID Application: Hunter Sadler (79S467K965)" \
  scripts/sign-macos-binary.sh dist/tprompt-darwin-arm64/tprompt
```

Submit the signed binary for notarization:

```sh
TPROMPT_APPLE_TEAM_ID=79S467K965 \
  scripts/notarize-macos-binary.sh dist/tprompt-darwin-arm64/tprompt
```

The notarization helper wraps the CLI in a temporary zip because
`notarytool` submits archives. Bare CLI binaries and zip archives
cannot be stapled; the notary ticket is associated with the signed code
hash and Gatekeeper looks it up online at first launch.

After local sign + notarize, verify the binary:

```sh
codesign --verify --strict --verbose=4 dist/tprompt-darwin-arm64/tprompt
spctl --assess --type execute -vv dist/tprompt-darwin-arm64/tprompt
```

`spctl` may reject with the message
`valid but does not seem to be an app` — that is the documented CLI
binary bypass; the signature is still valid. Any other denial is a
real failure.

## GitHub Actions secrets

The release workflow (`.github/workflows/release.yml`) signs and
notarizes only the `darwin/arm64` artifact. Configure these
repository secrets:

- `APPLE_DEVELOPER_IDENTITY` — signing identity name, e.g.
  `Developer ID Application: Hunter Sadler (79S467K965)`.
- `APPLE_DEVELOPER_ID_CERTIFICATE_BASE64` — base64-encoded `.p12`
  export of the Developer ID certificate and private key.
- `APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD` — password used when
  exporting the `.p12`. Set the secret to an empty value only if the
  `.p12` was exported without a password.
- `APPLE_KEYCHAIN_PASSWORD` — random CI-only password used for the
  temporary keychain.
- `APPLE_ID` — Apple ID email for notarization.
- `APPLE_APP_SPECIFIC_PASSWORD` — app-specific password for `APPLE_ID`.
- `APPLE_TEAM_ID` — Apple Developer Team ID, e.g. `79S467K965`.

### Exporting the `.p12`

1. Open Keychain Access.
2. Select the `login` keychain and the `My Certificates` category.
3. Expand `Developer ID Application: …` and confirm the private key
   is nested underneath the certificate. Exporting only the
   certificate is not enough.
4. Select the certificate row and its private key, then choose
   `File > Export Items…`.
5. Save as `DeveloperIDApplication.p12` and set an export password
   unless you intentionally want an empty `.p12` password. Store that
   password as `APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD`; for an
   empty-password export, set the GitHub secret to an empty value.

### Verifying the export

Verify the exported file contains a usable signing identity before
adding it to GitHub:

```sh
# Atomic mkdir -p sidesteps the TOCTOU window that `mktemp -u`
# (filename-only) would otherwise leave between path generation and
# create-keychain.
tmp_dir="$(mktemp -d)"
tmp_keychain="$tmp_dir/tprompt-signing.keychain-db"
security create-keychain -p test-password "$tmp_keychain"
security unlock-keychain -p test-password "$tmp_keychain"
security import DeveloperIDApplication.p12 \
  -P "P12_EXPORT_PASSWORD" \
  -A \
  -t cert \
  -f pkcs12 \
  -k "$tmp_keychain"
security find-identity -v -p codesigning "$tmp_keychain"
security delete-keychain "$tmp_keychain"
rm -rf "$tmp_dir"
```

The output should include the same identity used in
`APPLE_DEVELOPER_IDENTITY`.

### Encoding for the secret

```sh
base64 -i DeveloperIDApplication.p12 | pbcopy
```

Use the clipboard contents as `APPLE_DEVELOPER_ID_CERTIFICATE_BASE64`.
Do not commit the `.p12`, the base64 output, or the export password
to the repo.

## Release behavior

For macOS releases on a `v*` tag push, `.github/workflows/release.yml`:

1. **Verify**: asserts the tag base (everything before the first `-`,
   so `v0.1.0-rc1` → `0.1.0`) matches the `VERSION` file.
2. **Build matrix** in parallel on native runners
   (`darwin/arm64`, `linux/amd64`, `linux/arm64`), injecting the
   version via `-ldflags -X` and stripping debug info.
3. **macOS-only**: imports the Developer ID certificate into a
   temporary keychain, signs via `scripts/sign-macos-binary.sh`,
   stores `tprompt-notary` credentials scoped to that keychain, and
   notarizes via `scripts/notarize-macos-binary.sh`. The notarize
   step's combined output is tee'd to `$RUNNER_TEMP/notarize.log`
   and uploaded as an artifact on failure (the script's EXIT trap
   would otherwise nuke its workdir before the upload runs).
4. **macOS-only**: re-runs `codesign --verify --strict --verbose=4`
   and `spctl --assess` to gate the release. Anything other than
   acceptance or the documented CLI bypass blocks publishing.
5. **Package**: tarballs each binary as `tprompt-<os>-<arch>.tar.gz`.
6. **Release**: downloads all artifacts, generates `SHA256SUMS`, and
   publishes a **draft** release via `softprops/action-gh-release`.
   The operator publishes the draft manually after verifying the
   tarballs locally — `draft: true` is intentional so a flaky
   notarization run can't auto-publish a half-broken release.

Linux artifacts are unsigned and pass through without any Apple
steps. Intel macOS users build from source until a future release
adds a `darwin/amd64` matrix row.

### Testing the workflow before tagging

The workflow exposes `workflow_dispatch` for testing the
build/sign/notarize path on a feature branch without publishing.
Trigger it via the GitHub UI; the release job is unconditionally
gated on `github.event_name == 'push'`, so a dispatch run produces
the artifacts (downloadable from the Actions run page) but never
publishes. Tag-push is the only sanctioned publish path.

### Force-pushed tags

`concurrency: cancel-in-progress: false` lets in-flight notarization
finish on a force-pushed tag rather than killing it mid-submission.
When the second run completes, `softprops/action-gh-release`
overwrites the draft's assets — the second run wins, which is the
correct semantics for a force-pushed tag re-release.

## Install path

Tagged releases are installable via [mise](https://mise.jdx.dev/) (which
uses [ubi](https://github.com/houseabsolute/ubi) under the hood):

```sh
mise use -g ubi:aurokin/tprompt@latest
```

Or pin a specific version (e.g. `mise use -g ubi:aurokin/tprompt@v0.1.0`).

Or download a tarball from the GitHub Releases page and verify the
SHA256:

```sh
# macOS (built-in via Perl)
shasum -a 256 -c SHA256SUMS

# Linux (GNU coreutils)
sha256sum --check SHA256SUMS
```

Both paths require the release to be **published** — the workflow
ships drafts (see step 6 above), so a freshly-built draft tag is not
visible to anonymous fetchers until the operator publishes it.

## References

- [scripts/sign-macos-binary.sh](../../scripts/sign-macos-binary.sh) — local signing wrapper.
- [scripts/notarize-macos-binary.sh](../../scripts/notarize-macos-binary.sh) — local notarization wrapper.
- [.github/workflows/release.yml](../../.github/workflows/release.yml) — release pipeline.
- [auto-start.md](auto-start.md) — daemon lifecycle and trust-gate semantics.
