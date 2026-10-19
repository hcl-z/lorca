//! A bot's browser. Two things can drive one, and a Runner uses one of them at a time: ego (lite)'s
//! browser, which a bot runs from bash with `ego-browser` as the `ego-browser` skill says, and the
//! Playwright plugin Lorca carries on every Runner. The Runner's setting picks one (`auto` takes
//! ego where it is installed); the turn's prompt names the one in use and the other is left out
//! of its plugin catalog or skills. Whether a bot may use a browser at all is one switch in its
//! Access, the grant to the `playwright` plugin.

use crate::app::App;
use crate::config::BrowserEngine;
use crate::model::Bot;
use serde_json::{json, Value};
use std::sync::Arc;

/// The Playwright plugin's id, which the Access switch for Browser is keyed by.
pub const PLUGIN_ID: &str = "playwright";
/// The skill ego (lite) publishes for driving its browser.
pub const EGO_SKILL: &str = "ego-browser";

/// The browser a turn uses.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Engine {
    Ego,
    Playwright,
}

/// Which browser the setting names, given whether ego is installed and whether the bot may run
/// shell commands, which is how it drives ego.
pub fn resolve(setting: BrowserEngine, ego_installed: bool, shell: bool) -> Engine {
    match setting {
        BrowserEngine::Playwright => Engine::Playwright,
        BrowserEngine::Ego if shell => Engine::Ego,
        BrowserEngine::Auto if shell && ego_installed => Engine::Ego,
        _ => Engine::Playwright,
    }
}

/// Whether ego (lite) is on this computer: its app, or the `ego-browser` command its first run
/// puts in `~/.local/bin`.
pub fn ego_installed() -> bool {
    if !cfg!(target_os = "macos") {
        return false;
    }
    let Some(home) = dirs::home_dir() else { return false };
    [
        std::path::PathBuf::from("/Applications/ego lite.app"),
        home.join("Applications/ego lite.app"),
        home.join(".local/bin/ego-browser"),
    ]
    .iter()
    .any(|path| path.exists())
}

/// The browser `bot` uses on this Runner now, or none when its Access leaves Browser out.
pub fn engine_for(app: &App, bot: &Bot) -> Option<Engine> {
    let policy = bot.permissions.as_ref();
    if !policy.is_none_or(|policy| policy.allows_connection(PLUGIN_ID)) {
        return None;
    }
    let setting = app.settings.lock().unwrap().browser_engine;
    Some(resolve(setting, ego_installed(), policy.is_none_or(|policy| policy.shell)))
}

/// Whether the turn's plugin catalog lists the Browser plugin.
pub fn plugin_offered(app: &App, bot: &Bot) -> bool {
    engine_for(app, bot) == Some(Engine::Playwright)
}

/// Whether the turn's skills list the `ego-browser` skill.
pub fn skill_offered(app: &App, bot: &Bot) -> bool {
    engine_for(app, bot) == Some(Engine::Ego)
}

/// The Browser part of the system prompt: which browser to use, and not the other. It is the same
/// text for a given browser, so the prompt cache holds, and empty when Access leaves Browser out.
pub fn prompt(app: &App, bot: &Bot) -> String {
    match engine_for(app, bot) {
        None => String::new(),
        Some(Engine::Ego) => EGO_PROMPT.to_string(),
        Some(Engine::Playwright) => PLUGIN_PROMPT.to_string(),
    }
}

const EGO_PROMPT: &str = "\nBrowser: for anything that needs a web browser, read the ego-browser skill, then run its scripts with \
    `ego-browser nodejs` in bash. It is the user's own browser, with their sign-ins. Use one task space for the job and finish it when \
    the work is done (`task.finish({ keep: [] })`), which closes its pages. If a page needs the user (a sign-in, a captcha, a code), \
    stop and say so. Give a password to a script with `request_secret` (use: command) and bash's `secrets`, read as process.env.NAME.\n";

const PLUGIN_PROMPT: &str = "\nBrowser: for anything that needs a web browser, call the Browser plugin's tools from a codemode script. \
    Read pages with browser_snapshot, and close the browser with browser_close when the work is done. It is headless, with none of the \
    user's sign-ins. If a page needs the user (a sign-in, a captcha, a code), stop and say so. For a password, `request_secret` \
    (use: browser, site), typed as {{secret:NAME}} with browser_type or browser_fill_form.\n";

/// The words of a shell command that name a program: its pieces split at whitespace and shell
/// punctuation, each reduced to its last path component.
fn programs(command: &str) -> impl Iterator<Item = &str> {
    command
        .split(|c: char| c.is_whitespace() || ";|&()<>`$\"'={}".contains(c))
        .filter(|word| !word.is_empty())
        .map(|word| word.rsplit('/').next().unwrap_or(word))
}

/// Whether a shell command names `ego-browser` anywhere, as a command or an argument. A bot whose
/// Access leaves Browser out may not run one that does.
pub fn mentions_ego(command: &str) -> bool {
    programs(command).any(|word| word == EGO_SKILL)
}

