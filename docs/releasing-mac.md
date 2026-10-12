# Releasing the Mac app

Lorca.app updates itself with [Sparkle](https://sparkle-project.org). The app carries no Apple
certificate: it is signed ad hoc and never notarized. Releases are assets of one GitHub release of
this repository, `mac-releases`, which is never the repository's latest release (that stays the
CLI's): a `.dmg` for a first download, a `.zip` Sparkle installs (with binary deltas from recent
versions), and `appcast.xml`, the feed the app polls at
`https://github.com/hcl-z/lorca/releases/download/mac-releases/appcast.xml`. Sparkle checks every
archive against the EdDSA public key inside the app before it installs. One command produces all of
it, from a Mac or on GitHub Actions:

```sh
bun run release-mac 0.2.0                          # from a Mac
git tag mac-v0.2.0 && git push origin mac-v0.2.0   # on GitHub Actions
```

- Updater: [`macos/Sources/Lorca/App/Updater.swift`](../macos/Sources/Lorca/App/Updater.swift).
  **Check for Updates…** in the app menu, and the Updates rows in Settings ▸ General.
- Bundle: [`scripts/app.ts`](../scripts/app.ts) writes the version, `SUFeedURL`, and
  `SUPublicEDKey` into Info.plist, embeds Sparkle.framework, and signs.
- Release: [`scripts/release-mac.ts`](../scripts/release-mac.ts),
  [`scripts/generate-appcast.ts`](../scripts/generate-appcast.ts),
  [`scripts/changelog.ts`](../scripts/changelog.ts).
- Workflow: [`.github/workflows/release-mac.yml`](../.github/workflows/release-mac.yml).

## One-time setup

