//! Coding agents that speak the Agent Client Protocol (ACP): JSON-RPC over stdio, the protocol
//! editors use to drive an agent, through the official Rust SDK. Lorca is the client. It starts
//! the agent's ACP command in the folder, opens a session, sends the bot's messages as prompts
//! one at a time, writes the agent's updates into the transcript, and decides what the agent
//! asks permission for. The agent runs its own tools: Lorca offers it no file system and no
//! terminal, so it asks only what its own settings make it ask, and every ask goes through
//! Auto-review as a command's does. Claude Code and Codex are driven through adapters that wrap
//! the CLI the user has installed; the others speak ACP themselves.

use std::collections::{HashMap, VecDeque};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use agent_client_protocol::schema::v1::{
    CancelNotification, ClientCapabilities, ClientSessionCapabilities, CloseSessionRequest, CompactionCapabilities, ContentBlock, CreateElicitationRequest, CreateElicitationResponse, ElicitationAcceptAction, ElicitationAction, ElicitationCapabilities,
    ElicitationContentValue, ElicitationFormCapabilities, ElicitationMode, Implementation, InitializeRequest, NoticeCapabilities, PermissionOption, PermissionOptionId, PermissionOptionKind, PromptRequest, PromptResponse,
    RequestPermissionOutcome, RequestPermissionRequest, RequestPermissionResponse, SelectedPermissionOutcome, SessionConfigId, SessionId, SessionNotification, SetSessionConfigOptionRequest,
    StopReason, TextContent,
};
use agent_client_protocol::schema::ProtocolVersion;
use agent_client_protocol::{Agent as Peer, ByteStreams, Client, ConnectionTo, UntypedMessage};
use async_trait::async_trait;
use futures::future::BoxFuture;
use futures::FutureExt;
use serde_json::{json, Value};
use tokio::sync::{mpsc, oneshot};
use tokio_util::compat::{TokioAsyncReadCompatExt, TokioAsyncWriteCompatExt};

use super::driver::{Approval, Ask, Choice, Control, Driver, Ended, Event, Events, Field, FieldKind};
use super::entries::{self, Entry, FileDiff, Step};
use super::lines;
use super::process::{self, Process};

/// An agent Lorca drives over ACP: the name in `coding_agent`'s `agent`, what it is called, and
/// how it starts. The differences between agents live here and nowhere else.
pub(crate) struct Spec {
    pub kind: &'static str,
    pub name: &'static str,
    /// The programs it may be installed as. For an agent behind an adapter, the CLI it wraps.
    pub bins: &'static [&'static str],
    /// The arguments that start its ACP mode.
    pub args: &'static [&'static str],
    pub adapter: Option<Adapter>,
    /// The value of its `mode` setting under which it asks before it acts.
    pub safe_mode: Option<&'static str>,
}

/// A CLI that does not speak ACP, driven through an adapter run with `npx`: a package pinned to
/// the version Lorca was tried with, told which installed CLI to use.
pub(crate) struct Adapter {
    pub package: &'static str,
    /// The adapter's variable naming the CLI to run, so it uses the user's install and sign-in
    /// and not a copy of its own.
    pub binary_env: &'static str,
    pub env: &'static [(&'static str, &'static str)],
}

/// The agents Lorca drives over ACP, in the order they are listed.
pub(crate) const SPECS: &[Spec] = &[
    Spec {
        kind: "claude",
        name: "Claude Code",
        bins: &["claude"],
        args: &[],
        adapter: Some(Adapter { package: "@agentclientprotocol/claude-agent-acp@0.94.0", binary_env: "CLAUDE_CODE_EXECUTABLE", env: &[] }),
        safe_mode: Some("default"),
    },
    Spec {
        kind: "codex",
        name: "Codex",
        bins: &["codex"],
        args: &[],
        adapter: Some(Adapter { package: "@agentclientprotocol/codex-acp@2.2.2", binary_env: "CODEX_PATH", env: &[] }),
        safe_mode: Some("workspace-write"),
    },
    Spec { kind: "gemini", name: "Gemini CLI", bins: &["gemini"], args: &["--acp"], adapter: None, safe_mode: None },
    Spec { kind: "opencode", name: "OpenCode", bins: &["opencode"], args: &["acp"], adapter: None, safe_mode: None },
    Spec { kind: "kimi", name: "Kimi Code", bins: &["kimi"], args: &["acp"], adapter: None, safe_mode: None },
    Spec { kind: "copilot", name: "GitHub Copilot", bins: &["copilot"], args: &["--acp", "--stdio"], adapter: None, safe_mode: None },
    Spec { kind: "cursor", name: "Cursor", bins: &["cursor-agent", "agent"], args: &["acp"], adapter: None, safe_mode: None },
    Spec { kind: "kiro", name: "Kiro", bins: &["kiro-cli"], args: &["acp"], adapter: None, safe_mode: None },
    // pi has no ACP mode of its own; its adapter runs `pi --mode rpc`. pi asks before nothing, so
    // none of its commands or edits reaches Lorca to decide.
    Spec {
        kind: "pi",
        name: "pi",
        bins: &["pi"],
        args: &[],
        adapter: Some(Adapter { package: "pi-acp@0.0.34", binary_env: "PI_ACP_PI_COMMAND", env: &[] }),
        safe_mode: None,
    },
];

pub(crate) fn spec(kind: &str) -> Option<&'static Spec> {
    SPECS.iter().find(|spec| spec.kind == kind)
}

/// Where the CLI is installed, under whichever program name it goes by.
async fn find_cli(spec: &Spec) -> Option<PathBuf> {
    for bin in spec.bins {
        if let Some(path) = process::find(bin).await {
            return Some(path);
        }
    }
    None
}

/// The program that starts the agent, when it can start here: the agent's own CLI, or `npx` for
/// one behind an adapter, whose CLI must be installed too.
pub(crate) async fn find(spec: &Spec) -> Option<PathBuf> {
    let cli = find_cli(spec).await?;
    match spec.adapter {
        Some(_) => process::find("npx").await,
        None => Some(cli),
    }
}

/// How long an agent has to answer the handshake and open a session. An old CLI without an ACP
/// mode ignores the flag and never answers. An adapter's first run downloads it.
const SETUP_TIMEOUT: Duration = Duration::from_secs(30);
const ADAPTER_SETUP_TIMEOUT: Duration = Duration::from_secs(240);

type Transport = ByteStreams<tokio_util::compat::Compat<tokio::process::ChildStdin>, tokio_util::compat::Compat<tokio::process::ChildStdout>>;
/// Answers `start` once the session is open, or why it could not be.
type Ready = Arc<Mutex<Option<oneshot::Sender<Result<(), String>>>>>;

/// What the agent said and showed since its last tool call, kept to write whole lines and to
/// report its last message when it is done.
#[derive(Default)]
struct Said {
    /// The unfinished line of what it is saying.
    partial: String,
    /// The message so far, since its last tool call.
    message: String,
    /// The message before that call.
    before: String,
    /// What the entry it is saying now holds, and its key; none between entries.
    segment: Option<(String, String)>,
    /// The message the entry is, when the agent names its messages (`messageId`).
    message_id: Option<String>,
    /// What it is thinking now, under its key, beside what it says.
    thought: Option<(String, String)>,
    thought_id: Option<String>,
    /// When the thought it is thinking now began.
    thought_started: Option<Instant>,
}

/// A tool call the transcript follows. Some agents begin one with a bare title ("Terminal") and
/// say what it runs in a later update, so a call is shown once it can say what it acts on, or
/// when it ends.
struct Call {
    title: String,
    /// The tool's own name (`Bash`, an MCP tool's), when the agent gives it.
    name: String,
    input: Value,
    shown: bool,
    kind: String,
    status: String,
    /// What its terminal wrote, as it came, from its end.
    output: String,
    exit_code: Option<i64>,
    diffs: Vec<FileDiff>,
    result: String,
}

#[derive(Default)]
struct Shared {
    said: Mutex<Said>,
    /// The tool calls of the session, by id.
    calls: Mutex<HashMap<String, Call>>,
    /// What it offers: `(name, description)`.
    commands: Mutex<Vec<(String, String)>>,
    /// The last plan the transcript showed.
    plan: Mutex<String>,
    /// The context compactions of the session, by id: how each stands and its summary so far.
    compactions: Mutex<HashMap<String, (String, String)>>,
    /// What the session said as it opened: its settings, in `configOptions` or the older `modes` and
    /// `models`.
    opening: Mutex<Value>,
    /// What the card shows of its settings, and of its use so far.
    settings: Mutex<Vec<(String, String)>>,
    usage: Mutex<Vec<(String, String)>>,
    /// Where setup stands, for the error when it fails.
    stage: Mutex<&'static str>,
    /// What its process wrote last on stderr, once it is gone before it was ready.
    last_words: Mutex<Option<String>>,
    /// Its session history is being replayed (`session/load`): none of it is new.
    replaying: AtomicBool,
    /// It said or did something since the turn began.
    produced: AtomicBool,
    /// Keys of the entries it puts, and the turn its plan belongs to.
    keys: std::sync::atomic::AtomicU64,
    turn: std::sync::atomic::AtomicU64,
}

enum Command {
    Send { text: String, interrupt: bool, from: String },
    /// Change its model or effort now; the answer says whether the session took it.
    Set { wants: Vec<(String, String)>, reply: oneshot::Sender<Result<(), String>> },
    /// It is being stopped: the session closes first when the agent can close one.
    Close { done: oneshot::Sender<()> },
}

/// How long the agent has to close its session before its process is stopped.
const CLOSE_TIMEOUT: Duration = Duration::from_secs(3);

pub(crate) struct Acp {
    commands: mpsc::UnboundedSender<Command>,
    process: Arc<Process>,
    shared: Arc<Shared>,
}

impl Drop for Acp {
    fn drop(&mut self) {
        self.process.stop();
    }
}

#[async_trait]
impl Driver for Acp {
    async fn send(&self, text: &str, interrupt: bool, from: &str) -> Result<(), String> {
        self.commands.send(Command::Send { text: text.to_string(), interrupt, from: from.to_string() }).map_err(|_| "It is no longer running".to_string())
    }

    async fn commands(&self) -> Vec<(String, String)> {
        self.shared.commands.lock().unwrap().clone()
    }

    async fn configure(&self, wants: &[(String, String)]) -> Result<(), String> {
        let (reply, answer) = oneshot::channel();
        self.commands.send(Command::Set { wants: wants.to_vec(), reply }).map_err(|_| "It is no longer running".to_string())?;
        answer.await.map_err(|_| "It is no longer running".to_string())?
    }

    fn controls(&self) -> Vec<Control> {
        let opening = self.shared.opening.lock().unwrap();
        opening["configOptions"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|option| {
                let what = match option["category"].as_str()? {
                    "model" => "model",
                    "thought_level" => "effort",
                    _ => return None,
                };
                let choices = option_pairs(option);
                if choices.len() < 2 {
                    return None;
                }
                Some(Control { what, name: option["name"].as_str()?.to_string(), value: option["currentValue"].as_str()?.to_string(), choices })
            })
            .collect()
    }

    async fn stop(&self) {
        let (done, closed) = oneshot::channel();
        if self.commands.send(Command::Close { done }).is_ok() {
            let _ = tokio::time::timeout(CLOSE_TIMEOUT, closed).await;
        }
        self.process.stop();
    }
}

/// What a run needs to open its session.
struct Setup {
    spec: &'static Spec,
    folder: PathBuf,
    /// The first prompt.
    first: String,
    /// The session of an earlier run, to resume.
    resume: Option<String>,
    /// The settings of the earlier run, to set again.
    config: Vec<(String, String)>,
    /// What the bot asked for at the start: `plan`, `model`, or `effort`, with its value.
    wants: Vec<(String, String)>,
}

