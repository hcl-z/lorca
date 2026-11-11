//! A bot's Access: which plugins and tools it may use, whether each of them asks first, and the
//! skills it sees. Checked before Auto-review and again when a call runs.

use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::sync::Arc;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::app::App;
use crate::model::Bot;

/// One switch, as the Access sheet shows it: on or off, and whether an on tool asks first. A
/// tool that asks can run only when the user answers its card, whatever Auto-review or an
/// `allow` rule would say.
#[derive(Debug, Clone, Copy, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ToolPermission {
    #[serde(default = "enabled")]
    pub enabled: bool,
    #[serde(default)]
    pub ask: bool,
}

fn enabled() -> bool {
    true
}

impl ToolPermission {
    /// Off, as a bot starts for a plugin, and on without asking, as the older model's grants read.
    pub const OFF: ToolPermission = ToolPermission { enabled: false, ask: false };
    pub const ON: ToolPermission = ToolPermission { enabled: true, ask: false };
    pub const ASK: ToolPermission = ToolPermission { enabled: true, ask: true };

    /// `None` for a tool that is off, the `ask` flag for one that is on.
    pub fn allowing(self) -> Option<bool> {
        self.enabled.then_some(self.ask)
    }
}

impl fmt::Display for ToolPermission {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(if !self.enabled {
            "off"
        } else if self.ask {
            "ask"
        } else {
            "on"
        })
    }
}

/// The bot's own tools, each switched like a plugin's.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct LocalPermissions {
    /// `bash`, `bash_input`, `bash_output`, and `coding_agent`.
    #[serde(default = "on")]
    pub shell: ToolPermission,
    /// `read`, `grep`, `find`, `ls`.
    #[serde(default = "on")]
    pub read: ToolPermission,
    /// `write` and `edit`.
    #[serde(default = "on")]
    pub write: ToolPermission,
}

fn on() -> ToolPermission {
    ToolPermission::ON
}

impl Default for LocalPermissions {
    fn default() -> Self {
        Self { shell: ToolPermission::ON, read: ToolPermission::ON, write: ToolPermission::ON }
    }
}

/// Which skills a bot sees. Skills are folders with no per-bot record of their own, so this is
/// a list of exceptions over the default: the shared ones every bot has, except those Lorca
/// ships, which no switch hides.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SkillPermissions {
    /// `scope/name` a bot turned on that the default leaves off.
    #[serde(default, skip_serializing_if = "BTreeSet::is_empty")]
    pub enabled: BTreeSet<String>,
    /// `scope/name` a bot turned off that the default leaves on.
    #[serde(default, skip_serializing_if = "BTreeSet::is_empty")]
    pub disabled: BTreeSet<String>,
}

impl SkillPermissions {
    /// Whether the bot sees the skill `name` in `scope`. A skill Lorca ships always shows, since
    /// it is how a bot reads its own docs and writes one. A bot's own and a group's start off,
    /// and the shared ones every bot has start on.
    pub fn shows(&self, scope: &str, name: &str, builtin: bool) -> bool {
        if builtin {
            return true;
        }
        let key = format!("{scope}/{name}");
        if self.enabled.contains(&key) {
            return true;
        }
        if self.disabled.contains(&key) {
            return false;
        }
        scope == "shared"
    }
}

/// One plugin the bot may use, with the tools of it it may call. A tool name in `tools` decides
/// that tool; one not listed inherits `enabled`, or is off when `listed_only` holds.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct ConnectionPermissions {
    #[serde(default)]
    pub enabled: bool,
    /// Original MCP tool names, each its own switch.
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub tools: BTreeMap<String, ToolPermission>,
    /// A policy from before the switches listed the tools a bot may call and nothing else, so a
    /// tool not in `tools` is off rather than taking the plugin's switch. Saving from the apps
    /// clears it, since they write every tool they show.
    #[serde(default, skip_serializing_if = "is_false")]
    pub listed_only: bool,
}

fn is_false(value: &bool) -> bool {
    !*value
}

impl ConnectionPermissions {
    /// The switch for one tool: its own, else the plugin's, or off when the policy listed the
    /// tools it may call and this one is not among them.
    pub fn tool(&self, name: &str) -> ToolPermission {
        if let Some(permission) = self.tools.get(name) {
            return *permission;
        }
        if self.listed_only {
            return ToolPermission::OFF;
        }
        ToolPermission { enabled: self.enabled, ask: false }
    }
}

