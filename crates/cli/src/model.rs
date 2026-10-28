//! Plaintext domain model. Lives on Devices; crosses the network only inside encrypted blobs.
//! Field names match what the macOS app decodes.

use serde::{Deserialize, Serialize};

pub const MAX_GROUP_BOTS: usize = 6;

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ProviderStatus {
    pub kind: String,
    pub is_connected: bool,
    pub detail: String,
    /// The model Auto-review runs on it unless the user picks another.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub review_model: Option<String>,
    /// A custom API base URL, when the credential has one.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub base_url: Option<String>,
    /// A custom provider's name, wire protocol, and models. Built-in providers have none.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub api: Option<crate::credentials::CustomApi>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub models: Vec<StatusModel>,
}

/// A custom provider's model as the apps show it: what its server's list said, and the thinking
/// levels it takes.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct StatusModel {
    #[serde(flatten)]
    pub model: crate::credentials::CustomModel,
    pub levels: Vec<lorca_models::ThinkingLevel>,
}

/// A paired machine or phone. `os` decides whether it is a Runner.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct Device {
    /// The machine signing public key, base64url.
    pub id: String,
    pub name: String,
    pub model: String,
    pub os: String,
    pub os_version: String,
    pub box_pubkey: String,
    /// Plugins installed on that Runner, with their setup state. Secrets stay on the Runner.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub plugins: Vec<PluginStatus>,
    /// The channels this Runner's bots listen on, with their state. Tokens stay on the Runner.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub channels: Vec<crate::channels::ChannelStatus>,
    /// The `lorca` this Device runs, as `lorca --version` says it.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub version: String,
    /// This Runner takes a message for a coding agent (`agent_message` jobs) and does not turn
    /// it into a bot turn. A Device from before it does not, so the apps hide agents there.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub agent_messages: bool,
    /// How a CLI that replaces itself keeps current: one installed with the site's script. Unset
    /// where an app updates the CLI it carries, and on phones.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub update: Option<UpdateStatus>,
    #[serde(default)]
    pub updated_at: i64,
}

/// A self-updating CLI's updates, as every Device sees them in its `machine` blob.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct UpdateStatus {
    /// Installs a newer release by itself, and restarts into it once no bot is at work there.
    pub auto: bool,
    /// The newest release the last check found, when it is newer than the running one.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub latest: Option<String>,
    /// `installing` while it downloads and swaps the binary; `restarting` once the new one is in
    /// place and the CLI waits for its bots to finish; empty otherwise.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub state: String,
    /// Why the last check or install failed, until one goes through.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// A plugin as its Runner advertises it in the machine blob: what is installed and whether it
/// is ready to use. Variables, tokens, and the package itself never leave the Runner.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct PluginStatus {
    pub id: String,
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub version: String,
    /// An SF Symbol name for the apps.
    #[serde(default)]
    pub icon: String,
    /// `ready`, `needs_setup` (a required variable is missing), `needs_auth` (sign in on the
    /// Runner), `connecting`, or `error`.
    pub state: String,
    /// What the state means, for a row's subtitle.
    #[serde(default)]
    pub detail: String,
    /// `mcp.json` for a server from the Runner's `mcp.json`, which the apps edit as one; none for
    /// a plugin from the marketplace or its own manifest.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    /// Marketplace service and user label of a named connection. `id` selects the instance.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub service_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub account_name: Option<String>,
}

impl Device {
    pub fn is_runner(&self) -> bool {
        is_runner_os(&self.os)
    }
}

/// A Device with this `os` is a Runner: a computer, not a phone or a tablet.
pub fn is_runner_os(os: &str) -> bool {
    matches!(os, "macos" | "linux" | "windows")
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Bot {
    pub id: String,
    pub name: String,
    /// What the bot does and how it should work, shown in its profile and used in its prompt.
    pub description: String,
    /// SF Symbol drawn on the accent gradient; the look when there is no image.
    pub symbol_name: String,
    pub accent: String,
    /// A custom profile image, a `file` blob like a message attachment, shown in place of the
    /// symbol and accent wherever the bot's avatar appears.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub avatar: Option<Attachment>,
    pub runner_id: String,
    pub provider: String,
    /// Model id for the provider. `None` means the provider's default.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    /// How much the model thinks: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`.
    /// `None` means the provider's default.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub thinking: Option<String>,
    /// Compatibility with profiles written before Description became the single behavioral
    /// field. It remains on the wire for one-version rolling upgrades, but is folded into
    /// `description` as soon as the profile is read.
    #[serde(default, rename = "instructions")]
    pub legacy_instructions: String,
    /// A bot made as `NEW_BOT_NAME` that is still working out its job with the user in its DM: it
    /// has a placeholder name and no description until `finish_setup` gives it both.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub setting_up: bool,
    /// Working directory for the coding tools on the Runner. Defaults to
    /// `<LORCA_HOME>/workspaces/<bot id>`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub workdir: Option<String>,
    /// User-controlled capabilities, synced in the encrypted roster. Missing means full
    /// existing access; an explicit policy can only be changed through the user's API.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub permissions: Option<crate::permissions::BotPermissions>,
    pub created_at: f64,
}

/// The placeholder name of a bot that sets itself up with the user.
pub const NEW_BOT_NAME: &str = "New Bot";

/// One Auto-review rule: what a bot wants to do, in the user's words, and whether that runs
/// on its own or asks first. Always allow on a shell command's card adds the rule Auto-review
/// proposed in plain language; on a plugin tool's card it adds a rule for that exact tool,
/// matched without another model review.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct AutoReviewRule {
    pub id: String,
    pub text: String,
    /// `allow` (runs automatically) or `ask` (asks first; wins when rules conflict).
    pub behavior: String,
    /// The exact plugin tool (`github/create_issue`) a card's Always allow saved this rule for.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool: Option<String>,
}

/// Auto-review, after Grok Bot: with it on, a Runner checks effectful plugin actions and shell
/// commands before they run and asks the user only when needed; off, every such action asks.
/// Shared by every Device through the roster.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct AutoReview {
    pub is_enabled: bool,
    #[serde(default)]
    pub rules: Vec<AutoReviewRule>,
    /// The provider that reviews, any the account has connected; unset for the bot's own.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub provider: Option<String>,
    /// Each provider's review model the user picked, by kind: a chat model or a decision model.
    /// A provider without one reviews with its default
    /// ([`Credentials::review_model`](crate::credentials::Credentials::review_model)).
    #[serde(default, skip_serializing_if = "std::collections::BTreeMap::is_empty")]
    pub models: std::collections::BTreeMap<String, String>,
}

impl Default for AutoReview {
    fn default() -> Self {
        AutoReview { is_enabled: true, rules: Vec::new(), provider: None, models: Default::default() }
    }
}

impl AutoReview {
    /// The rule made for this exact tool, if any.
    pub fn rule_for(&self, plugin_id: &str, tool: &str) -> Option<&AutoReviewRule> {
        let key = format!("{plugin_id}/{tool}");
        self.rules.iter().find(|r| r.tool.as_deref() == Some(&key))
    }
}

/// The part of a bot's profile its system prompt is made from and that applies mid-turn. Its
/// provider, model, thinking, and working directory apply from its next turn: a turn's tools and
/// provider are made with them when it starts.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Profile {
    pub name: String,
    pub description: String,
    pub setting_up: bool,
}

impl Profile {
    pub fn of(bot: &Bot) -> Profile {
        Profile { name: bot.name.clone(), description: bot.description.clone(), setting_up: bot.setting_up }
    }
}

/// A change to a bot's profile that its direct chat notes: a finished setup, or a new name or
/// description. A new look, Access, or model is not one.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProfileChange {
    SetUp,
    Renamed,
    Described,
    RenamedAndDescribed,
}

/// What a profile notice in a chat marks, read back from its message id.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProfileNotice {
    SetUp,
    Changed,
}

/// How every profile notice's message id starts; the kind follows it.
const PROFILE_NOTICE_PREFIX: &str = "profile-";
const PROFILE_NOTICE_SETUP: &str = "setup-";

impl ProfileChange {
    pub fn between(before: &Profile, after: &Profile) -> Option<ProfileChange> {
        if before.setting_up && !after.setting_up {
            return Some(ProfileChange::SetUp);
        }
        if after.setting_up {
            return None;
        }
        match (before.name != after.name, before.description != after.description) {
            (true, true) => Some(ProfileChange::RenamedAndDescribed),
            (true, false) => Some(ProfileChange::Renamed),
            (false, true) => Some(ProfileChange::Described),
            (false, false) => None,
        }
    }

    /// The notice's words, as every Device shows them.
    pub fn notice(self, before: &Profile, after: &Profile) -> String {
        match self {
            ProfileChange::SetUp => format!("Set up as {}. Its job is saved; from here on it works as {}.", after.name, after.name),
            ProfileChange::RenamedAndDescribed => format!("{} was renamed {} and its description changed.", before.name, after.name),
            ProfileChange::Renamed => format!("{} was renamed {}.", before.name, after.name),
            ProfileChange::Described => format!("{}'s description changed.", after.name),
        }
    }

    /// A new notice's message id, which carries what it marks.
    pub fn notice_id(self) -> String {
        let kind = if self == ProfileChange::SetUp { PROFILE_NOTICE_SETUP } else { "" };
        format!("{PROFILE_NOTICE_PREFIX}{kind}{}", uuid::Uuid::new_v4())
    }
}

impl ProfileNotice {
    /// What the profile notice with this message id marks; `None` for any other message.
    pub fn of_id(id: &str) -> Option<ProfileNotice> {
        let rest = id.strip_prefix(PROFILE_NOTICE_PREFIX)?;
        Some(if rest.starts_with(PROFILE_NOTICE_SETUP) { ProfileNotice::SetUp } else { ProfileNotice::Changed })
    }
}