/// Starts `spec` in `folder` on `prompt`, or resumes `session` with it when the agent can, with
/// `config` set again. It answers once the session is open.
pub(crate) async fn start(
    spec: &'static Spec,
    program: &Path,
    folder: &Path,
    prompt: &str,
    session: Option<&str>,
    config: &[(String, String)],
    wants: &[(String, String)],
    variables: &[process::Variable],
    events: Events,
) -> Result<Arc<dyn Driver>, String> {
    let mut variables = variables.to_vec();
    let args: Vec<String> = match &spec.adapter {
        Some(adapter) => {
            let cli = find_cli(spec).await.ok_or_else(|| format!("{} is not installed on this Runner.", spec.name))?;
            variables.push((adapter.binary_env.into(), cli.into_os_string()));
            variables.extend(adapter.env.iter().map(|(name, value)| ((*name).into(), (*value).into())));
            vec!["-y".to_string(), adapter.package.to_string()]
        }
        None => spec.args.iter().map(|arg| arg.to_string()).collect(),
    };
    let (process, stdout, exit) = process::spawn(program, &args, folder, &variables).await?;
    let stdin = process.take_stdin().await.ok_or("No stdin")?;
    let transport = ByteStreams::new(stdin.compat_write(), stdout.compat());
    let (commands, receiver) = mpsc::unbounded_channel();
    let (ready, mut waiting) = oneshot::channel();
    let ready: Ready = Arc::new(Mutex::new(Some(ready)));
    let shared = Arc::new(Shared::default());
    let setup = Setup { spec, folder: folder.to_path_buf(), first: prompt.to_string(), resume: session.map(str::to_string), config: config.to_vec(), wants: wants.to_vec() };
    tokio::spawn(run(setup, transport, events, receiver, shared.clone(), ready, exit));
    let limit = if spec.adapter.is_some() { ADAPTER_SETUP_TIMEOUT } else { SETUP_TIMEOUT };
    let opened = match tokio::time::timeout(limit, &mut waiting).await {
        Ok(Ok(opened)) => opened,
        Ok(Err(_)) => Err(format!("{} stopped before it was ready", spec.name)),
        Err(_) => {
            process.stop();
            let _ = tokio::time::timeout(Duration::from_secs(3), waiting).await;
            let said = shared.last_words.lock().unwrap().take().map(|said| format!(" ({said})")).unwrap_or_default();
            Err(format!("{} did not answer ACP within {} seconds{said}. Its version may be too old to speak it; update it and start again.", spec.name, limit.as_secs()))
        }
    };
    if let Err(error) = opened {
        process.stop();
        return Err(error);
    }
    Ok(Arc::new(Acp { commands, process, shared }))
}

/// One run of the agent's process: its connection, then how it ended. A run that never opened its
/// session tells `start` why, and nothing else.
async fn run(setup: Setup, transport: Transport, events: Events, commands: mpsc::UnboundedReceiver<Command>, shared: Arc<Shared>, ready: Ready, exit: oneshot::Receiver<process::Exit>) {
    let name = setup.spec.name;
    let result = drive(setup, transport, events.clone(), commands, shared.clone(), ready.clone()).await;
    let exit = exit.await.ok();
    let said = exit.as_ref().and_then(|exit| exit.said.clone());
    let pending = ready.lock().unwrap().take();
    if let Some(pending) = pending {
        let reason = result.err().unwrap_or_else(|| format!("{name} stopped before it was ready"));
        let reason = match &said {
            Some(said) if !reason.contains(said.as_str()) => format!("{reason} ({said})"),
            _ => reason,
        };
        *shared.last_words.lock().unwrap() = said;
        let _ = pending.send(Err(reason));
        return;
    }
    let ended = match exit {
        Some(process::Exit { stopped: true, .. }) => Ended { outcome: "Stopped".into(), failed: false },
        Some(process::Exit { code: Some(0), .. }) => Ended { outcome: format!("{name} exited"), failed: false },
        Some(process::Exit { code, said, .. }) => {
            let code = code.map(|code| format!(" with code {code}")).unwrap_or_default();
            Ended { outcome: format!("{name} exited{code}{}", said.map(|said| format!(": {said}")).unwrap_or_default()), failed: true }
        }
        None => Ended { outcome: format!("{name} exited"), failed: true },
    };
    let _ = events.send(Event::Ended(ended));
}

/// The connection: the handshake and the session, then the prompts one after another until the
/// process is gone. The SDK holds the dispatch loop while a callback runs, so a callback only
/// hands its work off: a permission request is answered from a task of its own.
async fn drive(setup: Setup, transport: Transport, events: Events, mut commands: mpsc::UnboundedReceiver<Command>, shared: Arc<Shared>, ready: Ready) -> Result<(), String> {
    let Setup { spec, folder, first, resume, mut config, wants } = setup;
    let updates = (events.clone(), shared.clone(), folder.clone());
    let asks = (events.clone(), shared.clone(), folder.clone());
    let questions = (events.clone(), shared.clone());
    let failing = shared.clone();
    Client
        .builder()
        .on_receive_notification(
            async move |notification: SessionNotification, _cx| {
                let value = serde_json::to_value(&notification.update).unwrap_or_default();
                update(&updates.0, &updates.1, &value, &updates.2);
                Ok(())
            },
            agent_client_protocol::on_receive_notification!(),
        )
        .on_receive_request(
            async move |request: RequestPermissionRequest, responder, _cx| {
                let (events, shared, folder) = asks.clone();
                tokio::spawn(async move {
                    let outcome = decide(spec, &request, &events, &shared, &folder).await;
                    let _ = responder.respond(RequestPermissionResponse::new(outcome));
                });
                Ok(())
            },
            agent_client_protocol::on_receive_request!(),
        )
        .on_receive_request(
            async move |request: CreateElicitationRequest, responder, _cx| {
                let (events, shared) = questions.clone();
                tokio::spawn(async move {
                    let response = elicit(&request, &events, &shared).await;
                    let _ = responder.respond(response);
                });
                Ok(())
            },
            agent_client_protocol::on_receive_request!(),
        )
        .connect_with(transport, move |connection: ConnectionTo<Peer>| async move {
            *shared.stage.lock().unwrap() = "initialize";
            let capabilities = ClientCapabilities::new()
                .elicitation(ElicitationCapabilities::new().form(ElicitationFormCapabilities::new()))
                // Notices and compactions as updates of their own, not words in what the agent says.
                .session(ClientSessionCapabilities::new().notices(NoticeCapabilities::new()).compaction(CompactionCapabilities::new()))
                // A command's output as its terminal writes it, in chunks (`_meta.terminal_output_delta`), as Zed takes it.
                .meta(serde_json::Map::from_iter([("terminal_output_delta".to_string(), Value::Bool(true))]));
            let lorca = Implementation::new("lorca", env!("CARGO_PKG_VERSION")).title("Lorca");
            let initialized = connection.send_request(InitializeRequest::new(ProtocolVersion::V1).client_capabilities(capabilities).client_info(lorca)).block_task().await?;
            let sessions = &initialized.agent_capabilities.session_capabilities;
            let (can_resume, can_close) = (sessions.resume.is_some(), sessions.close.is_some());
            *shared.stage.lock().unwrap() = "session";
            let mut notes: Vec<String> = Vec::new();
            let mut opened: Option<(SessionId, Value)> = None;
            if let Some(old) = &resume {
                // Resumed without its history where the agent can, which Lorca has; else loaded,
                // its history replayed and passed over.
                if can_resume || initialized.agent_capabilities.load_session {
                    let method = if can_resume { "session/resume" } else { "session/load" };
                    shared.replaying.store(!can_resume, Ordering::SeqCst);
                    let session = SessionId::new(Arc::<str>::from(old.as_str()));
                    let params = json!({ "sessionId": old, "cwd": folder, "mcpServers": [] });
                    let loaded = connection.send_request(UntypedMessage::new(method, params)?).block_task().await;
                    shared.replaying.store(false, Ordering::SeqCst);
                    match loaded {
                        Ok(loaded) => opened = Some((session, loaded)),
                        Err(_) => notes.push(format!("{} could not take up its earlier session, so this one starts fresh.", spec.name)),
                    }
                } else {
                    notes.push(format!("{} cannot resume a session, so this one starts fresh.", spec.name));
                }
            }
            let (session, opening) = match opened {
                Some(opened) => opened,
                None => {
                    let created = connection.send_request(UntypedMessage::new("session/new", json!({ "cwd": folder, "mcpServers": [] }))?).block_task().await?;
                    let id = created["sessionId"].as_str().ok_or_else(|| agent_client_protocol::Error::invalid_params().data("session/new answered no sessionId"))?;
                    (SessionId::new(Arc::<str>::from(id)), created)
                }
            };
            let options = opening["configOptions"].clone();
            *shared.stage.lock().unwrap() = "options";
            config.extend(requested(spec, &options, &wants).map_err(|error| agent_client_protocol::Error::new(-32602, error))?);
            *shared.stage.lock().unwrap() = "session";
            *shared.opening.lock().unwrap() = opening;
            let mut applied: Vec<(String, String)> = Vec::new();
            for (id, value) in settings(spec, &options, &config) {
                let request = SetSessionConfigOptionRequest::new(session.clone(), SessionConfigId::new(Arc::<str>::from(id.as_str())), value.as_str());
                match connection.send_request(request).block_task().await {
                    Ok(_) => applied.push((id, value)),
                    Err(error) => notes.push(format!("{} would not take {id} = {value}: {error}", spec.name)),
                }
            }
            patch_current(&mut shared.opening.lock().unwrap()["configOptions"], &applied);
            let _ = events.send(Event::Session(session.to_string()));
            let _ = events.send(Event::Config(snapshot(&options, &applied)));
            *shared.settings.lock().unwrap() = describe(&shared.opening.lock().unwrap(), &applied);
            announce(&events, &shared);
            for line in notes {
                note(&events, &shared, line);
            }
            *shared.stage.lock().unwrap() = "";
            let waiting = ready.lock().unwrap().take();
            if let Some(waiting) = waiting {
                let _ = waiting.send(Ok(()));
            }

            let mut queue: VecDeque<(String, String)> = VecDeque::from([(first, String::new())]);
            let mut running: Option<BoxFuture<'static, Result<PromptResponse, agent_client_protocol::Error>>> = None;
            let mut interrupting = false;
            let mut closed = false;
            loop {
                if running.is_none() {
                    if let Some((text, from)) = queue.pop_front() {
                        shared.said.lock().unwrap().message.clear();
                        shared.produced.store(false, Ordering::SeqCst);
                        let _ = events.send(Event::Lines(lines::sent(&text)));
                        end_segment(&shared);
                        shared.turn.fetch_add(1, Ordering::SeqCst);
                        put(&events, &shared, None, Entry::sent(text.trim(), &from));
                        let _ = events.send(Event::Working);
                        let prompt = PromptRequest::new(session.clone(), vec![ContentBlock::Text(TextContent::new(text))]);
                        running = Some(connection.send_request(prompt).block_task().boxed());
                    }
                }
                tokio::select! {
                    result = async { running.as_mut().unwrap().await }, if running.is_some() => {
                        running = None;
                        let said = turn_ended(spec, &events, &shared, result, std::mem::take(&mut interrupting));
                        if queue.is_empty() {
                            let _ = events.send(Event::Idle(said));
                        }
                    }
                    command = commands.recv() => match command {
                        Some(Command::Send { text, interrupt, from }) if interrupt && running.is_some() => {
                            queue.push_front((text, from));
                            interrupting = true;
                            connection.send_notification(CancelNotification::new(session.clone()))?;
                        }
                        Some(Command::Send { text, from, .. }) => queue.push_back((text, from)),
                        Some(Command::Set { wants, reply }) => {
                            let _ = reply.send(set_options(spec, &connection, &session, &shared, &events, &wants).await);
                        }
                        // The agent cancels what it does and frees the session; the process is
                        // stopped once this answers.
                        Some(Command::Close { done }) => {
                            if can_close && !closed {
                                closed = true;
                                let _ = tokio::time::timeout(CLOSE_TIMEOUT, connection.send_request(CloseSessionRequest::new(session.clone())).block_task()).await;
                            }
                            let _ = done.send(());
                        }
                        None => break,
                    }
                }
            }
            Ok(())
        })
        .await
        .map_err(|error| setup_error(spec, *failing.stage.lock().unwrap(), &error.to_string()))
}

