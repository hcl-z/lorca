# Releasing the phone app

The phone app in `mobile/` ships as files of this repository's GitHub releases, tagged
`mobile-v<version>`: an unsigned `.ipa` for iPhone and a signed APK for Android. No Apple account or
certificate is involved. The version is `version` in
[`mobile/app.config.ts`](../mobile/app.config.ts), on both platforms. An iOS build carries the time
it was made, in UTC, as its build number; Android orders its builds by a versionCode made from the
version (1.2.3 is 1002003), so each Android release is a new version.

A run of the release workflow on `main` builds both on GitHub Actions:

```sh
gh workflow run release-mobile.yml --repo hcl-z/lorca --ref main
```

- Workflow: [`.github/workflows/release-mobile.yml`](../.github/workflows/release-mobile.yml).
- iOS: [`scripts/release-ios.ts`](../scripts/release-ios.ts) (`bun run release-ios`).
- Android: [`scripts/release-android.ts`](../scripts/release-android.ts) (`bun run release-android`).
- The Android updater: [`mobile/src/core/updates.ts`](../mobile/src/core/updates.ts) and
  `Updater.kt` in [`mobile/modules/lorca-core`](../mobile/modules/lorca-core/android/src/main/java/app/lorca/core/Updater.kt).

## How the Android app updates

A release holds `lorca-<version>-android.apk`. The repository's latest release stays the CLI's,
which `install-cli.sh` downloads, so the app reads the `mobile-v` tags through the GitHub API and
asks for the newest one's release; a draft or a prerelease is passed over. Once a day, at launch
or back in the foreground, the release build checks, and offers a newer version in an alert with
its notes: Install Update, Remind Me Later, Skip This Version. Settings › Updates checks at once
or installs what a check found, and holds the switch for the daily check.

Installing downloads the APK, checks it against the size and SHA-256 that GitHub lists for the
asset, checks that it is `app.lorca` and newer than the running build, and hands it to Android's
package installer. Android installs it only when it is signed with the installed app's key, and
then stops the app and replaces it; the user opens it again. The first time, Android asks the user
to let Lorca install apps, then to confirm the update. From Android 12 an app that may install
apps and updates itself is not asked to confirm again (`UPDATE_PACKAGES_WITHOUT_USER_ACTION`); some
phones, Xiaomi's among them, ask every time. Apart from that, on phones with Google Play, Play
Protect may stop an APK it has not seen before, a first install or an update, with "App scan
recommended": Scan app, or Install without scanning under More details.

Lorca Dev never updates, nor do iOS builds.

## The iPhone build

`release-ios` archives the app with code signing off and packs it as
`dist/ios/lorca-<version>-ios.ipa`. iOS installs only signed apps, so a person installs the file by
signing it with their own Apple ID through a sideloading tool such as AltStore or Sideloadly; with a
free Apple ID the signature lasts seven days and the tool renews it. A new release is installed
over the old one the same way. The unsigned build has none of the entitlements the App Store build
had, so remote push notifications (APNs) do not reach a sideloaded app.

## One-time setup

The workflow reads these Actions secrets (Settings ▸ Secrets and variables ▸ Actions), for Android;
the iOS job needs none. The Android job stops before building when one is missing.

| Secret | What |
| --- | --- |
| `ANDROID_KEYSTORE` | The Android release keystore, base64 |
| `ANDROID_KEYSTORE_PASSWORD` | Its password |

### Android release key

The release key is a PKCS12 keystore with one key, alias `lorca`, whose certificate's SHA-256 is
`CERTIFICATE_SHA256` in [`scripts/release-android.ts`](../scripts/release-android.ts):

```
dcc11cdcb6bbf9602cec70b39455984ed24f1d92fac3bd84b31e5e4dfa9a5751
```

`release-android` refuses an APK signed by any other key, since no installed app would take it.
The key in use was made on the maintainer's Mac and is kept at
`~/Library/Application Support/lorca/android-keys/release.keystore`, with its password in
`release.keystore.password` beside it, where `release-android` finds both (`ANDROID_KEYSTORE` and
`ANDROID_KEYSTORE_PASSWORD` name others). Keep a copy of both in a password manager. Without the
key no installed app can be updated again: each would have to be uninstalled, which deletes its
account and chats, and installed afresh.

