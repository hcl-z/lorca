// Build the iPhone app as an unsigned .ipa for a GitHub release:
//   Rust core → production prebuild in a copy of mobile/ → pods → archive without signing → .ipa.
//
//   bun run release-ios                    writes dist/ios/lorca-<version>-ios.ipa
//   BUILD_NUMBER=42 bun run release-ios    the same under a chosen build number
//
// No Apple account or certificate is involved: the archive is built with code signing off, so the
// .ipa installs only after a sideloading tool (AltStore, Sideloadly, or the like) signs it with the
// user's own Apple ID. The marketing version is `version` in mobile/app.config.ts. The Release phone
// app workflow (.github/workflows/release-mobile.yml) runs this and uploads the .ipa to the release
// mobile-v<version>; docs/releasing-mobile.md has the steps.
import { $ } from "bun"
import { existsSync } from "node:fs"
import { mkdir, readdir, rename, rm } from "node:fs/promises"
import { join } from "node:path"
import { color, log, ROOT } from "./common.ts"

function die(message: string): never {
  log(color.red(message))
  process.exit(1)
}

if (process.argv.length > 2) die(`unknown argument: ${process.argv[2]}`)

for (const tool of ["cargo", "xcodebuild", "pod", "bunx", "plutil", "rsync", "ditto"]) {
  if (!Bun.which(tool)) die(`missing required tool: ${tool}`)
}

const BUNDLE_ID = "app.lorca"
const MOBILE = join(ROOT, "mobile")
const BUILD_DIR = join(ROOT, "dist", "ios")
// The production project is generated in a copy, so mobile/ios stays the dev loop's Lorca Dev project.
// The copy stays between releases at the same path: Xcode's compilation cache keys hold absolute
// paths, and the pods installed in it are used again.
const PROJECT = join(BUILD_DIR, "mobile")
const IOS = join(PROJECT, "ios")
// Pods and its lockfile wait here while prebuild writes a new ios/.
const KEPT = join(BUILD_DIR, "kept")
const ARCHIVE = join(BUILD_DIR, "Lorca.xcarchive")

// The build number is the time as YYYYMMDDHHmm: it only grows, and it names when the build was
// made. UTC, so a build from a Mac and one from the workflow's runner count on the same clock.
const now = new Date()
const pad = (n: number) => String(n).padStart(2, "0")
const buildNumber =
  process.env.BUILD_NUMBER ??
  `${now.getUTCFullYear()}${pad(now.getUTCMonth() + 1)}${pad(now.getUTCDate())}${pad(now.getUTCHours())}${pad(now.getUTCMinutes())}`
if (!/^\d+$/.test(buildNumber)) die(`BUILD_NUMBER must be digits, got "${buildNumber}"`)

// CocoaPods dies on a non-UTF-8 locale, and the CommandLineTools SDK breaks the pod install and
// the build with "unknown architecture" from tapi. The variant variables would make a Lorca Dev build.
const env: Record<string, string> = {
  ...(process.env as Record<string, string>),
  LANG: "en_US.UTF-8",
  LC_ALL: "en_US.UTF-8",
  DEVELOPER_DIR: process.env.DEVELOPER_DIR ?? "/Applications/Xcode.app/Contents/Developer",
  LORCA_IOS_BUILD_NUMBER: buildNumber,
}
delete env.LORCA_MOBILE_VARIANT
delete env.EAS_BUILD_PROFILE

// ---- 1. Rust core
// The xcframework is gitignored and the dev loop may hold an older one, so a release builds its own.
// The iPhone app runs the device slice; the dev loop also builds the simulator's.
log(`${color.bold("core")} ${color.dim("release build for iOS, device slice")}`)
await $`bun run core ios`.cwd(MOBILE).env({ ...env, LORCA_CORE_RELEASE: "1" })

