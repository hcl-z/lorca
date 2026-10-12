// Cut a release of the CLI, the desktop app, and the phone app, which share one version.
//
//   bun run release 1.0.17              set the version everywhere, commit, and tag it locally
//   bun run release 1.0.17 --push       the same, then push the branch and the tags one by one
//   bun run release 1.0.17 --dry-run    say what would change; change nothing
//   bun run release --check             say whether the versions agree
//
// The version lives in the root Cargo.toml (the CLI), the root package.json, desktop/mygo.config.ts
// (the desktop app), and mobile/app.config.ts (the phone app). CHANGELOG.md needs a `## [<version>]`
// section, which is the notes of all three releases. Each release has its own workflow, started by
// its tag: cli-v<version>, desktop-v<version>, and mobile-v<version>. GitHub starts no workflow for
// a push of more than three tags, and the tags go one at a time here so none is missed.
// docs/releasing.md has the steps.
import { $ } from "bun"
import { color, log, ROOT } from "./common.ts"
import { extractReleaseNotes } from "./changelog.ts"
import { join } from "node:path"

process.chdir(ROOT)

function die(message: string): never {
  log(color.red(message))
  process.exit(1)
}

type Holder = { name: string; file: string; tag: string; pattern: RegExp }

/** Where each product keeps its version. The pattern's first group is the text before the quoted
 * version, so a replacement leaves the rest of the line as it was. */
const HOLDERS: Holder[] = [
  { name: "CLI", file: "Cargo.toml", tag: "cli-v", pattern: /(\[workspace\.package\][^[]*?\nversion\s*=\s*)"([^"]*)"/ },
  { name: "root package", file: "package.json", tag: "", pattern: /^(\s*"version"\s*:\s*)"([^"]*)"/m },
  { name: "desktop app", file: "desktop/mygo.config.ts", tag: "desktop-v", pattern: /^(\s*version:\s*)"([^"]*)"/m },
  { name: "phone app", file: "mobile/app.config.ts", tag: "mobile-v", pattern: /^(const version = )"([^"]*)"/m },
]

const isVersion = (value: string) => /^\d+\.\d+\.\d+$/.test(value)
const compare = (a: string, b: string) => new Intl.Collator("en", { numeric: true }).compare(a, b)

async function current(holder: Holder): Promise<string> {
  const match = (await Bun.file(join(ROOT, holder.file)).text()).match(holder.pattern)
  if (!match) die(`${holder.file} has no version to read`)
  return match[2]!
}

const args = process.argv.slice(2)
const flags = new Set(args.filter((arg) => arg.startsWith("--")))
for (const flag of flags) if (!["--push", "--dry-run", "--check"].includes(flag)) die(`unknown option: ${flag}`)
const positional = args.filter((arg) => !arg.startsWith("--"))
if (positional.length > 1) die("expected at most one version")

const versions = await Promise.all(HOLDERS.map(async (holder) => ({ holder, version: await current(holder) })))
const agree = new Set(versions.map(({ version }) => version)).size === 1
for (const { holder, version } of versions) console.log(`  ${holder.name.padEnd(13)} ${version}  ${color.dim(holder.file)}`)

if (flags.has("--check")) {
  log(agree ? color.green("the versions agree") : color.red("the versions differ"))
  process.exit(agree ? 0 : 1)
}

const version = positional[0]
if (!version) die("usage: bun run release <x.y.z> [--push] [--dry-run]")
if (!isVersion(version)) die(`not a version: ${version} (expected x.y.z)`)

const newest = versions.map(({ version }) => version).sort(compare).at(-1)!
if (compare(version, newest) <= 0) die(`${version} is not newer than ${newest}`)

const changelog = Bun.file(join(ROOT, "CHANGELOG.md"))
if (!extractReleaseNotes(await changelog.text(), version)) die(`CHANGELOG.md has no "## [${version}]" section: it is the notes of every release`)

const tags = HOLDERS.filter(({ tag }) => tag).map(({ tag }) => `${tag}${version}`)
const existing = (await $`git tag --list ${tags}`.text()).trim()
if (existing) die(`already tagged: ${existing.split("\n").join(", ")}`)
const remote = (await $`git ls-remote --tags origin ${tags.map((tag) => `refs/tags/${tag}`)}`.nothrow().text()).trim()
if (remote) die(`already tagged on origin: ${remote.split("\n").map((line) => line.split("refs/tags/")[1]).join(", ")}`)

const branch = (await $`git branch --show-current`.text()).trim()
if (flags.has("--dry-run")) {
  log(`${color.bold("would")} set ${HOLDERS.map(({ file }) => file).join(", ")} to ${version}, commit on ${branch}, and tag ${tags.join(", ")}${flags.has("--push") ? ", then push them" : ""}`)
  process.exit(0)
}
if ((await $`git status --porcelain`.text()).trim()) die("the working tree has changes: commit or stash them first")

// ---- 1. the version, everywhere
// A failure puts the files back, so a refused release leaves the tree as it found it.
const files = [...HOLDERS.map(({ file }) => file), "Cargo.lock"]
try {
  for (const holder of HOLDERS) {
    const path = join(ROOT, holder.file)
    // Rewriting the one value keeps each file's formatting, so the commit is a few one-line diffs.
    await Bun.write(path, (await Bun.file(path).text()).replace(holder.pattern, `$1"${version}"`))
  }
  // Cargo.lock follows the crates' version; the dependencies stay where they are.
  await $`cargo update --workspace --offline`.quiet()
} catch (error) {
  await $`git checkout -- ${files}`.quiet()
  die(`could not set ${version}: ${error instanceof Error ? error.message : error}`)
}

// ---- 2. the commit and the tags
await $`git add ${files}`
await $`git commit -q -m ${`Release ${version}`}`
for (const tag of tags) await $`git tag ${tag}`
log(`${color.green("committed")} Release ${version} on ${branch}, tagged ${tags.join(", ")}`)

// ---- 3. the push
if (!flags.has("--push")) {
  console.log(`  git push origin ${branch}\n${tags.map((tag) => `  git push origin ${tag}`).join("\n")}`)
  process.exit(0)
}
await $`git push origin ${branch}`
for (const tag of tags) {
  await $`git push origin ${`refs/tags/${tag}`}`
  await Bun.sleep(5000)
}
log(`${color.green("pushed")} ${tags.join(", ")}: the release workflows are running`)
