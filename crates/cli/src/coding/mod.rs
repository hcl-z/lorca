//! Coding agents a bot runs on its Runner and supervises: Claude Code, Codex, or another agent
//! that speaks the Agent Client Protocol, in a git worktree of a repository there, on a prompt and
//! and a prompt. The agent is a process of its own that outlives the turn
//! that started it. The bot keeps its id across turns, reads its transcript, sends it follow-ups
//! (queued, or cutting in), and stops it; it gets a turn of its own when the agent is done,
//! stalls, or exits, with the outputs Lorca published from its work. Every command the agent
//! runs goes through the bot's Access and Auto-review, as the bot's own commands do, and its card
//! in the chat shows the user how it stands and what it asks.
//!
//! Every agent is driven through [`driver::Driver`], by the one driver in [`acp`].

pub(crate) mod acp;
pub(crate) mod driver;
pub(crate) mod entries;
pub(crate) mod lines;
pub(crate) mod process;
pub(crate) mod proof;
pub(crate) mod tool;
pub(crate) mod workspace;

use std::collections::VecDeque;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tokio::sync::{mpsc, oneshot};
use tokio::time::Instant;

use crate::app::App;
use crate::model::{AgentAsk, AgentAskChoice, AgentAskField, AgentQuestion, AgentRun, AgentSetting, Author, Body, Message};
use driver::{Approval, Ask, Driver, Event, FieldKind};

pub use tool::{review_start, CodingAgentTool};

/// The coding agents a bot gets without asking for one, in the order the first installed is
/// used, by the name the bot and the apps use.
pub const KINDS: [&str; 2] = ["claude", "codex"];

/// Every coding agent a bot can ask for: those above, then the others.
pub fn kinds() -> Vec<&'static str> {
    acp::SPECS.iter().map(|spec| spec.kind).collect()
}

pub fn is_kind(kind: &str) -> bool {
    acp::spec(kind).is_some()
}

/// The program that starts agent `kind` on this Runner, when it can start here.
pub(crate) async fn find_program(kind: &str) -> Option<PathBuf> {
    acp::find(acp::spec(kind)?).await
}

/// Working with nothing new for this long reads as stalled: the bot hears it, once.
pub const STALL_AFTER: Duration = Duration::from_secs(10 * 60);
/// An agent Lorca runs that is done with its work and hears nothing for this long stops; a
/// follow-up starts it again on its session.
pub const IDLE_LIMIT: Duration = Duration::from_secs(60 * 60);
/// The card catches up with the transcript once it pauses this long, and at least this often.
const ROW_SETTLE: Duration = Duration::from_millis(500);
/// How often what the agent is saying is written to the chat while it says it.
const CHAT_FLUSH: Duration = Duration::from_millis(800);
const ROW_EVERY: Duration = Duration::from_secs(2);
/// The lines the card carries.
const ROW_LINES: usize = 20;
/// The lines of transcript kept in memory; the file keeps them all.
const KEEP_LINES: usize = 5000;
/// What `coding.transcript` sends the apps, at most.
const APP_LINES: usize = 2000;
/// The most entries one `coding.transcript` page carries.
const MAX_APP_ENTRIES: u64 = 200;
/// What the bot reads at once, at most.
const READ_LINES: usize = 200;
/// Ended agents are forgotten this long after they ended.
const FORGET_AFTER: f64 = 30.0 * 24.0 * 3600.0;

/// The agent's name in what Lorca writes: "Claude Code", "Codex".
pub fn name(kind: &str) -> &'static str {
    acp::spec(kind).map_or("Claude Code", |spec| spec.name)
}

/// `~/dev/lorca` for a folder in the home folder.
pub(crate) fn home_relative(path: &Path) -> String {
    match dirs::home_dir().and_then(|home| path.strip_prefix(&home).ok().map(Path::to_path_buf)) {
        Some(rest) if rest.as_os_str().is_empty() => "~".into(),
        Some(rest) => format!("~/{}", rest.display()),
        None => path.display().to_string(),
    }
}

/// How long an agent's question waits for an answer before the agent hears it was not answered.
const ASK_TIMEOUT: Duration = Duration::from_secs(10 * 60);

/// How the form an agent asks came back.
enum Form {
    Filled(serde_json::Map<String, Value>),
    Declined,
}

/// The question an agent waits on: its card in the chat, its form, and where the answer goes, with
/// whether the bot gave it.
struct Pending {
    card_id: String,
    ask: Ask,
    reply: oneshot::Sender<(Form, bool)>,
}

/// What the bot has yet to hear about its agent.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub(crate) enum News {
    /// It is done with what it was asked, and Lorca published these outputs from its work.
    Finished { said: Option<String>, outputs: Vec<proof::Reference> },
    /// It has shown nothing new for a while.
    Stalled,
    /// It exited on its own.
    Ended { outcome: String },
    /// Lorca quit while it was at work, so its process went with it.
    Interrupted,
    /// It asks a question, which a card in the chat shows and the bot may answer.
    Asked,
}

/// An agent as it survives a restart, in `agents/agents.json`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub(crate) struct Record {
    pub id: String,
    pub chat_id: String,
    pub bot_id: String,
    /// The `coding_agent` row that started it, which shows its card.
    pub message_id: String,
    /// The message behind the turn that started it: what Auto-review weighs its commands against.
    pub trigger_message_id: String,
    /// The user is talking to it in the chat: its turns are theirs, so its being done is not
    /// news for the bot to report.
    #[serde(default)]
    pub direct: bool,
    /// The user made the bot their proxy in this agent's conversation: its questions wake the
    /// bot even while the user drives it directly, so it can answer for them.
    #[serde(default)]
    pub proxy: bool,
    /// Nobody is watching the turn that started it (a routine's or an event's): its questions
    /// are the bot's to answer, as there is no user at the card.
    #[serde(default)]
    pub unattended: bool,
    pub kind: String,
    /// What resumes it: the agent's session.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session: Option<String>,
    /// The settings of its session (mode, model) as `(id, value)`, set again when it resumes its
    /// session in a new process.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub config: Vec<(String, String)>,
    /// What its card shows of its session, as `(name, value)`: its model, how hard it thinks, its
    /// mode, its context in use.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub settings: Vec<(String, String)>,
    pub folder: PathBuf,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub branch: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub base: Option<String>,
    pub task: String,
    pub state: String,
    #[serde(default)]
    pub stalled: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub outcome: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub pull_request: Option<String>,
    pub started_at: f64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ended_at: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub news: Option<News>,
    #[serde(default)]
    pub published: proof::Published,
    /// The bot's command secrets it has in its environment, by name.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub secrets: Vec<String>,
    /// The event behind the turn that started it (a channel's message, a service's event): its
    /// commands are weighed against the owner's task for it, never against the event's text.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub event: Option<crate::event_triggers::EventTask>,
    /// The card in the chat of the question it waits on, which a restart closes.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ask_card: Option<String>,
    /// The title the agent gave its session.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
}

impl Record {
    /// What Auto-review weighs the agent's actions against: the turn that started it.
    fn trigger(&self) -> crate::plugins::review::Trigger {
        crate::plugins::review::Trigger { message_id: self.trigger_message_id.clone(), routine: None, event: self.event.clone() }
    }
}

/// `text` with this Runner's saved secrets replaced by their placeholders, as a tool's result
/// is: what an agent prints, shows, or is sent goes to the card, its log, the bot, and outputs.
pub(crate) fn without_secrets(app: &App, text: String) -> String {
    crate::secrets::Redactions::load(app).text(&text).unwrap_or(text)
}

/// The transcript: the lines kept, how many went before them, and how far the bot has read.
#[derive(Default)]
struct Transcript {
    lines: VecDeque<String>,
    dropped: usize,
    read: usize,
}

impl Transcript {
    fn total(&self) -> usize {
        self.dropped + self.lines.len()
    }

    fn tail(&self, count: usize) -> Vec<String> {
        self.lines.iter().skip(self.lines.len().saturating_sub(count)).cloned().collect()
    }
}

pub struct Agent {
    record: Mutex<Record>,
    driver: Mutex<Option<Arc<dyn Driver>>>,
    transcript: Mutex<Transcript>,
    /// Its transcript as the apps draw it.
    entries: Mutex<entries::Entries>,
    /// What its card asks the user now: whether it may start, or run a command.
    question: Mutex<Option<AgentQuestion>>,
    /// The question it asks, on a card in the chat, waiting for an answer.
    pending: Mutex<Option<Pending>>,
    /// One question to the user at a time.
    asking: tokio::sync::Mutex<()>,
    /// When it last showed something new.
    active_at: Mutex<Instant>,
    /// Its work counts toward the limits of the turn that gave it the work, while it works.
    budget: Mutex<Option<crate::budgets::BudgetContext>>,
    hold: Mutex<Option<crate::budgets::RuntimeHold>>,
    /// The user or the bot is stopping it.
    stopping: AtomicBool,
    /// Which run of its process the supervisor follows: a resumed agent is a new one.
    generation: AtomicU64,
    /// The chat messages its words become, one per `said` segment, the one it is saying open.
    said: Mutex<SaidChat>,
}

/// The conversation as the chat shows it: the agent's words projected into messages of its own,
/// while its card keeps the tool calls, thoughts, and plan.
#[derive(Default)]
struct SaidChat {
    /// The entry key of the segment being said, and its message.
    current: Option<(String, Message)>,
    /// What the message held before the segment being said.
    prefix: String,
    /// What separates the segment being said from the prefix: a space inside one message, a
    /// paragraph after a tool call, plan, or note.
    separator: String,
    /// A tool call, plan, or note came since the last words: the next ones start a paragraph.
    paragraph: bool,
    /// When the open message was last written to the chat.
    flushed: Option<Instant>,
}

impl Agent {
    fn new(record: Record) -> Arc<Agent> {
        Arc::new(Agent {
            record: Mutex::new(record),
            driver: Mutex::new(None),
            transcript: Mutex::new(Transcript::default()),
            entries: Mutex::new(entries::Entries::default()),
            question: Mutex::new(None),
            pending: Mutex::new(None),
            asking: tokio::sync::Mutex::new(()),
            active_at: Mutex::new(Instant::now()),
            budget: Mutex::new(None),
            hold: Mutex::new(None),
            stopping: AtomicBool::new(false),
            generation: AtomicU64::new(0),
            said: Mutex::new(SaidChat::default()),
        })
    }