/// None on a Bot means it has the Runner's every plugin, files, and shell, as bots made before
/// this model did. An explicit policy lists the plugins it may use; a plugin installed later,
/// or one the policy leaves out, is off until the user turns it on.
#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct BotPermissions {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub connections: Option<BTreeMap<String, ConnectionPermissions>>,
    #[serde(default)]
    pub tools: LocalPermissions,
    #[serde(default)]
    pub skills: SkillPermissions,
    #[serde(default = "enabled")]
    pub drafts: bool,
}

impl Default for BotPermissions {
    fn default() -> Self {
        Self { connections: None, tools: LocalPermissions::default(), skills: SkillPermissions::default(), drafts: true }
    }
}

impl BotPermissions {
    /// What a bot the user just made starts with: every plugin off, its own tools on, and only
    /// the skills every bot has. The setup turn asks `recommend_plugins` what its job needs, and
    /// the user turns those on.
    pub fn new_bot() -> Self {
        Self { connections: Some(BTreeMap::new()), ..Self::default() }
    }
}

/// Whether `bot` drafts messages for the user to send: on unless its Access turns it off.
pub fn drafts_messages(bot: &Bot) -> bool {
    bot.permissions.as_ref().is_none_or(|permissions| permissions.drafts)
}

impl<'de> Deserialize<'de> for BotPermissions {
    /// Reads a policy, including one from before the switches: `filesystem` and `shell` become
    /// the own tools' switches, and a plugin's `capabilities` and its tool list become its
    /// switch and its tools'. A roster only ever carries the newer form after a save.
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        #[derive(Deserialize)]
        struct Wire {
            #[serde(default)]
            connections: Option<BTreeMap<String, WireConnection>>,
            #[serde(default)]
            tools: Option<LocalPermissions>,
            #[serde(default)]
            skills: SkillPermissions,
            #[serde(default = "enabled")]
            drafts: bool,
            // The older shape.
            #[serde(default)]
            filesystem: Option<WireFilesystem>,
            #[serde(default)]
            shell: Option<bool>,
        }
        #[derive(Deserialize)]
        #[serde(untagged)]
        enum WireFilesystem {
            Level(String),
            /// `false` is no file access, `true` the older full grant.
            On(bool),
        }
        #[derive(Deserialize)]
        struct WireConnection {
            #[serde(default)]
            enabled: Option<bool>,
            #[serde(default)]
            tools: Option<WireTools>,
            /// The older shape: what the bot may do, from which every tool follows.
            #[serde(default)]
            capabilities: Option<Vec<String>>,
        }
        #[derive(Deserialize)]
        #[serde(untagged)]
        enum WireTools {
            Each(BTreeMap<String, ToolPermission>),
            Listed(Vec<String>),
        }

        let wire = Wire::deserialize(deserializer)?;
        let tools = wire.tools.unwrap_or_else(|| LocalPermissions {
            // `filesystem` was `none`, `read`, or `write`; `shell` a boolean.
            read: match wire.filesystem.as_ref() {
                Some(WireFilesystem::Level(level)) if level == "none" => ToolPermission::OFF,
                Some(WireFilesystem::On(false)) => ToolPermission::OFF,
                _ => ToolPermission::ON,
            },
            write: match wire.filesystem.as_ref() {
                Some(WireFilesystem::Level(level)) if level != "write" => ToolPermission::OFF,
                Some(WireFilesystem::On(false)) => ToolPermission::OFF,
                _ => ToolPermission::ON,
            },
            shell: if wire.shell == Some(false) { ToolPermission::OFF } else { ToolPermission::ON },
        });
        let connections = wire.connections.map(|connections| {
            connections
                .into_iter()
                .map(|(id, grant)| {
                    // The older grant's capabilities were read, draft, and write; an empty set was
                    // no access.
                    let enabled = grant.enabled.unwrap_or_else(|| grant.capabilities.as_ref().is_none_or(|capabilities| !capabilities.is_empty()));
                    // A list of names was an allowlist, so a tool not on it is off.
                    let (tools, listed_only) = match grant.tools {
                        Some(WireTools::Each(tools)) => (tools, false),
                        Some(WireTools::Listed(names)) => (names.into_iter().map(|name| (name, ToolPermission::ON)).collect(), true),
                        None => (BTreeMap::new(), false),
                    };
                    (id, ConnectionPermissions { enabled, tools, listed_only })
                })
                .collect()
        });
        Ok(BotPermissions { connections, tools, skills: wire.skills, drafts: wire.drafts })
    }
}