impl From<ProfileChange> for ProfileNotice {
    fn from(change: ProfileChange) -> ProfileNotice {
        if change == ProfileChange::SetUp { ProfileNotice::SetUp } else { ProfileNotice::Changed }
    }
}

impl Bot {
    /// Folds the old Instructions field into Description once. Existing paired Devices may
    /// still send the old field during a rolling upgrade, so an empty compatibility value is
    /// kept on the wire while current clients expose only Description.
    pub fn normalize_description(&mut self) -> bool {
        let legacy = self.legacy_instructions.trim();
        if legacy.is_empty() {
            return false;
        }
        let description = self.description.trim();
        if description.is_empty() {
            self.description = legacy.to_string();
        } else if description != legacy && !description.ends_with(&format!("\n\n{legacy}")) {
            self.description = format!("{description}\n\n{legacy}");
        } else {
            self.description = description.to_string();
        }
        self.legacy_instructions.clear();
        true
    }

    /// Where this bot's tools run, keyed by id so renames never move files. Created on first use.
    pub fn working_directory(&self, lorca_home: &std::path::Path) -> std::path::PathBuf {
        let home = dirs::home_dir().unwrap_or_else(|| std::path::PathBuf::from("."));
        match self.workdir.as_deref().map(str::trim).filter(|w| !w.is_empty()) {
            Some(dir) if dir.starts_with('~') => home.join(dir.trim_start_matches('~').trim_start_matches('/')),
            Some(dir) => std::path::PathBuf::from(dir),
            None => lorca_home.join("workspaces").join(&self.id),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Author {
    You,
    Bot { bot_id: String },
    System,
    /// Someone outside Lorca writing in a channel's conversation (a Telegram group, a Slack
    /// thread). Their words are data for the bot, never the user's instructions or approval.
    Contact { name: String },
}

/// A file sent with a message. Its bytes travel as a `file` blob whose id is this id,
/// encrypted with the account key; Devices keep a copy under `~/.lorca/files/<id>`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Attachment {
    pub id: String,
    pub name: String,
    pub mime: String,
    pub size: u64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub width: Option<u32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub height: Option<u32>,
    /// SHA-256 of the bytes, hex. The message carries it under the newest account key, so a
    /// download that does not match it is refused (`files::ensure_local`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sha256: Option<String>,
}

impl Attachment {
    pub fn is_image(&self) -> bool {
        self.mime.starts_with("image/")
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Body {
    Text {
        text: String,
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        attachments: Vec<Attachment>,
        /// The bots the user's `@Name`s address, by id, in the order the user picked them: a
        /// bot reads "@Scout (id bot-1a2b3c4d)", and a group offers them the first turns.
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        mentions: Vec<String>,
        /// The coding agents the user's `@Name`s address, by id, in the order picked: the message
        /// goes to them, and no bot turn starts.
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        agents: Vec<String>,
        /// The message the user answers with this one, quoted.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        reply_to: Option<ReplyTo>,
    },
    Tool {
        name: String,
        summary: String,
        detail: String,
        is_running: bool,
        #[serde(default)]
        call_id: String,
        #[serde(default)]
        arguments: serde_json::Value,
        #[serde(default)]
        result: Option<String>,
        #[serde(default)]
        is_error: bool,
        /// What the call does, in the bot's words: a shell command's `description`, which the
        /// status line reads as "Running command: Install dependencies…".
        #[serde(default, skip_serializing_if = "Option::is_none")]
        description: Option<String>,
        /// The bot a message_bot call goes to, by id: the apps read "Messaging Scout…" while it
        /// runs and show the finished row as "Messaged ◉ Scout".
        #[serde(default, skip_serializing_if = "Option::is_none")]
        target_bot_id: Option<String>,
        /// A codemode script's latest command, by its `description`, while no plugin call came
        /// after it: the status line reads "Running command: Run the tests…" instead of the
        /// plugin `description` names.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        script_command: Option<String>,
        /// A `bash` call's card, from Auto-review's question to how the command ended. Every
        /// `bash` row has one; the apps show it in place of the row.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        run: Option<CommandRun>,
        /// The card of the coding agent a `coding_agent` call started, for as long as the agent
        /// runs and after: the apps show it in place of the row.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        agent: Option<AgentRun>,
    },
    Handoff {
        from: String,
        to: String,
        reason: String,
    },
    Notice {
        text: String,
        /// Set on the "Routine · Name" marker that opens a routine's run.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        routine_id: Option<String>,
    },
    /// The bot asks before a reviewed plugin or shell action (or before installing a
    /// plugin, with `tool` = `install`). The turn waits for `decision`: `pending`, `allowed`
    /// (once), `always`, `denied`, or `expired`.
    Permission {
        plugin_id: String,
        plugin_name: String,
        tool: String,
        /// One line about the call: "create_issue · repo: lorca, title: …".
        summary: String,
        #[serde(default)]
        arguments: serde_json::Value,
        decision: String,
        /// Why Auto-review paused the action, when it did.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        reason: Option<String>,
        /// The plain-language rule Always allow adds, which Auto-review proposed for a shell
        /// command. A shell card without one offers only Allow once and Deny.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        rule: Option<String>,
        /// A shell card's whole command, for the apps, which never get `arguments`: `for_app`
        /// fills it with the first `APP_COMMAND_CHARS` characters.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        command: Option<String>,
        /// A sign-in card mid-flow: where to go and the code to enter there (device flow).
        #[serde(default, skip_serializing_if = "Option::is_none")]
        link: Option<String>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        code: Option<String>,
        /// A secret request (`tool` = `secret`): the values the bot asks for and where they go.
        /// The answer never comes back into the card: it is sealed to the bot's Runner.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        secret: Option<SecretAsk>,
        /// A question with options (`tool` = `ask`): what the bot asks, its options, and, once
        /// answered, what the user picked, which every Device shows.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        ask: Option<AskQuestion>,
        /// A coding agent's question (`tool` = `agent_ask`): its form, and once it is answered,
        /// what was given and by whom.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        agent_ask: Option<AgentAsk>,
    },
    /// An email or Slack message the bot wrote in the chat, waiting for the user to send it: the
    /// chat's view of its review item (`review-status-<review id>`), rewritten as it changes.
    Draft {
        review_id: String,
        /// The item's version, which an edit, Send, and Discard name.
        version: u64,
        /// The item's state: `pending`, `approved`, `executing`, `succeeded`, `failed`,
        /// `rejected`, `cancelled`, or `uncertain`.
        state: String,
        plugin_id: String,
        /// The account it goes out from: "Gmail · Work".
        account: String,
        draft: MessageDraft,
        /// Why it was not sent, or why it needs another look.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        note: Option<String>,
        /// Whether, with drafts off, the bot sends such a message itself: false for a mail
        /// server that only makes drafts, which keeps them in the account's Drafts.
        #[serde(default)]
        direct: bool,
    },
}

/// A message a bot wrote to people, as its draft card shows and edits it.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct MessageDraft {
    /// `email` or `slack`.
    #[serde(default)]
    pub kind: String,
    /// Email addresses, or the Slack channel or person's id.
    #[serde(default)]
    pub to: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub cc: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub bcc: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub subject: String,
    #[serde(default)]
    pub body: String,
    /// The files it carries, by name; an edit keeps the ones it still names.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub attachments: Vec<DraftAttachment>,
    /// What it answers: the email's id, or the Slack thread.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reply: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct DraftAttachment {
    pub name: String,
    #[serde(default)]
    pub size: u64,
}

/// A question the bot asks with options (`ask_user`): one pick, or several with `multiple`, and a
/// line of the user's own words with `allow_other`.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AskQuestion {
    pub question: String,
    pub options: Vec<AskOption>,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub multiple: bool,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub allow_other: bool,
    /// The option the bot recommends, by its place; the apps mark it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub recommended: Option<usize>,
    /// What the user answered, once they did.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub answer: Option<AskAnswer>,
}

/// One option of a question: its words, and a line that says what it means.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AskOption {
    pub label: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
}

/// The user's answer: the options picked, by their place, and their own words for Other.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AskAnswer {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub choices: Vec<usize>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub other: Option<String>,
}

/// The `tool` of a coding agent's question card. It is here rather than in `coding`, which only a
/// Runner builds, because every Device reads the card.
pub const ASK_TOOL: &str = "agent_ask";

/// What a coding agent asks the user, on a card of its own in the chat (`tool` = `agent_ask`): its
/// message and the fields of its form. The card is the bot's; its answer goes to the agent through
/// the `coding_agent` row that started it (`coding.answer`). The card's `decision` is `pending`,
/// `allowed` (answered), `denied` (declined), `expired` (nobody answered in time), or `dismissed`
/// (the agent stopped first).
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentAsk {
    /// The agent's handle and kind, and what it is called: `agent-3f9a2c1d`, `claude`, "Claude Code".
    pub agent_id: String,
    pub kind: String,
    pub name: String,
    /// The `coding_agent` row whose agent asks, which the answer names.
    pub row_id: String,
    pub message: String,
    pub fields: Vec<AgentAskField>,
    /// Once answered: each field the answer filled, in words, in the form's order.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub answers: Vec<AgentSetting>,
    /// Who answered or declined it: `you` or `bot`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub by: Option<String>,
}

/// One field of a coding agent's question.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentAskField {
    /// What the answer names it by.
    pub key: String,
    pub label: String,
    /// `one` (a pick among `choices`), `many` (any of them), `flag` (yes or no), `text`,
    /// `number`, or `integer`.
    pub kind: String,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub required: bool,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub choices: Vec<AgentAskChoice>,
    /// A pick may be the user's own words instead (`one`), or beside the picks (`many`).
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub other: bool,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentAskChoice {
    /// What the answer gives for it.
    pub value: String,
    pub title: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
}

