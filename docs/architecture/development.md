# Repo layout and development

Where each crate and app lives in the repository, the development loops and builds that run them, and how a change goes from a branch to a release. The desktop app's own loop and builds are in [Desktop app](desktop-app.md). Releases: [docs/releasing.md](../releasing.md).

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
  .github/workflows/   # release-cli-desktop.yml and release-mobile.yml: release builds; test.yml: every app's and crate's tests on each pull request; docs.yml: the doc check
```

## Production and development installs

Production installs are named **Lorca** and use `app.lorca`; development installs are named **Lorca Dev** and use `app.lorca.dev`. They are separate applications on macOS, Windows, Linux, iOS, and Android. The production CLI stores its account under `~/.lorca` and listens on `4862`, while the desktop-app development build sets `LORCA_HOME=~/.lorca-dev` and listens on `4863`. On phones the distinct application ids give each build its own OS sandbox, and the core uses `lorca/core` or `lorca-dev/core` inside that sandbox. Their iOS keychain groups are `group.app.lorca` and `group.app.lorca.dev`.

## Development loops and builds

`bun run android` rebuilds the Rust core for Android, then builds and runs Lorca Dev's Expo dev client on the Android emulator or a connected phone, beside an installed Lorca. `cd mobile && bun run core` rebuilds the Rust core for both phone platforms; `bun run mobile:dev` is the iOS development loop described below.

`bun run desktop` is the desktop app's loop and `bun run desktop:build` builds its release bundles ([Desktop app](desktop-app.md)). `bun run relay` runs a local relay. `bun run mobile:dev` (`scripts/mobile.ts`) is the Lorca Dev phone loop on the iOS Simulator, or on `--device <name or udid>`: it fingerprints the crates the phone links, the prebuild inputs (Expo config, assets, `package.json`, plugins, targets), the pod inputs with the checkout's path, and the native module sources (stamps in `mobile/.expo/dev-stamps.json`), rebuilds what is stale (`bun run core ios`, a clean `expo prebuild`, `pod install`, `expo run:ios`), starts Metro, and opens the dev client on it. A Rust save while it runs rebuilds the core and installs the app again. The desktop and phone loops leave production Lorca processes alone, so all builds run side by side.

**Timing.** Lorca Dev keeps a few figures about its own speed (`mobile/src/core/timing.ts`): when a launch started the core, applied its snapshot, and drew the chat list; how long a tapped chat takes to show its transcript; how long a new message took from its author's Device to the phone's store (on a phone this includes the two clocks' difference, and a backlog's messages are left out); and how many frames the JS thread missed per transcript scroll. Settings lists them under Timing with Copy as JSON and Reset, for a change's before and after on the same account. Lorca leaves them out.

**Build size.** Cargo never removes what it no longer uses, so `target/debug` grows with each build and with each set of flags (`cargo test`, `cargo build`, the editor's checks). The workspace's `dev` profile keeps line tables only (`debug = "line-tables-only"`): a panic's backtrace has files and lines, and a debugger shows no local variables. `bun run desktop` clears the incremental cache once it passes 3 GiB and, with `cargo install cargo-sweep`, sweeps artifacts nothing has used for a week (`buildCLI` in `scripts/common.ts`). `bun run clean:rust` does it at once, sweeping anything unused for a day, and `bun run clean:rust --all` removes `target/debug` for the next build to restore.

## Making a change

Every change is a short-lived branch merged into `main` through a pull request; `main` takes no direct commits. Releases are cut from a clean `main`, a batch of merged changes at a time.

1. **Branch.** `git switch -c <topic>`, or, for work beside another branch, a worktree next to the checkout: `git worktree add ../lorca-<topic> -b <topic>`. A worktree under `/tmp` goes when macOS clears it.
2. **Run and test what the change touches**, with the quick tests while changing it and the area's whole suite before it is done:

   | Area | Loop | While changing | Before it is done |
   | --- | --- | --- | --- |
   | CLI and Rust crates | `bun run cli` | `cargo test -p lorca --lib <module>` | `cargo test --workspace --exclude lorca-mobile` |
   | Desktop app | `bun run desktop` | `go test . -run 'TestXxx' -timeout 120s`, `go test ./model`, `go tool mygo vet .` | `gofmt -l .`, `go vet ./...`, `go test ./...` (about four minutes), in `desktop/` |
   | Phone app | `bun run mobile:dev`, `bun run android` | `bun run typecheck && bun run test`, in `mobile/` | the same, and `cargo test -p lorca-mobile` when `crates/mobile` or `crates/markdown` changed |
   | Relay | `bun run relay` | `cargo test -p lorca-relay` | the same |
   | Docs and the model catalog | | `bun run check:docs`, `bun run check:catalog` | the same |

   The checks follow what the change touched: a formatting fix needs `gofmt -l .`, not the four-minute desktop suite. CI's gofmt is the one of the Go in `desktop/go.mod`, whose alignment can differ from an older `gofmt` on `PATH`; `go fmt ./...` in `desktop/` runs that toolchain's. The `lorca` crate's first test build compiles the whole CLI and takes several GB under `target/debug`; `cargo check -p lorca --tests` type-checks it in a fraction of that, and CI runs the suite on all three platforms. A native build of the phone app goes through `bun run mobile:dev` (or `bun run android`), which sets the environment CocoaPods and Xcode need (`NATIVE_ENV` in `scripts/mobile.ts`); a bare `xcodebuild` against `mobile/ios` skips it. The Android core needs `cargo-ndk` and an NDK. `mobile/bun.lock` is a lockfile v2, which Bun reads from 1.4.

3. **Docs.** A change to a mechanism changes the doc that describes it in the same branch ([AGENTS.md](../../AGENTS.md)).
4. **Pull request.** `gh pr create` runs Tests (`test.yml`: the Rust workspace on Linux with Postgres, macOS, and Windows; the desktop app's gofmt, vet, and tests; the phone app's typecheck and tests) and Docs (`docs.yml`). The branch merges when they pass. The Windows suite runs only there and takes longest. Codex reviews each pull request as it opens and leaves its findings as review comments (`gh api repos/hcl-z/lorca/pulls/<n>/comments`). A failed check is read from its log (`gh run view <run> --job <job> --log-failed`) before anything changes, and a fix goes up as a new commit on the same branch. Nothing runs on commit: the repository has no Git hooks, so step 2's checks are what catch formatting and docs before CI.
5. **Release.** When a batch of changes is merged, a `## [<version>]` section in `CHANGELOG.md` and `bun run release <version> --push` release the CLI, the desktop app, and the phone app together ([Releasing](../releasing.md#cutting-a-release)).