/// Whether the command is one `ego-browser nodejs` script and nothing else: the program, `nodejs`,
/// and a heredoc whose quoted tag closes it on the last line, once. The shell hands the body to
/// the script untouched, so nothing in it runs as a shell command.
pub fn is_plain_script(command: &str) -> bool {
    let command = command.trim_end_matches('\n');
    let Some((head, body)) = command.split_once('\n') else { return false };
    let mut words = head.split_whitespace();
    let (Some(EGO_SKILL), Some("nodejs"), Some(redirect), None) = (words.next(), words.next(), words.next(), words.next()) else { return false };
    let Some(redirect) = redirect.strip_prefix("<<") else { return false };
    let tag = redirect
        .strip_prefix('\'')
        .and_then(|rest| rest.strip_suffix('\''))
        .or_else(|| redirect.strip_prefix('"').and_then(|rest| rest.strip_suffix('"')));
    let Some(tag) = tag.filter(|tag| !tag.is_empty() && tag.chars().all(|c| c.is_ascii_alphanumeric() || c == '_')) else { return false };
    let lines: Vec<&str> = body.split('\n').collect();
    let Some((last, script)) = lines.split_last() else { return false };
    *last == tag && !script.contains(&tag)
}

/// What the apps show for the Browser setting: the choice, the browser it comes to, and whether
/// ego (lite) is installed.
pub fn status(app: &App) -> Value {
    let setting = app.settings.lock().unwrap().browser_engine;
    let installed = ego_installed();
    let effective = match resolve(setting, installed, true) {
        Engine::Ego => "ego",
        Engine::Playwright => "playwright",
    };
    json!({ "engine": setting.as_str(), "effective": effective, "ego_installed": installed })
}

/// `browser.engine` and `browser.set_engine`, on this Runner.
pub fn dispatch(app: &Arc<App>, method: &str, params: &Value) -> Result<Value, String> {
    match method {
        "browser.engine" => Ok(status(app)),
        "browser.set_engine" => {
            let engine = params["engine"].as_str().and_then(BrowserEngine::parse).ok_or("engine is auto, ego, or playwright")?;
            {
                let mut settings = app.settings.lock().unwrap();
                settings.browser_engine = engine;
                settings.save(&app.config).map_err(|e| e.to_string())?;
            }
            Ok(status(app))
        }
        _ => Err(format!("Unknown method {method}")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_setting_picks_the_browser_and_ego_needs_a_shell() {
        use BrowserEngine::*;
        assert_eq!(resolve(Auto, true, true), Engine::Ego);
        assert_eq!(resolve(Auto, false, true), Engine::Playwright);
        assert_eq!(resolve(Ego, false, true), Engine::Ego, "asked for ego, installed or not: its skill says how to install");
        assert_eq!(resolve(Playwright, true, true), Engine::Playwright);
        assert_eq!(resolve(Ego, true, false), Engine::Playwright, "a bot without a shell cannot run ego-browser");
        assert_eq!(resolve(Auto, true, false), Engine::Playwright);
    }

    #[test]
    fn a_command_names_ego_browser_as_a_program_or_an_argument() {
        assert!(mentions_ego("ego-browser nodejs <<'EOF'\nEOF"));
        assert!(mentions_ego("cd /tmp && ~/.local/bin/ego-browser nodejs -e 'x'"));
        assert!(mentions_ego("which ego-browser"));
        assert!(mentions_ego("FOO=1 \"ego-browser\" nodejs"));
        assert!(!mentions_ego("echo hello"));
        assert!(!mentions_ego("ego-browser-notes.txt"));
    }

    #[test]
    fn only_one_quoted_heredoc_script_is_plain() {
        assert!(is_plain_script("ego-browser nodejs <<'EOF'\nconsole.log(1)\nEOF"));
        assert!(is_plain_script("ego-browser nodejs <<\"JS\"\nconsole.log(1)\nJS\n"));
        assert!(!is_plain_script("ego-browser nodejs <<EOF\nconsole.log(1)\nEOF"), "an unquoted tag expands in the body");
        assert!(!is_plain_script("ego-browser nodejs <<'EOF'\nconsole.log(1)\nEOF\nrm -rf ~"), "something after the script");
        assert!(!is_plain_script("ego-browser nodejs <<'EOF'\na\nEOF\nrm -rf ~\nEOF"), "the tag closes the script early");
        assert!(!is_plain_script("ego-browser nodejs <<'EOF' ; rm -rf ~\nEOF"));
        assert!(!is_plain_script("/tmp/ego-browser nodejs <<'EOF'\nEOF"));
        assert!(!is_plain_script("ego-browser nodejs -e 'console.log(1)'"));
        assert!(!is_plain_script("ego-browser upgrade"));
    }

    #[test]
    fn each_browser_gets_only_its_own_words_and_ends_by_closing_it() {
        let (ego, plugin) = (EGO_PROMPT, PLUGIN_PROMPT);
        assert!(ego.contains("ego-browser skill") && ego.contains("task.finish") && ego.contains("process.env.NAME"));
        assert!(plugin.contains("browser_snapshot") && plugin.contains("browser_close") && plugin.contains("{{secret:NAME}}"));
        assert!(!ego.contains("plugin") && !ego.contains("Playwright") && !ego.contains("{{secret:"), "ego's words do not name the other browser");
        assert!(!plugin.contains("ego") && !plugin.contains("process.env"), "the plugin's words do not name the other browser");
        for text in [ego, plugin] {
            assert!(text.starts_with("\nBrowser: ") && text.ends_with(".\n") && !text.contains("  "), "one paragraph, single-spaced");
            assert!(text.contains("stop and say so"));
        }
    }
}