/// What a secret request asks for: one or more named values, and where the Runner uses them.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SecretAsk {
    /// `browser` (typed into a sign-in page of `site` in the bot's Browser), `command` (an
    /// environment variable of the bot's commands), or `plugin` (a setting of the card's plugin).
    #[serde(rename = "use")]
    pub target: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub site: Option<String>,
    pub fields: Vec<SecretField>,
}

/// One value a secret request asks for: the name the bot uses it by, and what the card calls it.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SecretField {
    pub name: String,
    pub label: String,
}

/// A message quoted by the user's reply: who wrote it and how it opens, kept with the reply, so
/// the quote reads the same where the original has not loaded or has gone.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ReplyTo {
    pub message_id: String,
    pub author: Author,
    /// Its text on one line, at most `REPLY_QUOTE_CHARS` characters, or the names of its files.
    pub text: String,
    /// The coding agent that wrote it, when one did, so the quote shows the agent's mark.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub agent: Option<AgentAuthor>,
}

/// How much of a quoted message a reply keeps.
pub const REPLY_QUOTE_CHARS: usize = 280;

/// Message Markdown as the words it shows: no markers, a space where a line or a block breaks.
fn plain_text(markdown: &str) -> String {
    use pulldown_cmark::{Event, Parser, TagEnd};
    let mut out = String::with_capacity(markdown.len());
    for event in Parser::new(markdown) {
        match event {
            Event::Text(text) | Event::Code(text) | Event::InlineMath(text) | Event::DisplayMath(text) => out.push_str(&text),
            Event::SoftBreak | Event::HardBreak | Event::Rule => out.push(' '),
            Event::End(TagEnd::Paragraph | TagEnd::Item | TagEnd::Heading(_) | TagEnd::CodeBlock | TagEnd::TableCell | TagEnd::TableRow) => out.push(' '),
            _ => {}
        }
    }
    out
}

impl ReplyTo {
    /// The quote of `message`, when it is one a reply can answer: a text message, the user's or
    /// a bot's.
    pub fn quoting(message: &Message) -> Option<ReplyTo> {
        let Body::Text { text, attachments, .. } = &message.body else { return None };
        let mut line = plain_text(text).split_whitespace().collect::<Vec<_>>().join(" ");
        if line.is_empty() {
            line = attachments.iter().map(|attachment| attachment.name.as_str()).collect::<Vec<_>>().join(", ");
        }
        if line.chars().count() > REPLY_QUOTE_CHARS {
            line = format!("{}…", line.chars().take(REPLY_QUOTE_CHARS).collect::<String>().trim_end());
        }
        Some(ReplyTo { message_id: message.id.clone(), author: message.author.clone(), text: line, agent: message.agent.clone() })
    }
}

/// What a coding agent's work changed against the commit it started from: its files, and the
/// lines added and removed.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentChanges {
    pub files: u32,
    pub added: u32,
    pub removed: u32,
}

/// What a message to a coding agent carries to the Runner that runs it: the agent's id and the
/// user's text as typed. The Runner strips the agent's `@Name` before it sends it.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentMessage {
    pub id: String,
    pub text: String,
}

/// The Job kind that hands a message to a coding agent rather than a bot turn.
pub const AGENT_MESSAGE_JOB: &str = "agent_message";
/// A coding agent (Claude Code, Codex, or another that speaks ACP) a bot runs on its Runner, as the card on the
/// `coding_agent` row that started it shows it (`crate::coding`): Auto-review's question before
/// it starts, what it works on and where, how it stands, its last lines, and what it asks.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentRun {
    /// The bot's handle for it, across turns: `agent-3f9a2c1d`.
    pub id: String,
    /// `claude` (Claude Code), `codex`, or another agent that speaks ACP, such as `gemini`.
    pub kind: String,
    /// What the apps call it: "Claude Code", "Gemini CLI". Empty on a card from before agents
    /// over ACP, which the apps name from `kind`.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    /// What it shows of its session besides the work: its model, how hard it thinks, its mode,
    /// how much of its context it has used.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub settings: Vec<AgentSetting>,
    /// The settings that can be changed while it runs (`coding.set`), with what each offers.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub controls: Vec<AgentControl>,
    /// What it was asked: the prompt's first line.
    pub task: String,
    /// Where it works, from the home folder: the worktree Lorca made for it, or a folder that
    /// is not a repository.
    #[serde(default)]
    pub folder: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub branch: Option<String>,
    /// `checking` (Auto-review is judging the start), `asking` (a `question` waits for the
    /// user), `starting`, `working`, `idle` (done with what it was asked; it takes a follow-up).
    /// Once it ended: `exited`, `failed`, `stopped`, `denied` (not allowed to start), `expired`
    /// (nobody answered in time), or `dismissed` (the user wrote instead of answering).
    pub state: String,
    /// It is working, and has shown nothing new for a while.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub stalled: bool,
    /// Auto-review held its start and told the bot why, without asking the user: the card is
    /// `denied`, and the apps read it as "Held by Auto-review".
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub held: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub question: Option<AgentQuestion>,
    /// The card in the chat of the question it asks, by message id, while it waits for an answer
    /// there (`Body::Permission` with an `agent_ask`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub asking: Option<String>,
    /// The title the agent gave its session.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    /// Its last lines, as its transcript shows them.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub output: Option<String>,
    /// How it ended, in words: "Stopped", "Claude Code exited".
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub outcome: Option<String>,
    /// The pull request it opened, when its transcript names one.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub pull_request: Option<String>,
    /// What its work changed against the commit it started from.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub changes: Option<AgentChanges>,
    /// The Runner it runs on, by name: "Workbench".
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub device: Option<String>,
    /// When it started, in Unix seconds.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub started_at: Option<f64>,
    /// How it stands, which the apps tint it by: `ask` (it needs the user, or went quiet while it
    /// works), `busy` (it is checked, starts, or works), `done` (idle), `fail`, or `quiet` (it
    /// ended otherwise). Lorca fills it, with `busy`, `context`, and `now`, on every card it
    /// sends the apps (`AgentRun::summarize`).
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub tone: String,
    /// It is doing something now: the apps' dot breathes, and its time ticks.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub busy: bool,
    /// How much of its context it has used, from its `Context` setting.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub context: Option<AgentContextUse>,
    /// What it does now: its last tool's line, without the mark (`Bash(npm test)`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub now: Option<String>,
    /// How it stands, as the key of the words its pill says, which the apps translate:
    /// `starting`, `needs_you`, `working`, `quiet`, `done`, `ended`, `failed`, `held`,
    /// `not_allowed`, `no_answer`, `dismissed`, or `stopped`.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    /// Its model's name as a cell has room for: without a gateway's prefix and the context hints
    /// after it ("openrouter/claude-sonnet[1M]" reads "claude-sonnet").
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    /// Its pull request's name, "acme/shop #7", when `pull_request` is a pull request's link.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub pull_request_name: Option<String>,
    /// Its last lines with something on them, at most `AGENT_TAIL_LINES`, while it starts or works
    /// and asks nothing: what the consoles show.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub tail: Vec<String>,
}

/// How many of its last lines a coding agent's console shows.
pub const AGENT_TAIL_LINES: usize = 5;

/// The tokens of its context a coding agent has used, of how many it has.
#[derive(Debug, Clone, Copy, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentContextUse {
    pub used: u64,
    pub size: u64,
}

/// One thing a coding agent's card shows about its session, as the agent names it: "Model" and
/// "Opus", "Context" and "34k / 200k".
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentSetting {
    pub name: String,
    pub value: String,
}

/// A setting of a running coding agent that the card lets be changed.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentControl {
    /// `model` or `effort`, which `coding.set` names it by.
    pub what: String,
    /// What the agent calls it: "Model".
    pub name: String,
    /// The value it has now.
    pub value: String,
    pub choices: Vec<AgentChoice>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentChoice {
    pub value: String,
    pub name: String,
}

impl AgentRun {
    /// What the apps call it: the card's name, else the name of its kind.
    pub fn display_name(&self) -> &str {
        if self.name.is_empty() { agent_name(&self.kind) } else { &self.name }
    }

    /// It has not ended: it may still ask, work, or take a follow-up.
    pub fn is_open(&self) -> bool {
        matches!(self.state.as_str(), "checking" | "asking" | "starting" | "working" | "idle")
    }

    /// Fills what the apps read of how it stands from the rest of the card: `tone`, `busy`,
    /// `context`, `now`, `status`, `model`, `pull_request_name`, and `tail`. A card stored or
    /// synced before these existed gets them too.
    pub fn summarize(&mut self) {
        let asks = self.question.is_some() || self.asking.is_some() || self.state == "asking";
        let state = self.state.as_str();
        self.tone = match state {
            _ if asks || (self.stalled && state == "working") => "ask",
            "starting" | "checking" | "working" => "busy",
            "idle" => "done",
            "failed" => "fail",
            _ => "quiet",
        }
        .into();
        self.busy = !asks && !self.stalled && matches!(state, "starting" | "working");
        self.context = self.settings.iter().find(|setting| setting.name == "Context").and_then(|setting| {
            let (used, size) = setting.value.split_once('/')?;
            let (used, size) = (token_count(used)?, token_count(size)?);
            (size > 0).then_some(AgentContextUse { used, size })
        });
        self.now = self.output.as_deref().and_then(|output| output.lines().rev().find_map(|line| line.trim().strip_prefix("● ").map(str::to_string)));
        self.status = match state {
            _ if self.question.is_some() || self.asking.is_some() => "needs_you",
            "checking" | "starting" => "starting",
            "asking" => "needs_you",
            "working" if self.stalled => "quiet",
            "working" => "working",
            "idle" => "done",
            "exited" => "ended",
            "failed" => "failed",
            "denied" if self.held => "held",
            "denied" => "not_allowed",
            "expired" => "no_answer",
            "dismissed" => "dismissed",
            _ => "stopped",
        }
        .into();
        // The model as the card shows it: the name of the choice in force where it can change,
        // else the agent's own word for it.
        self.model = self.settings.iter().find(|setting| setting.name == "Model").map(|setting| {
            let control = self.controls.iter().find(|control| control.name == setting.name);
            let name = control.and_then(|control| control.choices.iter().find(|choice| choice.value == control.value)).map_or(setting.value.as_str(), |choice| choice.name.as_str());
            model_short_name(name).to_string()
        });
        self.pull_request_name = self.pull_request.as_deref().and_then(pull_request_name);
        self.tail = match (&self.output, self.question.is_none() && matches!(state, "starting" | "working")) {
            (Some(output), true) => {
                let lines: Vec<&str> = output.lines().filter(|line| !line.trim().is_empty()).collect();
                lines[lines.len().saturating_sub(AGENT_TAIL_LINES)..].iter().map(|line| line.to_string()).collect()
            }
            _ => Vec::new(),
        };
    }
}

