import { join, resolve } from "node:path"

export const ROOT = resolve(import.meta.dir, "..")
export const CRATES_DIR = join(ROOT, "crates")
export const CLI_NAME = "lorca"

/** The desktop app's names: the production install, and the development one (`bun run desktop`). */
export const APP_NAME = "Lorca"
export const DEBUG_APP_NAME = "Lorca Dev"
export const BUNDLE_ID = "app.lorca"
export const DEBUG_BUNDLE_ID = "app.lorca.dev"

export const color = {
  dim: (s: string) => `\x1b[2m${s}\x1b[0m`,
  bold: (s: string) => `\x1b[1m${s}\x1b[0m`,
  red: (s: string) => `\x1b[31m${s}\x1b[0m`,
  green: (s: string) => `\x1b[32m${s}\x1b[0m`,
  yellow: (s: string) => `\x1b[33m${s}\x1b[0m`,
  cyan: (s: string) => `\x1b[36m${s}\x1b[0m`,
}

export function log(message: string) {
  const time = new Date().toLocaleTimeString("en-US", { hour12: false })
  console.log(`${color.dim(time)} ${color.cyan("lorca")} ${message}`)
}

export type Config = "debug" | "release"

const GIB = 1024 ** 3
/** The incremental cache is cleared once it passes this: it only ever grows, since Cargo keeps every session. */
const INCREMENTAL_LIMIT = 3 * GIB
/** Artifacts that nothing has built or run against for this long are swept. */
const SWEEP_DAYS = 7

/** The size of a folder in bytes, 0 when it is not there. */
async function folderSize(path: string): Promise<number> {
  const du = Bun.spawn(["du", "-sk", path], { stdout: "pipe", stderr: "ignore" })
  const text = await new Response(du.stdout).text()
  return (await du.exited) === 0 ? Number(text.split(/\s/)[0]) * 1024 : 0
}

/**
 * Keeps `target/debug` from growing without bound. Cargo never removes what it stops using: each
 * build adds incremental data, and every set of flags (`cargo test`, `cargo build`, the editor's
 * checks) leaves a copy of its own. This clears the incremental cache past a limit and, when
 * `cargo-sweep` is installed (`cargo install cargo-sweep`), sweeps the artifacts nothing has used
 * for a week. `force` clears the cache whatever its size and sweeps anything older than a day.
 */
export async function pruneBuildCache({ force = false } = {}) {
  const debug = join(ROOT, "target", "debug")
  const incremental = join(debug, "incremental")
  const size = await folderSize(incremental)
  if (size > (force ? 0 : INCREMENTAL_LIMIT)) {
    await Bun.spawn(["rm", "-rf", incremental]).exited
    log(`cleared the incremental cache (${(size / GIB).toFixed(1)} GiB)`)
  }
  const sweep = Bun.spawn(["cargo", "sweep", "--time", String(force ? 1 : SWEEP_DAYS)], { cwd: ROOT, stdout: "ignore", stderr: "ignore" })
  if ((await sweep.exited) === 0) return
  if ((await folderSize(debug)) > 10 * GIB) {
    log(`${color.yellow("target/debug is over 10 GiB.")} ${color.dim("cargo install cargo-sweep, and bun run clean:rust sweeps what is old.")}`)
  }
}

/** Compile the Rust CLI the apps bundle and launch. */
export async function buildCLI(config: Config): Promise<{ ok: boolean; path: string }> {
  if (config === "debug") await pruneBuildCache()
  const args = ["build", "-p", CLI_NAME]
  if (config === "release") args.push("--release")
  const build = Bun.spawn(["cargo", ...args], { cwd: ROOT, stdout: "inherit", stderr: "inherit" })
  return { ok: (await build.exited) === 0, path: join(ROOT, "target", config, CLI_NAME) }
}