## Testing the desktop app

`go test ./...` in `desktop/` runs the model's tests and the views' tests in MyGo's offscreen tester ([Desktop app](desktop-app.md)); the whole package takes about four minutes, so it runs once before a change is done. While changing something, run the tests that cover it with a timeout, for example `go test . -run 'TestSettings|TestTray' -timeout 120s`, and `go test ./model` for the model, which takes a second. `go tool mygo vet .` is quick and catches build values kept in state.

- **Native calls.** MyGo's `App`, `Tray`, and window calls wait for a running application, so a test that reaches one blocks until the timeout. Code that must call the system goes through a variable a test replaces (`setNativeBadge` in `host.go`, `settingsUpdaterState` in `settings_general.go`), and a test restores it with `t.Cleanup`. A test sets the preference or the store directly and checks the state the app would show, not the system's side of it.
- **Views.** `demoWindow(t)` builds the main window over the demo's store, and `ui.NewTester` clicks, types, and reads the texts a view shows. A switch is 26 wide, 12 in from its row's end. Run `runPosts()` and a few `Frame()`s between steps, since the store's replies come back on the main-thread queue.
- **Looking at a frame.** `LORCA_RENDER=<folder>` makes `renderTo` write the frame as a PNG; read pixel rows of it to check alignment and spacing when the real fonts are not at hand. A fill that is painted from `Bounds` needs two frames before it shows, because `Bounds` is the previous frame's.
- **The CLI.** `cargo test -p lorca --lib <module>` runs one module's unit tests; a first build of the crate's tests takes a few minutes and later ones seconds.