/// A model's name without the provider a gateway puts before it and the context hints after it.
fn model_short_name(name: &str) -> &str {
    let mut short = &name[name.rfind('/').map_or(0, |at| at + 1)..];
    while short.ends_with(']') {
        match short.rfind('[') {
            Some(at) if at > 0 => short = &short[..at],
            _ => break,
        }
    }
    short
}

/// "acme/shop #7" for a pull request's link on GitHub, GitLab, or a host like them.
fn pull_request_name(link: &str) -> Option<String> {
    let path = link.strip_prefix("https://").or_else(|| link.strip_prefix("http://")).unwrap_or(link);
    let parts: Vec<&str> = path.trim_end_matches('/').split('/').collect();
    // The repository is what lies between the host and the marker: a GitLab group may nest, and
    // GitLab puts `-` before the marker.
    let at = parts.iter().position(|part| matches!(*part, "pull" | "pulls" | "merge_requests"))?;
    let number = parts.get(at + 1).filter(|number| !number.is_empty() && number.bytes().all(|b| b.is_ascii_digit()))?;
    let repo: Vec<&str> = parts[1..at].iter().copied().filter(|part| *part != "-").collect();
    (repo.len() >= 2).then(|| format!("{} #{number}", repo.join("/")))
}

/// A token count as a card's `Context` setting writes it: "34k", "1.2M", or "512".
fn token_count(text: &str) -> Option<u64> {
    let text = text.trim();
    let (number, scale) = match text.strip_suffix('k') {
        Some(number) => (number, 1e3),
        None => match text.strip_suffix('M') {
            Some(number) => (number, 1e6),
            None => (text, 1.0),
        },
    };
    let value: f64 = number.trim().parse().ok()?;
    (value >= 0.0).then(|| (value * scale).round() as u64)
}

/// What a coding agent's card asks the user: whether it may start or do something. What the
/// agent itself asks is a card of its own in the chat (`AgentAsk`).
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentQuestion {
    /// `start`: Auto-review asks before the agent starts. `command`: the agent wants to run
    /// `command`. Answered with `chats.permission`. (`choices` and `text`, from before the
    /// agent's questions had a card of their own, appear only on old cards.)
    pub kind: String,
    /// The question (`choices`, `text`).
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub text: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub command: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub choices: Vec<String>,
    /// Why Auto-review asked.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    /// The plain-language rule Always allow adds, which Auto-review proposed.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rule: Option<String>,
}

/// One line about what a coding agent's card asks, for a notification: "Claude Code: $ git push".
pub fn agent_question_summary(agent: &AgentRun, question: &AgentQuestion) -> String {
    let name = match agent.name.as_str() {
        "" if agent.kind == "codex" => "Codex",
        "" => "Claude Code",
        name => name,
    };
    let what = match question.kind.as_str() {
        "start" => format!("start in {}", agent.folder),
        "command" => format!("$ {}", question.command.as_deref().unwrap_or("").lines().next().unwrap_or("").trim()),
        _ => question.text.lines().map(str::trim).filter(|line| !line.is_empty()).next_back().unwrap_or("").to_string(),
    };
    format!("{name}: {}", what.chars().take(180).collect::<String>())
}

/// A `bash` call as its card shows it: Auto-review checking it, the question it asks, the
/// command running in its terminal (`lorca_agent::tools::BashSession`), what the command asks,
/// and how it ended.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct CommandRun {
    /// The terminal session running it, once one does: what `bash_input`, `bash_output`,
    /// `bash.stdin`, and `bash.stop` name it by (`bash-3f9a2c`). None before it starts, and on
    /// Windows, where a command runs on pipes.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
    /// The command, for the card, which the apps get without the call's arguments: its first
    /// `APP_COMMAND_CHARS` characters.
    #[serde(default)]
    pub command: String,
    /// `checking` (Auto-review is judging it), `asking` (for the user's permission), `running`
    /// (printing, or silent), `waiting` (at a question the user can read). Once it ended:
    /// `exited`, `failed` (a nonzero code), `stopped`, `denied` (not allowed, or held by
    /// Auto-review, so never run),
    /// `expired` (nobody answered in time), or `dismissed` (the user sent a new message instead
    /// of answering, so never run).
    pub state: String,
    /// The line it asks with while it waits: "[sudo] password for ana:".
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub prompt: Option<String>,
    /// The bot left the command to the user: its turn ended with the command still running, or
    /// it waits on the command while the command asks. The apps show a live command's card from
    /// then until it ends; before, the bot is still dealing with it.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub handed_over: bool,
    /// It runs in the background: the bot started it there, or the user sent it from Running
    /// tasks (`bash.background`). Stop in the chat leaves it running.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub background: bool,
    /// Its last lines, as the bottom of a terminal shows them: what it said after an answer
    /// ("Sorry, try again."). Never what was typed, which the terminal does not echo.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub output: Option<String>,
    /// How it ended, in words: "Command exited with code 0", "Stopped".
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub outcome: Option<String>,
    /// The Runner it runs on, by name, for Auto-review's question: "Workbench".
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub device: Option<String>,
    /// Why Auto-review asked, or held it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    /// Auto-review held it and told the bot why, without asking the user: the card is
    /// `denied`, and the apps read it as "Held by Auto-review".
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub held: bool,
    /// The plain-language rule Always allow adds, which Auto-review proposed. Without one the
    /// question offers only Allow once and Deny.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rule: Option<String>,
    /// The answer to Auto-review's question: `allowed` (once), `always`, `denied`, `expired`, or
    /// `dismissed`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub decision: Option<String>,
}

impl CommandRun {
    /// The command still runs: the card takes answers and a Stop when a session runs it.
    pub fn is_live(&self) -> bool {
        matches!(self.state.as_str(), "waiting" | "running")
    }

    /// It has not ended: Auto-review checks it, it asks, or the command runs.
    pub fn is_open(&self) -> bool {
        self.is_live() || matches!(self.state.as_str(), "checking" | "asking")
    }
}

impl Body {
    pub fn text(text: impl Into<String>) -> Self {
        Body::Text { text: text.into(), attachments: Vec::new(), mentions: Vec::new(), agents: Vec::new(), reply_to: None }
    }

    /// The coding agents a text message addresses, by id; none for every other body.
    pub fn agents(&self) -> &[String] {
        match self {
            Body::Text { agents, .. } => agents,
            _ => &[],
        }
    }