    pub fn id(&self) -> String {
        self.record.lock().unwrap().id.clone()
    }

    pub(crate) fn record(&self) -> Record {
        self.record.lock().unwrap().clone()
    }

    fn driver(&self) -> Option<Arc<dyn Driver>> {
        self.driver.lock().unwrap().clone()
    }

    /// Its process runs.
    pub fn is_running(&self) -> bool {
        self.driver().is_some()
    }

    fn is_open(&self) -> bool {
        matches!(self.record.lock().unwrap().state.as_str(), "checking" | "asking" | "starting" | "working" | "idle")
    }

    /// It waits for an answer: on its card, or to its question in the chat.
    fn waits(&self) -> bool {
        self.question.lock().unwrap().is_some() || self.pending.lock().unwrap().is_some()
    }

    fn add_lines(&self, app: &App, new: Vec<String>) {
        if new.is_empty() {
            return;
        }
        let redactions = crate::secrets::Redactions::load(app);
        let new: Vec<String> = new.into_iter().map(|line| redactions.text(&line).unwrap_or(line)).collect();
        let id = self.id();
        append_log(&app.config.home, &id, &new);
        let mut transcript = self.transcript.lock().unwrap();
        transcript.lines.extend(new);
        while transcript.lines.len() > KEEP_LINES {
            transcript.lines.pop_front();
            transcript.dropped += 1;
        }
        drop(transcript);
        *self.active_at.lock().unwrap() = Instant::now();
    }

    /// Puts entries of its transcript for the apps, without a saved secret, as its lines are.
    fn put_entries(&self, app: &App, new: Vec<(String, entries::Entry)>) {
        if new.is_empty() {
            return;
        }
        let redactions = crate::secrets::Redactions::load(app);
        let mut project: Vec<(String, String, bool)> = Vec::new();
        let mut paragraph = self.said.lock().unwrap().paragraph;
        {
            let mut entries = self.entries.lock().unwrap();
            for (key, entry) in new {
                let entry = entry.cleaned(|text| redactions.text(&text).unwrap_or(text));
                // What it says is a message of its own in the chat. A tool, a plan, or a note
                // separates the next words with a paragraph, but does not end the message: one
                // turn is one message. A new prompt ends it, and the next turn starts another.
                if entry.kind == "said" {
                    project.push((key.clone(), entry.text.clone(), paragraph));
                    paragraph = false;
                } else if entry.kind == "sent" {
                    project.push((String::new(), String::new(), true));
                    paragraph = false;
                } else if matches!(entry.kind.as_str(), "tool" | "plan" | "note") {
                    paragraph = true;
                }
                entries.put(key, entry);
            }
        }
        self.said.lock().unwrap().paragraph = paragraph;
        for (key, text, paragraph) in project {
            if key.is_empty() {
                self.end_said(app);
            } else {
                self.say_in_chat(app, &key, &text, paragraph);
            }
        }
        *self.active_at.lock().unwrap() = Instant::now();
    }

    /// Its words in the chat: one message for the turn, growing as it streams and complete when
    /// the turn is. A new `said` segment continues the same message; a tool call, plan, or note
    /// only makes the next segment start a paragraph, and a new user message ends it. `@Name` in
    /// the chat sends it here (`from_user`).
    fn say_in_chat(&self, app: &App, key: &str, text: &str, paragraph: bool) {
        let text = text.trim();
        if text.is_empty() {
            return;
        }
        let record = self.record();
        let author = crate::model::AgentAuthor { id: record.id, name: name(&record.kind).to_string(), kind: record.kind };
        let mut said = self.said.lock().unwrap();
        let same = matches!(&said.current, Some((open, _)) if open == key);
        if !same {
            // Keep the turn's one message, with its id and when it started: a new segment only
            // grows its text. A tool call, plan, or note makes the next segment a paragraph.
            let (mut message, prefix, separator) = match said.current.take() {
                Some((_, message)) => {
                    let Body::Text { text: body, .. } = &message.body else {
                        return;
                    };
                    let prefix = body.clone();
                    let separator = if paragraph { "\n\n".to_string() } else { " ".to_string() };
                    (message, prefix, separator)
                }
                None => (
                    Message::new(&record.chat_id, Author::Bot { bot_id: record.bot_id }, Body::text(text)),
                    String::new(),
                    String::new(),
                ),
            };
            let body = format!("{prefix}{separator}{text}");
            message.body = Body::text(body);
            message.agent = Some(author);
            message.state = crate::model::MessageState::Streaming;
            app.upsert_message(message.clone(), true);
            said.prefix = prefix;
            said.separator = separator;
            said.current = Some((key.to_string(), message));
            said.flushed = Some(Instant::now());
            return;
        }
        let due = said.flushed.is_none_or(|flushed| flushed.elapsed() >= CHAT_FLUSH);
        let body = format!("{}{}{}", said.prefix, said.separator, text);
        let (_, message) = said.current.as_mut().expect("a said message");
        let changed = match &mut message.body {
            Body::Text { text: current, .. } => {
                if current.as_str() == body {
                    false
                } else {
                    *current = body;
                    true
                }
            }
            _ => false,
        };
        if changed && due {
            message.state = crate::model::MessageState::Streaming;
            app.upsert_message(message.clone(), true);
            said.flushed = Some(Instant::now());
        }
    }

    /// The turn it was saying is over: its message in the chat is complete.
    fn end_said(&self, app: &App) {
        let mut said = self.said.lock().unwrap();
        if let Some((_, mut message)) = said.current.take() {
            message.state = crate::model::MessageState::Complete;
            app.upsert_message(message, true);
        }
        said.prefix.clear();
        said.separator.clear();
        said.paragraph = false;
        said.flushed = None;
    }

    /// A line of Lorca's own: in the bot's transcript, and a note in the apps'.
    fn note(&self, app: &App, line: &str) {
        self.add_lines(app, vec![line.to_string()]);
        let text = line.trim_start().trim_start_matches("⎿").trim().to_string();
        let key = format!("lorca:{}", uuid::Uuid::new_v4().simple());
        self.put_entries(app, vec![(key, entries::Entry::text("note", text))]);
    }

    /// The transcript the bot has not read, up to `READ_LINES` of its end, or its last `lines`.
    fn read(&self, lines: Option<usize>) -> (String, usize) {
        let mut transcript = self.transcript.lock().unwrap();
        let total = transcript.total();
        let from = match lines {
            Some(count) => total.saturating_sub(count),
            None => transcript.read.max(total.saturating_sub(READ_LINES)),
        };
        let skipped = if lines.is_none() { from.saturating_sub(transcript.read) } else { 0 };
        let start = from.saturating_sub(transcript.dropped);
        let text = transcript.lines.iter().skip(start).cloned().collect::<Vec<_>>().join("\n");
        transcript.read = total;
        (text, skipped)
    }
}

/// The coding agents on this Runner.
#[derive(Default)]
pub struct Agents {
    agents: Mutex<Vec<Arc<Agent>>>,
    /// Held while a card is read and written, so the latest state lands last.
    rows: Mutex<()>,
    /// `coding_agent` calls that start an agent, and the rows that show them: chat, call, row.
    calls: Mutex<Vec<(String, String, String)>>,
}

impl Agents {
    /// A `coding_agent` call that starts an agent began, and row `message_id` shows its card.
    pub fn begin(&self, chat_id: &str, call_id: &str, message_id: &str) {
        self.calls.lock().unwrap().push((chat_id.to_string(), call_id.to_string(), message_id.to_string()));
    }

    /// The row that shows call `call_id`'s card.
    pub fn row(&self, chat_id: &str, call_id: &str) -> Option<String> {
        self.calls.lock().unwrap().iter().find(|(chat, call, _)| chat == chat_id && call == call_id).map(|(_, _, row)| row.clone())
    }

    /// Changes the card in row `message_id` and puts it up.
    pub fn update_row(&self, app: &App, chat_id: &str, message_id: &str, change: impl FnOnce(&mut AgentRun)) {
        let _row = self.rows.lock().unwrap();
        let Some(mut message) = app.message(chat_id, message_id) else { return };
        let Body::Tool { agent: Some(card), .. } = &mut message.body else { return };
        change(card);
        app.upsert_message(message, true);
    }

    /// Call `call_id` returned, and `finish` writes its result into row `message_id`, with the
    /// card as it stands in the same write. A call that started no agent ends its card here.
    pub fn call_ended(&self, app: &App, chat_id: &str, call_id: &str, message_id: &str, is_error: bool, text: &str, finish: impl FnOnce(&mut Message)) {
        self.calls.lock().unwrap().retain(|(chat, call, _)| !(chat == chat_id && call == call_id));
        let started = self.by_row(chat_id, message_id);
        let _row = self.rows.lock().unwrap();
        let Some(mut message) = app.message(chat_id, message_id) else { return };
        finish(&mut message);
        if let Body::Tool { agent: Some(card), .. } = &mut message.body {
            match started {
                Some(agent) => *card = self.card(app, &agent),
                None if card.is_open() => {
                    if !matches!(card.state.as_str(), "denied" | "expired" | "dismissed" | "stopped") {
                        card.state = if is_error { "failed" } else { "stopped" }.into();
                        card.outcome = text.lines().next().map(|line| line.trim().to_string()).filter(|line| !line.is_empty());
                    }
                    card.question = None;
                }
                None => {}
            }
        }
        app.upsert_message(message, true);
    }

    /// The turn ended before call `call_id` returned: a card no agent took over stops.
    pub fn abandon_call(&self, app: &App, chat_id: &str, call_id: &str, message_id: &str) {
        self.calls.lock().unwrap().retain(|(chat, call, _)| !(chat == chat_id && call == call_id));
        if self.by_row(chat_id, message_id).is_some() {
            return;
        }
        self.update_row(app, chat_id, message_id, |card| {
            if card.is_open() {
                card.state = "stopped".into();
                card.question = None;
                card.outcome = Some("Stopped".into());
            }
        });
    }

    fn all(&self) -> Vec<Arc<Agent>> {
        self.agents.lock().unwrap().clone()
    }

    fn by_row(&self, chat_id: &str, message_id: &str) -> Option<Arc<Agent>> {
        self.all().into_iter().find(|agent| {
            let record = agent.record.lock().unwrap();
            record.chat_id == chat_id && record.message_id == message_id
        })
    }