impl BotPermissions {
    pub fn validate(&self) -> Result<(), String> {
        let valid = |name: &str| !name.trim().is_empty() && name.len() <= 256 && name == name.trim();
        if let Some(connections) = &self.connections {
            if connections.len() > 1000 || connections.keys().any(|id| !valid(id)) {
                return Err("Connection allowlists need at most 1000 non-empty instance IDs.".into());
            }
            if connections.values().any(|grant| grant.tools.len() > 1000 || grant.tools.keys().any(|name| !valid(name))) {
                return Err("Connection tool lists need at most 1000 non-empty exact names.".into());
            }
        }
        Ok(())
    }

    /// Whether the bot may use the plugin at all: its catalog and system prompt leave out one
    /// it may not.
    pub fn allows_connection(&self, connection: &str) -> bool {
        self.connections.as_ref().is_none_or(|connections| connections.get(connection).is_some_and(|grant| grant.enabled))
    }

    /// The switch for one of the bot's own tools, by the tool's name.
    pub fn local_tool(&self, tool: &str) -> Option<ToolPermission> {
        match tool {
            "bash" | "bash_input" | "bash_output" | "coding_agent" => Some(self.tools.shell),
            "read" | "grep" | "find" | "ls" => Some(self.tools.read),
            "write" | "edit" => Some(self.tools.write),
            _ => None,
        }
    }

    fn local_denial(&self, tool: &str) -> Option<String> {
        let permission = self.local_tool(tool)?;
        if permission.enabled {
            return None;
        }
        Some(match tool {
            "bash" | "bash_input" | "bash_output" | "coding_agent" => "shell commands are off for this bot".into(),
            "write" | "edit" => "changing files is off for this bot".into(),
            _ => "reading files is off for this bot".into(),
        })
    }

    /// The switch for one tool of an installed plugin, by the plugin's id as the Runner holds it
    /// and the tool's original MCP name. A plugin the policy does not list is off.
    pub fn connection_tool(&self, connection: &str, tool: &str) -> ToolPermission {
        let Some(connections) = &self.connections else { return ToolPermission::ON };
        connections.get(connection).map(|grant| grant.tool(tool)).unwrap_or(ToolPermission::OFF)
    }

    fn connection_denial(&self, connection: &str, name: &str, tool: &str) -> Option<String> {
        let connections = self.connections.as_ref()?;
        let Some(grant) = connections.get(connection).filter(|grant| grant.enabled) else {
            return Some(format!("{name} is off for this bot"));
        };
        if !grant.tool(tool).enabled {
            return Some(format!("{tool} is off for this bot"));
        }
        None
    }
}

#[derive(Debug, Clone)]
pub struct AccessDenied {
    pub tool: String,
    pub connection_id: Option<String>,
    /// The tool asks the user before it runs, rather than being off.
    pub asks: bool,
    pub reason: String,
    /// The bot's Access refused it, so the user can grant it. A bot deleted or moved to another
    /// Runner mid-turn is refused without asking anyone.
    pub grantable: bool,
}

impl fmt::Display for AccessDenied {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if !self.grantable {
            return write!(f, "{} cannot run: {}.", self.tool, self.reason);
        }
        write!(f, "Access refused for {}: {}. The user can change this bot's Access settings in its profile. Auto-review and Always allow cannot override this restriction.", self.tool, self.reason)
    }
}

/// The bot as it is stored now, or why it may do nothing: every call reads the current
/// profile, so a change takes effect during an active turn.
fn gone(tool: &str, connection_id: Option<&str>, reason: &str) -> AccessDenied {
    AccessDenied { tool: tool.into(), connection_id: connection_id.map(str::to_string), asks: false, reason: reason.into(), grantable: false }
}

fn current(app: &Arc<App>, bot: &Bot, tool: &str, connection_id: Option<&str>) -> Result<Bot, AccessDenied> {
    match app.bot(&bot.id) {
        Some(current) if current.runner_id != bot.runner_id => Err(gone(tool, connection_id, "the bot was moved to another Runner")),
        Some(current) => Ok(current),
        None => Err(gone(tool, connection_id, "the bot no longer exists")),
    }
}

pub fn check_tool(app: &Arc<App>, bot: &Bot, tool: &str) -> Result<(), AccessDenied> {
    let current = current(app, bot, tool, None)?;
    match current.permissions.as_ref().and_then(|policy| policy.local_denial(tool)) {
        Some(reason) => Err(AccessDenied { tool: tool.into(), connection_id: None, asks: false, reason, grantable: true }),
        None => Ok(()),
    }
}

/// Whether the bot's Access makes this one of its own tools ask first: on, and its switch set
/// to ask. A tool that is off is not an ask, it is a refusal.
pub fn tool_asks(app: &Arc<App>, bot: &Bot, tool: &str) -> bool {
    app.bot(&bot.id)
        .and_then(|bot| bot.permissions)
        .and_then(|policy| policy.local_tool(tool))
        .is_some_and(|permission| permission.enabled && permission.ask)
}