    /// The user addressed at least one coding agent: the message is not bot work.
    pub fn addresses_agent(&self) -> bool {
        !self.agents().is_empty()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum MessageState {
    Thinking,
    Streaming,
    Complete,
    Failed { error: String },
}

/// The coding agent that wrote a message on its bot's behalf: its id, what it is called, and its
/// kind, for the mark and color. Kept with the message so the conversation outlives the agent's
/// record, which is forgotten a month after it ends.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct AgentAuthor {
    pub id: String,
    pub name: String,
    pub kind: String,
}

/// What every app calls a coding agent of `kind`, when its card does not carry a name.
pub fn agent_name(kind: &str) -> &'static str {
    match kind {
        "claude" | "claude-acp" => "Claude Code",
        "codex" | "codex-acp" => "Codex",
        "gemini" => "Gemini CLI",
        "copilot" => "GitHub Copilot",
        "cursor" => "Cursor",
        "opencode" => "OpenCode",
        "kimi" => "Kimi Code",
        "kiro" => "Kiro",
        "pi" => "pi",
        _ => "Coding agent",
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Message {
    pub id: String,
    pub chat_id: String,
    pub author: Author,
    pub body: Body,
    pub state: MessageState,
    /// Unix seconds.
    pub created_at: f64,
    /// A user message typed during a turn becomes model-visible at the next safe boundary.
    /// The apps keep showing when it was typed; transcript rebuilding uses this later time.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub promoted_at: Option<f64>,
    /// A user message the turn at work holds for its next step: the turn reads it once the
    /// step's reply and tools are done, or at once when the user asks (`chats.send_now`).
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub queued: bool,
    /// A published deliverable or verification artifact; each version is a separate message.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub output: Option<crate::outputs::Output>,
    /// Coordinator summaries, urgent escalations, and quiet specialist reports have their
    /// own notification policy, shared by the Runner and the apps.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub notification: Option<crate::attention::Notification>,
    /// The message's id on the channel a conversation mirrors: a Telegram message id or a
    /// Slack `ts`. Set on a contact's message and on what the bot sent there.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    /// When the user last edited this message of theirs and sent it again (`chats.rerun`), in
    /// Unix seconds; the apps say Edited.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub edited_at: Option<f64>,
    /// The coding agent that wrote this message for its bot, when one did: the chat shows its
    /// bubble as the agent's, and the bot reads it as its delegate's words, not its own.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub agent: Option<AgentAuthor>,
}

/// How much of a tool call's detail the apps get: enough for the "Messaged ◉ X" marker.
const APP_TOOL_DETAIL_CHARS: usize = 400;
/// How much of a shell command a permission card or a command's card carries to the apps.
pub const APP_COMMAND_CHARS: usize = 8000;
/// How many of a chat's newest messages a snapshot carries; older ones are asked for by page.
pub const SNAPSHOT_MESSAGES: usize = 60;

impl Message {
    /// The message as the apps get it. A tool row keeps what they show (the name, the summary,
    /// whether it runs) and drops what only a later turn's context needs: the arguments and
    /// the result, which run to hundreds of kilobytes for a file read or a command's output.
    /// Permission cards likewise keep their summary and decision, not their reviewed payload,
    /// except a shell command's text, which the card shows in full on request.
    pub fn for_app(&self) -> Message {
        let mut message = self.clone();
        match &mut message.body {
            Body::Tool { detail, arguments, result, agent, .. } => {
                *arguments = serde_json::Value::Null;
                *result = None;
                if let Some(agent) = agent {
                    agent.summarize();
                }
                if detail.chars().count() > APP_TOOL_DETAIL_CHARS {
                    *detail = detail.chars().take(APP_TOOL_DETAIL_CHARS).collect();
                }
            }
            Body::Permission { plugin_id, arguments, command, .. } => {
                if let Some(text) = arguments.get("command").and_then(serde_json::Value::as_str).filter(|_| plugin_id == "computer") {
                    *command = Some(text.chars().take(APP_COMMAND_CHARS).collect());
                }
                *arguments = serde_json::Value::Null;
            }
            _ => {}
        }
        message
    }

    pub fn new(chat_id: &str, author: Author, body: Body) -> Self {
        Message {
            id: format!("msg-{}", uuid::Uuid::new_v4()),
            chat_id: chat_id.to_string(),
            author,
            body,
            state: MessageState::Complete,
            notification: None,
            created_at: crate::config::now_secs(),
            promoted_at: None,
            queued: false,
            output: None,
            external_id: None,
            edited_at: None,
            agent: None,
        }
    }

    pub fn is_complete(&self) -> bool {
        matches!(self.state, MessageState::Complete)
    }


    /// A reply, failure, or pending confirmation the user has not seen. Tool activity and
    /// updates to an answered question do not add to the count.
    pub fn counts_unread(&self) -> bool {
        if !matches!(self.author, Author::Bot { .. }) || self.notification == Some(crate::attention::Notification::Quiet) {
            return false;
        }
        match &self.body {
            Body::Text { .. } => matches!(self.state, MessageState::Complete | MessageState::Failed { .. }),
            Body::Permission { .. } => self.is_complete() && self.confirmation().is_some(),
            Body::Tool { .. } => self.confirmation().is_some(),
            _ => false,
        }
    }

    /// What a question waiting for the user's answer asks about: a pending permission card's
    /// summary, or the command of a `bash` card that asks.
    pub fn confirmation(&self) -> Option<String> {
        match &self.body {
            Body::Permission { summary, decision, .. } if decision == "pending" => Some(summary.clone()),
            Body::Tool { run: Some(run), .. } if run.state == "asking" => {
                let line = run.command.lines().next().unwrap_or("").trim();
                Some(format!("$ {}", line.chars().take(180).collect::<String>()))
            }
            Body::Tool { agent: Some(agent), .. } if agent.state == "asking" => agent.question.as_ref().map(|question| crate::model::agent_question_summary(agent, question)),
            _ => None,
        }
    }
}

/// Roster-level chat description. Messages live beside it in the store.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ChatMeta {
    pub id: String,
    /// `dm` or `group`.
    pub kind: String,
    /// A group's optional custom title. A direct chat is named after its bot.
    #[serde(default)]
    pub title: Option<String>,
    pub bot_ids: Vec<String>,
    /// A group's member holding the work, marked as the owner in every member's system prompt.
    /// Empty means the first member; `chats.set_owner` changes it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_bot_id: Option<String>,
    /// What a group is for, in the user's words; every member reads it in its system prompt.
    /// `chats.set_description` changes it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(default)]
    pub is_pinned: bool,
    /// The sidebar section the chat is listed under; none lists it with the chats in no section.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub section_id: Option<String>,
    /// Out of the sidebar but kept: search finds it, and the sidebar's Hidden group lists it.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub is_hidden: bool,
    /// No alerts for the chat on any Device while set; its unread count is kept.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub mute: Option<Mute>,
    pub created_at: f64,
    /// A conversation a channel opened: a direct chat with its bot that mirrors one thread on
    /// Telegram or Slack, named by `title`. The bot's own DM has none.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub channel: Option<ChatChannel>,
}

/// Where a channel's conversation happens outside Lorca.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ChatChannel {
    /// The channel (its event subscription) that opened the conversation.
    pub channel_id: String,
    /// `telegram` or `slack`.
    pub service: String,
    /// The plugin instance of the account the bot speaks through.
    pub account_id: String,
    /// The chat on the service: a Telegram chat id, a Slack channel id.
    pub chat_id: String,
    /// A Telegram forum topic or a Slack thread's `ts`, when the conversation is one.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub thread_id: Option<String>,
}

/// A chat's notifications are off until `until` (unix seconds), or until unmuted without one.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct Mute {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub until: Option<f64>,
}

/// A named group of chats in the sidebar, in the order the roster lists them. Every Device
/// shows the same sections, folded the same way.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Section {
    pub id: String,
    pub name: String,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub collapsed: bool,
}

impl ChatMeta {
    pub fn is_group(&self) -> bool {
        self.kind == "group"
    }

    /// Whether this is `bot_id`'s one direct chat with the user (not a channel's conversation).
    pub fn is_dm_of(&self, bot_id: &str) -> bool {
        self.kind == "dm" && self.channel.is_none() && self.bot_ids.len() == 1 && self.bot_ids[0] == bot_id
    }

    /// Whether the chat's alerts are off at `now` (unix seconds).
    pub fn is_muted(&self, now: f64) -> bool {
        self.mute.as_ref().is_some_and(|mute| mute.until.is_none_or(|until| now < until))
    }

    /// A group's owner: the one set while it is a member, else the first member.
    pub fn owner(&self) -> Option<&str> {
        if !self.is_group() {
            return None;
        }
        self.owner_bot_id.as_deref().filter(|id| self.bot_ids.iter().any(|member| member == id)).or(self.bot_ids.first().map(String::as_str))
    }

    /// A group's description, when it has one.
    pub fn purpose(&self) -> Option<&str> {
        self.description.as_deref().map(str::trim).filter(|text| self.is_group() && !text.is_empty())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Chat {
    #[serde(flatten)]
    pub meta: ChatMeta,
    #[serde(default)]
    pub unread_count: u32,
    /// What the turns run here used. Kept on this Runner, never synced.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub usage: Option<ChatUsage>,
    /// Summaries standing in for the older part of the chat, one per bot. Kept on this Runner.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub compactions: Vec<Compaction>,
}

/// The tokens and money the turns in a chat used. `context_tokens` and `context_window` are
/// the last turn's; the rest accumulate.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct ChatUsage {
    pub context_tokens: u64,
    pub context_window: u64,
    pub input_tokens: u64,
    pub output_tokens: u64,
    pub cache_read_tokens: u64,
    pub cost_usd: f64,
    #[serde(default)]
    pub api_cost_usd: f64,
    #[serde(default)]
    pub subscription_estimate_usd: f64,
    #[serde(default)]
    pub unknown_price_calls: u64,
    #[serde(default)]
    pub pricing_kinds: Vec<crate::budgets::Pricing>,
    pub turns: u64,
    /// The model of the last turn.
    pub model: String,
    pub updated_at: f64,
}

/// A summary that stands in for everything a bot saw in the chat up to and including
/// `after_message_id`; the bot's context starts with it, then the messages after that one.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Compaction {
    pub bot_id: String,
    pub summary: String,
    pub after_message_id: String,
    pub tokens_before: u64,
    pub created_at: f64,
}

// MARK: - Routines

/// A recurring task a bot runs on a schedule, in its direct chat: Grok Bot's routine. It lives
/// in the roster, so every Device lists it and can pause it; the bot's Runner runs it.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Routine {
    pub id: String,
    pub bot_id: String,
    pub name: String,
    /// The task, written to the bot, handed to it on every run.
    pub prompt: String,
    /// Original task authority retained when feedback revises the workflow text.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub feedback_authorization_prompt: Option<String>,
    /// `every 30m`, `every 2h`, `every 1d`, or five cron fields in `timezone`.
    pub schedule: String,
    /// The IANA timezone a cron schedule reads in: the Runner's when the routine was made,
    /// unless the bot named another.
    #[serde(default = "crate::schedule::local_timezone")]
    pub timezone: String,
    /// What a Runner that was off at a due time does when it is back: one run, or none.
    #[serde(default)]
    pub missed_run_policy: crate::routine_health::MissedRunPolicy,
    /// The last due time the Runner took or skipped, so a restart neither repeats nor drops it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_scheduled_at: Option<f64>,
    /// How the routine's checks and runs have gone, as its Runner records them.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub health: Option<crate::routine_health::CheckHealth>,
    pub is_enabled: bool,
    /// When the schedule started counting: creation, or the last resume.
    pub enabled_at: f64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_run_at: Option<f64>,
    /// How the last run ended: `sent`, `pass`, or `error`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_outcome: Option<String>,
    /// Why Lorca paused it, when it did: `away` or `authentication`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub paused_reason: Option<String>,
    /// JavaScript the Runner runs at each due time before the bot does, without a model: a
    /// codemode script with read-only tools. What it returns starts the run; returning nothing
    /// skips it. `last_run_at` and `last_outcome` count the runs, not the checks.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub check: Option<String>,
    /// What the routine listens to when it runs on events: each event about its subject starts
    /// a run, and the routine ends after one that says it ends.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub events: Option<crate::routine_events::RoutineEvents>,
    /// The Calendar account whose events place the runs of a schedule around events.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub calendar: Option<crate::routine_triggers::CalendarTrigger>,
    pub created_at: f64,
}

