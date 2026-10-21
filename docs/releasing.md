# Releasing

The command line, the desktop app, and the phone app are released together under one version, with
one set of notes. No Apple account or certificate is involved anywhere: the macOS app is signed ad
hoc, the iPhone build is an unsigned `.ipa`, and every update is checked against a key of this
repository's own.

| Product | Tag | Workflow | Doc |
| --- | --- | --- | --- |
| The CLI, for macOS, Linux, and Windows | `cli-v<version>` | [`release-cli-desktop.yml`](../.github/workflows/release-cli-desktop.yml) | [Releasing the CLI](releasing-cli.md) |
| The desktop app, for Windows, Linux, and macOS | `desktop-v<version>` | [`release-cli-desktop.yml`](../.github/workflows/release-cli-desktop.yml) | [Releasing the desktop app](releasing-desktop.md) |
| The phone app: an Android APK and an iPhone `.ipa` | `mobile-v<version>` | [`release-mobile.yml`](../.github/workflows/release-mobile.yml) | [Releasing the phone app](releasing-mobile.md) |

## Cutting a release

1. Add a `## [<version>]` section to [`CHANGELOG.md`](../CHANGELOG.md), above the last release's.
   Its text is the notes of all three GitHub releases and of the apps' update windows.
2. From a clean `main`, run:

   ```sh
   bun run release 1.0.17          # sets the version, commits "Release 1.0.17"
   bun run release 1.0.17 --push   # the same, then pushes main and starts the release workflows on it
   ```

   The version is set in the root [`Cargo.toml`](../Cargo.toml) (the CLI, with `Cargo.lock`), the
   root [`package.json`](../package.json), [`desktop/mygo.config.ts`](../desktop/mygo.config.ts), and
   [`mobile/app.config.ts`](../mobile/app.config.ts). `--dry-run` says what would change and
   `bun run release --check` whether the four agree. The command refuses a version that is not newer
   than every current one, a missing changelog section, a dirty tree, and a tag that exists, here
   or on `origin`. Without `--push` it prints the push and the `gh workflow run` commands.
3. `release-cli-desktop.yml` and `release-mobile.yml` run on `main`'s new commit and draft the three
   releases there; each tag is made when its draft is published. A run on `main`, not on a tag,
   saves its Rust, Gradle, and Xcode caches where the next release restores them: a cache saved on
   a tag is seen by no other tag.
4. When the workflows are done, publish the drafts: the CLI's as the latest release, the others not.

   ```sh
   gh release edit cli-v1.0.17 --draft=false --latest
   gh release edit desktop-v1.0.17 --draft=false --latest=false
   gh release edit mobile-v1.0.17 --draft=false --latest=false
   ```

The repository's latest release has to be the CLI's: the install scripts download from
`releases/latest/download/`. The desktop and Android apps find their releases by tag prefix through
the GitHub API.

## Keys

Every update is signed with a key of this repository's own, kept outside the repository and as an
Actions secret. Keep a copy of each in a password manager: without it no installed copy can be
updated again.

| Key | Secret | Public half | Kept at |
| --- | --- | --- | --- |
| CLI update key (Ed25519) | `CLI_UPDATE_SIGNING_KEY` | `KEYS` in `crates/cli/src/update.rs` | `~/Library/Application Support/lorca/update-keys/cli-update.key` |
| Desktop update key (MyGo) | `MYGO_UPDATER_PRIVATE_KEY` | `updates.publicKey` in `desktop/mygo.config.ts` | MyGo's `update-keys` folder |
| Android release keystore | `ANDROID_KEYSTORE`, `ANDROID_KEYSTORE_PASSWORD` | `CERTIFICATE_SHA256` in `scripts/release-android.ts` | `~/Library/Application Support/lorca/android-keys/` |