/// Whether one tool of a plugin asks first. The plugin is `None` when the tool belongs to the
/// bot itself, as Browser's `browser_session` is checked as its plugin's.
pub fn connection_asks(app: &Arc<App>, bot: &Bot, connection: Option<&str>, tool: &str) -> bool {
    let Some(policy) = app.bot(&bot.id).and_then(|bot| bot.permissions) else { return false };
    match connection {
        Some(connection) => policy.connection_tool(connection, tool).allowing() == Some(true),
        None => policy.local_tool(tool).is_some_and(|permission| permission.enabled && permission.ask),
    }
}

/// Whether a reading call names only a path inside a skills folder the bot uses. A bot with no
/// file access still reads its skills, which are how it is told to work.
fn reads_skills(app: &App, bot: &Bot, tool: &str, args: &Value) -> bool {
    if !matches!(tool, "read" | "grep" | "find" | "ls") {
        return false;
    }
    let Some(path) = args["path"].as_str().filter(|path| !path.is_empty()) else { return false };
    let path = if path == "~" || path.starts_with("~/") {
        dirs::home_dir().map(|home| home.join(path.trim_start_matches('~').trim_start_matches('/')))
    } else {
        Some(std::path::PathBuf::from(path))
    };
    let Some(path) = path.filter(|path| path.is_absolute()).and_then(|path| std::fs::canonicalize(path).ok()) else { return false };
    crate::skills::readable_roots(app, &bot.id).into_iter().filter_map(|root| std::fs::canonicalize(root).ok()).any(|root| path.starts_with(root))
}

/// `check_tool` for a call whose arguments are known: reading a skills folder needs no file access.
pub fn check_tool_call(app: &Arc<App>, bot: &Bot, tool: &str, args: &Value) -> Result<(), AccessDenied> {
    if reads_skills(app, bot, tool, args) {
        current(app, bot, tool, None)?;
        return Ok(());
    }
    check_tool(app, bot, tool)
}

/// A call that reads a Runner file through another tool, as `publish_output` with a `path` does,
/// needs what `read` needs.
pub fn check_file_read(app: &Arc<App>, bot: &Bot, tool: &str) -> Result<(), AccessDenied> {
    let current = current(app, bot, tool, None)?;
    match current.permissions.as_ref().and_then(|policy| policy.local_denial("read")) {
        Some(reason) => Err(AccessDenied { tool: tool.into(), connection_id: None, asks: false, reason, grantable: true }),
        None => Ok(()),
    }
}

/// Whether the plugin is on and the tool within it: a call to one that is off is refused before
/// anything runs, whether it asks or not.
pub fn check_connection(app: &Arc<App>, bot: &Bot, connection: &str, tool: &str) -> Result<(), AccessDenied> {
    let current = current(app, bot, tool, Some(connection))?;
    let name = connection_name(app, connection);
    match current.permissions.as_ref().and_then(|policy| policy.connection_denial(connection, &name, tool)) {
        Some(reason) => Err(AccessDenied { tool: tool.into(), connection_id: Some(connection.into()), asks: false, reason, grantable: true }),
        None => Ok(()),
    }
}

/// The plugin as a whole, for a tool of Lorca's own that works with it (`browser_session` for
/// Browser): the plugin's switch covers it, whichever of the plugin's tools the bot may call.
pub fn check_plugin(app: &Arc<App>, bot: &Bot, connection: &str, tool: &str) -> Result<(), AccessDenied> {
    let current = current(app, bot, tool, Some(connection))?;
    match current.permissions.as_ref().filter(|policy| !policy.allows_connection(connection)) {
        Some(_) => Err(AccessDenied { tool: tool.into(), connection_id: Some(connection.into()), asks: false, reason: format!("{} is off for this bot", connection_name(app, connection)), grantable: true }),
        None => Ok(()),
    }
}

/// What the user calls an installed plugin: its status name, which tells two accounts of one
/// service apart.
fn connection_name(app: &App, connection: &str) -> String {
    app.plugins.lock().unwrap().status(connection).map(|status| status.name).unwrap_or_else(|| connection.to_string())
}

/// A roster from a Device that predates policies cannot silently remove restrictions. Users
/// restore full access by writing an explicit default policy, never by dropping the field.
pub fn keep_policies(current: &[Bot], incoming: &mut [Bot]) -> bool {
    let mut kept = false;
    for bot in incoming {
        if bot.permissions.is_none() {
            if let Some(policy) = current.iter().find(|old| old.id == bot.id).and_then(|old| old.permissions.clone()) {
                bot.permissions = Some(policy);
                kept = true;
            }
        }
    }
    kept
}