impl Routine {
    /// The time the next run counts from: the last run, due time taken, or check, else when the
    /// routine was armed.
    pub fn anchor(&self) -> i64 {
        self.last_run_at
            .unwrap_or(0.0)
            .max(self.last_scheduled_at.unwrap_or(0.0))
            .max(self.health.as_ref().and_then(|health| health.last_check_at).unwrap_or(0.0))
            .max(self.enabled_at) as i64
    }

    /// When the next run is due, or `None` when paused or the schedule is unreadable.
    pub fn next_run_at(&self) -> Option<i64> {
        self.next_run_after(self.anchor())
    }

    /// When the routine is next due counting from `since` as well as its anchor, such as the
    /// check that last ran on its Runner.
    pub fn next_run_after(&self, since: i64) -> Option<i64> {
        if !self.is_enabled {
            return None;
        }
        let schedule = crate::schedule::parse(&self.schedule).ok()?;
        match &schedule {
            // A one-time routine is due at its time until a run at or after it, by schedule or by hand.
            crate::schedule::Schedule::Once(_) => return schedule.once_instant(&self.timezone).filter(|at| !crate::routine_triggers::once_taken(self, *at)),
            crate::schedule::Schedule::Events(offset) => return crate::routine_triggers::next_event_run(self, *offset).map(|(due, _)| due),
            // A routine on events runs when they come, never on a clock.
            crate::schedule::Schedule::OnEvents => return None,
            _ => {}
        }
        let next = schedule.next_after(since.max(self.anchor()), &self.timezone)?;
        // After a failure that backs off, no sooner than the retry.
        let retry = self.health.as_ref().and_then(|health| health.retry_at()).unwrap_or(0.0);
        Some(next.max(retry as i64))
    }
}

/// How a routine's check went before the run it started.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct CheckReport {
    /// What the check returned, as text; empty when it returned nothing.
    #[serde(default)]
    pub found: String,
    /// How the check failed, when it did.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

// MARK: - Blob payloads

/// `kind = roster`: bots, chat metadata, routines, and the names the account gives its Devices.
/// Latest wins.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Default)]
pub struct RosterBlob {
    /// Workflow setups, merged one by one (`workflows::merge`); a build that does not know
    /// workflows leaves the field out.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub workflows: Option<Vec<crate::workflows::Setup>>,
    /// The account's profile of the user, entry by entry (`profile_sync`); a build that does not
    /// know it leaves the field out.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub profile: Option<crate::profile_sync::Entries>,
    pub bots: Vec<Bot>,
    pub chats: Vec<ChatMeta>,
    /// The sidebar's sections, in order.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub sections: Vec<Section>,
    #[serde(default)]
    pub routines: Vec<Routine>,
    #[serde(default)]
    pub auto_review: AutoReview,
    /// The bots the account shares as links, with their keys.
    #[serde(default)]
    pub shared_links: Vec<crate::templates::links::SharedLink>,
    /// The names Devices show as, by machine pubkey, given on any Device for any Device. A Device
    /// without one shows the name its own `machine` blob carries.
    #[serde(default, skip_serializing_if = "std::collections::BTreeMap::is_empty")]
    pub device_names: std::collections::BTreeMap<String, String>,
    /// The machine pubkeys of the Devices unpaired from the account. A Device keeps every key it
    /// reads here, and puts the ones a roster left out back in its own.
    #[serde(default, skip_serializing_if = "std::collections::BTreeSet::is_empty")]
    pub unpaired: std::collections::BTreeSet<String>,
    pub updated_at: f64,
}

/// `kind = chat`: one transcript operation.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "op", rename_all = "snake_case")]
pub enum ChatBlob {
    /// Insert or replace by message id.
    Upsert { message: Message },
    Remove { chat_id: String, message_id: String },
    ClearUnread { chat_id: String },
}

impl ChatBlob {
    /// Versions of a message share a slot, and so does its removal, which leaves the relay
    /// nothing of the message but the removal. A chat's read marks share another. A tool row
    /// goes up while its call runs, for the status line on other Devices, and the finished row
    /// replaces it outright, so the relay keeps one version of each call.
    pub fn slot(&self) -> crate::app::Slot {
        match self {
            // A command's card shows in the transcript, so like a message it keeps the place it
            // first took in the log however often it changes.
            ChatBlob::Upsert { message } if matches!(message.body, Body::Tool { run: None, agent: None, .. }) => crate::app::Slot::latest(relay_name(&message.id)),
            ChatBlob::Upsert { message } => crate::app::Slot::first_and_latest(relay_name(&message.id)),
            ChatBlob::Remove { message_id, .. } => crate::app::Slot::latest(relay_name(message_id)),
            ChatBlob::ClearUnread { chat_id } => crate::app::Slot::latest(relay_name(&format!("read-{chat_id}"))),
        }
    }

    /// The chat, as the relay's group: deleting the chat deletes the group.
    pub fn group(&self) -> String {
        match self {
            ChatBlob::Upsert { message } => relay_name(&message.chat_id),
            ChatBlob::Remove { chat_id, .. } | ChatBlob::ClearUnread { chat_id } => relay_name(chat_id),
        }
    }
}

/// A slot or group name. The relay takes 1–64 characters of `[A-Za-z0-9._-]`; any other name
/// goes up as its hash.
pub fn relay_name(name: &str) -> String {
    let plain = !name.is_empty()
        && name.len() <= 64
        && name != "."
        && name != ".."
        && name.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'));
    if plain {
        return name.to_string();
    }
    <sha2::Sha256 as sha2::Digest>::digest(name.as_bytes()).iter().map(|b| format!("{b:02x}")).collect()
}

/// `kind = machine`: a Device's own metadata and presence claims.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct MachineBlob {
    pub device: Device,
    /// The turns in flight on this Device, so every other Device shows the bots at work.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub turns: Vec<LiveTurn>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub budgets: Vec<crate::budgets::BudgetSnapshot>,
    /// The chat the desktop app shows in front of the user, so no Runner pushes it to a phone.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub watching: Option<String>,
}

/// `kind = job`, sealed to the Runner's box key: run one bot turn in one chat.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Job {
    pub id: String,
    pub chat_id: String,
    pub bot_id: String,
    /// Canonical durable task context. Child/handoff Jobs may reference it without owning
    /// the task's execution claim; only `kind = task` is a durable task run.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub task_id: Option<String>,
    /// The authority's immutable dispatch snapshot, so a Job can arrive before task sync.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub task_context: Option<crate::tasks::Task>,
    /// `turn` for a user message in a DM, `room_turn` for one member's turn in a group,
    /// `message` for a teammate's message_bot, `routine` for a run of a routine, `command` for
    /// a command the bot left running that ended, `agent` for what a coding agent the bot runs
    /// did, `event` for a service event an event subscription's inbox admitted.
    pub kind: String,
    pub trigger_message_id: String,
    /// `agent_message`: the coding agent the user addressed and the text as typed. The Runner
    /// hands it to the agent before the turn job and chat lock, and starts no bot turn.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub agent_message: Option<AgentMessage>,
    /// A durable delegated request or the continuation that reads its report.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub handoff: Option<crate::handoffs::HandoffJob>,
    /// `routine`: which routine is running.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub routine_id: Option<String>,
    /// `routine`: what the routine's check found, when a due check started the run. A run
    /// without one runs the check itself first.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub check: Option<CheckReport>,
    /// The Device that created the job; a `room_turn` result goes back to it.
    pub requested_by: String,
    /// The bot that sent a `message` job, so the recipient knows who to answer.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub from_bot_id: Option<String>,
    /// Bot-to-bot hops since the last user message. Bounded so two bots cannot loop.
    #[serde(default)]
    pub hops: u32,
    /// `room_turn`: which round of the group exchange this is, from 1.
    #[serde(default)]
    pub round: u32,
    /// `room_turn`: the last round; the member should only add what is essential.
    #[serde(default)]
    pub is_winding_down: bool,
    /// The first turn of a bot added from a marketplace template: what it sets up before it
    /// answers.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub setup: Option<TemplateSetup>,
    pub created_at: f64,
}

impl Job {
    /// Work for one bot in one chat that never leaves this Runner, as the prompt preview and a
    /// memory capture are: no trigger message, no handoff, no routine.
    pub fn local(kind: &str, chat_id: &str, bot_id: &str, requested_by: &str) -> Job {
        Job {
            agent_message: None,
            id: format!("{kind}-{}", uuid::Uuid::new_v4()),
            chat_id: chat_id.into(),
            bot_id: bot_id.into(),
            task_id: None,
            task_context: None,
            kind: kind.into(),
            trigger_message_id: String::new(),
            handoff: None,
            routine_id: None,
            check: None,
            requested_by: requested_by.into(),
            from_bot_id: None,
            hops: 0,
            round: 0,
            is_winding_down: false,
            setup: None,
            created_at: crate::config::now_secs(),
        }
    }
}

/// What a bot added from a marketplace template sets up on its first turn, as Grok Bot's
/// template import asks its new bot to.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct TemplateSetup {
    /// The template's name, such as "Researcher".
    pub template: String,
    /// The plugins it works with. The Runner checks which it has installed.
    #[serde(default)]
    pub plugins: Vec<SetupPlugin>,
    /// Its routines, added paused, by name.
    #[serde(default)]
    pub routines: Vec<String>,
    /// Facts it saves to its memory.
    #[serde(default)]
    pub memory: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub struct SetupPlugin {
    pub id: String,
    pub name: String,
    #[serde(default)]
    pub description: String,
}

pub const MAX_BOT_HOPS: u32 = 8;