// ---- 2. production project
log(`${color.bold("prebuild")} ${color.dim(`Lorca, ${BUNDLE_ID}, build ${buildNumber}`)}`)
// The native projects and prebuild's cache in .expo are the copy's own.
if (existsSync(join(PROJECT, "package.json"))) {
  // rsync copies only what changed, and leaves alone the xcframework that expo-modules-jsi's build
  // phase makes in its package and keys to this copy's Pods path.
  const jsi = "/node_modules/expo-modules-jsi/apple"
  await $`rsync -a --delete --exclude /ios --exclude /android --exclude /.expo --exclude ${`${jsi}/.*`} --exclude ${`${jsi}/Products`} ${`${MOBILE}/`} ${`${PROJECT}/`}`
} else {
  // -c clones on APFS, so node_modules costs next to nothing to copy. An ios/ already there holds
  // the pods the release workflow's cache put back, which the pod install below uses again.
  await mkdir(PROJECT, { recursive: true })
  for (const name of await readdir(PROJECT)) {
    if (name !== "ios") await rm(join(PROJECT, name), { recursive: true, force: true })
  }
  for (const name of await readdir(MOBILE)) {
    if (!["ios", "android", ".expo"].includes(name)) await $`cp -cR ${join(MOBILE, name)} ${PROJECT}`
  }
}
// A clean prebuild writes ios/ from the Expo config. The pods installed last time go back in, so
// pod install uses them again and installs only what changed.
const KEEP = ["Pods", "Podfile.lock"]
await mkdir(KEPT, { recursive: true })
for (const name of KEEP) {
  if (!existsSync(join(IOS, name))) continue
  await rm(join(KEPT, name), { recursive: true, force: true })
  await rename(join(IOS, name), join(KEPT, name))
}
await rm(IOS, { recursive: true, force: true })
await $`bunx expo prebuild --platform ios --no-install`.cwd(PROJECT).env(env)
for (const name of KEEP) {
  if (existsSync(join(KEPT, name))) await rename(join(KEPT, name), join(IOS, name))
}
await $`pod install`.cwd(IOS).env(env)

const pbxproj = await Bun.file(join(IOS, "Lorca.xcodeproj", "project.pbxproj")).text()
const bundleIds = new Set([...pbxproj.matchAll(/PRODUCT_BUNDLE_IDENTIFIER = "?([^";]+)"?;/g)].map((m) => m[1]))
if (!bundleIds.has(BUNDLE_ID)) die(`the project builds ${[...bundleIds].join(", ")}, not ${BUNDLE_ID}`)

// ---- 3. archive
// CURRENT_PROJECT_VERSION carries the build number to the notify extension, whose Info.plist reads
// it; the app's own Info.plist has it from the Expo config. Code signing is off for every target,
// the pods' included, so the archive needs no identity or provisioning profile.
//
// An archive starts from an empty build database, so every compile runs again. Xcode's compilation
// cache (DerivedData/CompilationCache.noindex) answers the C, C++, and Objective-C ones from the
// last release: React Native's libraries drop from about seven minutes to seconds. Swift compiles
// in full, because the cache needs explicit Swift modules and React Native's prebuilt core turns
// them off.
log(`${color.bold("archiving")} ${color.dim(ARCHIVE)}`)
await rm(ARCHIVE, { recursive: true, force: true })
await $`xcodebuild -workspace ${join(IOS, "Lorca.xcworkspace")} -scheme Lorca -configuration Release -destination generic/platform=iOS -archivePath ${ARCHIVE} CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY= CURRENT_PROJECT_VERSION=${buildNumber} COMPILATION_CACHE_ENABLE_CACHING=YES archive -quiet`.env(env)
if (!existsSync(ARCHIVE)) die("xcodebuild produced no archive")

const archivedApp = join(ARCHIVE, "Products", "Applications", "Lorca.app")
const plist = join(archivedApp, "Info.plist")
const version = (await $`plutil -extract CFBundleShortVersionString raw ${plist}`.text()).trim()
const built = (await $`plutil -extract CFBundleVersion raw ${plist}`.text()).trim()
if (built !== buildNumber) die(`the app carries build ${built}, expected ${buildNumber}`)

// ---- 4. the .ipa
// An .ipa is a zip with the app under Payload/.
const PACKAGE = join(BUILD_DIR, "package")
const ipa = join(BUILD_DIR, `lorca-${version}-ios.ipa`)
await rm(PACKAGE, { recursive: true, force: true })
await rm(ipa, { force: true })
await mkdir(join(PACKAGE, "Payload"), { recursive: true })
await $`ditto ${archivedApp} ${join(PACKAGE, "Payload", "Lorca.app")}`
await $`ditto -c -k --norsrc ${PACKAGE} ${ipa}`
await rm(PACKAGE, { recursive: true, force: true })

const bytes = await Bun.file(ipa).bytes()
log(`${color.green("built")} Lorca ${version} (${buildNumber}), unsigned`)
console.log(`  ${ipa}`)
console.log(`  ${(bytes.length / 1024 / 1024).toFixed(1)} MB, sha256 ${new Bun.CryptoHasher("sha256").update(bytes).digest("hex")}`)
