// Release the Mac app:
//   build → ad-hoc sign → .dmg → Sparkle archive → appcast → the GitHub release mac-releases.
//
//   bun run release-mac 0.2.0        bump "version" in package.json, then release
//   bun run release-mac              release the version package.json holds
//   bun run release-mac --local      build and pack the disk image; publish nothing
//   FORCE=1 bun run release-mac      replace a version that is already published
//   NO_HISTORY=1 bun run release-mac skip the old archives (a full download, no deltas)
//
// No certificate is involved: the app is signed ad hoc, and Sparkle checks each update against the
// EdDSA key in SPARKLE_PRIVATE_KEY (or SPARKLE_KEY_FILE). Every disk image, archive, delta, and the
// appcast are assets of one release, mac-releases, in RELEASES_REPO, which the GitHub CLI uploads
// to with its login or GH_TOKEN. docs/releasing-mac.md has the one-time setup.
import { $ } from "bun"
import { existsSync } from "node:fs"
import { mkdir, readdir, rm } from "node:fs/promises"
import { join } from "node:path"
import {
  APP_NAME,
  buildApp,
  bundlePath,
  color,
  FEED_URL,
  log,
  MAC_RELEASE_TAG,
  PACKAGE_JSON,
  readVersion,
  RELEASES_REPO,
  RELEASES_URL,
  ROOT,
  VERSION_FIELD,
} from "./app.ts"
import { extractReleaseNotes } from "./changelog.ts"
import { generateAppcast } from "./generate-appcast.ts"

process.chdir(ROOT)

function die(message: string): never {
  log(color.red(message))
  process.exit(1)
}

const args = process.argv.slice(2)
const local = args.includes("--local")
const unknown = args.find((arg) => arg.startsWith("--") && arg !== "--local")
if (unknown) die(`unknown option: ${unknown}`)
const positional = args.filter((arg) => !arg.startsWith("--"))
if (positional.length > 1) die("expected at most one version")

// How many earlier archives generate_appcast builds deltas against.
const HISTORY_COUNT = Number(process.env.HISTORY_COUNT ?? "3")
if (!Number.isSafeInteger(HISTORY_COUNT) || HISTORY_COUNT < 0) die("HISTORY_COUNT must be a non-negative integer")

for (const tool of ["cargo", "swift", "ditto", "plutil", "create-dmg", ...(local ? [] : ["gh"])]) {
  if (!Bun.which(tool)) die(`missing required tool: ${tool}`)
}

const isVersion = (value: string) => /^\d+\.\d+\.\d+$/.test(value)
const newestFirst = (a: string, b: string) => new Intl.Collator("en", { numeric: true }).compare(b, a)

/** The names of the assets on the release mac-releases, none when the release does not exist yet. */
async function publishedAssets(): Promise<string[]> {
  const view = await $`gh release view ${MAC_RELEASE_TAG} --repo ${RELEASES_REPO} --json assets --jq ${".assets[].name"}`.nothrow().quiet()
  return view.exitCode === 0 ? view.stdout.toString().split("\n").filter(Boolean) : []
}

// ---- 1. version
if (positional[0]) {
  if (!isVersion(positional[0])) die(`not a version: ${positional[0]} (expected x.y.z)`)
  // Rewriting the one line keeps the file's formatting, so the release commit is a one-line diff.
  const manifest = await Bun.file(PACKAGE_JSON).text()
  await Bun.write(PACKAGE_JSON, manifest.replace(VERSION_FIELD, `$1"${positional[0]}"`))
  log(`bumped package.json to ${positional[0]}`)
}
const version = readVersion()
if (!isVersion(version)) die(`package.json holds version "${version}", which is not x.y.z`)

const BUILD_DIR = join(ROOT, "dist", "mac")
const UPDATES_DIR = join(BUILD_DIR, "updates")
const zipName = `${APP_NAME}-${version}.zip`
const dmgName = `${APP_NAME}-${version}.dmg`
const dmgPath = join(BUILD_DIR, dmgName)

let published: string[] = []
if (!local) {
  published = await publishedAssets()
  if (published.includes(zipName) && process.env.FORCE !== "1") die(`${zipName} is already published: pass a new version, or set FORCE=1`)
}

// ---- 2. build and sign
log(`${color.bold("building")} ${color.dim(`${APP_NAME} ${version}`)}`)
const app = bundlePath("release")
// A clean bundle: nothing from an earlier build rides along into the archive. buildApp compiles
// the Rust CLI (`cargo build --release -p lorca`) and the Markdown library, then the Swift app,
// copies the CLI into Contents/Resources/bin, embeds Sparkle, and signs ad hoc.
await rm(app, { recursive: true, force: true })
const build = await buildApp("release", { onStep: (step) => log(color.dim(step)) })
if (!build.ok) die("build failed")