/// Editing access answers the outstanding requests; it never resumes refused calls.
pub fn dismiss_requests(app: &Arc<App>, bot_id: &str) {
    use crate::model::{Author, Body};
    let chats: Vec<String> =
        app.state.lock().unwrap().chats.iter().filter(|chat| chat.meta.bot_ids.iter().any(|id| id == bot_id)).map(|chat| chat.meta.id.clone()).collect();
    for chat_id in chats {
        for mut message in app.store.page(&chat_id, None, 200).map(|(messages, _)| messages).unwrap_or_default() {
            if message.author != (Author::Bot { bot_id: bot_id.into() }) {
                continue;
            }
            if let Body::Permission { tool, decision, .. } = &mut message.body {
                if tool == "access" && decision == "pending" {
                    *decision = "dismissed".into();
                    app.upsert_message(message, true);
                }
            }
        }
    }
}

/// Refuses the call and, when the user could grant it, asks them in the chat: a card that
/// opens the bot's Access settings or is dismissed, and never allows the call itself.
#[cfg(feature = "runner")]
pub fn refuse(app: &Arc<App>, chat_id: &str, bot: &Bot, denied: AccessDenied) -> lorca_agent::BeforeToolCallResult {
    use crate::model::{Author, Body, Message};
    let reason = denied.to_string();
    if !denied.grantable {
        return crate::local_review::blocked(reason);
    }
    // One waiting request per missing grant; repeated calls never flood the user's chat.
    let arguments = serde_json::json!({ "connection_id": denied.connection_id, "requested_tool": denied.tool, "asks": denied.asks });
    let recent = app.store.page(chat_id, None, 200).map(|(messages, _)| messages).unwrap_or_default();
    let duplicate = recent.iter().any(|message| {
        message.author == (Author::Bot { bot_id: bot.id.clone() })
            && matches!(&message.body, Body::Permission { tool, decision, arguments: existing, .. } if tool == "access" && decision == "pending" && *existing == arguments)
    });
    if !duplicate {
        // The apps word the card: a plugin and its tool, or what the bot wanted to do here.
        let (plugin_id, plugin_name, summary) = match &denied.connection_id {
            Some(connection) => {
                let name = connection_name(app, connection);
                (connection.clone(), name.clone(), format!("{name} · {}", denied.tool))
            }
            None => {
                let what = match denied.tool.as_str() {
                    "bash" | "bash_input" | "bash_output" | "coding_agent" => "Shell commands",
                    "write" | "edit" => "Changing files",
                    _ => "Reading files",
                };
                ("computer".into(), String::new(), what.into())
            }
        };
        let message = Message::new(
            chat_id,
            Author::Bot { bot_id: bot.id.clone() },
            Body::Permission {
                plugin_id,
                plugin_name,
                tool: "access".into(),
                summary,
                arguments,
                decision: "pending".into(),
                reason: None,
                command: None,
                rule: None,
                code: None,
                link: None,
                secret: None,
            ask: None,
            agent_ask: None,
            },
        );
        app.upsert_message(message, true);
    }
    crate::local_review::blocked(reason)
}

#[cfg(feature = "runner")]
pub(crate) mod guarded {
    use super::*;
    use async_trait::async_trait;
    use lorca_agent::{Tool, ToolError, ToolExecutionMode, ToolResult, ToolRunner, ToolUpdateFn};
    use serde_json::Value;
    use tokio_util::sync::CancellationToken;

    /// A local tool that checks the bot's Access again when it starts, after any review.
    struct GuardedTool {
        app: Arc<App>,
        bot: Bot,
        chat_id: String,
        tool: Arc<dyn Tool>,
    }

    pub fn tools(app: &Arc<App>, bot: &Bot, chat_id: &str, tools: Vec<Arc<dyn Tool>>) -> Vec<Arc<dyn Tool>> {
        tools.into_iter().map(|tool| Arc::new(GuardedTool { app: app.clone(), bot: bot.clone(), chat_id: chat_id.into(), tool }) as Arc<dyn Tool>).collect()
    }

    impl GuardedTool {
        fn check(&self, args: &Value) -> Result<(), ToolError> {
            check_tool_call(&self.app, &self.bot, self.name(), args).map_err(|denied| {
                let result = refuse(&self.app, &self.chat_id, &self.bot, denied);
                ToolError(result.reason.unwrap_or_default())
            })
        }
    }