The scripts run on [Bun](https://bun.sh). The disk image comes from
[`create-dmg`](https://github.com/create-dmg/create-dmg) and uploads go through the
[GitHub CLI](https://cli.github.com), signed in with `gh auth login` (or `GH_TOKEN`):

```sh
brew install create-dmg gh
```

Sparkle's command-line tools arrive with the package. `swift package resolve` in `macos/` (or any
build) puts them in `macos/.build/artifacts/sparkle/Sparkle/bin`.

### Sparkle signing key

Every update is signed with an Ed25519 key. The private half is the Actions secret
`SPARKLE_PRIVATE_KEY`, base64 of the 32-byte seed, which is what `generate_keys -x` exports, and
`generate_appcast` reads it with `--ed-key-file` (`SPARKLE_KEY_FILE` names the file). Its public
half is `SPARKLE_PUBLIC_KEY` in [`scripts/app.ts`](../scripts/app.ts), which ships inside the app.
The workflow stops when the secret is not the key that constant names. To make a key without
Sparkle's tools:

```sh
openssl genpkey -algorithm ed25519 -out sparkle.pem
openssl pkey -in sparkle.pem -outform DER | tail -c 32 | base64 > sparkle_private_key.txt   # the secret
openssl pkey -in sparkle.pem -pubout -outform DER | tail -c 32 | base64                     # SPARKLE_PUBLIC_KEY
gh secret set SPARKLE_PRIVATE_KEY < sparkle_private_key.txt
```

Keep a copy of `sparkle_private_key.txt` in a password manager. Without it no install in the field
can be updated again. A Mac that releases by hand points `SPARKLE_KEY_FILE` at that file.

## Cutting a release

1. Add a `## [0.2.0]` section at the top of [`CHANGELOG.md`](../CHANGELOG.md). The heading matches
   the version; the section becomes the notes Sparkle shows in the update window.
2. Run it:

   ```sh
   bun run release-mac 0.2.0
   ```

   The argument bumps `"version"` in the root [`package.json`](../package.json). Without an
   argument the script releases the version already there.
3. Commit `package.json` and `CHANGELOG.md`.

The script:

1. calls `buildApp("release")` from `scripts/app.ts`, the same build as `bun run build`: `cargo build
   --release` for the `lorca` CLI and the Markdown library, `swift build -c release` for the app,
   the CLI copied into `Contents/Resources/bin`, Sparkle embedded, and everything signed ad hoc
   (`macos/.build/bundle/release/Lorca.app`);
2. packs the app into `Lorca-<version>.dmg`, unsigned;
3. pulls the 3 most recent archives and the published `appcast.xml` from the release, so
   `generate_appcast` can build deltas and keep the older entries;
4. zips the app as `Lorca-<version>.zip`, writes the changelog section beside it as
   `Lorca-<version>.md`, and regenerates `appcast.xml`, signing with the Sparkle key;
5. uploads the image, the archives, the deltas, and the notes to the release `mac-releases`, made on
   the first release, and `appcast.xml` last.

Everything is staged under `dist/mac/`.

To test an update, keep an older build in /Applications and choose **Check for Updates…**.
`bun run release-mac --local` stops after step 2: an app and `.dmg` to try on another Mac, with
nothing published.

## On GitHub Actions

The Release Mac app workflow runs `bun run release-mac` on a `macos-26` runner (Apple silicon, the
macOS 26 SDK). A tag `mac-v<version>` starts it; the tag has to name `"version"` in the root
`package.json`, and `CHANGELOG.md` needs that version's section. So the version bump and the notes
are a commit first:

1. Set `"version"` in `package.json` and add the `## [0.2.0]` section to `CHANGELOG.md`; commit and
   push.
2. Tag that commit:

   ```sh
   git tag mac-v0.2.0 && git push origin mac-v0.2.0
   ```

Actions ▸ Release Mac app ▸ Run workflow does the same on the commit it runs on. With **publish**
off, it stops after packing the image, as `--local` does, and keeps `Lorca-<version>.dmg` as the
run's artifact. A version already in the release fails the run before the build.

The workflow's own token uploads, and the one secret it reads is `SPARKLE_PRIVATE_KEY`; it stops
before building when that is missing.

### Options

| Env | Default | Purpose |
| --- | --- | --- |
| `RELEASES_REPO` | `hcl-z/lorca` | repository whose release `mac-releases` holds the files |
| `SPARKLE_KEY_FILE` | | the Sparkle private key file |
| `SPARKLE_BIN` | the SwiftPM copy | directory holding `generate_appcast` |
| `DOWNLOAD_URL_PREFIX` | the release's download URL | base URL of the appcast's links, and of `SUFeedURL` |
| `HISTORY_COUNT` | `3` | recent archives pulled for deltas |
| `FORCE=1` | | replace a version that is already published |
| `NO_HISTORY=1` | | skip the old archives: a full download, no deltas |

## Notes

- **First open.** The disk image is downloaded with the quarantine flag, and Gatekeeper refuses an
  app without a Developer ID. Drag Lorca to Applications, try to open it once, then allow it under
  System Settings ▸ Privacy & Security ▸ Open Anyway; or clear the flag with
  `xattr -dr com.apple.quarantine /Applications/Lorca.app`. Updates Sparkle installs are not
  quarantined, so only the first launch asks.
- **One version.** `"version"` in the root `package.json` is what `scripts/app.ts` writes into both
  `CFBundleShortVersionString` and `CFBundleVersion`.
  Sparkle compares `CFBundleVersion`, so it goes up with every release.
- **The update replaces the whole bundle**, the CLI in `Contents/Resources/bin` included. The app
  stops its CLI when it quits for the install and starts the new one on relaunch.
- **Sparkle lives in the bundle.** SwiftPM links against the framework under
  `macos/.build/artifacts`; `buildApp` copies it into `Contents/Frameworks`, which the
  `@executable_path/../Frameworks` rpath in `Package.swift` resolves against. Debug bundles carry it
  too, since the binary links it.
- **Automatic checks are on** (`SUEnableAutomaticChecks`), so Sparkle never asks for permission on
  the second launch. Settings ▸ General ▸ Updates turns them off, and has the switch for installing
  updates without asking.
- **A debug build never updates.** `Updater.isEnabled` is false under `DEBUG`: the menu item and
  the settings rows leave themselves out of the dev loop's bundle.
- **The build is for the Mac that runs it.** `cargo build` and `swift build` target the host
  architecture, so a release cut on Apple silicon, the workflow's runner included, is an arm64
  app.
- Old archives stay in the release, so an install far behind still has a full archive to move to.