/// How long the agent has to take a setting while it runs.
const SET_TIMEOUT: Duration = Duration::from_secs(20);

/// Changes the session's model or effort as it runs: what is asked for is found among what the
/// session offers (`requested`), set one at a time, and then the card and the settings kept for a
/// resumed session catch up.
async fn set_options(spec: &'static Spec, connection: &ConnectionTo<Peer>, session: &SessionId, shared: &Shared, events: &Events, wants: &[(String, String)]) -> Result<(), String> {
    let options = shared.opening.lock().unwrap()["configOptions"].clone();
    let todo = requested(spec, &options, wants)?;
    let mut done: Vec<String> = Vec::new();
    for (id, value) in todo {
        let current = options.as_array().into_iter().flatten().find(|option| option["id"] == id.as_str()).cloned().unwrap_or_default();
        if current["currentValue"] == value.as_str() {
            continue;
        }
        let request = SetSessionConfigOptionRequest::new(session.clone(), SessionConfigId::new(Arc::<str>::from(id.as_str())), value.as_str());
        match tokio::time::timeout(SET_TIMEOUT, connection.send_request(request).block_task()).await {
            Ok(Ok(response)) => {
                let list = serde_json::to_value(&response.config_options).unwrap_or_default();
                let mut opening = shared.opening.lock().unwrap();
                if list.as_array().is_some_and(|list| !list.is_empty()) {
                    opening["configOptions"] = list;
                }
                patch_current(&mut opening["configOptions"], &[(id.clone(), value.clone())]);
                done.push(format!("{} to {}", current["name"].as_str().unwrap_or(&id).to_lowercase(), value_name(&current, &value)));
            }
            Ok(Err(error)) => return Err(format!("{} would not take {id} = {value}: {}", spec.name, first_line(&error.to_string()))),
            Err(_) => return Err(format!("{} did not answer within {} seconds", spec.name, SET_TIMEOUT.as_secs())),
        }
    }
    if !done.is_empty() {
        let opening = shared.opening.lock().unwrap().clone();
        *shared.settings.lock().unwrap() = describe(&opening, &[]);
        let _ = events.send(Event::Config(snapshot(&opening["configOptions"], &[])));
        note(events, shared, format!("● Set {}", done.join(", ")));
        announce(events, shared);
    }
    Ok(())
}

/// Writes the values just set into the options as the session now has them.
fn patch_current(options: &mut Value, applied: &[(String, String)]) {
    for option in options.as_array_mut().into_iter().flatten() {
        if let Some((_, value)) = applied.iter().find(|(id, _)| option["id"] == id.as_str()) {
            option["currentValue"] = Value::String(value.clone());
        }
    }
}

/// A prompt came back. Writes how it ended into the transcript, and answers what the bot hears:
/// the agent's last message, or why there was none.
fn turn_ended(spec: &Spec, events: &Events, shared: &Shared, result: Result<PromptResponse, agent_client_protocol::Error>, interrupted: bool) -> Option<String> {
    let mut out = flush(shared);
    let mut failure: Option<String> = None;
    match &result {
        Ok(response) => match response.stop_reason {
            StopReason::Cancelled if interrupted => out.push("  ⎿ Interrupted".into()),
            StopReason::Cancelled => failure = Some(format!("{} cancelled its turn", spec.name)),
            StopReason::Refusal => failure = Some(format!("{} refused the request", spec.name)),
            StopReason::MaxTokens => failure = Some(format!("{} ran out of tokens", spec.name)),
            StopReason::MaxTurnRequests => failure = Some(format!("{} reached its limit of steps", spec.name)),
            // A turn that ends with nothing said or done did not answer: some agents do so when
            // the model they start with is not one they can use.
            _ if !shared.produced.load(Ordering::SeqCst) => failure = Some(format!("{} ended its turn without answering", spec.name)),
            _ => {}
        },
        Err(error) => failure = Some(first_line(&error.to_string())),
    }
    let interrupted_line = out.last().is_some_and(|line| line == "  ⎿ Interrupted");
    if interrupted_line {
        out.pop();
    }
    if !out.is_empty() {
        let _ = events.send(Event::Lines(out));
    }
    if interrupted_line {
        note(events, shared, "  ⎿ Interrupted".into());
    }
    if let Some(failure) = &failure {
        note(events, shared, format!("Error: {failure}"));
    }
    failure.or_else(|| last_said(shared))
}

/// Answers a permission request: Lorca decides it as it decides a command, and only a choice
/// for this once ever allows it.
async fn decide(spec: &Spec, request: &RequestPermissionRequest, events: &Events, shared: &Shared, folder: &Path) -> RequestPermissionOutcome {
    // The history of a session being loaded again asks about what is done.
    if shared.replaying.load(Ordering::SeqCst) {
        return RequestPermissionOutcome::Cancelled;
    }
    shared.produced.store(true, Ordering::SeqCst);
    let call = serde_json::to_value(&request.tool_call).unwrap_or_default();
    let (tx, rx) = oneshot::channel();
    if events.send(Event::Approve(approval(&call, folder), tx)).is_err() {
        return RequestPermissionOutcome::Cancelled;
    }
    let verdict = rx.await.unwrap_or_else(|_| Err("Lorca stopped before deciding".into()));
    match choose(&request.options, verdict.is_ok()) {
        Some(option) => {
            if let Err(reason) = &verdict {
                note(events, shared, format!("  ⎿ Not allowed: {}", first_line(reason)));
            }
            RequestPermissionOutcome::Selected(SelectedPermissionOutcome::new(option))
        }
        None => {
            if verdict.is_ok() {
                note(events, shared, format!("  ⎿ {} offered no way to allow this once, so Lorca declined it", spec.name));
            }
            RequestPermissionOutcome::Cancelled
        }
    }
}

/// Answers a question the agent puts to the user: the form goes to the supervisor, which asks on
/// the card, and what comes back fills it in. A question nobody answers is cancelled.
async fn elicit(request: &CreateElicitationRequest, events: &Events, shared: &Shared) -> CreateElicitationResponse {
    let cancel = || CreateElicitationResponse::new(ElicitationAction::Cancel);
    // The history of a session being loaded again asks what is answered.
    if shared.replaying.load(Ordering::SeqCst) {
        return cancel();
    }
    // Only forms are declared; a link to open somewhere else is refused.
    let ElicitationMode::Form(form) = &request.mode else {
        return CreateElicitationResponse::new(ElicitationAction::Decline);
    };
    shared.produced.store(true, Ordering::SeqCst);
    let ask = form_of(&request.message, &serde_json::to_value(&form.requested_schema).unwrap_or_default());
    note(events, shared, format!("● Asks: {}", first_line(&ask.message)));
    let (tx, rx) = oneshot::channel();
    if events.send(Event::Ask(ask, tx)).is_err() {
        return cancel();
    }
    match rx.await.unwrap_or_else(|_| Err("Lorca stopped before it was answered".into())) {
        Ok(None) => {
            note(events, shared, "  ⎿ Declined".into());
            CreateElicitationResponse::new(ElicitationAction::Decline)
        }
        Ok(Some(content)) => {
            let shown: Vec<String> = content.values().map(shown_answer).filter(|answer| !answer.is_empty()).collect();
            note(events, shared, format!("  ⎿ {}", if shown.is_empty() { "Skipped".to_string() } else { shown.join("; ") }));
            let values = content.into_iter().filter_map(|(key, value)| content_value(&value).map(|value| (key, value))).collect::<std::collections::BTreeMap<_, _>>();
            CreateElicitationResponse::new(ElicitationAction::Accept(ElicitationAcceptAction::new().content(values)))
        }
        Err(reason) => {
            note(events, shared, format!("  ⎿ Not answered: {}", first_line(&reason)));
            cancel()
        }
    }
}

fn shown_answer(value: &Value) -> String {
    match value {
        Value::String(text) => text.clone(),
        Value::Array(items) => items.iter().map(shown_answer).collect::<Vec<_>>().join(", "),
        Value::Null => String::new(),
        other => other.to_string(),
    }
}

fn content_value(value: &Value) -> Option<ElicitationContentValue> {
    match value {
        Value::String(text) => Some(text.as_str().into()),
        Value::Bool(flag) => Some((*flag).into()),
        Value::Number(number) => number.as_i64().map(ElicitationContentValue::from).or_else(|| number.as_f64().map(ElicitationContentValue::from)),
        Value::Array(items) => items.iter().map(|item| item.as_str().map(str::to_string)).collect::<Option<Vec<_>>>().map(ElicitationContentValue::from),
        _ => None,
    }
}

/// The items of a menu in a field's schema: titled `oneOf`/`anyOf` entries, or a plain `enum`.
fn choices_of(schema: &Value) -> Vec<Choice> {
    for key in ["oneOf", "anyOf"] {
        if let Some(options) = schema[key].as_array() {
            return options
                .iter()
                .filter_map(|option| {
                    let value = option["const"].as_str()?;
                    let title = option["title"].as_str().filter(|title| !title.trim().is_empty()).unwrap_or(value).to_string();
                    let description = option["description"].as_str().map(str::trim).filter(|about| !about.is_empty()).map(str::to_string);
                    Some(Choice { value: value.to_string(), title, description })
                })
                .collect();
        }
    }
    schema["enum"].as_array().into_iter().flatten().filter_map(Value::as_str).map(|value| Choice { value: value.into(), title: value.into(), description: None }).collect()
}

/// A form an agent asks, as fields to fill in order. A free-text field named after a menu's, as
/// `question_0_custom` follows `question_0`, is the menu's own "other" and not a field of its own.
fn form_of(message: &str, schema: &Value) -> Ask {
    let required: Vec<&str> = schema["required"].as_array().into_iter().flatten().filter_map(Value::as_str).collect();
    let mut fields: Vec<Field> = Vec::new();
    for (key, property) in schema["properties"].as_object().into_iter().flatten() {
        let words = |name: &str| property[name].as_str().map(str::trim).filter(|text| !text.is_empty()).map(str::to_string);
        let label = match (words("title"), words("description")) {
            (Some(title), Some(about)) => format!("{title}: {about}"),
            (Some(text), None) | (None, Some(text)) => text,
            (None, None) => key.clone(),
        };
        let kind = match property["type"].as_str() {
            Some("boolean") => FieldKind::Flag,
            Some("integer") => FieldKind::Number { integer: true },
            Some("number") => FieldKind::Number { integer: false },
            Some("array") => match choices_of(&property["items"]) {
                choices if choices.is_empty() => FieldKind::Text,
                choices => FieldKind::Many(choices),
            },
            _ => match choices_of(property) {
                choices if choices.is_empty() => FieldKind::Text,
                choices => FieldKind::One(choices),
            },
        };
        fields.push(Field { key: key.clone(), label, required: required.contains(&key.as_str()), kind, other: None });
    }
    let mut index = 0;
    while index < fields.len() {
        if matches!(fields[index].kind, FieldKind::One(_) | FieldKind::Many(_)) {
            let prefix = format!("{}_", fields[index].key);
            if let Some(at) = fields.iter().position(|field| matches!(field.kind, FieldKind::Text) && !field.required && field.key.starts_with(&prefix)) {
                fields[index].other = Some(fields.remove(at).key);
                if at < index {
                    index -= 1;
                }
            }
        }
        index += 1;
    }
    Ask { message: message.to_string(), fields }
}