    #[async_trait]
    impl Tool for GuardedTool {
        fn name(&self) -> &str {
            self.tool.name()
        }
        fn label(&self) -> &str {
            self.tool.label()
        }
        fn description(&self) -> &str {
            self.tool.description()
        }
        fn parameters(&self) -> Value {
            self.tool.parameters()
        }
        fn output_schema(&self) -> Option<Value> {
            self.tool.output_schema()
        }
        fn execution_mode(&self) -> Option<ToolExecutionMode> {
            self.tool.execution_mode()
        }
        fn prepare_arguments(&self, args: Value) -> Value {
            self.tool.prepare_arguments(args)
        }
        async fn execute(&self, id: &str, args: Value, cancel: CancellationToken, update: ToolUpdateFn) -> Result<ToolResult, ToolError> {
            self.check(&args)?;
            self.tool.execute(id, args, cancel, update).await
        }
        async fn execute_with(
            &self,
            id: &str,
            args: Value,
            cancel: CancellationToken,
            update: ToolUpdateFn,
            runner: &dyn ToolRunner,
        ) -> Result<ToolResult, ToolError> {
            self.check(&args)?;
            self.tool.execute_with(id, args, cancel, update, runner).await
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    struct Scratch(Arc<App>, std::path::PathBuf);
    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.1);
        }
    }
    fn scratch() -> (Scratch, Bot, String) {
        let home = std::env::temp_dir().join(format!("lorca-permissions-{}", uuid::Uuid::new_v4()));
        let app = App::load(crate::config::Config { home: home.clone(), port: 0 }).unwrap();
        crate::identity::create(&app, Some("Policy Runner".into())).unwrap();
        let bot = app.state.lock().unwrap().bots[0].clone();
        let chat_id = app.dm_with(&bot.id, None).unwrap().meta.id;
        (Scratch(app, home), bot, chat_id)
    }

    fn inbox_policy() -> BotPermissions {
        serde_json::from_value(json!({
            "connections": {"gmail-work": {"enabled": true, "tools": {"send_message": {"enabled": false}}}},
            "tools": {"shell": {"enabled": false}}
        }))
        .unwrap()
    }

    #[test]
    fn a_plugin_tool_is_on_off_or_asks_by_its_own_switch() {
        let policy = inbox_policy();
        let denial = |connection: &str, tool: &str| policy.connection_denial(connection, connection, tool);
        assert_eq!(denial("gmail-work", "list_messages"), None);
        assert!(denial("gmail-personal", "list_messages").is_some(), "a plugin the policy does not list is off");
        assert!(denial("gmail-work", "send_message").is_some(), "a tool switched off is refused");
        assert_eq!(policy.connection_tool("gmail-work", "list_messages"), ToolPermission::ON, "a tool not listed takes its plugin's switch");
        assert_eq!(policy.connection_tool("gmail-work", "send_message"), ToolPermission::OFF);
        assert!(policy.allows_connection("gmail-work") && !policy.allows_connection("gmail-personal"));
        let mut asking = inbox_policy();
        asking.connections.as_mut().unwrap().get_mut("gmail-work").unwrap().tools.insert("send_message".into(), ToolPermission::ASK);
        assert_eq!(asking.connection_denial("gmail-work", "Work", "send_message"), None, "asking is on: the card decides");
        assert_eq!(asking.connection_tool("gmail-work", "send_message").allowing(), Some(true));
        let mut off = inbox_policy();
        off.connections.as_mut().unwrap().get_mut("gmail-work").unwrap().enabled = false;
        assert!(!off.allows_connection("gmail-work") && off.connection_denial("gmail-work", "Work", "list_messages").is_some());
        assert!(BotPermissions::default().allows_connection("anything"), "a bot made before the model has every plugin");
    }

    #[test]
    fn a_new_bot_starts_with_every_plugin_off_and_its_own_tools_on() {
        let policy = BotPermissions::new_bot();
        assert!(!policy.allows_connection("github"), "a plugin the user has not turned on is off");
        assert_eq!(policy.connection_tool("github", "create_issue"), ToolPermission::OFF);
        assert_eq!(policy.local_tool("bash"), Some(ToolPermission::ON));
        assert_eq!(policy.local_denial("bash"), None);
    }

    #[test]
    fn shell_and_filesystem_gates_apply_even_to_readonly_commands() {
        let policy = inbox_policy();
        // The policy switches shell off and leaves read and write on.
        for tool in ["bash", "bash_input", "bash_output", "coding_agent"] {
            assert!(policy.local_denial(tool).is_some(), "{tool}");
        }
        for tool in ["read", "grep", "find", "ls", "write", "edit", "codemode", "memory_update", "message_bot", "attention", "project_context", "workflow_feedback"] {
            assert_eq!(policy.local_denial(tool), None, "{tool}");
        }
        let none = BotPermissions {
            tools: LocalPermissions { read: ToolPermission::OFF, write: ToolPermission::OFF, ..Default::default() },
            ..Default::default()
        };
        assert!(none.local_denial("read").is_some());
        let read_only = BotPermissions { tools: LocalPermissions { write: ToolPermission::OFF, ..Default::default() }, ..Default::default() };
        assert_eq!(read_only.local_denial("read"), None);
        assert!(read_only.local_denial("write").is_some(), "write does not follow read");
        assert!(serde_json::from_value::<BotPermissions>(json!({"connections": {"mail": {"enabled": "yes"}}})).is_err());
        // A policy from before the switches is read into them rather than refused.
        let old: BotPermissions = serde_json::from_value(json!({
            "filesystem": "read", "shell": false,
            "connections": {"gmail": {"capabilities": ["read", "draft"], "tools": ["list_messages"]}}
        }))
        .unwrap();
        assert_eq!((old.tools.read, old.tools.write, old.tools.shell), (ToolPermission::ON, ToolPermission::OFF, ToolPermission::OFF));
        assert_eq!(old.connection_tool("gmail", "list_messages").enabled, true);
        assert!(!old.connection_tool("gmail", "send_message").enabled, "an old tool allowlist leaves other tools off");
        assert!(!old.connection_tool("other", "anything").enabled);
    }

    #[test]
    fn a_running_turn_reads_revocation_and_a_moved_bot_is_refused_without_a_request() {
        let (scratch, snapshot, chat_id) = scratch();
        let app = &scratch.0;
        assert!(check_tool(app, &snapshot, "bash").is_ok());
        app.update_bot(&snapshot.id, |bot| bot.permissions = Some(inbox_policy())).unwrap();
        assert!(check_tool(app, &snapshot, "bash").unwrap_err().grantable);
        assert!(check_connection(app, &snapshot, "gmail-personal", "list_messages").is_err());
        let reopened = App::load(crate::config::Config { home: scratch.1.clone(), port: 0 }).unwrap();
        assert_eq!(reopened.bot(&snapshot.id).unwrap().permissions, Some(inbox_policy()), "the persisted profile retains policy");
        app.state.lock().unwrap().bots.iter_mut().find(|bot| bot.id == snapshot.id).unwrap().runner_id = "another-runner".into();
        let moved = check_tool(app, &snapshot, "read").unwrap_err();
        assert!(!moved.grantable && moved.reason.contains("moved"));
        #[cfg(feature = "runner")]
        {
            assert!(refuse(app, &chat_id, &snapshot, moved).block);
            assert!(app.store.page(&chat_id, None, 200).unwrap().0.is_empty(), "nobody is asked about a moved bot");
        }
        let _ = chat_id;
    }

    #[test]
    fn an_old_roster_cannot_drop_an_explicit_policy() {
        let (_scratch, mut bot, _) = scratch();
        bot.permissions = Some(inbox_policy());
        let mut old = bot.clone();
        old.permissions = None;
        assert!(keep_policies(&[bot.clone()], std::slice::from_mut(&mut old)));
        assert_eq!(old.permissions, bot.permissions);
        old.permissions = Some(BotPermissions::default());
        assert!(!keep_policies(&[bot], std::slice::from_mut(&mut old)), "a user's explicit full policy stands");
    }

    #[tokio::test]
    async fn api_policy_edits_reject_null_and_invalid_fields_without_changing_access() {
        let (scratch, bot, _) = scratch();
        let app = &scratch.0;
        crate::api::dispatch(app, "bots.update", json!({"id": bot.id, "permissions": inbox_policy()})).await.unwrap();
        for invalid in [json!(null), json!({"connections": []}), json!({"tools": "off"}), json!({"connections": {"": {"enabled": true}}})] {
            assert!(crate::api::dispatch(app, "bots.update", json!({"id": bot.id, "permissions": invalid})).await.is_err());
            assert_eq!(app.bot(&bot.id).unwrap().permissions, Some(inbox_policy()));
        }
        crate::api::dispatch(app, "bots.update", json!({"id": bot.id, "name": "Inbox"})).await.unwrap();
        assert_eq!(app.bot(&bot.id).unwrap().permissions, Some(inbox_policy()));
    }

    #[cfg(feature = "runner")]
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn nested_file_writes_and_shell_calls_cannot_execute_after_revocation() {
        use lorca_agent::codemode::{CodemodeOptions, CodemodeTool};
        use lorca_agent::{DirectRunner, ToolUpdateFn};
        use tokio_util::sync::CancellationToken;
        let (scratch, bot, chat_id) = scratch();
        let app = &scratch.0;
        // Build the execution catalog before the user changes the policy, like an active turn.
        let tools = guarded::tools(app, &bot, &chat_id, lorca_agent::tools::coding_tools(scratch.1.clone()));
        let catalog = crate::plugins::mcp::bot_catalog(app, &bot, &chat_id, tools);
        // Only the own tools go off: the plugins this bot may use are untouched.
        let mut policy = inbox_policy();
        policy.tools.write = ToolPermission::OFF;
        policy.tools.shell = ToolPermission::OFF;
        app.update_bot(&bot.id, |bot| bot.permissions = Some(policy)).unwrap();
        let script =
            "await tools.write({path:'forbidden.txt',content:'write happened'}); await tools.bash({command:'touch forbidden-shell',description:'touch'});";
        let codemode = CodemodeTool::new(catalog, CodemodeOptions::default());
        let run = codemode.run_script("nested", script, CancellationToken::new(), &DirectRunner).await.unwrap();
        assert!(run.result.is_error, "{}", run.result.text_content());
        assert!(run.result.text_content().contains("changing files is off"));
        assert!(!scratch.1.join("forbidden.txt").exists());
        assert!(!scratch.1.join("forbidden-shell").exists());
        let tools = guarded::tools(app, &bot, &chat_id, lorca_agent::tools::coding_tools(scratch.1.clone()));
        let bash = tools.iter().find(|tool| tool.name() == "bash").unwrap();
        let update: ToolUpdateFn = Arc::new(|_| {});
        let refused =
            bash.execute("direct", json!({"command": "touch forbidden-shell", "description": "Touch"}), CancellationToken::new(), update).await.unwrap_err();
        assert!(refused.0.contains("shell commands are off"));
        assert!(!scratch.1.join("forbidden-shell").exists());
    }

    #[cfg(feature = "runner")]
    #[tokio::test]
    async fn access_requests_route_to_the_user_and_never_add_grants_or_always_allow() {
        use crate::model::{AutoReviewRule, Body};
        let (scratch, bot, chat_id) = scratch();
        let app = &scratch.0;
        app.update_bot(&bot.id, |bot| bot.permissions = Some(inbox_policy())).unwrap();
        app.add_auto_review_rule(None, AutoReviewRule {
            id: "always".into(),
            text: "allow everything".into(),
            behavior: "allow".into(),
            tool: Some("gmail-personal/send_message".into()),
        });
        let denied = check_connection(app, &bot, "gmail-personal", "send_message").unwrap_err();
        assert!(refuse(app, &chat_id, &bot, denied.clone()).block);
        refuse(app, &chat_id, &bot, denied);
        let requests = app.store.page(&chat_id, None, 200).unwrap().0;
        assert_eq!(requests.len(), 1, "repeated missing grant makes one request");
        let card = &requests[0];
        assert!(matches!(&card.body, Body::Permission { tool, decision, summary, rule: None, reason: None, .. }
            if tool == "access" && decision == "pending" && summary == "gmail-personal · send_message"));
        for decision in ["allow", "always"] {
            let response = crate::api::dispatch(app, "chats.permission", json!({"chat_id": chat_id, "message_id": card.id, "decision": decision})).await;
            assert!(response.unwrap_err().contains("cannot grant permissions"));
        }
        assert_eq!(app.auto_review().rules.len(), 1);
        assert!(check_connection(app, &bot, "gmail-personal", "send_message").is_err());
        crate::api::dispatch(app, "chats.permission", json!({"chat_id": chat_id, "message_id": card.id, "decision": "deny"})).await.unwrap();
        assert!(matches!(app.message(&chat_id, &card.id).unwrap().body, Body::Permission { decision, .. } if decision == "dismissed"));
        // A shell refusal is worded for the apps, and saving Access answers what is still open.
        refuse(app, &chat_id, &bot, check_tool(app, &bot, "bash").unwrap_err());
        let shell = app.store.page(&chat_id, None, 200).unwrap().0.into_iter().find(|message| message.id != card.id).unwrap();
        assert!(matches!(&shell.body, Body::Permission { plugin_id, summary, .. } if plugin_id == "computer" && summary == "Shell commands"));
        crate::api::dispatch(app, "bots.update", json!({"id": bot.id, "permissions": {"tools": {"shell": {"enabled": true}}}})).await.unwrap();
        assert!(matches!(app.message(&chat_id, &shell.id).unwrap().body, Body::Permission { decision, .. } if decision == "dismissed"));
    }
}
