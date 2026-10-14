// Frees the disk a Rust debug build holds: `bun run clean:rust` clears the incremental cache and, with
// cargo-sweep installed, the artifacts nothing has used for a day. `bun run desktop` does the milder
// version before it builds. Pass --all to remove target/debug entirely, which the next build restores.

import { join } from "node:path"
import { ROOT, log, pruneBuildCache } from "./common.ts"

if (process.argv.includes("--all")) {
  await Bun.spawn(["rm", "-rf", join(ROOT, "target", "debug")]).exited
  log("removed target/debug")
} else {
  await pruneBuildCache({ force: true })
  log("done")
}
