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

/** Compile the Rust CLI the apps bundle and launch. */
export async function buildCLI(config: Config): Promise<{ ok: boolean; path: string }> {
  const args = ["build", "-p", CLI_NAME]
  if (config === "release") args.push("--release")
  const build = Bun.spawn(["cargo", ...args], { cwd: ROOT, stdout: "inherit", stderr: "inherit" })
  return { ok: (await build.exited) === 0, path: join(ROOT, "target", config, CLI_NAME) }
}
