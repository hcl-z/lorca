// Lorca for Windows, Linux, and macOS, an app of native UI. `go tool mygo dev` builds Lorca Dev
// (app.lorca.dev), which keeps its account in ~/.lorca-dev and its CLI on port 4863, apart from an
// installed Lorca.
export default ({ command }: { command: string }) => ({
  name: "Lorca",
  identifier: "app.lorca",
  // The app's own version, apart from the Mac app's (the root package.json's). `bun run
  // release-desktop` and the Release desktop workflow read it here.
  version: "1.0.19",
  icon: command === "dev" ? "assets/icon-dev.png" : "assets/icon.png",
  out: "build",
  // Open in Lorca on a shared bot's page (lorca.app/t/…). The installer registers the scheme on
  // Windows, the desktop entry on Linux, and Info.plist on macOS; Lorca Dev answers its own.
  urlSchemes: [command === "dev" ? "lorca-dev" : "lorca"],
  // Release builds update themselves from this repository's newest release tagged
  // desktop-v<version>, which MyGo finds through the GitHub API since the latest release is the
  // CLI's, and install only what the key of `mygo keygen` signed. `bun run release-desktop`
  // uploads a release as a draft: docs/releasing-desktop.md. The version's section of the root
  // CHANGELOG.md is the update's release notes.
  updates: {
    publicKey: "yWfwDxb318+6V2nPfn6Gm44YWhOxz9eOp3ZxShADzts=",
    github: "hcl-z/lorca",
    tagPrefix: "desktop-v",
    // The notes of every release, the CLI's and the phone app's too.
    changelog: "../CHANGELOG.md",
  },
  // Signed ad hoc, which needs no certificate, unless SIGN_IDENTITY names an identity, and notarized
  // with the notarytool profile NOTARY_PROFILE (in the keychain NOTARY_KEYCHAIN, else the login
  // keychain) only when one is set. Releases set neither.
  macos: {
    // The terminal plugin's libghostty-vt runs on macOS 13 or later.
    minimumSystemVersion: "13.0",
    signingIdentity: process.env.SIGN_IDENTITY,
    notarize: process.env.NOTARY_PROFILE
      ? { keychainProfile: process.env.NOTARY_PROFILE, keychain: process.env.NOTARY_KEYCHAIN }
      : undefined,
  },
  linux: {
    comment: "Chat with your bots, which run on computers you own",
    categories: ["Network", "Chat"],
  },
});