/// The option that answers a permission request, or none: allowing picks only a choice for this
/// once, never one that also stops the agent asking again; refusing picks a refusal for this once.
fn choose(options: &[PermissionOption], allow: bool) -> Option<PermissionOptionId> {
    options
        .iter()
        .find(|option| if allow { matches!(option.kind, PermissionOptionKind::AllowOnce) } else { matches!(option.kind, PermissionOptionKind::RejectOnce) })
        .map(|option| option.option_id.clone())
}

/// What a permission request asks, as Lorca decides it.
fn approval(tool: &Value, folder: &Path) -> Approval {
    let input = tool["rawInput"].clone();
    let title = tool["title"].as_str().unwrap_or("").to_string();
    match tool["kind"].as_str().unwrap_or("") {
        "execute" => {
            Approval::Command { command: command_of(&input).unwrap_or(title) }
        }
        "edit" | "delete" | "move" => {
            let mut paths: Vec<String> = tool["locations"].as_array().into_iter().flatten().filter_map(|location| location["path"].as_str()).map(str::to_string).collect();
            paths.extend(["path", "file_path", "filePath"].iter().filter_map(|key| input[*key].as_str()).map(str::to_string));
            // A relative path is the folder's.
            let paths = paths.into_iter().map(|path| if Path::new(&path).is_absolute() { path } else { folder.join(path).to_string_lossy().to_string() }).collect();
            Approval::Files { paths }
        }
        // The agent finished planning and asks to go ahead: the plan is what it would do.
        "switch_mode" => Approval::Tool { name: "Approve plan".into(), input },
        kind => Approval::Tool { name: if title.is_empty() { kind.to_string() } else { title }, input },
    }
}

/// What the agent says next is an entry of its own, after what came between.
fn end_segment(shared: &Shared) {
    let mut said = shared.said.lock().unwrap();
    said.segment = None;
    said.thought = None;
    said.thought_started = None;
}

/// Puts an entry under a key of its own, or under `key`.
fn put(events: &Events, shared: &Shared, key: Option<String>, entry: Entry) {
    let key = key.unwrap_or_else(|| format!("{}:{}", entry.kind, shared.keys.fetch_add(1, Ordering::SeqCst)));
    let _ = events.send(Event::Entries(vec![(key, entry)]));
}

/// A line of Lorca's own in the transcript: for the bot as it is, and for the apps as a note
/// without the marks that set it apart there.
fn note(events: &Events, shared: &Shared, line: String) {
    let _ = events.send(Event::Lines(vec![line.clone()]));
    let text = line.trim_start().trim_start_matches("⎿").trim_start_matches("●").trim().to_string();
    end_segment(shared);
    put(events, shared, None, Entry::text("note", text));
}

/// Writes out what the agent has said that is not a whole line yet.
fn flush(shared: &Shared) -> Vec<String> {
    let mut said = shared.said.lock().unwrap();
    let rest = std::mem::take(&mut said.partial);
    lines::said(&rest)
}

/// What the agent said last: its message after its last tool call, else the one before.
fn last_said(shared: &Shared) -> Option<String> {
    let said = shared.said.lock().unwrap();
    let last = [&said.message, &said.before].into_iter().map(|text| text.trim()).find(|text| !text.is_empty()).map(str::to_string);
    last
}

fn first_line(text: &str) -> String {
    text.lines().map(str::trim).find(|line| !line.is_empty()).unwrap_or(text).to_string()
}

/// The text of an ACP content block.
fn block_text(content: &Value) -> String {
    match content["type"].as_str() {
        Some("text") => content["text"].as_str().unwrap_or("").to_string(),
        Some("resource_link") => content["uri"].as_str().unwrap_or("").to_string(),
        Some("resource") => content["resource"]["text"].as_str().unwrap_or("").to_string(),
        _ => String::new(),
    }
}

/// The text a tool call reports: the content blocks it carries, and a diff as the file it changes.
fn tool_text(content: &Value, folder: &Path) -> String {
    content
        .as_array()
        .into_iter()
        .flatten()
        .map(|item| match item["type"].as_str() {
            Some("content") => unfence(&block_text(&item["content"])),
            Some("diff") => item["path"].as_str().map(|path| format!("changed {}", lines::path(path, folder))).unwrap_or_default(),
            _ => String::new(),
        })
        .filter(|text| !text.is_empty())
        .collect::<Vec<_>>()
        .join("\n")
}

/// A plan as the transcript shows it: its steps, each done or not.
fn plan_lines(entries: &Value) -> Vec<String> {
    let steps: Vec<String> = entries
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|entry| {
            let content = entry["content"].as_str()?.trim();
            let mark = if entry["status"] == "completed" { "☑" } else { "☐" };
            Some(format!("  {mark} {content}"))
        })
        .collect();
    if steps.is_empty() {
        return steps;
    }
    std::iter::once("● Plan".to_string()).chain(steps).collect()
}

/// The command a tool call runs: a string, or the words of one.
fn command_of(input: &Value) -> Option<String> {
    input["command"]
        .as_str()
        .map(str::to_string)
        .or_else(|| input["command"].as_array().map(|parts| parts.iter().filter_map(Value::as_str).collect::<Vec<_>>().join(" ")))
        .or_else(|| input["cmd"].as_str().map(str::to_string))
}

/// What a tool call acts on, from its input: the command, the path, the pattern, or the address.
fn call_detail(input: &Value, locations: &Value, folder: &Path) -> String {
    let text = |key: &str| input[key].as_str().map(str::to_string);
    command_of(input)
        .or_else(|| ["file_path", "path", "filePath", "notebook_path"].iter().find_map(|key| text(key)).map(|path| lines::path(&path, folder)))
        .or_else(|| ["pattern", "query", "url"].iter().find_map(|key| text(key)))
        .or_else(|| locations.as_array().and_then(|found| found.first()).and_then(|location| location["path"].as_str()).map(|path| lines::path(path, folder)))
        .unwrap_or_default()
}

/// A tool's output without the Markdown fence some agents wrap a terminal's in.
fn unfence(text: &str) -> String {
    let text = text.trim();
    let Some(rest) = text.strip_prefix("```") else { return text.to_string() };
    let body = rest.split_once('\n').map(|(_, body)| body).unwrap_or("").trim_end();
    body.strip_suffix("```").unwrap_or(body).trim_end().to_string()
}