```sh
keys="$HOME/Library/Application Support/lorca/android-keys"
base64 -i "$keys/release.keystore" | gh secret set ANDROID_KEYSTORE
gh secret set ANDROID_KEYSTORE_PASSWORD < "$keys/release.keystore.password"
```

It was made with OpenSSL, no JDK needed (`keytool -genkeypair -keystore release.keystore
-storetype PKCS12 -alias lorca -keyalg RSA -keysize 4096 -validity 36500` makes an equivalent one):

```sh
pw=$(openssl rand -hex 24); printf '%s' "$pw" > release.keystore.password
openssl req -x509 -newkey rsa:4096 -nodes -keyout key.pem -out cert.pem -days 36500 -subj "/CN=Lorca/O=Lorca"
openssl pkcs12 -export -inkey key.pem -in cert.pem -name lorca -out release.keystore -passout "pass:$pw" \
  -certpbe PBE-SHA1-3DES -keypbe PBE-SHA1-3DES -macalg sha1
openssl x509 -in cert.pem -outform DER | openssl dgst -sha256   # CERTIFICATE_SHA256
rm key.pem cert.pem
```

### Android developer verification

Certified Android devices install apps from outside Google Play only from verified developers:
enforced in Brazil, Indonesia, Singapore, and Thailand from September 30, 2026, and everywhere in
2027. Register `app.lorca` in the [Android Developer Console](https://developer.android.com/developer-verification/guides/android-developer-console)
with the certificate's SHA-256 above, once the developer account is verified.

## Cutting a release

1. Run `bun run release <version>` ([Releasing](releasing.md)): it sets `version` in
   [`mobile/app.config.ts`](../mobile/app.config.ts) with the CLI's and the desktop app's, and
   commits. The version's section of the root [`CHANGELOG.md`](../CHANGELOG.md) becomes the
   release's notes and the update alert's text, and the release stops without it.
2. With `--push` it pushes `main` and starts the workflow on it; you can also run
   **Release phone app** by hand from the Actions tab. It drafts the release on the commit it runs
   on, and publishes it once both platforms it was asked for have finished, which makes the tag. A
   run on a commit other than the one an existing `mobile-v<version>` tag names is refused, and so
   is a version whose release is already published.
3. The workflow drafts `mobile-v<version>`, then builds the Android APK on Ubuntu and the iPhone
   `.ipa` on a macOS runner side by side, each uploading into the draft. When both finish, a final
   job publishes the draft without making it the latest release, which stays the CLI's. Runs on
   `main` keep the Rust core's build, Gradle's caches, and for iOS Xcode's compilation cache and
   the installed pods for the next release. The iPhone build takes the device slice, and the APK is
   arm64 only, what phones run; the dev loop still builds the simulator slice and x86_64 for the
   emulator. Android apps see the release at their next daily check.

A run by hand can release one platform:

```sh
gh workflow run release-mobile.yml --repo hcl-z/lorca --ref main -f platforms=iOS
gh workflow run release-mobile.yml --repo hcl-z/lorca --ref main -f platforms=Android
```

## From a Mac

`bun run release-ios` writes `dist/ios/lorca-<version>-ios.ipa`, and `bun run release-android`
writes `dist/android/lorca-<version>-android.apk`, signed with the key in the folder above; upload
either to the draft with `gh release upload mobile-v<version> <file>`. `release-android` needs the
Android SDK (`ANDROID_HOME`, else `~/Library/Android/sdk`) with an NDK, `cargo-ndk`, the Rust
targets `aarch64-linux-android` and `x86_64-linux-android`, and a JDK 17 or later (`JAVA_HOME`,
else Android Studio's). `release-ios` needs Xcode, CocoaPods, and the Rust targets
`aarch64-apple-ios` and `aarch64-apple-ios-sim`. Each script generates the production project in a
copy (`dist/ios/mobile`, `dist/android/mobile`), so `mobile/ios` and `mobile/android` stay the dev
loop's Lorca Dev projects.

To try an update, install an older release's APK, then publish a newer one and choose
Settings › Updates › Check for Updates.