    /// The agent `id`, when bot `bot_id` started it in chat `chat_id`.
    pub fn find(&self, chat_id: &str, bot_id: &str, id: &str) -> Option<Arc<Agent>> {
        let id = id.trim();
        self.all().into_iter().find(|agent| {
            let record = agent.record.lock().unwrap();
            record.id == id && record.chat_id == chat_id && record.bot_id == bot_id
        })
    }

    /// The agents bot `bot_id` started in chat `chat_id`, oldest first.
    pub fn of(&self, chat_id: &str, bot_id: &str) -> Vec<Arc<Agent>> {
        self.all().into_iter().filter(|agent| {
            let record = agent.record.lock().unwrap();
            record.chat_id == chat_id && record.bot_id == bot_id
        }).collect()
    }

    /// The card for `agent`, as it stands.
    fn card(&self, app: &App, agent: &Agent) -> AgentRun {
        let record = agent.record();
        let question = agent.question.lock().unwrap().clone();
        let output = agent.transcript.lock().unwrap().tail(ROW_LINES).join("\n");
        let device = app.bot(&record.bot_id).and_then(|bot| app.device(&bot.runner_id)).map(|device| device.name);
        let open = matches!(record.state.as_str(), "starting" | "working" | "idle");
        // The card goes to every Device: what the agent printed, asks, or ended on, without
        // a saved secret, as a command's card.
        let redactions = crate::secrets::Redactions::load(app);
        let clean = |text: String| redactions.text(&text).unwrap_or(text);
        let question = question.map(|question| AgentQuestion {
            text: clean(question.text),
            command: question.command.map(clean),
            choices: question.choices.into_iter().map(clean).collect(),
            ..question
        });
        let asking = record.ask_card.clone().filter(|_| open);
        let mut run = AgentRun {
            id: record.id,
            name: name(&record.kind).to_string(),
            settings: record.settings.into_iter().map(|(name, value)| crate::model::AgentSetting { name, value }).collect(),
            controls: match (open, agent.driver()) {
                (true, Some(driver)) => driver
                    .controls()
                    .into_iter()
                    .map(|control| crate::model::AgentControl {
                        what: control.what.into(),
                        name: control.name,
                        value: control.value,
                        choices: control.choices.into_iter().map(|(value, name)| crate::model::AgentChoice { value, name }).collect(),
                    })
                    .collect(),
                _ => Vec::new(),
            },
            kind: record.kind,
            task: record.task,
            folder: home_relative(&record.folder),
            branch: record.branch,
            state: if (question.is_some() || asking.is_some()) && open { "asking".into() } else { record.state },
            stalled: record.stalled,
            // A held start never makes an agent.
            held: false,
            question: question.filter(|_| open),
            asking,
            title: record.title.map(clean),
            output: (!output.trim().is_empty()).then_some(output),
            outcome: record.outcome,
            pull_request: record.pull_request,
            device,
            started_at: Some(record.started_at),
            ..AgentRun::default()
        };
        run.task = clean(run.task);
        redactions.option(&mut run.output);
        redactions.option(&mut run.outcome);
        run.summarize();
        run
    }

    /// Writes where `agent` stands to its card.
    fn sync(&self, app: &App, agent: &Agent) {
        let (chat_id, message_id) = {
            let record = agent.record.lock().unwrap();
            (record.chat_id.clone(), record.message_id.clone())
        };
        let card = self.card(app, agent);
        let _row = self.rows.lock().unwrap();
        let Some(mut message) = app.message(&chat_id, &message_id) else { return };
        let Body::Tool { agent: Some(current), .. } = &mut message.body else { return };
        if *current == card {
            return;
        }
        *current = card;
        app.upsert_message(message, true);
    }

    /// Stop in the chat: every agent there stops, and its card says so.
    pub fn stop_chat(&self, app: &Arc<App>, chat_id: &str) {
        for agent in self.all().into_iter().filter(|agent| agent.record.lock().unwrap().chat_id == chat_id) {
            stop(app, &agent, "Stopped");
        }
    }

    /// Stops and forgets the agents whose chat is gone, or whose bot is no longer in it.
    pub fn close_orphans(&self, app: &App) {
        let members: Vec<(String, Vec<String>)> = app.state.lock().unwrap().chats.iter().map(|chat| (chat.meta.id.clone(), chat.meta.bot_ids.clone())).collect();
        let orphans: Vec<Arc<Agent>> = self
            .all()
            .into_iter()
            .filter(|agent| {
                let record = agent.record.lock().unwrap();
                !members.iter().any(|(chat_id, bot_ids)| *chat_id == record.chat_id && bot_ids.contains(&record.bot_id))
            })
            .collect();
        if orphans.is_empty() {
            return;
        }
        for agent in &orphans {
            agent.stopping.store(true, Ordering::SeqCst);
            if let Some(driver) = agent.driver.lock().unwrap().take() {
                tokio::spawn(async move { driver.stop().await });
            }
        }
        self.agents.lock().unwrap().retain(|agent| !orphans.iter().any(|orphan| Arc::ptr_eq(agent, orphan)));
        save(app);
    }

    /// Lorca is quitting: every agent stops, and its card says so.
    pub async fn shutdown(&self, app: &App) {
        for agent in self.all() {
            let Some(driver) = agent.driver.lock().unwrap().take() else { continue };
            // Read before the stop, which the supervisor may answer first.
            let at_work = matches!(agent.record.lock().unwrap().state.as_str(), "starting" | "working");
            agent.stopping.store(true, Ordering::SeqCst);
            driver.stop().await;
            if at_work {
                let mut record = agent.record.lock().unwrap();
                record.state = "stopped".into();
                record.outcome = Some("Stopped when Lorca quit".into());
                record.ended_at = Some(crate::config::now_secs());
                record.news = Some(News::Interrupted);
            }
            *agent.question.lock().unwrap() = None;
            if let Some(waiting) = agent.pending.lock().unwrap().take() {
                close_ask_card(app, &agent.record().chat_id, &waiting.card_id, "dismissed");
            }
            agent.record.lock().unwrap().ask_card = None;
            self.sync(app, &agent);
            save_entries(app, &agent);
        }
        save(app);
    }

    /// An agent is at work: a restart would cut it off.
    pub fn is_busy(&self) -> bool {
        self.all().iter().any(|agent| matches!(agent.record.lock().unwrap().state.as_str(), "starting" | "working" | "asking"))
    }
}

/// The agents this Runner's last run left: their cards and handles stay. One stopped with it,
/// unless it was done with its work, which a follow-up resumes.
pub fn load(app: &Arc<App>) {
    let path = records_path(app);
    let records: Vec<Record> = std::fs::read(&path).ok().and_then(|bytes| serde_json::from_slice(&bytes).ok()).unwrap_or_default();
    let now = crate::config::now_secs();
    let this_device = app.this_device_id();
    for mut record in records {
        if record.ended_at.is_some_and(|ended| now - ended > FORGET_AFTER) {
            continue;
        }
        if app.bot(&record.bot_id).is_none_or(|bot| Some(&bot.runner_id) != this_device.as_ref()) {
            continue;
        }
        // The names the two adapters' agents went by before they were the only way to run them.
        record.kind = match record.kind.as_str() {
            "claude-acp" => "claude".into(),
            "codex-acp" => "codex".into(),
            _ => record.kind,
        };
        // A question dies with the process that asked it.
        if matches!(record.news, Some(News::Asked)) {
            record.news = None;
        }
        if let Some(card) = record.ask_card.take() {
            close_ask_card(app, &record.chat_id, &card, "dismissed");
        }
        if matches!(record.state.as_str(), "checking" | "asking" | "starting" | "working") {
            if record.state != "checking" {
                record.news = Some(News::Interrupted);
            }
            record.state = "stopped".into();
            record.outcome = Some("Stopped when Lorca quit".into());
            record.ended_at = Some(now);
        }
        let agent = Agent::new(record.clone());
        let lines = read_log(app, &record.id);
        {
            let mut transcript = agent.transcript.lock().unwrap();
            transcript.dropped = lines.len().saturating_sub(KEEP_LINES);
            transcript.lines = lines.into_iter().skip(transcript.dropped).collect();
            transcript.read = transcript.total();
        }
        let kept = std::fs::read(entries_path(&app.config.home, &record.id)).ok().and_then(|bytes| serde_json::from_slice::<Vec<entries::Entry>>(&bytes).ok());
        // One from before the apps' entries shows what it did as the bot's text, then entries.
        let kept = kept.unwrap_or_else(|| {
            let text = agent.transcript.lock().unwrap().tail(APP_LINES).join("\n");
            if text.trim().is_empty() { Vec::new() } else { vec![entries::Entry { rev: 1, ..entries::Entry::text("log", text) }] }
        });
        *agent.entries.lock().unwrap() = entries::Entries::restore(kept);
        app.coding_agents.agents.lock().unwrap().push(agent.clone());
        app.coding_agents.sync(app, &agent);
    }
    save(app);
}

/// The bot hears what its agent did that no turn has told it yet, which `load` brings back: an
/// agent that Lorca's quitting cut off, or whose news the last run lost.
pub fn wake_unheard(app: &Arc<App>) {
    for agent in app.coding_agents.all() {
        if agent.record.lock().unwrap().news.is_some() {
            wake(app, &agent);
        }
    }
}

fn records_path(app: &App) -> PathBuf {
    app.config.home.join("agents").join("agents.json")
}

fn log_path(home: &Path, id: &str) -> PathBuf {
    home.join("agents").join(format!("{id}.log"))
}

/// Writes every agent's record.
pub(crate) fn save(app: &App) {
    let records: Vec<Record> = app.coding_agents.all().iter().map(|agent| agent.record()).collect();
    if let Err(error) = crate::config::write_json_private(&records_path(app), &records) {
        tracing::warn!(%error, "saving the coding agents");
    }
}

