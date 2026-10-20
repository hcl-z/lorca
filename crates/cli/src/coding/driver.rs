//! The one interface every coding agent is driven through (`acp`): what the bot sends it, and the
//! events it reports back to its supervisor (`super::supervise`).

use async_trait::async_trait;
use serde_json::Value;
use tokio::sync::{mpsc, oneshot};

/// What a coding agent reports.
pub(crate) enum Event {
    /// Its session, which resumes it after its process is gone.
    Session(String),
    /// The settings of its session (its mode, its model) as `(id, value)`, kept to set again
    /// when the session resumes in a new process.
    Config(Vec<(String, String)>),
    /// What its card shows of its session besides the work, as `(name, value)`: its model, how
    /// hard it thinks, its mode, how much of its context it has used. Replaces the last.
    Settings(Vec<(String, String)>),
    /// More of its transcript, as the bot reads it.
    Lines(Vec<String>),
    /// Its transcript as the apps draw it: each entry under its key, in place of the one the key
    /// had in this run, or after the rest.
    Entries(Vec<(String, super::entries::Entry)>),
    /// It took up a message and works on it.
    Working,
    /// It is done with what it was asked, with its last message.
    Idle(Option<String>),
    /// It wants to do something that needs a decision first; the answer goes back on the
    /// channel: `Ok` lets it, `Err` says why not.
    Approve(Approval, oneshot::Sender<Result<(), String>>),
    /// It asks the user something: a form to fill in. The answers go back on the channel by field
    /// key, or `None` when the question was declined; `Err` says why there are none, and the agent
    /// hears it was not answered.
    Ask(Ask, oneshot::Sender<Result<Option<serde_json::Map<String, Value>>, String>>),
    /// The title it gave its session.
    Title(String),
    /// It is gone.
    Ended(Ended),
}

/// A question an agent puts to the user: what it says, and the fields it wants filled.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct Ask {
    pub message: String,
    pub fields: Vec<Field>,
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) struct Field {
    pub key: String,
    /// What the field asks, in a few words.
    pub label: String,
    pub required: bool,
    pub kind: FieldKind,
    /// A free-text field beside a menu, for the answer the menu has no item for.
    pub other: Option<String>,
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) enum FieldKind {
    /// One of these.
    One(Vec<Choice>),
    /// Any number of these.
    Many(Vec<Choice>),
    /// Yes or no.
    Flag,
    Text,
    /// A number; whole when `integer`.
    Number { integer: bool },
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) struct Choice {
    /// What the agent is told when it is picked.
    pub value: String,
    /// What the user reads, and a line about it.
    pub title: String,
    pub description: Option<String>,
}

/// What an agent asks to do.
#[derive(Debug, Clone)]
pub(crate) enum Approval {
    /// Run a shell command.
    Command { command: String },
    /// Change files outside the folder it works in.
    Files { paths: Vec<String> },
    /// Use one of its other tools: a web fetch, an MCP tool.
    Tool { name: String, input: Value },
}

#[derive(Debug, Clone)]
pub(crate) struct Ended {
    pub outcome: String,
    pub failed: bool,
}

pub(crate) type Events = mpsc::UnboundedSender<Event>;

#[async_trait]
pub(crate) trait Driver: Send + Sync {
    /// Gives it a message: taken up when it is done with what it does now, or at once with
    /// `interrupt`, which stops that first.
    async fn send(&self, text: &str, interrupt: bool) -> Result<(), String>;
    /// The commands it offers (`/name`, `$name`, its skills among them) with what each does.
    async fn commands(&self) -> Vec<(String, String)> {
        Vec::new()
    }
    /// Changes its model or how hard it thinks while it runs, as `(what, value)` with `what`
    /// `model` or `effort`, the way `start` asks for them. An `Err` says why not, with what it offers.
    async fn configure(&self, _wants: &[(String, String)]) -> Result<(), String> {
        Err("It cannot change its settings while it runs.".into())
    }
    /// The settings it lets be changed now, with what each offers.
    fn controls(&self) -> Vec<Control> {
        Vec::new()
    }
    /// Ends it and what it started. Its `Ended` event follows.
    async fn stop(&self);
}

/// A setting of the session that can be changed while it runs.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct Control {
    /// `model` or `effort`.
    pub what: &'static str,
    /// What the agent calls it.
    pub name: String,
    /// The value it has now.
    pub value: String,
    /// The values it takes, with their names.
    pub choices: Vec<(String, String)>,
}