/// `kind = job_result`, sealed to the requesting Device's box key: how a `room_turn` ended, so
/// the Device running the group exchange can offer the next turn.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct JobResult {
    pub job_id: String,
    pub chat_id: String,
    pub bot_id: String,
    /// `sent`, `pass`, or `error`.
    pub outcome: String,
}

/// A turn in flight on the Device whose machine blob lists it: a bot's turn on its Runner, or
/// a group exchange on the Device that offers its members their turns.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct LiveTurn {
    pub job_id: String,
    pub chat_id: String,
    /// Empty while a group exchange is between member turns.
    pub bot_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub routine_id: Option<String>,
    /// What the turn is doing that no message says yet. The bot's next message ends it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub activity: Option<JobActivity>,
}

/// What a turn is doing between its messages: the working row's "Thinking…" and "Retrying…".
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum JobActivity {
    /// The model started thinking; the bot's next message is what came of it.
    Thinking,
    /// A model call failed in a way worth another try; the turn waits `delay_ms` and asks again.
    Retry { attempt: u32, max_attempts: u32, delay_ms: u64, error: String },
}

impl JobActivity {
    /// The event an app hears for it.
    pub fn event(&self, chat_id: &str, bot_id: &str) -> crate::events::Event {
        let (chat_id, bot_id) = (chat_id.to_string(), bot_id.to_string());
        match self {
            JobActivity::Thinking => crate::events::Event::JobThinking { chat_id, bot_id },
            JobActivity::Retry { attempt, max_attempts, delay_ms, error } => crate::events::Event::JobRetry {
                chat_id,
                bot_id,
                attempt: *attempt,
                max_attempts: *max_attempts,
                delay_ms: *delay_ms,
                error: error.clone(),
            },
        }
    }
}

/// `kind = job_cancel`, sealed to the Runner's box key: the hard Stop control reaches a job
/// that another Device dispatched there. The job registers before waiting for its chat lock,
/// so Stop also removes admitted work that has not begun yet.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct JobCancel {
    pub job_id: String,
}

/// `kind = request`: a question for one Runner from another Device, in an envelope the asking
/// Device signs (`envelope.rs`); the answer goes back to the signer.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Request {
    pub id: String,
    /// `memory.read`, `mcp.save`, and the rest that `requests::answer` takes.
    pub verb: String,
    #[serde(default)]
    pub body: serde_json::Value,
}

/// `kind = response`: the Runner's answer to a `Request`, signed by it and sealed to the Device
/// that asked.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Response {
    pub request_id: String,
    #[serde(default)]
    pub body: serde_json::Value,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Pairing handshake payloads. The request is sealed to the pairing string's `ek`, which the
/// relay never sees; the reply is encrypted with the request's `reply_key`, so only the Device
/// that holds the pairing string could have written it.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairRequest {
    pub machine_pubkey: String,
    pub box_pubkey: String,
    pub device: Device,
    /// A random key, base64url, that the reply is encrypted with.
    pub reply_key: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairReply {
    pub identity_pubkey: String,
    pub content_pubkey: String,
    /// Generation 0 of the account key.
    pub account_dek: String,
    pub relay_url: String,
    /// The account key's later generations, as the identity signed them.
    #[serde(default)]
    pub keyrings: Vec<crate::keys::SignedKeyring>,
}

/// Facts a host passes in when they cannot be probed: a phone names itself through the app.
static HOST_FACTS: std::sync::OnceLock<(String, String, String, String)> = std::sync::OnceLock::new();

/// Sets the name, os, os version, and model `host_facts` answers with, once per process.
pub fn set_host_facts(name: String, os: String, os_version: String, model: String) {
    let _ = HOST_FACTS.set((name, os, os_version, model));
}

/// Machine facts for this host: what the app set, or probed from the system once per process.
pub fn host_facts() -> (String, String, String, String) {
    static PROBED: std::sync::OnceLock<(String, String, String, String)> = std::sync::OnceLock::new();
    HOST_FACTS.get().unwrap_or_else(|| PROBED.get_or_init(probe_host)).clone()
}

/// A Mac's marketing name with its chip, "MacBook Air (M5)". `hw.model` is an identifier such as
/// `Mac17,3` on current Macs, which says nothing a person or an icon can use.
fn mac_model_name() -> Option<String> {
    let output = std::process::Command::new("system_profiler").args(["SPHardwareDataType", "-json"]).output().ok()?;
    let json: serde_json::Value = serde_json::from_slice(&output.stdout).ok()?;
    let hardware = json.get("SPHardwareDataType")?.get(0)?;
    let name = hardware.get("machine_name")?.as_str()?.trim();
    if name.is_empty() {
        return None;
    }
    let chip = hardware.get("chip_type").and_then(|c| c.as_str()).map(|c| c.trim_start_matches("Apple ").trim()).filter(|c| !c.is_empty());
    Some(match chip {
        Some(chip) => format!("{name} ({chip})"),
        None => name.to_string(),
    })
}

/// What the user calls this computer: a Mac's Computer Name, the pretty hostname a Linux
/// desktop's Device Name setting writes, else the host name.
fn computer_name() -> Option<String> {
    let given = if cfg!(target_os = "macos") {
        std::process::Command::new("scutil")
            .args(["--get", "ComputerName"])
            .output()
            .ok()
            .and_then(|o| String::from_utf8(o.stdout).ok())
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
    } else if cfg!(target_os = "linux") {
        std::fs::read_to_string("/etc/machine-info").ok().and_then(|info| pretty_hostname(&info))
    } else {
        None
    };
    given.or_else(host_name)
}

/// `PRETTY_HOSTNAME` from `/etc/machine-info`: shell-style `KEY=value` lines, where systemd
/// double-quotes a value with spaces or quotes in it and escapes with a backslash.
fn pretty_hostname(machine_info: &str) -> Option<String> {
    let value = machine_info.lines().find_map(|line| {
        let (key, value) = line.split_once('=')?;
        (key.trim() == "PRETTY_HOSTNAME").then_some(value.trim())
    })?;
    let name = if let Some(quoted) = value.strip_prefix('"').and_then(|v| v.strip_suffix('"')) {
        let mut name = String::new();
        let mut chars = quoted.chars();
        while let Some(c) = chars.next() {
            name.push(if c == '\\' { chars.next().unwrap_or(c) } else { c });
        }
        name
    } else if let Some(quoted) = value.strip_prefix('\'').and_then(|v| v.strip_suffix('\'')) {
        quoted.to_string()
    } else {
        value.to_string()
    };
    let name = name.trim();
    (!name.is_empty()).then(|| name.to_string())
}

/// The kernel's host name, as `hostname` prints it. `$HOSTNAME` is a shell variable most shells
/// never export.
#[cfg(unix)]
fn host_name() -> Option<String> {
    let mut buffer = [0u8; 256];
    if unsafe { libc::gethostname(buffer.as_mut_ptr().cast(), buffer.len()) } != 0 {
        return None;
    }
    let length = buffer.iter().position(|&byte| byte == 0).unwrap_or(buffer.len());
    let name = String::from_utf8_lossy(&buffer[..length]).trim().to_string();
    (!name.is_empty()).then_some(name)
}

/// The computer name Windows sets in every process's environment.
#[cfg(windows)]
fn host_name() -> Option<String> {
    std::env::var("COMPUTERNAME").ok().map(|name| name.trim().to_string()).filter(|name| !name.is_empty())
}

fn probe_host() -> (String, String, String, String) {
    let os = if cfg!(target_os = "macos") {
        "macos"
    } else if cfg!(target_os = "linux") {
        "linux"
    } else if cfg!(target_os = "windows") {
        "windows"
    } else {
        "linux"
    };
    let os_version = std::process::Command::new("sw_vers")
        .args(["-productVersion"])
        .output()
        .ok()
        .and_then(|o| String::from_utf8(o.stdout).ok())
        .map(|s| format!("macOS {}", s.trim()))
        .unwrap_or_else(|| os.to_string());
    let model = mac_model_name()
        .or_else(|| {
            std::process::Command::new("sysctl")
                .args(["-n", "hw.model"])
                .output()
                .ok()
                .and_then(|o| String::from_utf8(o.stdout).ok())
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
        })
        .unwrap_or_else(|| std::env::consts::ARCH.to_string());
    // A computer with no name goes by its model, as a phone does.
    let name = computer_name().unwrap_or_else(|| model.clone());
    (name, os.to_string(), os_version, model)
}

#[cfg(test)]
mod app_view_tests {
    use super::*;

    #[test]
    fn old_bot_instructions_become_part_of_the_description_once() {
        let mut bot: Bot = serde_json::from_value(serde_json::json!({
            "id": "bot", "name": "Scout", "description": "Find sources.",
            "symbol_name": "binoculars", "accent": "teal", "runner_id": "runner",
            "provider": "deepseek", "instructions": "Cite every claim.", "created_at": 1.0
        }))
        .unwrap();

        assert!(bot.normalize_description());
        assert_eq!(bot.description, "Find sources.\n\nCite every claim.");
        assert!(bot.legacy_instructions.is_empty());
        assert!(!bot.normalize_description());
        let value = serde_json::to_value(bot).unwrap();
        assert!(value.get("label").is_none());
        assert_eq!(value["instructions"], "");
    }

    #[test]
    fn the_apps_get_a_tool_row_without_its_payload() {
        let tool = Message::new("c", Author::Bot { bot_id: "b".into() }, Body::Tool {
            name: "read".into(), summary: "Read a file".into(), detail: "x".repeat(5000), is_running: false,
            call_id: "call".into(), arguments: serde_json::json!({ "path": "big" }), result: Some("y".repeat(100_000)), is_error: false, description: None, target_bot_id: None, script_command: None,
            run: None,
            agent: None,
        });
        let Body::Tool { detail, arguments, result, summary, .. } = tool.for_app().body else { panic!() };
        assert_eq!((detail.len(), arguments.is_null(), result, summary.as_str()), (400, true, None, "Read a file"));
    }

