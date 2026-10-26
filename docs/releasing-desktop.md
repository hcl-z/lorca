# Releasing the desktop app

The app in `desktop/`, for Windows, Linux, and macOS, updates itself through MyGo's updater plugin. Its releases are releases of
this repository tagged `desktop-v<version>`. The repository's latest release stays the CLI's, which
`install-cli.sh` and `install-cli.ps1` download, and the app finds the newest `desktop-v` release
that is neither a draft nor a prerelease through the GitHub API (`updates.tagPrefix`). Each release
holds, for each platform, the installer (`Lorca Setup <version>.exe`, the Debian package, the
macOS disk image `Lorca <version>.dmg`), the app as an archive (`lorca-<version>-windows-amd64.tar.gz`), delta updates from the last three versions,
the Linux `install.sh`, and `update-<platform>.json`, the manifest the app checks. Installed apps
accept only archives and deltas signed with the update key, whose public half is
`updates.publicKey` in [`desktop/mygo.config.ts`](../desktop/mygo.config.ts). A run of the release
workflow on `main` builds Windows x64, Linux x64, Linux arm64, and macOS on Apple silicon on GitHub
Actions into a draft release:

```sh
gh workflow run release-cli-desktop.yml --repo hcl-z/lorca --ref main
```

- Updater: [`desktop/updater.go`](../desktop/updater.go). **Check for Updates…** in File and Help,
  and the Updates rows in Settings › General.
- Configuration: `updates` in [`desktop/mygo.config.ts`](../desktop/mygo.config.ts).
- Release: [`scripts/desktop.ts`](../scripts/desktop.ts), which runs `go tool mygo build -upload`, and
  [`.github/workflows/release-cli-desktop.yml`](../.github/workflows/release-cli-desktop.yml), which
  releases the CLI from the same workflow, in jobs of its own ([Releasing the CLI](releasing-cli.md)). MyGo's
  [auto-updates guide](https://github.com/egoist/mygo/blob/main/docs/updates.md) covers what it
  signs and uploads.

## One-time setup

### 1. Update key

`go tool mygo keygen` (run in `desktop/`) writes the key pair to MyGo's folder in the user's configuration
directory: `%APPDATA%\mygo\update-keys` on Windows, `~/Library/Application Support/mygo/update-keys`
on macOS, `~/.config/mygo/update-keys` on Linux. `mygo-update.pub` is `updates.publicKey`;
`mygo-update.key` is the secret. Keep a copy in a password manager, and put it in that folder on
every computer that releases, or in `MYGO_UPDATER_PRIVATE_KEY`, which `release-desktop` prefers.

Without the secret key, no install in the field can be updated again.

### 2. Secret

The workflow signs with the Actions secret `MYGO_UPDATER_PRIVATE_KEY` (Settings ▸ Secrets and
variables ▸ Actions), the contents of `mygo-update.key`, and uploads with the workflow's own
token. On a computer, `release-desktop` uses the GitHub CLI's login (or `GH_TOKEN`) and the key in
MyGo's folder (or `MYGO_UPDATER_PRIVATE_KEY`). It stops before building when either is missing.

### 3. macOS signing

The macOS app is signed ad hoc: no certificate, no notarization, and nothing to set up. `mygo.config.ts` signs with a named identity
and notarizes only when `SIGN_IDENTITY` and `NOTARY_PROFILE` are set, which a release leaves unset.
The first launch of a downloaded app needs Privacy & Security ▸ Open Anyway, or
`xattr -dr com.apple.quarantine "/Applications/Lorca.app"`; updates the app installs for itself do
not ask. The macOS build needs a Mac, which the workflow's `macos-26` runner is.

## Cutting a release

The version is `version` in [`desktop/mygo.config.ts`](../desktop/mygo.config.ts), shared with the
CLI and the phone app, and its notes are the version's section of the root
[`CHANGELOG.md`](../CHANGELOG.md) (`updates.changelog`): the section becomes the notes of the
release and of the update window, and the release stops without it.

1. Run `bun run release <version>` ([Releasing](releasing.md)). It sets the version and commits;
   with `--push` it pushes `main` and starts **Release CLI and desktop** on it. You can also run
   the workflow by hand from the Actions tab on the branch to release
   (`gh workflow run release-cli-desktop.yml --repo hcl-z/lorca --ref main`). It drafts the release
   on its commit, and publishing the draft makes the tag. A run on a commit other than the one an
   existing `desktop-v<version>` tag names is refused.
2. When the workflow is done, check the draft `desktop-v<version>` and publish it without making
   it the latest release:

   ```sh
   gh release edit desktop-v0.1.0 --draft=false --latest=false
   ```

   On the release's page, untick **Set as the latest release** before **Publish release**. The
   apps find it by its tag.

The **Release CLI and desktop** workflow names the release after `desktop/mygo.config.ts`, drafts
it with the changelog's section as its notes (not as the latest release), then builds
`windows/amd64`, `linux/amd64`, and `linux/arm64` side by side on Ubuntu and `darwin/arm64` on a
macOS runner. Each platform's job builds the CLI's release first, then runs
`bun run release-desktop <platform>`, whose CLI build takes the dependencies the first one compiled:
cargo-zigbuild builds the Linux and Windows CLIs, cargo the macOS one, NSIS the Windows installer,
and MyGo signs the macOS app ad hoc and makes its disk image. Each job's Rust cache is saved on
`main` for the next release. A platform that fails leaves the others to finish; re-run its job,
which uploads into the same draft. A version already published is refused.

`bun run release-desktop [platforms]` releases from this computer too, into the same draft.
Platforms are MyGo's, comma separated; the default is this computer's, or `darwin/arm64`,
`linux/amd64`, and `windows/amd64` from a Mac. `darwin/arm64` builds only on a Mac. The script:

1. refuses a version whose release is already published, unless `FORCE=1`, which replaces its
   files;
2. builds the CLI for each platform into `desktop/resources/<goos>-<goarch>/bin`, as
   `bun run desktop:build` does;
3. runs `go tool mygo build -platform … -upload`, which builds the apps and installers into
   `desktop/build`, reads the newest published release's manifests and archives to make delta
   updates, signs the archives and deltas, and uploads everything to the release
   `desktop-v<version>`, which it drafts, not as the latest release, when there is none.

To test an update, install an older release with its installer and choose **Check for Updates…**.

## Notes

- **Where apps update.** On macOS, `Lorca.app` wherever it was dragged from the disk image. On
  Windows, the per-user install of the installer, in `%LOCALAPPDATA%\Programs`. On Linux, the
  install of a release's `install.sh`
  (`curl -fsSL https://github.com/hcl-z/lorca/releases/download/desktop-v0.1.0/install.sh | sh`),
  which installs the newest release in `~/.local/lorca.app`. The Debian package installs in
  `/opt`, where the app cannot write: it leaves the menu item and the Settings rows out, and the
  next package updates it.
- **A development build never updates.** `mygo dev`'s Lorca Dev leaves the menu item and the
  Settings rows out.
- **The GitHub API allows 60 requests an hour** from an address without a token; an app checks
  once a day.
- **Old releases stay**, so the next release can make deltas from their archives.