fn append_log(home: &Path, id: &str, lines: &[String]) {
    use std::io::Write;
    let path = log_path(home, id);
    let _ = std::fs::create_dir_all(path.parent().unwrap_or(home));
    let mut options = std::fs::OpenOptions::new();
    options.create(true).append(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    if let Ok(mut file) = options.open(&path) {
        let _ = file.write_all(format!("{}\n", lines.join("\n")).as_bytes());
    }
}

fn entries_path(home: &Path, id: &str) -> PathBuf {
    home.join("agents").join(format!("{id}.entries.json"))
}

/// Keeps the apps' transcript of `agent` for after a restart, as it stands once it is done or gone.
fn save_entries(app: &App, agent: &Agent) {
    let list = agent.entries.lock().unwrap().all();
    let Ok(bytes) = serde_json::to_vec(&list) else { return };
    let path = entries_path(&app.config.home, &agent.id());
    let _ = std::fs::create_dir_all(path.parent().unwrap_or(&app.config.home));
    let _ = crate::config::write_private(&path, &bytes);
}

fn read_log(app: &App, id: &str) -> Vec<String> {
    std::fs::read_to_string(log_path(&app.config.home, id)).map(|text| text.lines().map(str::to_string).collect()).unwrap_or_default()
}

/// What starts an agent.
pub(crate) struct Start {
    pub chat_id: String,
    pub bot_id: String,
    pub message_id: String,
    pub trigger_message_id: String,
    pub kind: String,
    pub program: PathBuf,
    pub place: workspace::Place,
    pub task: String,
    pub prompt: String,
    /// The bot's command secrets to give it as environment variables, by name.
    pub secrets: Vec<String>,
    /// What the bot asked for beyond the defaults: `plan`, `model`, `effort`, each with its value.
    pub wants: Vec<(String, String)>,
    /// Nobody is watching the turn that starts it: its questions are the bot's to answer.
    pub unattended: bool,
    pub event: Option<crate::event_triggers::EventTask>,
}

/// Starts an agent and follows it. It returns once the agent runs; its work goes on.
pub(crate) async fn start(app: &Arc<App>, start: Start) -> Result<Arc<Agent>, String> {
    let id = format!("agent-{}", &uuid::Uuid::new_v4().simple().to_string()[..8]);
    let record = Record {
        id: id.clone(),
        chat_id: start.chat_id.clone(),
        bot_id: start.bot_id.clone(),
        message_id: start.message_id.clone(),
        trigger_message_id: start.trigger_message_id.clone(),
        direct: false,
        proxy: false,
        unattended: start.unattended,
        kind: start.kind.clone(),
        session: None,
        config: Vec::new(),
        settings: Vec::new(),
        folder: start.place.folder.clone(),
        branch: start.place.branch.clone(),
        base: start.place.base.clone(),
        task: without_secrets(app, start.task.clone()),
        state: "starting".into(),
        stalled: false,
        outcome: None,
        pull_request: None,
        started_at: crate::config::now_secs(),
        ended_at: None,
        news: None,
        published: proof::Published::default(),
        secrets: start.secrets.clone(),
        event: start.event.clone(),
        ask_card: None,
        title: None,
    };
    let agent = Agent::new(record);
    *agent.budget.lock().unwrap() = crate::budgets::current();
    app.coding_agents.agents.lock().unwrap().push(agent.clone());
    save(app);
    app.coding_agents.sync(app, &agent);
    let launched = launch(app, &agent, &start.program, &start.prompt, None, &start.wants).await;
    if let Err(error) = &launched {
        let mut record = agent.record.lock().unwrap();
        record.state = "failed".into();
        record.outcome = Some(error.clone());
        record.ended_at = Some(crate::config::now_secs());
    }
    app.coding_agents.sync(app, &agent);
    save(app);
    launched.map(|_| agent)
}

/// Starts the agent's process, on `prompt` (resuming `session`), with a supervisor that follows
/// it. An agent given secrets has them in its environment.
async fn launch(app: &Arc<App>, agent: &Arc<Agent>, program: &Path, prompt: &str, session: Option<&str>, wants: &[(String, String)]) -> Result<(), String> {
    use lorca_agent::tools::SecretVariables;
    let record = agent.record();
    // Looked up as it starts, so one the user deleted meanwhile is not given.
    let variables = match record.secrets.is_empty() {
        true => Vec::new(),
        false => crate::secrets::CommandSecrets { app: app.clone(), bot_id: record.bot_id.clone() }.variables(&record.secrets)?,
    };
    let spec = acp::spec(&record.kind).ok_or_else(|| format!("{} is not an agent this Lorca runs.", record.kind))?;
    let prompt = without_secrets(app, prompt.to_string());
    let (tx, rx) = mpsc::unbounded_channel();
    let generation = agent.generation.fetch_add(1, Ordering::SeqCst) + 1;
    agent.stopping.store(false, Ordering::SeqCst);
    agent.entries.lock().unwrap().new_run();
    tokio::spawn(supervise(app.clone(), agent.clone(), rx, generation));
    let driver = acp::start(spec, program, &record.folder, &prompt, session, &record.config, wants, &variables, tx).await?;
    *agent.driver.lock().unwrap() = Some(driver);
    // The card learns what can be changed now that the process is there to ask.
    app.coding_agents.sync(app, agent);
    Ok(())
}

/// The agent a chat message addresses with `@Name`, and its words without the mention. The most
/// recently started of two agents with one name wins.
pub(crate) fn mentioned(app: &App, chat_id: &str, bot_id: &str, text: &str) -> Option<(Arc<Agent>, String)> {
    let mut found: Option<(Arc<Agent>, String)> = None;
    for agent in app.coding_agents.of(chat_id, bot_id) {
        let record = agent.record();
        // An agent Auto-review is still deciding on has no session to take a message yet.
        if !matches!(record.state.as_str(), "starting" | "working" | "idle" | "asking") {
            continue;
        }
        let agent_name = name(&record.kind);
        let Some(words) = without_mention(agent_name, text) else {
            continue;
        };
        if found.as_ref().is_none_or(|(other, _)| record.started_at > other.record().started_at) {
            found = Some((agent, words));
        }
    }
    found
}

/// `text` without the first `@name`, when it addresses that agent.
fn without_mention(name: &str, text: &str) -> Option<String> {
    let (at, _) = text.match_indices('@').find(|(at, _)| crate::runtime::mention_at(text, *at, name))?;
    let end = at + 1 + name.len();
    Some(format!("{}{}", &text[..at], &text[end..]).trim().to_string())
}

/// The user's message in the chat goes to the agent as it was written, the `@Name` taken out,
/// cutting short what it does now so it reads at once. Its commands are weighed against this
/// message from here on.
pub(crate) fn from_user(app: &Arc<App>, agent: &Arc<Agent>, text: &str, message_id: &str) {
    let text = text.trim().to_string();
    if text.is_empty() {
        return;
    }
    {
        let mut record = agent.record.lock().unwrap();
        record.trigger_message_id = message_id.to_string();
        record.direct = true;
    }
    let (app, agent) = (app.clone(), agent.clone());
    tokio::spawn(async move {
        match send(&app, &agent, &text, true).await {
            Ok(_) => save(&app),
            Err(error) => {
                agent.note(&app, &format!("⎿ {error}"));
                save(&app);
            }
        }
    });
}

/// Delivers an `agent_message` job on this Runner: the user's message goes to the agent it
/// addresses, with the `@Name` taken out, before any turn job or chat lock. A job for the same
/// user message the agent is already answering is skipped, so a redelivery is harmless.
#[cfg(feature = "runner")]
pub(crate) fn from_job(app: &Arc<App>, job: &crate::model::Job) {
    let Some(target) = &job.agent_message else { return };
    let agent = app.coding_agents.find(&job.chat_id, &job.bot_id, &target.id);
    let (agent, words) = match agent {
        Some(agent) => {
            let record = agent.record();
            if !matches!(record.state.as_str(), "starting" | "working" | "idle" | "asking") {
                return;
            }
            if record.direct && record.trigger_message_id == job.trigger_message_id {
                return;
            }
            // A picked agent may be addressed by the id alone, without its `@Name` in the
            // text; then the whole text is the message. A typed `@Name` is stripped as usual.
            let words = without_mention(name(&record.kind), &target.text).unwrap_or_else(|| target.text.trim().to_string());
            (agent, words)
        }
        None => {
            let Some((agent, words)) = mentioned(app, &job.chat_id, &job.bot_id, &target.text) else { return };
            (agent, words)
        }
    };
    if words.is_empty() {
        return;
    }
    from_user(app, &agent, &words, &job.trigger_message_id);
}

/// Gives the agent a message from the bot: to its running process, or to a new process on its
/// session when its process is gone (it was done and stopped, or Lorca restarted).
pub(crate) async fn send(app: &Arc<App>, agent: &Arc<Agent>, text: &str, interrupt: bool) -> Result<String, String> {
    let text = &without_secrets(app, text.to_string());
    let record = agent.record();
    {
        let mut kept = agent.record.lock().unwrap();
        kept.news = None;
        kept.stalled = false;
    }
    // A job outside a turn has no budget of its own: keep the one the agent already carries.
    if let Some(budget) = crate::budgets::current() {
        *agent.budget.lock().unwrap() = Some(budget);
    }
    if let Some(driver) = agent.driver() {
        driver.send(text, interrupt).await?;
        *agent.active_at.lock().unwrap() = Instant::now();
        let busy = matches!(record.state.as_str(), "working" | "starting");
        return Ok(match (busy, interrupt) {
            (true, false) => format!("Sent. {} takes it up once it is done with what it is doing.", name(&record.kind)),
            (true, true) => format!("Sent. {} stopped what it was doing to take it up.", name(&record.kind)),
            _ => format!("Sent. {} is working on it.", name(&record.kind)),
        });
    }
    let program = find_program(&record.kind).await.ok_or_else(|| format!("{} is no longer installed on this Runner.", name(&record.kind)))?;
    if !record.folder.is_dir() {
        return Err(format!("{} is gone, so it cannot go on there.", home_relative(&record.folder)));
    }
    {
        let mut kept = agent.record.lock().unwrap();
        kept.state = "starting".into();
        kept.outcome = None;
        kept.ended_at = None;
    }
    app.coding_agents.sync(app, agent);
    match launch(app, agent, &program, text, record.session.as_deref(), &[]).await {
        Ok(()) => {
            save(app);
            Ok(format!("{} started again on its session and is working on it.", name(&record.kind)))
        }
        Err(error) => {
            {
                let mut kept = agent.record.lock().unwrap();
                kept.state = "failed".into();
                kept.outcome = Some(error.clone());
                kept.ended_at = Some(crate::config::now_secs());
            }
            app.coding_agents.sync(app, agent);
            save(app);
            Err(error)
        }
    }
}

/// Stops the agent; its card says `reason`.
pub(crate) fn stop(app: &Arc<App>, agent: &Arc<Agent>, reason: &str) {
    agent.end_said(app);
    agent.stopping.store(true, Ordering::SeqCst);
    *agent.question.lock().unwrap() = None;
    agent.pending.lock().unwrap().take();
    let driver = agent.driver.lock().unwrap().take();
    {
        let mut record = agent.record.lock().unwrap();
        if matches!(record.state.as_str(), "checking" | "asking" | "starting" | "working" | "idle") {
            record.state = "stopped".into();
            record.outcome = Some(reason.to_string());
            record.ended_at = Some(crate::config::now_secs());
            record.news = None;
        }
    }
    agent.hold.lock().unwrap().take();
    if let Some(driver) = driver {
        tokio::spawn(async move { driver.stop().await });
    }
    app.coding_agents.sync(app, agent);
    save(app);
}

/// Follows one run of the agent: its events, the card, a stall, and the idle limit.
async fn supervise(app: Arc<App>, agent: Arc<Agent>, mut events: mpsc::UnboundedReceiver<Event>, generation: u64) {
    let agents = &app.coding_agents;
    let mut dirty = false;
    let mut last_sync = Instant::now();
    loop {
        if agent.generation.load(Ordering::SeqCst) != generation {
            return;
        }
        let now = Instant::now();
        let active_at = *agent.active_at.lock().unwrap();
        let (state, stalled) = {
            let record = agent.record.lock().unwrap();
            (record.state.clone(), record.stalled)
        };
        // Working with nothing new for a while: the bot hears it once.
        if state == "working" && !stalled && !agent.waits() && now >= active_at + STALL_AFTER {
            {
                let mut record = agent.record.lock().unwrap();
                record.stalled = true;
                record.news = Some(News::Stalled);
            }
            agent.end_said(&app);
            agents.sync(&app, &agent);
            save(&app);
            wake(&app, &agent);
        }
        // Done and quiet for long: the agent stops, and a follow-up resumes it.
        if state == "idle" && !agent.waits() && now >= active_at + IDLE_LIMIT {
            if let Some(driver) = agent.driver.lock().unwrap().take() {
                agent.stopping.store(true, Ordering::SeqCst);
                tokio::spawn(async move { driver.stop().await });
            }
        }
        let mut wake_at = now + Duration::from_secs(60);
        if state == "working" && !stalled {
            wake_at = wake_at.min(active_at + STALL_AFTER);
        }
        if state == "idle" {
            wake_at = wake_at.min(active_at + IDLE_LIMIT);
        }
        if dirty {
            let due = (active_at + ROW_SETTLE).min(last_sync + ROW_EVERY);
            if now >= due {
                agents.sync(&app, &agent);
                dirty = false;
                last_sync = now;
            } else {
                wake_at = wake_at.min(due);
            }
        }
        tokio::select! {
            event = events.recv() => {
                let Some(event) = event else { return };
                if agent.generation.load(Ordering::SeqCst) != generation {
                    return;
                }
                dirty |= handle(&app, &agent, event, generation).await;
            }
            _ = tokio::time::sleep_until(wake_at) => {}
        }
    }
}

/// One event of the agent's. True when only the transcript changed, which the card catches up
/// with in a moment; a change of state goes up at once.
async fn handle(app: &Arc<App>, agent: &Arc<Agent>, event: Event, generation: u64) -> bool {
    let agents = &app.coding_agents;
    match event {
        Event::Session(session) => {
            agent.record.lock().unwrap().session = Some(session);
            save(app);
            false
        }
        Event::Config(config) => {
            agent.record.lock().unwrap().config = config;
            save(app);
            false
        }
        // The card catches up in a moment; the record is saved with the next state change.
        Event::Settings(settings) => {
            let mut record = agent.record.lock().unwrap();
            let changed = record.settings != settings;
            record.settings = settings;
            changed
        }
        Event::Entries(new) => {
            agent.put_entries(app, new);
            unstall(app, agent);
            false
        }
        Event::Lines(new) => {
            if let Some(link) = new.iter().find_map(|line| proof::pull_request(line)) {
                agent.record.lock().unwrap().pull_request = Some(link);
            }
            agent.add_lines(app, new);
            unstall(app, agent);
            true
        }
        Event::Working => {
            let was = {
                let mut record = agent.record.lock().unwrap();
                let was = record.state.clone();
                record.state = "working".into();
                record.stalled = false;
                was
            };
            *agent.active_at.lock().unwrap() = Instant::now();
            if was != "working" {
                // Its run time counts toward the limits of the turn that gave it the work.
                let budget = agent.budget.lock().unwrap().clone();
                if let Some(budget) = budget {
                    match budget.hold() {
                        Ok(hold) => {
                            let deadline = hold.deadline();
                            *agent.hold.lock().unwrap() = Some(hold);
                            if let Some(deadline) = deadline {
                                let (app, agent) = (app.clone(), agent.clone());
                                tokio::spawn(async move {
                                    tokio::time::sleep(deadline).await;
                                    if agent.generation.load(Ordering::SeqCst) == generation && agent.record.lock().unwrap().state == "working" {
                                        stop(&app, &agent, "Stopped at the bot's time limit");
                                    }
                                });
                            }
                        }
                        Err(reason) => {
                            stop(app, agent, &reason);
                            return false;
                        }
                    }
                }
                agents.sync(app, agent);
                save(app);
            }
            false
        }
        Event::Idle(said) => {
            agent.end_said(app);
            {
                let mut record = agent.record.lock().unwrap();
                if !matches!(record.state.as_str(), "working" | "starting") {
                    return false;
                }
                record.state = "idle".into();
                record.stalled = false;
            }
            agent.hold.lock().unwrap().take();
            agents.sync(app, agent);
            // What it made is published as outputs; the bot hears it unless the user drove this
            // turn from the chat, where its words are already showing.
            let outputs = proof::publish(app, agent).await;
            if agent.record.lock().unwrap().direct {
                agent.record.lock().unwrap().news = None;
                agents.sync(app, agent);
                save(app);
                save_entries(app, agent);
                return false;
            }
            agent.record.lock().unwrap().news = Some(News::Finished { said, outputs });
            agents.sync(app, agent);
            save(app);
            save_entries(app, agent);
            wake(app, agent);
            false
        }
        Event::Approve(approval, reply) => {
            let (app, agent) = (app.clone(), agent.clone());
            tokio::spawn(async move {
                let verdict = approve(&app, &agent, approval).await;
                let _ = reply.send(verdict);
            });
            false
        }
        Event::Ask(ask, reply) => {
            let (app, agent) = (app.clone(), agent.clone());
            tokio::spawn(async move {
                let answers = converse(&app, &agent, ask).await;
                let _ = reply.send(answers);
            });
            false
        }
        Event::Title(title) => {
            let title = Some(title).filter(|title| !title.is_empty());
            let changed = std::mem::replace(&mut agent.record.lock().unwrap().title, title.clone()) != title;
            changed
        }
        Event::Ended(end) => {
            ended(app, agent, end, generation);
            false
        }
    }
}

/// The agent shows something new: it is no longer stalled.
fn unstall(app: &App, agent: &Agent) {
    let was = std::mem::replace(&mut agent.record.lock().unwrap().stalled, false);
    if was {
        app.coding_agents.sync(app, agent);
    }
}

/// Its process is gone. One that went on its own tells the bot; one the bot or the user stopped
/// says so on its card, and one that was done with its work stays idle, to resume.
fn ended(app: &Arc<App>, agent: &Arc<Agent>, end: driver::Ended, generation: u64) {
    if agent.generation.load(Ordering::SeqCst) != generation {
        return;
    }
    agent.driver.lock().unwrap().take();
    agent.hold.lock().unwrap().take();
    *agent.question.lock().unwrap() = None;
    agent.pending.lock().unwrap().take();
    agent.end_said(app);
    let stopping = agent.stopping.load(Ordering::SeqCst);
    let tell = {
        let mut record = agent.record.lock().unwrap();
        match record.state.as_str() {
            // Stopped already, by the bot or the user.
            "stopped" | "failed" | "exited" | "denied" | "expired" | "dismissed" => false,
            // Done, and stopped while it waited: a follow-up resumes it.
            "idle" if stopping => false,
            _ if stopping => {
                record.state = "stopped".into();
                record.outcome = Some(end.outcome.clone());
                record.ended_at = Some(crate::config::now_secs());
                false
            }
            _ => {
                record.state = if end.failed { "failed" } else { "exited" }.into();
                record.outcome = Some(end.outcome.clone());
                record.ended_at = Some(crate::config::now_secs());
                record.news = Some(News::Ended { outcome: end.outcome.clone() });
                true
            }
        }
    };
    app.coding_agents.sync(app, agent);
    save(app);
    save_entries(app, agent);
    if tell {
        wake(app, agent);
    }
}

/// The bot hears what its agent did, in a turn of its own once no turn runs in the chat, unless
/// a turn read it meanwhile (`coding_agent read`).
fn wake(app: &Arc<App>, agent: &Arc<Agent>) {
    let record = agent.record();
    if record.news.is_none() {
        return;
    }
    let job = crate::runtime::agent_job(app, &record.chat_id, &record.bot_id, &record.message_id);
    let (app, agent) = (app.clone(), agent.clone());
    tokio::spawn(async move {
        let unheard = {
            let lock = app.chat_lock(&job.chat_id);
            let _turn = lock.lock().await;
            agent.record.lock().unwrap().news.is_some()
        };
        if unheard {
            crate::runtime::start_turn(&app, job);
        }
    });
}

/// What an `agent` turn opens with: what the agent did that the bot has not heard. None when it
/// heard it already.
pub fn wake_cue(app: &App, job: &crate::model::Job) -> Option<String> {
    let agent = app.coding_agents.by_row(&job.chat_id, &job.trigger_message_id)?;
    let (record, news) = {
        let mut record = agent.record.lock().unwrap();
        let news = record.news.take()?;
        (record.clone(), news)
    };
    save(app);
    let who = format!("{} ({})", name(&record.kind), record.id);
    let place = match &record.branch {
        Some(branch) => format!("in {} on branch {branch}", home_relative(&record.folder)),
        None => format!("in {}", home_relative(&record.folder)),
    };
    let tail = || {
        let lines = agent.transcript.lock().unwrap().tail(12).join("\n");
        if lines.trim().is_empty() { String::new() } else { format!(" Its last lines:\n{lines}\n") }
    };
    let cue = match news {
        News::Asked => {
            let ask = agent.pending.lock().unwrap().as_ref().map(|pending| pending.ask.clone())?;
            let guidance = if record.proxy {
                "The user made you their proxy in this conversation: answer it with coding_agent answer, in their interest, or decline it (decline: true) if that is what they would want."
            } else if record.unattended {
                "Nobody is watching this conversation: answer it with coding_agent answer, in the user's interest, or decline it (decline: true) if that is what they would want."
            } else {
                "Answer it with coding_agent answer only when what the user told you settles it, or decline it (decline: true); otherwise leave it, and the user answers on its card in the chat."
            };
            format!(
                "[{who}, working {place} on “{}”, asks:\n{}\n{} {guidance} Nobody answering in {} minutes cancels it.]",
                record.task,
                ask.message.trim(),
                ask_fields(&ask),
                ASK_TIMEOUT.as_secs() / 60
            )
        }
        News::Finished { said, outputs } => {
            let said = said.map(|said| format!(" It said:\n{}\n", said.chars().take(4000).collect::<String>())).unwrap_or_default();
            let outputs = if outputs.is_empty() {
                String::new()
            } else {
                let list = outputs.iter().map(|output| format!("{} (message {}, output {}, version {})", output.name, output.message_id, output.output_id, output.version)).collect::<Vec<_>>().join("; ");
                format!(" Lorca published from its work: {list}. Cite them as task evidence by those references.")
            };
            format!(
                "[{who}, which you started {place} on “{}”, is done.{said}{outputs} Check its work against what you asked, then send it a follow-up with coding_agent send, or tell the user how it went.]",
                record.task
            )
        }
        News::Stalled => format!(
            "[{who}, working {place}, has shown nothing new for {} minutes; it may be stuck.{} Read its transcript with coding_agent read, send it a message (interrupt: true cuts in), or stop it.]",
            STALL_AFTER.as_secs() / 60,
            tail()
        ),
        News::Ended { outcome } => format!(
            "[{who}, which you started {place}, has ended: {outcome}.{} Tell the user, or send it a message to start it again on its session.]",
            tail()
        ),
        News::Interrupted => format!(
            "[{who}, which you started {place} on “{}”, was cut off when Lorca quit while it worked; what it had changed in its folder is kept.{} Tell the user it was interrupted and how far it got, and send it a message with coding_agent send to start it again on its session if the job is not done.]",
            record.task,
            tail()
        ),
    };
    Some(without_secrets(app, cue))
}

/// What Auto-review hears about an agent's environment: the saved secrets in it.
pub(crate) fn holds_secrets(names: &[String]) -> String {
    format!(
        "It runs with the user's saved secrets {} in its environment, which the user gave this bot for its commands; sending one anywhere but the service it is for leaks it.",
        names.iter().map(|name| format!("${name}")).collect::<Vec<_>>().join(", ")
    )
}

/// Whether the agent may do what it asks: the bot's Access as it stands, then Auto-review, and
/// the user on its card when the review asks.
async fn approve(app: &Arc<App>, agent: &Arc<Agent>, approval: Approval) -> Result<(), String> {
    // Neither the review nor the card sees a saved secret the agent wrote out.
    let approval = match approval {
        Approval::Command { command } => Approval::Command { command: without_secrets(app, command) },
        Approval::Files { paths } => Approval::Files { paths: paths.into_iter().map(|path| without_secrets(app, path)).collect() },
        Approval::Tool { name, mut input } => {
            crate::secrets::Redactions::load(app).json(&mut input);
            Approval::Tool { name, input }
        }
    };
    let record = agent.record();
    let who = name(&record.kind);
    let Some(bot) = app.bot(&record.bot_id) else { return Err("Its bot is gone.".into()) };
    if crate::permissions::check_tool(app, &bot, "bash").is_err() {
        return Err("Shell commands are off for the bot that started you, so nothing may run. Stop and say what you needed.".into());
    }
    let folder = std::fs::canonicalize(&record.folder).unwrap_or(record.folder.clone());
    let runner_name = app.device(&bot.runner_id).map(|device| device.name).unwrap_or_else(|| "this Runner".into());
    let auto_review = app.auto_review().is_enabled;
    let (tool, description, args, question) = match &approval {
        Approval::Command { command } => {
            // Memory is the bot's, and only memory_update writes it.
            if crate::local_review::writes_memory_root(app, command, &folder) {
                return Err("Memory is changed only with memory_update, never by a command. Do not retry it.".into());
            }
            // An agent holding saved secrets has each command reviewed, knowing which: any of
            // them can read its environment.
            if auto_review && record.secrets.is_empty() && crate::local_review::needs_no_review(app, command, &folder) {
                return Ok(());
            }
            let mut description = format!(
                "{who}, a coding agent this bot started, wants to run this shell command as the user on {runner_name}, with full filesystem, process, credential, and network access. Working directory: {}.",
                home_relative(&folder)
            );
            if !record.secrets.is_empty() {
                description.push_str(&format!(" {}", holds_secrets(&record.secrets)));
            }
            let question = AgentQuestion { kind: "command".into(), command: Some(command.chars().take(crate::model::APP_COMMAND_CHARS).collect()), ..Default::default() };
            ("bash", description, json!({ "command": command }), question)
        }
        Approval::Files { paths } => {
            if paths.iter().any(|path| crate::local_review::path_writes_memory_root(app, path, &folder)) {
                return Err("Memory is changed only with memory_update, never by editing its files. Do not retry it.".into());
            }
            let inside = |path: &String| {
                let path = Path::new(path);
                let path = if path.is_absolute() { path.to_path_buf() } else { folder.join(path) };
                path.starts_with(&folder) || path.starts_with(&record.folder)
            };
            if !paths.is_empty() && paths.iter().all(inside) {
                return Ok(());
            }
            let description = format!("{who}, a coding agent this bot started in {}, wants to change files outside that folder, as the user on {runner_name}.", home_relative(&folder));
            let shown = paths.iter().map(|path| home_relative(Path::new(path))).collect::<Vec<_>>().join("\n");
            let question = AgentQuestion { kind: "command".into(), command: Some(format!("Change {shown}")), ..Default::default() };
            ("edit", description, json!({ "paths": paths }), question)
        }
        Approval::Tool { name: tool, input } => {
            let description = format!("{who}, a coding agent this bot started in {}, wants to use its tool {tool} as the user on {runner_name}.", home_relative(&folder));
            let detail = lines::claude_detail(tool, input, &folder);
            let question = AgentQuestion { kind: "command".into(), command: Some(lines::call(tool, &detail).trim_start_matches("● ").to_string()), ..Default::default() };
            ("coding_agent_tool", description, json!({ "tool": tool, "input": input }), question)
        }
    };
    let trigger = record.trigger();
    let cancel = tokio_util::sync::CancellationToken::new();
    let propose_rule = matches!(approval, Approval::Command { .. });
    let facts = match &approval {
        Approval::Command { command } => crate::local_review::facts(app, command, &folder).await,
        _ => Vec::new(),
    };
    let action = crate::plugins::review::Action { target_name: &runner_name, tool, description: &description, args: &args, script: None, facts: &facts, propose_rule };
    let (reason, rule) = match crate::plugins::review::review(app, &bot, &record.chat_id, &trigger, action, &cancel).await {
        crate::plugins::review::Outcome::Allow => return Ok(()),
        // The agent reads why and goes on another way, or stops and says what it needed.
        crate::plugins::review::Outcome::Held { reason, .. } => return Err(crate::plugins::review::agent_note(&reason)),
        crate::plugins::review::Outcome::Ask { reason, rule } => (reason, rule),
    };
    // Nobody watches the run that started it, so a question would wait for no one.
    if record.unattended {
        let why = reason.as_deref().unwrap_or("Auto-review is off, so every command asks");
        return Err(format!(
            "This needs the user's permission ({why}), and nobody is watching the run that started you, so it did not run. Go on \
             with work that does not depend on it, or stop and name this action exactly in your last message, so the bot that \
             started you can ask the user."
        ));
    }
    // One question at a time, on its card.
    let _one = agent.asking.lock().await;
    if !agent.is_running() {
        return Err("Stopped".into());
    }
    let always_rule = rule.clone().map(|text| crate::model::AutoReviewRule { id: uuid::Uuid::new_v4().to_string(), text, behavior: "allow".into(), tool: None });
    let question = AgentQuestion { reason, rule: always_rule.as_ref().map(|rule| rule.text.clone()), ..question };
    let decision = ask_on_card(app, agent, question, always_rule).await;
    use crate::plugins::mcp::Decision;
    match decision {
        Decision::Allowed | Decision::Always => Ok(()),
        Decision::Denied => Err("The user did not allow this in Lorca. Do not retry it; go on without it or stop and say what you needed.".into()),
        Decision::Expired => Err("Nobody answered in time, so it did not run. Go on without it or stop and say what you needed.".into()),
        Decision::Dismissed => Err("The user wrote to the bot instead of answering, so it did not run. Go on without it or stop and say what you needed.".into()),
    }
}

/// Puts `question` on the agent's card and waits for the user's Allow or Deny from any Device
/// (`chats.permission` names the card's row), as a command's card does.
async fn ask_on_card(app: &Arc<App>, agent: &Arc<Agent>, question: AgentQuestion, always_rule: Option<crate::model::AutoReviewRule>) -> crate::plugins::mcp::Decision {
    let (chat_id, message_id) = {
        let record = agent.record.lock().unwrap();
        (record.chat_id.clone(), record.message_id.clone())
    };
    let cancel = tokio_util::sync::CancellationToken::new();
    // Stopping the agent stops the question.
    let watch = {
        let (agent, cancel) = (agent.clone(), cancel.clone());
        tokio::spawn(async move {
            while !cancel.is_cancelled() {
                if !agent.is_running() || !agent.is_open() {
                    cancel.cancel();
                    break;
                }
                tokio::time::sleep(Duration::from_millis(500)).await;
            }
        })
    };
    let decision = crate::plugins::mcp::await_answer(app, &chat_id, &message_id, always_rule, &cancel, || {
        *agent.question.lock().unwrap() = Some(question);
        app.coding_agents.sync(app, agent);
        if let Some(message) = app.message(&chat_id, &message_id) {
            crate::push::permission(app, &message);
        }
    })
    .await;
    cancel.cancel();
    watch.abort();
    *agent.question.lock().unwrap() = None;
    app.coding_agents.sync(app, agent);
    decision
}

pub use crate::model::ASK_TOOL;

/// Whether the bot hears a question this agent asks: it does when the user made it their proxy,
/// and for an agent nobody is watching. A question in an attended conversation is the user's,
/// whichever turn directs the agent; a proxy mode sets `Record.proxy` and gets the bot's turn.
fn bot_answers_questions(record: &Record) -> bool {
    record.proxy || record.unattended
}

/// Asks the user what the agent asks, on a card of its own in the chat: the whole form, which the
/// user fills in from any Device or the bot fills in for them. What comes back is the form filled
/// in by field key, or `None` when it was declined; nobody answering in time, or the agent
/// stopping, is an `Err`, and the card says which.
async fn converse(app: &Arc<App>, agent: &Arc<Agent>, ask: Ask) -> Result<Option<serde_json::Map<String, Value>>, String> {
    // One question at a time, as a permission is asked.
    agent.end_said(app);
    let _one = agent.asking.lock().await;
    if !agent.is_running() {
        return Err("Stopped".into());
    }
    let record = agent.record();
    let card = ask_card(app, &record, &ask);
    let (tx, rx) = oneshot::channel();
    *agent.pending.lock().unwrap() = Some(Pending { card_id: card.id.clone(), ask, reply: tx });
    {
        let mut kept = agent.record.lock().unwrap();
        kept.ask_card = Some(card.id.clone());
        kept.news = Some(News::Asked);
    }
    app.upsert_message(card.clone(), true);
    crate::push::permission(app, &card);
    app.coding_agents.sync(app, agent);
    save(app);
    if bot_answers_questions(&record) {
        wake(app, agent);
    }
    let result = tokio::time::timeout(ASK_TIMEOUT, rx).await;
    agent.pending.lock().unwrap().take();
    {
        let mut kept = agent.record.lock().unwrap();
        kept.ask_card = None;
        if matches!(kept.news, Some(News::Asked)) {
            kept.news = None;
        }
    }
    let outcome = match result {
        Ok(Ok((form, by_bot))) => {
            if by_bot {
                agent.note(app, "  ⎿ Answered by the bot");
            }
            Ok(match form {
                Form::Filled(content) => Some(content),
                Form::Declined => None,
            })
        }
        Ok(Err(_)) => {
            close_ask_card(app, &record.chat_id, &card.id, "dismissed");
            Err("Stopped".into())
        }
        Err(_) => {
            close_ask_card(app, &record.chat_id, &card.id, "expired");
            Err(format!("Nobody answered in {} minutes", ASK_TIMEOUT.as_secs() / 60))
        }
    };
    app.coding_agents.sync(app, agent);
    save(app);
    outcome
}

/// The card that asks the user what the agent asks, from the bot that runs it, without a saved
/// secret the agent wrote into it.
fn ask_card(app: &App, record: &Record, ask: &Ask) -> Message {
    let redactions = crate::secrets::Redactions::load(app);
    let clean = |text: &str| redactions.text(text).unwrap_or_else(|| text.to_string());
    let fields = ask
        .fields
        .iter()
        .map(|field| {
            let (kind, choices) = match &field.kind {
                FieldKind::One(choices) => ("one", choices.as_slice()),
                FieldKind::Many(choices) => ("many", choices.as_slice()),
                FieldKind::Flag => ("flag", &[][..]),
                FieldKind::Text => ("text", &[][..]),
                FieldKind::Number { integer: true } => ("integer", &[][..]),
                FieldKind::Number { integer: false } => ("number", &[][..]),
            };
            AgentAskField {
                key: field.key.clone(),
                label: clean(&field.label),
                kind: kind.into(),
                required: field.required,
                choices: choices.iter().map(|choice| AgentAskChoice { value: clean(&choice.value), title: clean(&choice.title), description: choice.description.as_deref().map(clean) }).collect(),
                other: field.other.is_some(),
            }
        })
        .collect();
    let message = clean(ask.message.trim());
    let who = name(&record.kind);
    let summary = format!("{who} asks: {}", message.lines().map(str::trim).find(|line| !line.is_empty()).unwrap_or("a question"));
    Message::new(
        &record.chat_id,
        Author::Bot { bot_id: record.bot_id.clone() },
        Body::Permission {
            plugin_id: String::new(),
            plugin_name: who.to_string(),
            tool: ASK_TOOL.into(),
            summary: summary.chars().take(300).collect(),
            arguments: Value::Null,
            decision: "pending".into(),
            reason: None,
            rule: None,
            command: None,
            link: None,
            code: None,
            secret: None,
            ask: None,
            agent_ask: Some(AgentAsk { agent_id: record.id.clone(), kind: record.kind.clone(), name: who.to_string(), row_id: record.message_id.clone(), message, fields, answers: Vec::new(), by: None }),
        },
    )
}

/// Writes how a question card ended: `decision`, with what was answered and by whom.
fn set_ask_card(app: &App, chat_id: &str, card_id: &str, decision: &str, answers: Vec<AgentSetting>, by: Option<&str>) {
    let Some(mut card) = app.message(chat_id, card_id) else { return };
    let Body::Permission { decision: shown, agent_ask: Some(ask), .. } = &mut card.body else { return };
    if shown != "pending" {
        return;
    }
    *shown = decision.into();
    ask.answers = answers;
    ask.by = by.map(str::to_string);
    app.upsert_message(card, true);
}

fn close_ask_card(app: &App, chat_id: &str, card_id: &str, decision: &str) {
    set_ask_card(app, chat_id, card_id, decision, Vec::new(), None);
}

/// Fills in the form an agent asks with `given`, from the user or, with `by_bot`, the bot: each
/// field by its key or its label, a pick by its value or its title, the user's own words where the
/// field takes them. The card shows the answer at once, then the agent gets it. Answers what was
/// given, in words.
pub(crate) fn answer_form(app: &App, agent: &Agent, given: &serde_json::Map<String, Value>, by_bot: bool) -> Result<String, String> {
    let (card_id, ask) = {
        let pending = agent.pending.lock().unwrap();
        let pending = pending.as_ref().ok_or("It no longer asks")?;
        (pending.card_id.clone(), pending.ask.clone())
    };
    let (content, answers) = fill(&ask, given)?;
    let waiting = agent.pending.lock().unwrap().take().ok_or("It no longer asks")?;
    let chat_id = agent.record().chat_id;
    let shown = answers.iter().map(|answer| format!("{}: {}", answer.name, answer.value)).collect::<Vec<_>>().join("; ");
    set_ask_card(app, &chat_id, &card_id, "allowed", answers, Some(if by_bot { "bot" } else { "you" }));
    waiting.reply.send((Form::Filled(content), by_bot)).map_err(|_| "It no longer asks".to_string())?;
    Ok(shown)
}

/// Declines the question an agent asks, for the user or, with `by_bot`, the bot.
pub(crate) fn decline(app: &App, agent: &Agent, by_bot: bool) -> Result<(), String> {
    let waiting = agent.pending.lock().unwrap().take().ok_or("It no longer asks")?;
    set_ask_card(app, &agent.record().chat_id, &waiting.card_id, "denied", Vec::new(), Some(if by_bot { "bot" } else { "you" }));
    waiting.reply.send((Form::Declined, by_bot)).map_err(|_| "It no longer asks".to_string())
}

/// The fields of `ask` filled in from `given`: what the agent is told, by field key, and what the
/// card shows, by field label. A field is found by its key or its label, however cased.
fn fill(ask: &Ask, given: &serde_json::Map<String, Value>) -> Result<(serde_json::Map<String, Value>, Vec<AgentSetting>), String> {
    let mut content = serde_json::Map::new();
    let mut answers = Vec::new();
    for key in given.keys() {
        if !ask.fields.iter().any(|field| field.key == *key || field.label.eq_ignore_ascii_case(key)) {
            return Err(format!("It asks nothing called “{key}”. It asks: {}", ask.fields.iter().map(|field| format!("“{}”", field.label)).collect::<Vec<_>>().join(", ")));
        }
    }
    for field in &ask.fields {
        let value = given.get(&field.key).or_else(|| given.iter().find(|(key, _)| field.label.eq_ignore_ascii_case(key)).map(|(_, value)| value));
        let words = |value: &Value| match value {
            Value::String(text) => Some(text.trim().to_string()).filter(|text| !text.is_empty()),
            Value::Number(number) => Some(number.to_string()),
            Value::Bool(flag) => Some(flag.to_string()),
            _ => None,
        };
        let pick = |choices: &[driver::Choice], text: &str| choices.iter().find(|choice| choice.value == text || choice.title.eq_ignore_ascii_case(text) || choice.value.eq_ignore_ascii_case(text)).cloned();
        let offers = |choices: &[driver::Choice]| choices.iter().map(|choice| format!("“{}”", choice.title)).collect::<Vec<_>>().join(", ");
        let missing = || format!("Answer “{}”", field.label);
        match &field.kind {
            FieldKind::One(choices) => {
                let Some(text) = value.and_then(words) else {
                    if field.required {
                        return Err(missing());
                    }
                    continue;
                };
                match (pick(choices, &text), &field.other) {
                    (Some(choice), _) => {
                        content.insert(field.key.clone(), choice.value.into());
                        answers.push(AgentSetting { name: field.label.clone(), value: choice.title });
                    }
                    (None, Some(other)) => {
                        content.insert(other.clone(), text.clone().into());
                        answers.push(AgentSetting { name: field.label.clone(), value: text });
                    }
                    (None, None) => return Err(format!("“{}” takes one of {}", field.label, offers(choices))),
                }
            }
            FieldKind::Many(choices) => {
                let items: Vec<String> = match value {
                    Some(Value::Array(items)) => items.iter().filter_map(words).collect(),
                    Some(other) => words(other).into_iter().collect(),
                    None => Vec::new(),
                };
                let mut picked: Vec<driver::Choice> = Vec::new();
                let mut own: Vec<String> = Vec::new();
                for item in items {
                    match pick(choices, &item) {
                        Some(choice) if !picked.contains(&choice) => picked.push(choice),
                        Some(_) => {}
                        None if field.other.is_some() => own.push(item),
                        None => return Err(format!("“{}” takes any of {}", field.label, offers(choices))),
                    }
                }
                if picked.is_empty() && own.is_empty() {
                    if field.required {
                        return Err(missing());
                    }
                    continue;
                }
                content.insert(field.key.clone(), picked.iter().map(|choice| choice.value.clone()).collect::<Vec<_>>().into());
                if let (Some(other), false) = (&field.other, own.is_empty()) {
                    content.insert(other.clone(), own.join(", ").into());
                }
                let mut shown: Vec<String> = picked.into_iter().map(|choice| choice.title).collect();
                shown.extend(own);
                answers.push(AgentSetting { name: field.label.clone(), value: shown.join(", ") });
            }
            FieldKind::Flag => {
                let flag = match value {
                    Some(Value::Bool(flag)) => Some(*flag),
                    Some(other) => match words(other).map(|text| text.to_lowercase()).as_deref() {
                        Some("yes" | "true" | "y" | "1") => Some(true),
                        Some("no" | "false" | "n" | "0") => Some(false),
                        Some(_) => return Err(format!("“{}” is yes or no", field.label)),
                        None => None,
                    },
                    None => None,
                };
                match flag {
                    Some(flag) => {
                        content.insert(field.key.clone(), flag.into());
                        answers.push(AgentSetting { name: field.label.clone(), value: if flag { "Yes" } else { "No" }.into() });
                    }
                    None if field.required => return Err(missing()),
                    None => {}
                }
            }
            FieldKind::Text => match value.and_then(words) {
                Some(text) => {
                    let text: String = text.chars().take(MAX_ANSWER).collect();
                    content.insert(field.key.clone(), text.clone().into());
                    answers.push(AgentSetting { name: field.label.clone(), value: text });
                }
                None if field.required => return Err(missing()),
                None => {}
            },
            FieldKind::Number { integer } => match value.and_then(words) {
                Some(text) => {
                    let number = if *integer { text.parse::<i64>().ok().map(Value::from) } else { text.parse::<f64>().ok().filter(|number| number.is_finite()).map(Value::from) };
                    let Some(number) = number else {
                        return Err(format!("“{}” is {}", field.label, if *integer { "a whole number" } else { "a number" }));
                    };
                    answers.push(AgentSetting { name: field.label.clone(), value: number.to_string() });
                    content.insert(field.key.clone(), number);
                }
                None if field.required => return Err(missing()),
                None => {}
            },
        }
    }
    Ok((content, answers))
}

/// What the bot gives as its answer, as the form's values by field: `answers` as they are, a field's
/// choice by its number from 1, or `choice` and `text` for a form that asks one thing.
pub(crate) fn bot_answers(agent: &Agent, args: &Value) -> Result<serde_json::Map<String, Value>, String> {
    let ask = agent.pending.lock().unwrap().as_ref().map(|pending| pending.ask.clone()).ok_or("It no longer asks")?;
    let by_number = |field: &driver::Field, value: &Value| -> Result<Value, String> {
        let choices = match &field.kind {
            FieldKind::One(choices) | FieldKind::Many(choices) => choices.as_slice(),
            FieldKind::Flag => return Ok(match value.as_u64() { Some(1) => Value::Bool(true), Some(2) => Value::Bool(false), _ => value.clone() }),
            _ => return Ok(value.clone()),
        };
        let one = |value: &Value| match value.as_u64() {
            Some(at) if at >= 1 && (at as usize) <= choices.len() => Ok(Value::String(choices[at as usize - 1].value.clone())),
            Some(_) => Err(format!("“{}” has choices numbered 1 to {}", field.label, choices.len())),
            None => Ok(value.clone()),
        };
        match value {
            Value::Array(items) => items.iter().map(one).collect::<Result<Vec<_>, _>>().map(Value::Array),
            other => one(other),
        }
    };
    if let Some(answers) = args["answers"].as_object() {
        let mut given = serde_json::Map::new();
        for (key, value) in answers {
            let value = match ask.fields.iter().find(|field| field.key == *key || field.label.eq_ignore_ascii_case(key)) {
                Some(field) => by_number(field, value)?,
                None => value.clone(),
            };
            given.insert(key.clone(), value);
        }
        return Ok(given);
    }
    let [field] = ask.fields.as_slice() else {
        return Err(format!("It asks {} things: answer with `answers`, by field label.", ask.fields.len()));
    };
    let value = match (&args["choice"], args["text"].as_str()) {
        (Value::Number(_), _) => by_number(field, &args["choice"])?,
        (_, Some(text)) => Value::String(text.to_string()),
        _ => return Err("answer needs `answers`, a `choice`, or `text`, or `decline: true`".into()),
    };
    Ok(serde_json::Map::from_iter([(field.key.clone(), value)]))
}

/// The longest answer in words a field takes.
const MAX_ANSWER: usize = 4000;

/// The fields of a question as the bot reads them: each with its number, label, and what it takes.
fn ask_fields(ask: &Ask) -> String {
    let list = |choices: &[driver::Choice]| {
        choices
            .iter()
            .enumerate()
            .map(|(at, choice)| match &choice.description {
                Some(about) => format!("{}. {}: {about}", at + 1, choice.title),
                None => format!("{}. {}", at + 1, choice.title),
            })
            .collect::<Vec<_>>()
            .join("; ")
    };
    let fields: Vec<String> = ask
        .fields
        .iter()
        .map(|field| {
            let takes = match &field.kind {
                FieldKind::One(choices) => format!("one of {}{}", list(choices), if field.other.is_some() { ", or words of its own" } else { "" }),
                FieldKind::Many(choices) => format!("any of {}{}", list(choices), if field.other.is_some() { ", and words of its own" } else { "" }),
                FieldKind::Flag => "yes or no".into(),
                FieldKind::Text => "text".into(),
                FieldKind::Number { integer: true } => "a whole number".into(),
                FieldKind::Number { integer: false } => "a number".into(),
            };
            format!("- “{}”{}: {takes}", field.label, if field.required { "" } else { " (optional)" })
        })
        .collect();
    let how = if ask.fields.len() == 1 { "Answer with `choice` (its number) or `text`, or `answers` by field." } else { "Answer with `answers`, an object by field label: a choice's title or number, text, true or false, a number." };
    format!("Its form:\n{}\n{how}", fields.join("\n"))
}

/// Changes the model or how hard a running agent thinks, from the bot or the user: `wants` are
/// `(what, value)` as `start` takes them. Answers how it stands now.
pub(crate) async fn set_options(app: &Arc<App>, agent: &Arc<Agent>, wants: &[(String, String)]) -> Result<String, String> {
    let record = agent.record();
    if !matches!(record.state.as_str(), "starting" | "working" | "idle") {
        return Err(format!("{} ({}) has ended, so its settings stay as they were. Send it a message to start it again on its session first.", name(&record.kind), record.id));
    }
    let driver = agent.driver().ok_or_else(|| format!("{} ({}) is not running. Send it a message to start it again on its session first.", name(&record.kind), record.id))?;
    driver.configure(wants).await?;
    app.coding_agents.sync(app, agent);
    save(app);
    let now = driver
        .controls()
        .iter()
        .map(|control| format!("{}: {}", control.name, control.choices.iter().find(|(value, _)| *value == control.value).map_or(control.value.as_str(), |(_, name)| name.as_str())))
        .collect::<Vec<_>>()
        .join(", ");
    Ok(format!("Set. {} ({}) now has {now}.", name(&record.kind), record.id))
}

/// `coding.transcript`, `coding.stop`, `coding.set`, and `coding.answer` for a card on this Runner, from the
/// local app or a sealed request: `{ chat_id, message_id, since?, before?, limit?, choice?, text?, what?, value? }`.
pub async fn serve(app: &Arc<App>, verb: &str, body: &Value) -> Result<Value, String> {
    let chat_id = body["chat_id"].as_str().ok_or("missing chat_id")?;
    let message_id = body["message_id"].as_str().ok_or("missing message_id")?;
    let message = app.message(chat_id, message_id).ok_or("Unknown message")?;
    let Author::Bot { .. } = &message.author else { return Err("That row has no coding agent".into()) };
    let agent = app.coding_agents.by_row(chat_id, message_id).ok_or("That coding agent never started")?;
    match verb {
        "coding.transcript" => {
            // The entries changed after the change the app has (`since`), else the newest page of
            // the entries before `before` (all of them without it and without `limit`).
            let since = body["since"].as_u64();
            let before = body["before"].as_u64();
            let limit = body["limit"].as_u64().map_or(usize::MAX, |limit| limit.clamp(1, MAX_APP_ENTRIES) as usize);
            let (changed, rev, first, kept_no_entries) = {
                let entries = agent.entries.lock().unwrap();
                let kept_no_entries = entries.is_empty();
                let (changed, rev, first) = match since {
                    Some(since) => entries.since(Some(since)),
                    None => entries.page(before, limit),
                };
                (changed, rev, first, kept_no_entries)
            };
            // An agent that keeps no entries answers the bot's text transcript instead.
            let text = if kept_no_entries { agent.transcript.lock().unwrap().tail(APP_LINES).join("\n") } else { String::new() };
            Ok(json!({ "text": text, "card": app.coding_agents.card(app, &agent), "entries": changed, "rev": rev, "first": first }))
        }
        "coding.stop" => {
            if !agent.is_open() {
                return Err("It has already ended".into());
            }
            stop(app, &agent, "Stopped");
            Ok(json!({ "stopped": true }))
        }
        "coding.set" => {
            let what = body["what"].as_str().filter(|what| matches!(*what, "model" | "effort")).ok_or("Set model or effort")?;
            let value = body["value"].as_str().map(str::trim).filter(|value| !value.is_empty()).ok_or("Name the value")?;
            set_options(app, &agent, &[(what.to_string(), value.to_string())]).await?;
            Ok(json!({ "set": true }))
        }
        // The question the agent asks in the chat: its form filled in by field key, or declined.
        "coding.answer" => {
            if body["decline"] == true {
                decline(app, &agent, false)?;
                return Ok(json!({ "answered": true }));
            }
            let values = body["values"].as_object().ok_or("An answer is the form's values, or decline")?;
            answer_form(app, &agent, values, false)?;
            Ok(json!({ "answered": true }))
        }
        other => Err(format!("Unknown request {other}")),
    }
}

#[cfg(test)]
mod tests;