    #[test]
    fn permission_cards_drop_the_reviewed_payload() {
        let permission = Message::new("c", Author::Bot { bot_id: "b".into() }, Body::Permission {
            plugin_id: "computer".into(), plugin_name: "Mac".into(), tool: "bash".into(), summary: "Run a command".into(),
            arguments: serde_json::json!({ "command": "secret" }), decision: "pending".into(), reason: None, rule: None, command: None, link: None, code: None, secret: None, ask: None, agent_ask: None,
        });
        let app = permission.for_app();
        let Body::Permission { arguments, summary, command, .. } = &app.body else { panic!() };
        assert!(arguments.is_null());
        assert_eq!(summary, "Run a command");
        assert_eq!(command.as_deref(), Some("secret"));
        // A phone stores the app view; reading it again keeps the command.
        let Body::Permission { command, .. } = app.for_app().body else { panic!() };
        assert_eq!(command.as_deref(), Some("secret"));
    }

    /// The apps read how an agent stands off its card: its tone, whether it works now, how full
    /// its context is, and what it does now.
    #[test]
    fn a_coding_agents_card_says_how_it_stands() {
        let summarized = |mut agent: AgentRun| {
            agent.summarize();
            agent
        };
        let working = AgentRun { state: "working".into(), ..Default::default() };
        let tone = |agent: AgentRun| summarized(agent).tone;
        assert_eq!(tone(working.clone()), "busy");
        assert!(summarized(working.clone()).busy);
        assert_eq!(tone(AgentRun { stalled: true, ..working.clone() }), "ask");
        assert!(!summarized(AgentRun { stalled: true, ..working.clone() }).busy);
        assert_eq!(tone(AgentRun { asking: Some("card".into()), ..working.clone() }), "ask");
        assert_eq!(tone(AgentRun { state: "checking".into(), ..Default::default() }), "busy");
        assert!(!summarized(AgentRun { state: "checking".into(), ..Default::default() }).busy);
        assert_eq!(tone(AgentRun { state: "idle".into(), ..Default::default() }), "done");
        assert_eq!(tone(AgentRun { state: "failed".into(), ..Default::default() }), "fail");
        assert_eq!(tone(AgentRun { state: "stopped".into(), ..Default::default() }), "quiet");

        let setting = |value: &str| AgentSetting { name: "Context".into(), value: value.into() };
        let context = |value: &str| summarized(AgentRun { settings: vec![setting(value)], ..Default::default() }).context;
        assert_eq!(context("34k / 200k"), Some(AgentContextUse { used: 34_000, size: 200_000 }));
        assert_eq!(context("900 / 1.2M"), Some(AgentContextUse { used: 900, size: 1_200_000 }));
        assert_eq!(context("lots"), None);
        assert_eq!(context("5 / 0"), None);

        let output = "> fix it\n● Read(a.rs)\n  ⎿ ok\n● Bash(cargo test)\n  ⎿ 3 pass".to_string();
        assert_eq!(summarized(AgentRun { output: Some(output), ..working.clone() }).now.as_deref(), Some("Bash(cargo test)"));
    }

    /// The apps say how it stands, its model, its pull request, and its last lines as the CLI
    /// reads them, so they keep no rules of their own for them.
    #[test]
    fn a_coding_agents_card_names_its_status_model_pull_request_and_tail() {
        let summarized = |mut agent: AgentRun| {
            agent.summarize();
            agent
        };
        let status = |agent: AgentRun| summarized(agent).status;
        let working = AgentRun { state: "working".into(), ..Default::default() };
        assert_eq!(status(working.clone()), "working");
        assert_eq!(status(AgentRun { stalled: true, ..working.clone() }), "quiet");
        assert_eq!(status(AgentRun { asking: Some("card".into()), ..working.clone() }), "needs_you");
        assert_eq!(status(AgentRun { state: "checking".into(), ..Default::default() }), "starting");
        assert_eq!(status(AgentRun { state: "denied".into(), held: true, ..Default::default() }), "held");
        assert_eq!(status(AgentRun { state: "denied".into(), ..Default::default() }), "not_allowed");
        assert_eq!(status(AgentRun { state: "exited".into(), ..Default::default() }), "ended");
        assert_eq!(status(AgentRun { state: "something new".into(), ..Default::default() }), "stopped");

        let model = AgentSetting { name: "Model".into(), value: "openrouter/anthropic/claude-sonnet[1M]".into() };
        assert_eq!(summarized(AgentRun { settings: vec![model.clone()], ..Default::default() }).model.as_deref(), Some("claude-sonnet"));
        // Where it can change, the card shows the name of the choice in force.
        let control = AgentControl {
            what: "model".into(),
            name: "Model".into(),
            value: "opus".into(),
            choices: vec![AgentChoice { value: "opus".into(), name: "gateway/claude-opus[200k]".into() }],
        };
        assert_eq!(summarized(AgentRun { settings: vec![model], controls: vec![control], ..Default::default() }).model.as_deref(), Some("claude-opus"));
        assert_eq!(summarized(AgentRun::default()).model, None);

        let pr = |link: &str| summarized(AgentRun { pull_request: Some(link.into()), ..Default::default() }).pull_request_name;
        assert_eq!(pr("https://github.com/acme/shop/pull/7").as_deref(), Some("acme/shop #7"));
        assert_eq!(pr("https://gitlab.com/acme/shop/merge_requests/12/").as_deref(), Some("acme/shop #12"));
        assert_eq!(pr("https://gitlab.com/acme/web/shop/-/merge_requests/12").as_deref(), Some("acme/web/shop #12"));
        assert_eq!(pr("https://github.com/acme/shop/pull/7/files").as_deref(), Some("acme/shop #7"));
        assert_eq!(pr("https://github.com/acme/pull/7"), None);
        assert_eq!(pr("https://example.com/x"), None);

        let output = "a\n\nb\nc\n  \nd\ne\nf\n".to_string();
        assert_eq!(summarized(AgentRun { output: Some(output.clone()), ..working.clone() }).tail, ["b", "c", "d", "e", "f"]);
        assert!(summarized(AgentRun { output: Some(output), state: "idle".into(), ..Default::default() }).tail.is_empty());
    }
}

#[cfg(test)]
mod host_tests {
    use super::*;

    #[test]
    fn a_linux_desktop_goes_by_its_pretty_hostname() {
        assert_eq!(pretty_hostname("PRETTY_HOSTNAME=\"Kevin's ThinkPad\"\nCHASSIS=laptop\n").as_deref(), Some("Kevin's ThinkPad"));
        assert_eq!(pretty_hostname("PRETTY_HOSTNAME=devbox").as_deref(), Some("devbox"));
        assert_eq!(pretty_hostname(r#"PRETTY_HOSTNAME="The \"Lab\" \$box""#).as_deref(), Some(r#"The "Lab" $box"#));
        assert_eq!(pretty_hostname("PRETTY_HOSTNAME='My Box'").as_deref(), Some("My Box"));
        assert_eq!(pretty_hostname("#PRETTY_HOSTNAME=Old\nCHASSIS=server\n"), None);
        assert_eq!(pretty_hostname("PRETTY_HOSTNAME=\n"), None);
    }

    #[cfg(unix)]
    #[test]
    fn the_host_name_is_what_hostname_prints() {
        let printed = std::process::Command::new("hostname").output().unwrap().stdout;
        assert_eq!(host_name().as_deref(), Some(String::from_utf8_lossy(&printed).trim()));
    }
}

#[cfg(test)]
mod agent_wire_tests {
    use super::*;

    #[test]
    fn text_body_without_agents_still_serializes_as_before() {
        assert_eq!(serde_json::to_value(Body::text("hi")).unwrap(), serde_json::json!({"kind": "text", "text": "hi"}));
    }

    #[test]
    fn a_text_body_carries_the_coding_agents_it_addresses() {
        let body: Body = serde_json::from_value(serde_json::json!({
            "kind": "text", "text": "@Claude Code status", "agents": ["agent-1"]
        }))
        .unwrap();
        assert_eq!(body.agents(), ["agent-1"]);
        assert!(body.addresses_agent());
    }

    #[test]
    fn a_quote_keeps_the_agent_behind_the_message() {
        let mut message = Message::new("chat", Author::Bot { bot_id: "bot-1".into() }, Body::text("done"));
        message.agent = Some(AgentAuthor { id: "agent-1".into(), name: "Claude Code".into(), kind: "claude".into() });
        let quote = ReplyTo::quoting(&message).unwrap();
        assert_eq!(quote.agent.unwrap().name, "Claude Code");
    }

    #[test]
    fn each_agent_kind_has_a_name_without_its_card() {
        assert_eq!(agent_name("gemini"), "Gemini CLI");
        assert_eq!(agent_name("copilot"), "GitHub Copilot");
        assert_eq!(agent_name("cursor"), "Cursor");
        assert_eq!(agent_name("opencode"), "OpenCode");
        assert_eq!(agent_name("kimi"), "Kimi Code");
        assert_eq!(agent_name("pi"), "pi");
        assert_eq!(agent_name("kiro"), "Kiro");
        assert_eq!(agent_name("unknown"), "Coding agent");
    }

    #[test]
    fn a_device_advertises_agent_messages() {
        let device = Device { agent_messages: true, ..Device::default() };
        assert_eq!(serde_json::to_value(&device).unwrap()["agent_messages"], true);
    }

    #[test]
    fn a_job_carries_an_agent_message() {
        let job = Job {
            agent_message: Some(AgentMessage { id: "agent-1".into(), text: "@Claude Code status".into() }),
            ..Job::local(AGENT_MESSAGE_JOB, "chat", "bot", "me")
        };
        assert_eq!(job.kind, "agent_message");
        assert_eq!(job.agent_message.unwrap().id, "agent-1");
    }
}