const plist = join(app, "Contents", "Info.plist")
const built = (await $`plutil -extract CFBundleShortVersionString raw ${plist}`.text()).trim()
if (built !== version) die(`built ${built}, expected ${version}`)
await $`codesign --verify --deep --strict ${app}`

// ---- 3. disk image
log(`packing ${dmgName}`)
const staging = join(BUILD_DIR, "dmg")
await rm(staging, { recursive: true, force: true })
await rm(dmgPath, { force: true })
await mkdir(staging, { recursive: true })
await $`ditto ${app} ${join(staging, `${APP_NAME}.app`)}`
// create-dmg exits non-zero over Finder-scripting hiccups with the image intact, so the file decides.
// Its codesigning is skipped: the image carries no certificate.
await $`create-dmg --volname ${`${APP_NAME} ${version}`} --window-size 540 380 --icon-size 128 --icon ${`${APP_NAME}.app`} 150 195 --app-drop-link 390 195 --hide-extension ${`${APP_NAME}.app`} --no-internet-enable ${dmgPath} ${staging}`
  .nothrow()
  .quiet()
if (!existsSync(dmgPath)) die("create-dmg produced no disk image")

if (local) {
  log(`${color.green("built")} ${APP_NAME} ${version}; nothing was published`)
  console.log(`  app      ${app}\n  download ${dmgPath}`)
  process.exit(0)
}

// ---- 4. Sparkle archive, beside the recent ones
await rm(UPDATES_DIR, { recursive: true, force: true })
await mkdir(UPDATES_DIR, { recursive: true })

if (process.env.NO_HISTORY !== "1") {
  const archiveVersion = (name: string) => name.slice(`${APP_NAME}-`.length, -".zip".length)
  const recent = published
    .filter((name) => name.startsWith(`${APP_NAME}-`) && name.endsWith(".zip") && name !== zipName)
    .sort((a, b) => newestFirst(archiveVersion(a), archiveVersion(b)))
    .slice(0, HISTORY_COUNT)
  // The published appcast comes along, so versions older than the pulled archives keep their entries.
  const history = [...(published.includes("appcast.xml") ? ["appcast.xml"] : []), ...recent]
  if (history.length > 0) {
    await $`gh release download ${MAC_RELEASE_TAG} --repo ${RELEASES_REPO} --dir ${UPDATES_DIR} --clobber ${history.flatMap((name) => ["--pattern", name])}`
  }
  log(recent.length > 0 ? `deltas against ${recent.join(", ")}` : "no earlier archives: this update is a full download")
}

await $`ditto -c -k --keepParent ${app} ${join(UPDATES_DIR, zipName)}`

const changelog = Bun.file(join(ROOT, "CHANGELOG.md"))
const notes = (await changelog.exists()) ? extractReleaseNotes(await changelog.text(), version) : null
if (notes) await Bun.write(join(UPDATES_DIR, `${APP_NAME}-${version}.md`), `${notes}\n`)
else log(color.yellow(`CHANGELOG.md has no "${version}" section: releasing without notes`))

// ---- 5. appcast
log("generating appcast.xml")
if (!(await generateAppcast(UPDATES_DIR, RELEASES_URL))) die("generate_appcast failed")

// ---- 6. upload
// The release is made once, apart from the repository's latest release, which stays the CLI's. An
// archive never changes under its name; the appcast changes with every release, and goes last so
// no app is pointed at an archive that is not there yet.
log(`uploading to the release ${MAC_RELEASE_TAG} of ${RELEASES_REPO}`)
const exists = (await $`gh release view ${MAC_RELEASE_TAG} --repo ${RELEASES_REPO}`.nothrow().quiet()).exitCode === 0
if (!exists) {
  await $`gh release create ${MAC_RELEASE_TAG} --repo ${RELEASES_REPO} --latest=false --title ${"Lorca for Mac updates"} --notes ${"Disk images, update archives, and the appcast of the Mac app. Download Lorca-<version>.dmg to install; the app updates itself from here."}`
}
const archives = (await readdir(UPDATES_DIR)).filter((name) => name !== "appcast.xml" && !name.startsWith(".") && name !== "old_updates")
await $`gh release upload ${MAC_RELEASE_TAG} --repo ${RELEASES_REPO} --clobber ${dmgPath} ${archives.map((name) => join(UPDATES_DIR, name))}`
await $`gh release upload ${MAC_RELEASE_TAG} --repo ${RELEASES_REPO} --clobber ${join(UPDATES_DIR, "appcast.xml")}`

const dirty = (await $`git status --porcelain package.json CHANGELOG.md`.nothrow().text()).trim()
if (dirty) log(color.yellow("package.json or CHANGELOG.md has uncommitted changes: commit them so the tree matches what shipped"))

log(`${color.green("released")} ${APP_NAME} ${version}`)
console.log(`  download ${RELEASES_URL}${dmgName}\n  update   ${RELEASES_URL}${zipName}\n  feed     ${FEED_URL}`)
