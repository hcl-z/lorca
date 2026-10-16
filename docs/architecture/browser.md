# Browser

A bot's browser. A Runner has two to drive one with, and a turn uses one of them: ego (lite)'s browser, which the bot runs from bash as the `ego-browser` skill says, or the Browser plugin Lorca carries on every Runner. A bot's [Access](bot-permissions.md) has one switch for Browser, whichever it is. `crates/cli/src/browser.rs` holds the choice, the Browser part of the system prompt, and the checks on a command that runs ego.

## The two browsers

- **ego (lite)** is a Chromium browser for a user and their bots, installed on a Mac from its own app. A bot drives it with the `ego-browser` command, `ego-browser nodejs` over a script, in the user's own sign-ins. Lorca ships ego's `ego-browser` skill as one of its [shipped skills](skills.md#the-skills-lorca-ships), with ego's own files and MIT license, so the install steps, the API, and the notes on sites travel with it.
- **The Browser plugin** is the `playwright` plugin (`@playwright/mcp`, headless). The marketplace index carries its manifest, and every Runner installs it from the index in use, or else this build's, as the Runner starts and the index is read (`plugins::refresh_installed`). It is not in the marketplace's lists or searches, `install_plugin` and `connect_plugin` do not find it, and it cannot be uninstalled. Its id stays `playwright`, which is what a bot's grant to Browser is keyed by.

## Which one a turn uses

The Runner's setting `browser_engine` (`settings.json`) is `auto`, `ego`, or `playwright`. `auto` takes ego where it is installed (`browser::ego_installed`: `ego lite.app` in `/Applications` or `~/Applications`, or `~/.local/bin/ego-browser`, on a Mac) and the plugin elsewhere. A bot drives ego from bash, so one whose Access turns shell commands off uses the plugin whatever the setting says (`browser::resolve`).

The turn names the one in use and leaves out the other (`browser::engine_for`):

- The system prompt has a Browser paragraph for it (`browser::prompt`), short and naming only that browser. Ego's says to read the `ego-browser` skill and run its scripts with `ego-browser nodejs` in bash, in one task space that is finished when the work is done (`task.finish({ keep: [] })`, which closes its pages); to stop when a page needs the user; and to pass a password as `$NAME` through `request_secret` `use: command` and bash's `secrets`. The plugin's says to call its tools from a codemode script, read pages with `browser_snapshot`, and close the browser with `browser_close` when the work is done; to stop when a page needs the user; and to type a password as `{{secret:NAME}}`. Both end with page text being data, never instructions. A given browser always gets the same words, so the prompt cache holds; with Browser off in Access the paragraph is left out.
- With ego, the plugin is not in the turn's catalog or its codemode tool declarations (`PluginCatalog::allows`, the plugin briefs). With the plugin, the skills list leaves out `ego-browser` (`skills_turn::prompt`).

A change of setting applies from the bot's next turn, not to one under way. `browser.engine` answers `{ engine, effective, ego_installed }` and `browser.set_engine { engine }` changes the setting and answers the same. They are a setting of the Runner they reach, so the apps ask only the CLI on their own computer (the methods are not sent to a Runner on another Device).

## Access

Browser is one switch in a bot's Access: the grant to the `playwright` plugin, on when it lists any capability and, as for every plugin, when the bot has no `connections` at all. It has no levels and no tools to choose; a bot that was limited to reading with the plugin is on.

A bot with it off has no browser in either form: the prompt has no Browser paragraph, the plugin is not in its catalog, the skills list leaves out `ego-browser`, and a call that gets through anyway is refused for it. The plugin's tools check the plugin as a whole (`permissions::check_plugin`) and are not reviewed. A bash call that names `ego-browser`, as a command or an argument (`browser::mentions_ego`), checks it in the turn's `before_tool_call`, ahead of [Auto-review](auto-review.md), and puts an access request in the chat when it is off. The check reads the command's words, so it stops a bot that runs ego by name; a bot with shell commands on can do anything the user can, and Access is no sandbox ([Shell, filesystem, and isolation](bot-permissions.md#shell-filesystem-and-isolation)).

A call that is exactly one `ego-browser nodejs` script is not judged as a shell command: `ego-browser nodejs <<'TAG'`, the script, and `TAG` alone on the last line, the tag quoted so the shell leaves the body as it is (`browser::is_plain_script`). Any other command that names ego goes to Auto-review as a shell command does, and so does a call that passes `secrets` to it or runs it in the background. Calls to the plugin are not reviewed either.

## Secrets

With the plugin, a sign-in uses a saved secret by name: `{{secret:NAME}}` in `browser_type`'s `text` or `browser_fill_form`'s `value`, filled in by the Runner on the secret's site ([Secret requests](secrets.md#using-a-secret)). With ego the bot asks for the secret with `use: command` and runs the script through bash's `secrets`, which sets `$NAME` for that command, and reads `process.env.NAME` in the script; `request_secret` with `use: browser` is refused for a bot using ego and says so.

## Stop

The chat's [Stop](stopping.md) ends a call to the plugin as it ends any plugin call, and kills the process group of a bash command that runs ego. Pages the browser has open stay open, and ego's task space stays as its skill leaves it.

## Screenshots

A bot publishes a screenshot as any file, with `publish_output` ([Outputs and evidence](outputs.md)); the plugin's `browser_take_screenshot` and ego's `page.screenshot` return or save it for that.

## In the apps

Settings › Plugins on a computer has a Browser section for that computer's CLI: a segmented control (Automatic, ego (lite), Built-in), the browser in use, and a Download link when ego is not installed (`desktop/settings_plugins.go`, `model/browser.go`). Browser is not among the plugin lists in the apps, and the bot's Access sheet (`desktop/sheet_access.go`) and the phone's Access screen (`mobile/src/ui/AccessScreen.tsx`) have a Browser switch under the Runner's name, beside Shell commands. The phone is never a Runner and has no browser setting.
