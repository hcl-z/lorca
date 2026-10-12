# Repo layout and development

Where each crate and app lives in the repository, and the development loops and builds that run them. The desktop app's own loop and builds are in [Desktop app](desktop-app.md). Releases: [docs/releasing.md](../releasing.md).

## Layout

```
lorca/
  ARCHITECTURE.md      # the overview and the list of subjects
  README.md
  docs/architecture/   # one doc per subject
  docs/agent/          # lorca-agent's own documentation
  Cargo.toml           # workspace
  crates/agent/        # lorca-agent: loop, tools, codemode (QuickJS), and Messages, Chat Completions, Responses, ChatGPT, and Grok providers
  crates/models/       # lorca-models: the model catalog (catalog.json: windows, thinking levels, rates), on every Device and in every app's pickers; lorca.app serves it so Devices update without a release
  crates/provider-auth/ # OAuth token types and PKCE flows shared by every Device
  crates/tls/          # lorca-tls: the certificate trust of every Device's HTTPS, the system's on macOS and Windows
  crates/cli/          # lorca: the Device core as a library (keys, relay sync, jobs, the JSON API) + runner and server features + the binary
  crates/mobile/       # lorca-mobile: the core for the phone over UniFFI
  crates/markdown/     # lorca-markdown: message Markdown as the blocks and spans every app renders (pulldown-cmark, and GitHub's autolinks for bare URLs and addresses), for the phone over UniFFI
  crates/relay/        # lorca-relay: axum + SQLite or Postgres, and its Dockerfile
  desktop/             # the desktop app: Go on MyGo's native UI; the build bundles the CLI
  mobile/              # Expo app for iOS and Android: a paired Device over the core (modules/lorca-core)
  web/                 # the site
  scripts/             # bun scripts: the one-command release, the desktop and phone dev loops and builds, their releases, string and doc checks
  .github/workflows/   # release-cli.yml, release-desktop.yml, and release-mobile.yml: release builds; test.yml: every app's and crate's tests on each pull request; docs.yml: the doc check
```

## Development loops and builds

`bun run android` rebuilds the Rust core for Android, then builds and runs the Expo dev client on the Android emulator. `cd mobile && bun run core` rebuilds the Rust core for both phone platforms; `bun run mobile:dev` is the iOS development loop described below.

`bun run desktop` is the desktop app's loop and `bun run desktop:build` builds its release bundles ([Desktop app](desktop-app.md)). `bun run relay` runs a local relay. `bun run mobile:dev` (`scripts/mobile.ts`) is the Lorca Dev phone loop on the iOS Simulator, or on `--device <name or udid>`: it fingerprints the crates the phone links, the prebuild inputs (Expo config, assets, `package.json`, plugins, targets), the pod inputs with the checkout's path, and the native module sources (stamps in `mobile/.expo/dev-stamps.json`), rebuilds what is stale (`bun run core ios`, a clean `expo prebuild`, `pod install`, `expo run:ios`), starts Metro, and opens the dev client on it. A Rust save while it runs rebuilds the core and installs the app again. The desktop and phone loops leave production Lorca processes alone, so all builds run side by side.