/// One `session/update` into the transcript.
fn update(events: &Events, shared: &Shared, update: &Value, folder: &Path) {
    if shared.replaying.load(Ordering::SeqCst) {
        return;
    }
    match update["sessionUpdate"].as_str().unwrap_or("") {
        "agent_message_chunk" => {
            let text = block_text(&update["content"]);
            if text.is_empty() {
                return;
            }
            shared.produced.store(true, Ordering::SeqCst);
            let mut said = shared.said.lock().unwrap();
            said.thought = None;
            said.thought_started = None;
            // A message the agent names apart from the last is an entry of its own, and the one
            // it says last.
            let id = update["messageId"].as_str().map(str::to_string);
            if id.is_some() && said.message_id != id {
                said.segment = None;
                if !said.message.trim().is_empty() {
                    said.before = std::mem::take(&mut said.message);
                }
            }
            said.message_id = id;
            said.message.push_str(&text);
            said.partial.push_str(&text);
            let key = match &mut said.segment {
                Some((key, segment)) => {
                    segment.push_str(&text);
                    key.clone()
                }
                None => {
                    let key = format!("said:{}", shared.keys.fetch_add(1, Ordering::SeqCst));
                    said.segment = Some((key.clone(), text.clone()));
                    key
                }
            };
            let segment = said.segment.as_ref().map(|(_, segment)| segment.trim().to_string()).unwrap_or_default();
            let _ = events.send(Event::Entries(vec![(key, Entry::text("said", segment))]));
            // Whole lines go out now; the rest waits for its end.
            if let Some(end) = said.partial.rfind('\n') {
                let done: String = said.partial.drain(..=end).collect();
                drop(said);
                let out = lines::said(&done);
                if !out.is_empty() {
                    let _ = events.send(Event::Lines(out));
                }
            }
        }
        // What it thinks, for the apps only, beside what it says.
        "agent_thought_chunk" => {
            let text = block_text(&update["content"]);
            if text.is_empty() {
                return;
            }
            let mut said = shared.said.lock().unwrap();
            said.segment = None;
            let id = update["messageId"].as_str().map(str::to_string);
            if id.is_some() && said.thought_id != id {
                said.thought = None;
                said.thought_started = None;
            }
            said.thought_id = id;
            let key = match &mut said.thought {
                Some((key, thought)) => {
                    thought.push_str(&text);
                    key.clone()
                }
                None => {
                    let key = format!("thought:{}", shared.keys.fetch_add(1, Ordering::SeqCst));
                    said.thought = Some((key.clone(), text.clone()));
                    said.thought_started = Some(Instant::now());
                    key
                }
            };
            let thought = said.thought.as_ref().map(|(_, thought)| thought.trim().to_string()).unwrap_or_default();
            let seconds = said.thought_started.map(|started| started.elapsed().as_secs_f64());
            drop(said);
            let _ = events.send(Event::Entries(vec![(key, Entry { kind: "thought".into(), text: thought, seconds, ..Entry::default() })]));
        }
        "tool_call" | "tool_call_update" => {
            let id = update["toolCallId"].as_str().unwrap_or("").to_string();
            let failed = update["status"] == "failed";
            let finished = failed || update["status"] == "completed";
            shared.produced.store(true, Ordering::SeqCst);
            // The call's line, once it can say what it acts on, or when it ends.
            let line = {
                let mut calls = shared.calls.lock().unwrap();
                let call = calls.entry(id.clone()).or_insert_with(|| Call {
                    title: String::new(),
                    name: String::new(),
                    input: Value::Null,
                    shown: false,
                    kind: String::new(),
                    status: "pending".into(),
                    output: String::new(),
                    exit_code: None,
                    diffs: Vec::new(),
                    result: String::new(),
                });
                if let Some(kind) = update["kind"].as_str() {
                    call.kind = kind.to_string();
                }
                if let Some(status) = update["status"].as_str() {
                    call.status = status.to_string();
                }
                // What its terminal wrote, in chunks, and how it ended (`_meta`, as Zed takes it).
                let meta = &update["_meta"];
                for key in ["terminal_output_delta", "terminal_output"] {
                    if let Some(data) = meta[key]["data"].as_str() {
                        call.output.push_str(data);
                        if call.output.len() > 2 * entries::OUTPUT_BYTES {
                            call.output = entries::tail(&call.output, entries::OUTPUT_BYTES).to_string();
                        }
                    }
                }
                if let Some(code) = meta["terminal_exit"]["exit_code"].as_i64() {
                    call.exit_code = Some(code);
                }
                if let Some(content) = update["content"].as_array() {
                    let diffs: Vec<FileDiff> = content
                        .iter()
                        .filter(|item| item["type"] == "diff")
                        .filter_map(|item| Some(entries::diff(lines::path(item["path"].as_str()?, folder), item["oldText"].as_str().unwrap_or(""), item["newText"].as_str().unwrap_or(""))))
                        .collect();
                    if !diffs.is_empty() {
                        call.diffs = diffs;
                    }
                    let result = content.iter().filter(|item| item["type"] == "content").map(|item| unfence(&block_text(&item["content"]))).filter(|text| !text.is_empty()).collect::<Vec<_>>().join("\n");
                    if !result.is_empty() {
                        call.result = result;
                    }
                }
                if let Some(title) = update["title"].as_str().filter(|title| !title.trim().is_empty()) {
                    call.title = title.to_string();
                }
                if call.title.is_empty() {
                    call.title = update["kind"].as_str().unwrap_or("Tool").to_string();
                }
                if let Some(name) = update["name"].as_str().map(str::trim).filter(|name| !name.is_empty()) {
                    call.name = name.to_string();
                }
                if update["rawInput"].as_object().is_some_and(|input| !input.is_empty()) {
                    call.input = update["rawInput"].clone();
                }
                let detail = call_detail(&call.input, &update["locations"], folder);
                let shown_detail = if detail.is_empty() || call.title.contains(&detail) { String::new() } else { detail.clone() };
                let entry = Entry {
                    kind: "tool".into(),
                    tool: Some(entries::Tool {
                        title: call.title.clone(),
                        name: call.name.clone(),
                        detail: shown_detail.clone(),
                        kind: call.kind.clone(),
                        status: call.status.clone(),
                        output: entries::tail(&entries::terminal_text(&call.output), entries::OUTPUT_BYTES).to_string(),
                        exit_code: call.exit_code,
                        result: call.result.clone(),
                        diffs: call.diffs.clone(),
                    }),
                    ..Entry::default()
                };
                let output = (!call.output.is_empty()).then(|| entries::plain(&entries::terminal_text(&call.output)));
                let line = if call.shown || !(finished || !detail.is_empty()) {
                    None
                } else {
                    call.shown = true;
                    // `● Bash(git status)` where the agent names its tool, else its title.
                    Some(if call.name.is_empty() { lines::call(&call.title, &shown_detail) } else { lines::call(&call.name, &detail) })
                };
                (line, entry, output)
            };
            let (line, entry, output) = line;
            end_segment(shared);
            let _ = events.send(Event::Entries(vec![(format!("tool:{id}"), entry)]));
            let mut out = Vec::new();
            if let Some(line) = line {
                out = flush(shared);
                {
                    let mut said = shared.said.lock().unwrap();
                    if !said.message.trim().is_empty() {
                        said.before = std::mem::take(&mut said.message);
                    }
                }
                out.push(line);
            }
            if finished {
                let mut text = tool_text(&update["content"], folder);
                // An agent that writes a command's output to its terminal reports none beside it.
                if let Some(output) = output.filter(|output| !output.trim().is_empty()) {
                    text = if text.is_empty() { output } else { format!("{output}\n{text}") };
                }
                if !text.is_empty() || failed {
                    out.extend(lines::result(&text, failed));
                }
            }
            if !out.is_empty() {
                let _ = events.send(Event::Lines(out));
            }
        }
        "plan" => {
            let out = plan_lines(&update["entries"]);
            let mut last = shared.plan.lock().unwrap();
            let text = out.join("\n");
            if !out.is_empty() && *last != text {
                *last = text;
                drop(last);
                let _ = events.send(Event::Lines(out));
                let steps = update["entries"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(|step| Some(Step { text: step["content"].as_str()?.trim().to_string(), done: step["status"] == "completed" }))
                    .collect();
                end_segment(shared);
                let key = format!("plan:{}", shared.turn.load(Ordering::SeqCst));
                let _ = events.send(Event::Entries(vec![(key, Entry { kind: "plan".into(), steps, ..Entry::default() })]));
            }
        }
        "config_option_update" => {
            let mut opening = shared.opening.lock().unwrap();
            opening["configOptions"] = update["configOptions"].clone();
            settings_changed(events, shared, opening);
        }
        // Its mode changed on its own, as when a plan is approved: the older word for it.
        "current_mode_update" => {
            let Some(mode) = update["currentModeId"].as_str() else { return };
            let mut opening = shared.opening.lock().unwrap();
            let mut patched = false;
            for option in opening["configOptions"].as_array_mut().into_iter().flatten() {
                if option["category"] == "mode" {
                    option["currentValue"] = Value::String(mode.to_string());
                    patched = true;
                }
            }
            if !patched && opening["modes"].is_object() {
                opening["modes"]["currentModeId"] = Value::String(mode.to_string());
            }
            settings_changed(events, shared, opening);
        }
        // Something the agent wants the user to know, apart from what it says.
        "notice" => {
            let Some(title) = update["title"].as_str().map(str::trim).filter(|title| !title.is_empty()) else { return };
            let text = match update["description"].as_str().map(str::trim).filter(|about| !about.is_empty()) {
                Some(about) => format!("{title}: {about}"),
                None => title.to_string(),
            };
            let level = update["severity"].as_str().unwrap_or("info").to_string();
            let label = match level.as_str() {
                "warning" => "Warning: ",
                "error" => "Error: ",
                _ => "",
            };
            let _ = events.send(Event::Lines(vec![format!("  ⎿ {label}{text}")]));
            end_segment(shared);
            put(events, shared, None, Entry { level, ..Entry::text("notice", text) });
        }
        "session_info_update" => {
            if let Some(title) = update.get("title").filter(|title| title.is_string() || title.is_null()) {
                let _ = events.send(Event::Title(title.as_str().map(str::trim).unwrap_or("").to_string()));
            }
        }
        // The agent made room in its context: one entry for each time, which its updates change.
        "compaction_update" | "compaction_summary_chunk" => {
            let Some(id) = update["compactionId"].as_str() else { return };
            let mut all = shared.compactions.lock().unwrap();
            let first = !all.contains_key(id);
            let (status, summary) = all.entry(id.to_string()).or_insert_with(|| ("in_progress".into(), String::new()));
            let was = status.clone();
            if update["sessionUpdate"] == "compaction_summary_chunk" {
                summary.push_str(&block_text(&update["content"]));
            } else {
                if let Some(now) = update["status"].as_str() {
                    *status = now.to_string();
                }
                match update.get("summary") {
                    Some(Value::Array(blocks)) => *summary = blocks.iter().map(block_text).collect::<Vec<_>>().join(""),
                    Some(Value::Null) => summary.clear(),
                    _ => {}
                }
            }
            let (status, summary) = (status.clone(), summary.trim().to_string());
            drop(all);
            let mut out = Vec::new();
            if first {
                out.push("● Compacting the context".to_string());
            }
            if status != was || first {
                match status.as_str() {
                    "completed" => out.push("  ⎿ Compacted".into()),
                    "failed" => out.push(format!("  ⎿ Error: {}", update["error"].as_str().map(first_line).unwrap_or_else(|| "compaction failed".into()))),
                    "cancelled" => out.push("  ⎿ Cancelled".into()),
                    _ => {}
                }
            }
            if !out.is_empty() {
                let _ = events.send(Event::Lines(out));
            }
            end_segment(shared);
            let _ = events.send(Event::Entries(vec![(format!("compaction:{id}"), Entry { status, ..Entry::text("compaction", summary) })]));
        }
        "usage_update" => {
            let usage = usage_of(update);
            if !usage.is_empty() {
                *shared.usage.lock().unwrap() = usage;
                announce(events, shared);
            }
        }
        "available_commands_update" => {
            let commands = update["availableCommands"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|command| Some((command["name"].as_str()?.to_string(), command["description"].as_str().unwrap_or("").to_string())))
                .collect();
            *shared.commands.lock().unwrap() = commands;
        }
        _ => {}
    }
}

/// The values a select setting accepts, its groups flattened.
fn option_values(option: &Value) -> Vec<String> {
    fn collect(items: &Value, out: &mut Vec<String>) {
        for item in items.as_array().into_iter().flatten() {
            match item["value"].as_str() {
                Some(value) => out.push(value.to_string()),
                None => collect(&item["options"], out),
            }
        }
    }
    let mut out = Vec::new();
    collect(&option["options"], &mut out);
    out
}

/// The values a select setting accepts with the names it gives them, its groups flattened.
fn option_pairs(option: &Value) -> Vec<(String, String)> {
    fn collect(items: &Value, out: &mut Vec<(String, String)>) {
        for item in items.as_array().into_iter().flatten() {
            match item["value"].as_str() {
                Some(value) => out.push((value.to_string(), item["name"].as_str().unwrap_or(value).to_string())),
                None => collect(&item["options"], out),
            }
        }
    }
    let mut out = Vec::new();
    collect(&option["options"], &mut out);
    out
}

/// What the bot asked for at the start as settings of the session. `plan` is the setting that has
/// a plan mode, whatever it is called; `model` and `effort` are the settings of those categories.
/// A value is found by its id or its name, however the agent cases it, or by being the only one
/// that holds the words. What the agent cannot take is an error that says what it offers, so the
/// bot asks again with one of those or without.
fn requested(spec: &Spec, options: &Value, wants: &[(String, String)]) -> Result<Vec<(String, String)>, String> {
    let offered: Vec<&Value> = options.as_array().into_iter().flatten().collect();
    let mut out = Vec::new();
    for (what, wanted) in wants {
        let option = match what.as_str() {
            "plan" => offered.iter().copied().find(|option| {
                (option["category"] == "mode" || option["id"].as_str().is_some_and(|id| id.ends_with("mode")))
                    && option_pairs(option).iter().any(|(value, name)| value.eq_ignore_ascii_case("plan") || name.eq_ignore_ascii_case("plan"))
            }),
            "model" => offered.iter().copied().find(|option| option["category"] == "model"),
            _ => offered.iter().copied().find(|option| option["category"] == "thought_level"),
        };
        let Some(option) = option else {
            return Err(match what.as_str() {
                "plan" => format!("{} has no plan mode. Start it without `plan`.", spec.name),
                what => format!("{} has no {what} setting to change. Start it without `{what}`.", spec.name),
            });
        };
        let pairs = option_pairs(option);
        let lower = wanted.trim().to_lowercase();
        let is = |text: &str| text.to_lowercase() == lower;
        let holds = |text: &str| text.to_lowercase().contains(&lower);
        let found = pairs.iter().find(|(value, name)| is(value) || is(name)).or_else(|| {
            let mut hits = pairs.iter().filter(|(value, name)| holds(value) || holds(name));
            hits.next().filter(|_| hits.next().is_none())
        });
        let Some((value, _)) = found.filter(|_| !lower.is_empty()) else {
            let offers = pairs.iter().map(|(value, name)| if value == name { value.clone() } else { format!("{name} ({value})") }).collect::<Vec<_>>().join(", ");
            return Err(format!("{} has no {what} “{}”. It offers: {offers}.", spec.name, first_line(wanted)));
        };
        let id = option["id"].as_str().unwrap_or_default().to_string();
        out.retain(|(known, _): &(String, String)| *known != id);
        out.push((id, value.clone()));
    }
    Ok(out)
}

/// The settings to set on a session as it opens: the mode under which the agent asks before it
/// acts, then the ones an earlier run of it ended with, which a resumed session may have lost.
/// Only a setting the session offers, with a value it accepts and does not have already.
fn settings(spec: &Spec, options: &Value, recorded: &[(String, String)]) -> Vec<(String, String)> {
    let offered = || options.as_array().into_iter().flatten();
    let mut wanted: Vec<(String, String)> = Vec::new();
    if let Some(safe) = spec.safe_mode {
        if let Some(id) = offered().find(|option| option["category"] == "mode").and_then(|option| option["id"].as_str()) {
            wanted.push((id.to_string(), safe.to_string()));
        }
    }
    for (id, value) in recorded {
        match wanted.iter_mut().find(|(known, _)| known == id) {
            Some(entry) => entry.1 = value.clone(),
            None => wanted.push((id.clone(), value.clone())),
        }
    }
    wanted.retain(|(id, value)| {
        offered().find(|option| option["id"] == id.as_str()).is_some_and(|option| option["currentValue"] != value.as_str() && option_values(option).iter().any(|known| known == value))
    });
    wanted
}

/// The settings worth keeping to set again: the mode, the model, and how hard it thinks, as the
/// session has them now.
fn snapshot(options: &Value, applied: &[(String, String)]) -> Vec<(String, String)> {
    options
        .as_array()
        .into_iter()
        .flatten()
        .filter(|option| matches!(option["category"].as_str(), Some("mode" | "model" | "thought_level")))
        .filter_map(|option| {
            let id = option["id"].as_str()?;
            let value = applied.iter().find(|(known, _)| known == id).map(|(_, value)| value.clone()).or_else(|| option["currentValue"].as_str().map(str::to_string))?;
            Some((id.to_string(), value))
        })
        .collect()
}

/// The name an option gives one of its values.
fn value_name(option: &Value, value: &str) -> String {
    fn find(items: &Value, value: &str) -> Option<String> {
        items.as_array().into_iter().flatten().find_map(|item| match item["value"].as_str() {
            Some(known) if known == value => item["name"].as_str().map(str::to_string),
            Some(_) => None,
            None => find(&item["options"], value),
        })
    }
    find(&option["options"], value).unwrap_or_else(|| value.to_string())
}

/// What the card shows of a session's settings: its model, how hard it thinks, and its mode, each
/// as the agent titles it and names its value now. `session` is what the agent answered as it
/// opened the session: `configOptions`, or for what they leave out the older `models` and `modes`.
fn describe(session: &Value, applied: &[(String, String)]) -> Vec<(String, String)> {
    let rank = |category: &str| match category {
        "model" => 0,
        "thought_level" => 1,
        _ => 2,
    };
    let mut found: Vec<(usize, (String, String))> = session["configOptions"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|option| matches!(option["category"].as_str(), Some("mode" | "model" | "thought_level")))
        .filter_map(|option| {
            let id = option["id"].as_str()?;
            let current = applied.iter().find(|(known, _)| known == id).map(|(_, value)| value.as_str()).or_else(|| option["currentValue"].as_str())?;
            Some((rank(option["category"].as_str()?), (option["name"].as_str()?.to_string(), value_name(option, current))))
        })
        .collect();
    let has = |category: &str| found.iter().any(|(known, _)| *known == rank(category));
    let mut older: Vec<(usize, (String, String))> = Vec::new();
    if !has("model") {
        if let Some(name) = older_name(&session["models"], "currentModelId", "availableModels", "modelId") {
            older.push((rank("model"), ("Model".to_string(), name)));
        }
    }
    if !has("mode") {
        if let Some(name) = older_name(&session["modes"], "currentModeId", "availableModes", "id") {
            older.push((rank("mode"), ("Mode".to_string(), name)));
        }
    }
    found.extend(older);
    found.sort_by_key(|(rank, _)| *rank);
    found.into_iter().map(|(_, entry)| entry).collect()
}

/// The name of the mode or model the older words say is current: its `name` among the available,
/// else its id.
fn older_name(state: &Value, current: &str, available: &str, id: &str) -> Option<String> {
    let current = state[current].as_str()?;
    let named = state[available].as_array().into_iter().flatten().find(|item| item[id] == current).and_then(|item| item["name"].as_str());
    Some(named.unwrap_or(current).to_string())
}

/// 34123 as "34k", 1000000 as "1M".
fn compact(count: u64) -> String {
    if count >= 1_000_000 {
        format!("{:.1}M", count as f64 / 1_000_000.0).replace(".0M", "M")
    } else if count >= 1_000 {
        format!("{}k", (count + 500) / 1_000)
    } else {
        count.to_string()
    }
}

/// What a `usage_update` says of the session's use: its context, and what it cost.
fn usage_of(update: &Value) -> Vec<(String, String)> {
    let mut out = Vec::new();
    if let (Some(used), Some(size)) = (update["used"].as_u64(), update["size"].as_u64()) {
        out.push(("Context".to_string(), format!("{} / {}", compact(used), compact(size))));
    }
    if let (Some(amount), Some(currency)) = (update["cost"]["amount"].as_f64(), update["cost"]["currency"].as_str()) {
        let unit = if currency == "USD" { "$".to_string() } else { format!("{currency} ") };
        out.push(("Cost".to_string(), format!("{unit}{amount:.2}")));
    }
    out
}

/// The session's settings changed: the card shows them, and a resumed session gets them back.
fn settings_changed(events: &Events, shared: &Shared, opening: std::sync::MutexGuard<'_, Value>) {
    *shared.settings.lock().unwrap() = describe(&opening, &[]);
    let kept = snapshot(&opening["configOptions"], &[]);
    drop(opening);
    if !kept.is_empty() {
        let _ = events.send(Event::Config(kept));
    }
    announce(events, shared);
}

/// Tells the card what the session's settings and use are now.
fn announce(events: &Events, shared: &Shared) {
    let mut all = shared.settings.lock().unwrap().clone();
    all.extend(shared.usage.lock().unwrap().iter().cloned());
    let _ = events.send(Event::Settings(all));
}

/// Why the agent could not be made ready, by the step that failed.
fn setup_error(spec: &Spec, stage: &str, error: &str) -> String {
    let error = first_line(error);
    match stage {
        // What the bot asked for at the start, which the agent cannot take.
        "options" => error,
        "initialize" => format!("{} did not answer ACP: {error}. Its version may be too old to speak it; update it and start again.", spec.name),
        "session" if needs_sign_in(&error) => format!("{} needs you to sign in. Run `{}` once in a terminal, sign in there, and start it again.", spec.name, spec.bins[0]),
        "session" => format!("{} could not open a session: {error}", spec.name),
        _ => error,
    }
}

fn needs_sign_in(error: &str) -> bool {
    let error = error.to_lowercase();
    ["authentication required", "auth required", "sign in", "log in", "login", "unauthenticated", "not authenticated"].iter().any(|words| error.contains(words))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::collections::HashSet;

    #[test]
    fn a_permission_request_becomes_what_lorca_decides() {
        // Absolute on every platform: `/work/shop` is not on Windows, which needs a drive.
        let root = std::env::temp_dir();
        let folder = &root.join("shop");
        let hosts = root.join("hosts").to_string_lossy().to_string();
        let run = json!({ "kind": "execute", "title": "Run `git push`", "rawInput": { "command": "git push origin main" } });
        assert!(matches!(approval(&run, folder), Approval::Command { command } if command == "git push origin main"));
        let parts = json!({ "kind": "execute", "title": "Run", "rawInput": { "command": ["npm", "publish"] } });
        assert!(matches!(approval(&parts, folder), Approval::Command { command } if command == "npm publish"));
        let edit = json!({ "kind": "edit", "title": "Edit", "locations": [{ "path": hosts }], "rawInput": { "path": "notes.md" } });
        let notes = folder.join("notes.md").to_string_lossy().to_string();
        assert!(matches!(approval(&edit, folder), Approval::Files { paths } if paths == [hosts.clone(), notes]));
        let fetch = json!({ "kind": "fetch", "title": "Fetch https://example.com", "rawInput": { "url": "https://example.com" } });
        assert!(matches!(approval(&fetch, folder), Approval::Tool { name, .. } if name == "Fetch https://example.com"));
        let plan = json!({ "kind": "switch_mode", "title": "Approve Plan", "rawInput": { "plan": "1. Fix it" } });
        assert!(matches!(approval(&plan, folder), Approval::Tool { name, input } if name == "Approve plan" && input["plan"] == "1. Fix it"));
    }

    #[test]
    fn a_form_becomes_fields_to_fill_in_and_a_menus_free_text_is_its_other() {
        let schema = json!({
            "type": "object",
            "required": ["name"],
            "properties": {
                "question_0": { "type": "string", "title": "Database", "oneOf": [{ "const": "pg", "title": "Postgres", "description": "Real server" }, { "const": "lite", "title": "SQLite" }] },
                "question_0_custom": { "type": "string", "title": "Other" },
                "question_1": { "type": "array", "description": "Which targets?", "items": { "anyOf": [{ "const": "a", "title": "A" }, { "const": "b" }] } },
                "name": { "type": "string", "title": "Name" },
                "count": { "type": "integer" },
                "sure": { "type": "boolean", "description": "Sure?" },
                "mode": { "type": "string", "enum": ["fast", "slow"] }
            }
        });
        let ask = form_of("Set it up", &schema);
        assert_eq!(ask.message, "Set it up");
        let keys: Vec<&str> = ask.fields.iter().map(|field| field.key.as_str()).collect();
        assert_eq!(keys, ["question_0", "question_1", "name", "count", "sure", "mode"]);
        let field = |key: &str| ask.fields.iter().find(|field| field.key == key).unwrap();
        assert_eq!(field("question_0").other.as_deref(), Some("question_0_custom"));
        assert_eq!(field("question_0").label, "Database");
        let choice = |value: &str, title: &str, description: Option<&str>| Choice { value: value.into(), title: title.into(), description: description.map(str::to_string) };
        assert_eq!(field("question_0").kind, FieldKind::One(vec![choice("pg", "Postgres", Some("Real server")), choice("lite", "SQLite", None)]));
        assert_eq!(field("question_1").kind, FieldKind::Many(vec![choice("a", "A", None), choice("b", "b", None)]));
        assert_eq!(field("mode").kind, FieldKind::One(vec![choice("fast", "fast", None), choice("slow", "slow", None)]));
        assert_eq!((field("name").kind.clone(), field("name").required), (FieldKind::Text, true));
        assert_eq!(field("count").kind, FieldKind::Number { integer: true });
        assert_eq!((field("sure").kind.clone(), field("sure").label.as_str()), (FieldKind::Flag, "Sure?"));
        // A free-text field that is required, or named after no menu, stays a field of its own.
        let plain = form_of("Say", &json!({ "properties": { "a": { "type": "string", "enum": ["x"] }, "a_note": { "type": "string" }, "b_note": { "type": "string" } } }));
        assert_eq!(plain.fields.iter().map(|field| field.key.as_str()).collect::<Vec<_>>(), ["a", "b_note"]);
        assert_eq!(plain.fields[0].other.as_deref(), Some("a_note"));
    }

    #[test]
    fn what_the_bot_asks_for_is_found_by_name_and_refused_with_what_is_offered() {
        let spec = spec("claude").unwrap();
        let options = json!([
            { "id": "mode", "name": "Mode", "category": "mode", "currentValue": "default", "options": [{ "value": "default", "name": "Default" }, { "value": "plan", "name": "Plan" }, { "value": "bypassPermissions", "name": "Bypass" }] },
            { "id": "model", "name": "Model", "category": "model", "currentValue": "default", "options": [{ "value": "default", "name": "Default (recommended)" }, { "group": "g", "options": [{ "value": "sonnet", "name": "Sonnet" }, { "value": "opus", "name": "Opus" }] }] },
            { "id": "effort", "name": "Effort", "category": "thought_level", "currentValue": "medium", "options": [{ "value": "medium", "name": "Medium" }, { "value": "high", "name": "High" }] }
        ]);
        let want = |pairs: &[(&str, &str)]| pairs.iter().map(|(what, value)| (what.to_string(), value.to_string())).collect::<Vec<_>>();
        let got = requested(spec, &options, &want(&[("plan", "plan"), ("model", "Opus"), ("effort", "HIGH")])).unwrap();
        assert_eq!(got, [("mode".to_string(), "plan".to_string()), ("model".to_string(), "opus".to_string()), ("effort".to_string(), "high".to_string())]);
        assert_eq!(requested(spec, &options, &want(&[("model", "son")])).unwrap()[0].1, "sonnet");
        // "default" holds two of the model's words: it is its id, so it is found exactly.
        assert_eq!(requested(spec, &options, &want(&[("model", "default")])).unwrap()[0].1, "default");
        let error = requested(spec, &options, &want(&[("model", "gpt")])).unwrap_err();
        assert!(error.contains("has no model “gpt”") && error.contains("Sonnet (sonnet), Opus (opus)"), "{error}");
        assert!(requested(spec, &options, &want(&[("model", "")])).is_err());
        let none = json!([{ "id": "model", "category": "model", "options": [{ "value": "a", "name": "A" }] }]);
        assert_eq!(requested(spec, &none, &want(&[("plan", "plan")])).unwrap_err(), "Claude Code has no plan mode. Start it without `plan`.");
        assert_eq!(requested(spec, &none, &want(&[("effort", "high")])).unwrap_err(), "Claude Code has no effort setting to change. Start it without `effort`.");
        // The mode that asks first still goes first; what was asked for goes after and wins.
        let set = settings(spec, &options, &requested(spec, &options, &want(&[("plan", "plan")])).unwrap());
        assert_eq!(set, [("mode".to_string(), "plan".to_string())]);
    }

    #[test]
    fn answers_become_the_values_the_agent_reads() {
        assert!(matches!(content_value(&json!("x")), Some(ElicitationContentValue::String(text)) if text == "x"));
        assert!(matches!(content_value(&json!(3)), Some(ElicitationContentValue::Integer(3))));
        assert!(matches!(content_value(&json!(true)), Some(ElicitationContentValue::Boolean(true))));
        assert!(matches!(content_value(&json!(["a", "b"])), Some(ElicitationContentValue::StringArray(items)) if items == ["a", "b"]));
        assert!(content_value(&json!(null)).is_none());
        assert_eq!(shown_answer(&json!(["a", "b"])), "a, b");
    }

    #[test]
    fn only_a_choice_for_this_once_allows_it() {
        let options: Vec<PermissionOption> = serde_json::from_value(json!([
            { "optionId": "always", "name": "Always", "kind": "allow_always" },
            { "optionId": "once", "name": "Once", "kind": "allow_once" },
            { "optionId": "no", "name": "No", "kind": "reject_once" },
        ]))
        .unwrap();
        assert_eq!(choose(&options, true).map(|id| id.0.to_string()).as_deref(), Some("once"));
        assert_eq!(choose(&options, false).map(|id| id.0.to_string()).as_deref(), Some("no"));
        let only_always: Vec<PermissionOption> = serde_json::from_value(json!([{ "optionId": "always", "name": "Always", "kind": "allow_always" }])).unwrap();
        assert!(choose(&only_always, true).is_none(), "never a choice that stops it asking again");
    }

    #[test]
    fn an_update_writes_whole_lines_and_each_tool_once() {
        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
        let shared = Shared::default();
        let folder = Path::new("/work/shop");
        update(&tx, &shared, &json!({ "sessionUpdate": "agent_message_chunk", "content": { "type": "text", "text": "Looking at the" } }), folder);
        assert!(drain(&mut rx).0.is_empty(), "a line that is not finished waits");
        update(&tx, &shared, &json!({ "sessionUpdate": "agent_message_chunk", "content": { "type": "text", "text": " login.\nThen the tests" } }), folder);
        let (first, said) = drain(&mut rx);
        assert_eq!(first, [vec!["Looking at the login."]]);
        // The apps get what it says as it comes, in one entry.
        assert_eq!(said.last().map(|(key, entry)| (key.as_str(), entry.text.as_str())), Some(("said:0", "Looking at the login.\nThen the tests")));
        let call = json!({ "sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Run `cargo test`", "kind": "execute", "rawInput": { "command": "cargo test" } });
        update(&tx, &shared, &call, folder);
        update(&tx, &shared, &call, folder);
        assert_eq!(drain(&mut rx).0, [vec!["Then the tests", "● Run `cargo test`"]], "a tool call shows once");
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed", "content": [{ "type": "content", "content": { "type": "text", "text": "ok. 3 passed" } }] }), folder);
        let (result, tool) = drain(&mut rx);
        assert_eq!(result, [vec!["  ⎿ ok. 3 passed"]]);
        let tool = tool.last().unwrap().1.tool.clone().unwrap();
        assert_eq!((tool.title.as_str(), tool.status.as_str(), tool.result.as_str()), ("Run `cargo test`", "completed", "ok. 3 passed"));
        assert_eq!(last_said(&shared).as_deref(), Some("Looking at the login.\nThen the tests"));

        // A call that begins with a bare title is shown once its input arrives, and its terminal
        // output loses the fence it comes in.
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call", "toolCallId": "t2", "title": "Terminal", "kind": "execute", "rawInput": {} }), folder);
        assert!(drain(&mut rx).0.is_empty(), "a call that cannot say what it runs waits");
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call_update", "toolCallId": "t2", "rawInput": { "command": "git status --short" } }), folder);
        assert_eq!(drain(&mut rx).0, [vec!["● Terminal(git status --short)"]]);
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call_update", "toolCallId": "t2", "status": "completed", "content": [{ "type": "content", "content": { "type": "text", "text": "```console\n?? hi.txt\n```" } }] }), folder);
        assert_eq!(drain(&mut rx).0, [vec!["  ⎿ ?? hi.txt"]]);
        // One that ends without ever saying what it acts on is shown then.
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call", "toolCallId": "t3", "title": "Read File", "kind": "read" }), folder);
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call_update", "toolCallId": "t3", "status": "completed" }), folder);
        assert_eq!(drain(&mut rx).0, [vec!["● Read File"]]);

        let plan = json!({ "sessionUpdate": "plan", "entries": [{ "content": "Fix it", "status": "completed", "priority": "high" }, { "content": "Test it", "status": "pending", "priority": "high" }] });
        update(&tx, &shared, &plan, folder);
        let (steps, entries) = drain(&mut rx);
        assert_eq!(steps, [vec!["● Plan", "  ☑ Fix it", "  ☐ Test it"]]);
        assert_eq!(entries[0].1.steps, [Step { text: "Fix it".into(), done: true }, Step { text: "Test it".into(), done: false }]);
        update(&tx, &shared, &plan, folder);
        assert!(drain(&mut rx).0.is_empty(), "a plan that did not change shows once");

        update(&tx, &shared, &json!({ "sessionUpdate": "available_commands_update", "availableCommands": [{ "name": "review", "description": "Review the diff" }] }), folder);
        assert_eq!(*shared.commands.lock().unwrap(), [("review".to_string(), "Review the diff".to_string())]);
        update(&tx, &shared, &json!({ "sessionUpdate": "a_kind_nobody_knows" }), folder);
        assert!(rx.try_recv().is_err());

        shared.replaying.store(true, Ordering::SeqCst);
        update(&tx, &shared, &json!({ "sessionUpdate": "agent_message_chunk", "content": { "type": "text", "text": "old history\n" } }), folder);
        assert!(rx.try_recv().is_err(), "a replayed session shows nothing");
    }

    #[test]
    fn messages_thoughts_notices_modes_titles_and_compactions_reach_the_transcript() {
        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
        let shared = Shared::default();
        let folder = Path::new("/work/shop");
        let chunk = |kind: &str, id: &str, text: &str| json!({ "sessionUpdate": kind, "messageId": id, "content": { "type": "text", "text": text } });
        // A message the agent names apart from the last is an entry of its own; its thoughts are
        // entries for the apps only.
        update(&tx, &shared, &chunk("agent_thought_chunk", "t1", "Where is the login?"), folder);
        update(&tx, &shared, &chunk("agent_message_chunk", "m1", "First."), folder);
        update(&tx, &shared, &chunk("agent_message_chunk", "m2", "Second."), folder);
        let (lines, entries) = drain(&mut rx);
        assert!(lines.is_empty(), "a thought and an unfinished line give the bot nothing yet: {lines:?}");
        let kinds: Vec<(&str, &str)> = entries.iter().map(|(_, entry)| (entry.kind.as_str(), entry.text.as_str())).collect();
        assert_eq!(kinds, [("thought", "Where is the login?"), ("said", "First."), ("said", "Second.")]);
        assert_ne!(entries[1].0, entries[2].0);

        // A tool the agent names reads as its name for the bot.
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call", "toolCallId": "b1", "name": "Bash", "title": "`git status`", "kind": "execute", "rawInput": { "command": "git status" } }), folder);
        let (lines, entries) = drain(&mut rx);
        assert_eq!(lines.concat().last().map(String::as_str), Some("● Bash(git status)"));
        assert_eq!(entries.last().unwrap().1.tool.as_ref().unwrap().name, "Bash");

        // A notice is the agent's, apart from what it says.
        update(&tx, &shared, &json!({ "sessionUpdate": "notice", "severity": "warning", "title": "Model fallback", "description": "Using Sonnet." }), folder);
        let (lines, entries) = drain(&mut rx);
        assert_eq!(lines, [vec!["  ⎿ Warning: Model fallback: Using Sonnet."]]);
        assert_eq!((entries[0].1.kind.as_str(), entries[0].1.level.as_str()), ("notice", "warning"));
        assert_eq!(last_said(&shared).as_deref(), Some("Second."), "a notice is not what it said");

        // Its mode changed on its own: the card and the settings kept for a resumed session follow.
        *shared.opening.lock().unwrap() = json!({ "configOptions": [{ "id": "mode", "name": "Mode", "category": "mode", "currentValue": "plan", "options": [{ "value": "default", "name": "Default" }, { "value": "plan", "name": "Plan" }] }] });
        update(&tx, &shared, &json!({ "sessionUpdate": "current_mode_update", "currentModeId": "default" }), folder);
        let mut config = None;
        while let Ok(event) = rx.try_recv() {
            if let Event::Config(kept) = event {
                config = Some(kept);
            }
        }
        assert_eq!(config, Some(vec![("mode".to_string(), "default".to_string())]));
        assert_eq!(*shared.settings.lock().unwrap(), [("Mode".to_string(), "Default".to_string())]);

        update(&tx, &shared, &json!({ "sessionUpdate": "session_info_update", "title": " Fix the login " }), folder);
        assert!(matches!(rx.try_recv(), Ok(Event::Title(title)) if title == "Fix the login"));

        // A compaction is one entry, which its updates change.
        update(&tx, &shared, &json!({ "sessionUpdate": "compaction_update", "compactionId": "c1", "status": "in_progress" }), folder);
        update(&tx, &shared, &json!({ "sessionUpdate": "compaction_summary_chunk", "compactionId": "c1", "content": { "type": "text", "text": "Kept: the login fix." } }), folder);
        update(&tx, &shared, &json!({ "sessionUpdate": "compaction_update", "compactionId": "c1", "status": "completed" }), folder);
        let (lines, entries) = drain(&mut rx);
        assert_eq!(lines, [vec!["● Compacting the context".to_string()], vec!["  ⎿ Compacted".to_string()]]);
        let last = &entries.last().unwrap();
        assert!(entries.iter().all(|(key, _)| key == "compaction:c1"));
        assert_eq!((last.1.status.as_str(), last.1.text.as_str()), ("completed", "Kept: the login fix."));
    }

    /// What the updates sent so far: the bot's lines, and the apps' entries.
    fn drain(rx: &mut tokio::sync::mpsc::UnboundedReceiver<Event>) -> (Vec<Vec<String>>, Vec<(String, Entry)>) {
        let (mut lines, mut entries) = (Vec::new(), Vec::new());
        while let Ok(event) = rx.try_recv() {
            match event {
                Event::Lines(new) => lines.push(new),
                Event::Entries(new) => entries.extend(new),
                _ => {}
            }
        }
        (lines, entries)
    }

    #[test]
    fn a_commands_terminal_output_and_an_edits_diff_reach_the_transcript() {
        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
        let shared = Shared::default();
        let folder = Path::new("/work/shop");
        let meta = |meta: Value| json!({ "sessionUpdate": "tool_call_update", "toolCallId": "t1", "_meta": meta });
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call", "toolCallId": "t1", "title": "cargo test", "kind": "execute", "status": "in_progress", "rawInput": { "command": "cargo test" }, "content": [{ "type": "terminal", "terminalId": "t1" }], "_meta": { "terminal_info": { "terminal_id": "t1" } } }), folder);
        update(&tx, &shared, &meta(json!({ "terminal_output_delta": { "terminal_id": "t1", "data": "\x1b[32mtest a ... ok\x1b[0m\r\n" } })), folder);
        update(&tx, &shared, &meta(json!({ "terminal_output_delta": { "terminal_id": "t1", "data": "\x1b]52;c;ZXZpbA==\x07test b ... ok\r\n" } })), folder);
        let (_, entries) = drain(&mut rx);
        let tool = entries.last().unwrap().1.tool.clone().unwrap();
        assert_eq!(tool.output, "\x1b[32mtest a ... ok\x1b[0m\r\ntest b ... ok\r\n", "chunks add up, and only colors are kept");
        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed", "_meta": { "terminal_exit": { "terminal_id": "t1", "exit_code": 0, "signal": null } } }), folder);
        let (lines, entries) = drain(&mut rx);
        // The bot reads the output the terminal got, without its colors.
        assert_eq!(lines, [vec!["  ⎿ test a ... ok", "    test b ... ok"]]);
        assert_eq!(entries.last().unwrap().1.tool.as_ref().unwrap().exit_code, Some(0));

        update(&tx, &shared, &json!({ "sessionUpdate": "tool_call", "toolCallId": "t2", "title": "Edit src/login.rs", "kind": "edit", "status": "completed", "rawInput": { "file_path": "/work/shop/src/login.rs" }, "content": [{ "type": "diff", "path": "/work/shop/src/login.rs", "oldText": "a\nb\n", "newText": "a\nc\n" }] }), folder);
        let (lines, entries) = drain(&mut rx);
        assert_eq!(lines, [vec!["● Edit src/login.rs", "  ⎿ changed src/login.rs"]]);
        let diff = &entries.last().unwrap().1.tool.as_ref().unwrap().diffs[0];
        assert_eq!((diff.path.as_str(), diff.lines.clone(), diff.added, diff.removed), ("src/login.rs", vec![" a".to_string(), "-b".into(), "+c".into()], 1, 1));
    }

    #[test]
    fn a_session_is_set_to_ask_first_and_keeps_what_an_earlier_run_chose() {
        let codex = spec("codex").unwrap();
        let options = json!([
            { "id": "mode", "category": "mode", "currentValue": "agent", "options": [{ "value": "read-only" }, { "value": "workspace-write" }, { "value": "agent" }] },
            { "id": "model", "category": "model", "currentValue": "auto/default", "options": [{ "group": "g", "name": "G", "options": [{ "value": "auto/fast" }, { "value": "auto/default" }] }] },
        ]);
        assert_eq!(settings(codex, &options, &[]), [("mode".to_string(), "workspace-write".to_string())]);
        let recorded = [("mode".to_string(), "workspace-write".to_string()), ("model".to_string(), "auto/fast".to_string()), ("model".to_string(), "gone".to_string())];
        assert_eq!(settings(codex, &options, &recorded[..2]), [("mode".to_string(), "workspace-write".to_string()), ("model".to_string(), "auto/fast".to_string())]);
        assert_eq!(settings(codex, &options, &recorded[2..]).len(), 1, "a value the session does not accept is not set");
        let gemini = spec("gemini").unwrap();
        assert!(settings(gemini, &options, &[]).is_empty(), "an agent without a safe mode is left as it is");
        let applied = [("mode".to_string(), "workspace-write".to_string())];
        assert_eq!(snapshot(&options, &applied), [("mode".to_string(), "workspace-write".to_string()), ("model".to_string(), "auto/default".to_string())]);
        assert!(settings(codex, &Value::Null, &[]).is_empty());
    }

    #[test]
    fn the_card_shows_the_settings_as_the_agent_names_them_and_what_it_used() {
        let options = json!([
            { "id": "mode", "name": "Mode", "category": "mode", "currentValue": "default", "options": [{ "value": "default", "name": "Default" }, { "value": "plan", "name": "Plan" }] },
            { "id": "model", "name": "Model", "category": "model", "currentValue": "opus", "options": [{ "group": "g", "name": "G", "options": [{ "value": "opus", "name": "Opus" }, { "value": "haiku", "name": "Haiku" }] }] },
            { "id": "effort", "name": "Effort", "category": "thought_level", "currentValue": "high", "options": [{ "value": "high", "name": "High" }] },
            { "id": "fast", "name": "Fast mode", "category": "model_config", "currentValue": "off", "options": [] },
        ]);
        let shown = |settings: Vec<(String, String)>| settings.into_iter().map(|(name, value)| format!("{name}: {value}")).collect::<Vec<_>>();
        let session = json!({ "configOptions": options });
        assert_eq!(shown(describe(&session, &[])), ["Model: Opus", "Effort: High", "Mode: Default"]);
        assert_eq!(shown(describe(&session, &[("model".to_string(), "haiku".to_string())]))[0], "Model: Haiku");
        // An agent that says them in the older words, as Kimi does.
        let older = json!({
            "models": { "currentModelId": "kimi-code/kimi-for-coding,thinking", "availableModels": [{ "modelId": "kimi-code/kimi-for-coding,thinking", "name": "Kimi for coding (thinking)" }] },
            "modes": { "currentModeId": "default", "availableModes": [{ "id": "default", "name": "Default" }] },
        });
        assert_eq!(shown(describe(&older, &[])), ["Model: Kimi for coding (thinking)", "Mode: Default"]);
        let bare = json!({ "models": { "currentModelId": "m1" } });
        assert_eq!(shown(describe(&bare, &[])), ["Model: m1"]);
        assert!(describe(&Value::Null, &[]).is_empty());
        assert_eq!(usage_of(&json!({ "used": 34123, "size": 200000, "cost": { "amount": 0.4231, "currency": "USD" } })), [("Context".to_string(), "34k / 200k".to_string()), ("Cost".to_string(), "$0.42".to_string())]);
        assert_eq!(usage_of(&json!({ "used": 900, "size": 1000000 })), [("Context".to_string(), "900 / 1M".to_string())]);
        assert!(usage_of(&json!({})).is_empty());
    }

    #[test]
    fn a_failed_start_says_which_step_and_what_to_do() {
        let claude = spec("claude").unwrap();
        assert!(setup_error(claude, "initialize", "connection closed").contains("too old"));
        assert!(setup_error(claude, "session", "Authentication required").contains("sign in"));
        assert!(setup_error(claude, "session", "Internal error").contains("could not open a session"));
    }

    /// The agents the real tests try: those `LORCA_ACP_AGENTS` names (kinds, comma-separated),
    /// else every one installed here.
    fn chosen(spec: &Spec) -> bool {
        std::env::var("LORCA_ACP_AGENTS").map(|list| list.split(',').any(|kind| kind.trim() == spec.kind)).unwrap_or(true)
    }

    /// Run by hand: `cargo test -p lorca --lib real_agents_answer -- --ignored --nocapture`. Starts
    /// each agent on a one-word prompt, so what the SDK makes of each one's answers shows, and
    /// refuses whatever any of them asks to do.
    #[tokio::test]
    #[ignore = "talks to the agents installed on this machine"]
    async fn real_agents_answer_the_handshake_and_a_prompt() {
        let folder = std::env::temp_dir().join(format!("lorca-acp-real-{}", std::process::id()));
        std::fs::create_dir_all(&folder).unwrap();
        for spec in SPECS.iter().filter(|spec| chosen(spec)) {
            let Some(program) = find(spec).await else {
                println!("{}: not installed", spec.kind);
                continue;
            };
            let (tx, mut rx) = mpsc::unbounded_channel();
            let driver = match start(spec, &program, &folder, "Reply with exactly: ok", None, &[], &[], &[], tx).await {
                Ok(driver) => driver,
                Err(error) => {
                    println!("{}: could not start: {error}", spec.kind);
                    continue;
                }
            };
            let said = tokio::time::timeout(Duration::from_secs(180), async {
                while let Some(event) = rx.recv().await {
                    match event {
                        Event::Approve(_, reply) => {
                            let _ = reply.send(Err("not in a test".into()));
                        }
                        Event::Idle(said) => return said,
                        _ => {}
                    }
                }
                None
            })
            .await;
            println!("{}: {:?}; commands: {}", spec.kind, said, driver.commands().await.len());
            driver.stop().await;
        }
        let _ = std::fs::remove_dir_all(&folder);
    }

    /// What the test lets an agent do: change files in its folder, and run a command about
    /// `ok.txt` that reaches nowhere else.
    fn allowed_in_test(approval: &Approval, folder: &Path) -> bool {
        match approval {
            Approval::Files { paths } => paths.iter().all(|path| Path::new(path).starts_with(folder)),
            Approval::Command { command } => command.contains("ok.txt") && !["curl", "wget", "sudo", "rm ", "git "].iter().any(|word| command.contains(word)),
            Approval::Tool { .. } => false,
        }
    }

    /// Run by hand: `cargo test -p lorca --lib real_agents_write -- --ignored --nocapture`. Asks
    /// each agent for a file in a scratch folder and decides what it asks as Lorca would: the
    /// path from a request to the file on disk.
    #[tokio::test]
    #[ignore = "talks to the agents installed on this machine"]
    async fn real_agents_write_a_file_when_allowed() {
        let mut failures = Vec::new();
        for spec in SPECS.iter().filter(|spec| chosen(spec)) {
            let Some(program) = find(spec).await else {
                println!("{}: not installed", spec.kind);
                continue;
            };
            let folder = std::env::temp_dir().join(format!("lorca-acp-write-{}-{}", spec.kind, std::process::id()));
            std::fs::create_dir_all(&folder).unwrap();
            let (tx, mut rx) = mpsc::unbounded_channel();
            let prompt = "Create a file named ok.txt in the current folder whose content is exactly: ok";
            let driver = match start(spec, &program, &folder, prompt, None, &[], &[], &[], tx).await {
                Ok(driver) => driver,
                Err(error) => {
                    println!("{}: could not start: {error}", spec.kind);
                    continue;
                }
            };
            let mut asked: Vec<String> = Vec::new();
            let finished = tokio::time::timeout(Duration::from_secs(240), async {
                while let Some(event) = rx.recv().await {
                    match event {
                        Event::Approve(approval, reply) => {
                            let allow = allowed_in_test(&approval, &folder);
                            asked.push(format!("{} {approval:?}", if allow { "allowed" } else { "refused" }));
                            let _ = reply.send(if allow { Ok(()) } else { Err("not in a test".into()) });
                        }
                        Event::Idle(said) => return said,
                        _ => {}
                    }
                }
                None
            })
            .await;
            let wrote = std::fs::read_to_string(folder.join("ok.txt")).map(|text| text.trim() == "ok").unwrap_or(false);
            println!("{}: finished {}; asked {asked:?}; wrote ok.txt: {wrote}", spec.kind, finished.is_ok());
            if !wrote {
                failures.push(spec.kind);
            }
            driver.stop().await;
            let _ = std::fs::remove_dir_all(&folder);
        }
        assert!(failures.is_empty(), "these agents did not write the file: {failures:?}");
    }

    #[test]
    fn the_agents_are_found_by_their_kind() {
        assert_eq!(spec("gemini").map(|spec| spec.args), Some(&["--acp"][..]));
        assert_eq!(spec("claude").map(|spec| spec.name), Some("Claude Code"));
        assert_eq!(spec("codex").map(|spec| spec.name), Some("Codex"));
        let kinds: HashSet<_> = SPECS.iter().map(|spec| spec.kind).collect();
        assert_eq!(kinds.len(), SPECS.len());
        // Claude Code and Codex are set to ask first; pi asks before nothing.
        assert!(SPECS.iter().filter(|spec| spec.adapter.is_some() && spec.kind != "pi").all(|spec| spec.safe_mode.is_some()), "an agent behind an adapter asks first");
        assert_eq!(spec("pi").and_then(|spec| spec.adapter.as_ref()).map(|adapter| adapter.binary_env), Some("PI_ACP_PI_COMMAND"));
    }
}
