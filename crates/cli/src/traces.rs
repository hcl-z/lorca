//! A turn's trace on its Runner: every model request as it went out, every reply, tool call,
//! hold, steering message, and follow-up, one JSON line each, so a turn that stopped can be read
//! afterwards step by step. `traces/<bot id>/<UTC day>/<job id>.jsonl` under Lorca's folder,
//! with the system prompts and tool lists the requests name by hash in `blobs/` beside them.
//! Nothing leaves the Runner. Off with `LORCA_TRACES=0`.

use std::collections::HashMap;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::Instant;

use async_trait::async_trait;
use futures::StreamExt;
use lorca_agent::provider::{AssistantEvent, AssistantEventStream, ModelRequest, Provider};
use lorca_agent::{AgentEvent, AgentMessage, AssistantMessage, StopReason, ToolCall};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use tokio_util::sync::CancellationToken;

use crate::app::App;
use crate::model::{Bot, Chat, Job};

/// The record format; a reader skips lines of a version it does not know.
pub const VERSION: u32 = 1;
/// Traces older than this go.
const KEEP_DAYS: i64 = 14;
/// Past this, the oldest days go first.
const KEEP_BYTES: u64 = 500 * 1024 * 1024;
/// A string longer than this keeps its head and tail.
const MAX_TEXT: usize = 64 * 1024;

pub fn enabled() -> bool {
    std::env::var("LORCA_TRACES").ok().as_deref() != Some("0")
}

pub fn root(home: &Path) -> PathBuf {
    home.join("traces")
}

fn day(secs: f64) -> String {
    chrono::DateTime::from_timestamp(secs as i64, 0).map(|time| time.format("%Y-%m-%d").to_string()).unwrap_or_default()
}

fn hash(text: &str) -> String {
    Sha256::digest(text.as_bytes()).iter().take(12).map(|byte| format!("{byte:02x}")).collect()
}

/// A record as it is written: saved secrets replaced first, then long strings clipped.
fn clean(redactions: &crate::secrets::Redactions, value: &mut Value) {
    redactions.json(value);
    clip(value);
}

/// The largest char boundary of `text` at or before `at`.
fn boundary_before(text: &str, mut at: usize) -> usize {
    at = at.min(text.len());
    while !text.is_char_boundary(at) {
        at -= 1;
    }
    at
}

/// The smallest char boundary of `text` at or after `at`.
fn boundary_after(text: &str, mut at: usize) -> usize {
    while at < text.len() && !text.is_char_boundary(at) {
        at += 1;
    }
    at
}

/// Strings past `MAX_TEXT` bytes keep their first half and last quarter of it, cut at char
/// boundaries, and an image keeps its size, not its bytes. Runs after scrubbing, so a cut never
/// splits a saved secret out of the scrubber's reach.
fn clip(value: &mut Value) {
    match value {
        Value::String(text) if text.len() > MAX_TEXT => {
            let head = boundary_before(text, MAX_TEXT / 2);
            let tail = boundary_after(text, text.len() - MAX_TEXT / 4);
            *text = format!("{}\n[… {} bytes cut …]\n{}", &text[..head], tail - head, &text[tail..]);
        }
        Value::Array(items) => items.iter_mut().for_each(clip),
        Value::Object(map) => {
            if map.get("type").and_then(Value::as_str) == Some("image") {
                if let Some(Value::String(data)) = map.get("data") {
                    let size = data.len();
                    map.insert("data".into(), json!(format!("[image, {size} bytes of base64]")));
                }
            }
            map.values_mut().for_each(clip);
        }
        _ => {}
    }
}

struct State {
    file: Option<std::fs::File>,
    seq: u64,
    step: u64,
    /// `loop`, or the housekeeping a request belongs to (`compaction`).
    phase: &'static str,
    /// The messages of the loop's last request, as JSON, to record only what each adds.
    sent: Vec<String>,
    blobs: std::collections::HashSet<String>,
    redactions: crate::secrets::Redactions,
    tool_starts: HashMap<String, (Instant, Value)>,
    /// The last step: its reply, and whether every tool result asked to stop.
    last_reply: Option<AssistantMessage>,
    step_terminated: Vec<bool>,
    last_hold: Option<String>,
    followed_up: bool,
}

/// One job's trace.
pub struct Trace {
    job_id: String,
    dir: PathBuf,
    state: Mutex<State>,
}

/// The day a Runner's traces were last pruned, so pruning runs once a day.
static PRUNED: Mutex<Option<String>> = Mutex::new(None);

impl Trace {
    /// Opens the job's trace and writes its start, or `None` when traces are off or the file
    /// cannot be made (the turn runs the same without).
    pub fn start(app: &Arc<App>, job: &Job, bot: &Bot, chat: &Chat, provider: &dyn Provider, unattended: bool) -> Option<Arc<Trace>> {
        if !enabled() {
            return None;
        }
        let now = crate::config::now_secs();
        let today = day(now);
        let root = root(&app.config.home);
        if PRUNED.lock().unwrap().replace(today.clone()).as_deref() != Some(today.as_str()) {
            let root = root.clone();
            std::thread::spawn(move || prune(&root, now));
        }
        let dir = root.join(&bot.id).join(&today);
        let file = open(&dir, &format!("{}.jsonl", job.id)).map_err(|error| tracing::warn!(%error, "opening the turn's trace")).ok()?;
        let trace = Arc::new(Trace {
            job_id: job.id.clone(),
            dir,
            state: Mutex::new(State {
                file: Some(file),
                seq: 0,
                step: 0,
                phase: "loop",
                sent: Vec::new(),
                blobs: Default::default(),
                redactions: crate::secrets::Redactions::load(app),
                tool_starts: HashMap::new(),
                last_reply: None,
                step_terminated: Vec::new(),
                last_hold: None,
                followed_up: false,
            }),
        });
        trace.record("job_start", json!({
            "bot_id": bot.id, "bot": bot.name, "chat_id": chat.meta.id,
            "chat_kind": if chat.meta.is_group() { "group" } else if chat.meta.channel.is_some() { "channel" } else { "dm" },
            "kind": job.kind, "trigger_message_id": job.trigger_message_id, "routine_id": job.routine_id,
            "from_bot_id": job.from_bot_id, "task_id": job.task_id, "unattended": unattended,
            "provider": provider.provider_id(), "model": provider.model_id(), "thinking": bot.thinking,
            "lorca": crate::config::VERSION,
        }));
        Some(trace)
    }

    fn record(&self, kind: &str, fields: Value) {
        let mut state = self.state.lock().unwrap();
        self.write(&mut state, kind, fields);
    }

    fn write(&self, state: &mut State, kind: &str, mut fields: Value) {
        state.seq += 1;
        clean(&state.redactions, &mut fields);
        let mut line = json!({ "v": VERSION, "type": kind, "job_id": self.job_id, "seq": state.seq, "step": state.step, "ts": crate::config::now_secs() });
        if let (Value::Object(line), Value::Object(fields)) = (&mut line, fields) {
            line.extend(fields);
        }
        let Some(file) = state.file.as_mut() else { return };
        if let Err(error) = writeln!(file, "{line}") {
            tracing::warn!(%error, "writing the turn's trace");
            state.file = None;
        }
    }

    /// Requests from here on belong to `phase` (`loop` again when it is done).
    pub fn phase(&self, phase: &'static str) {
        self.state.lock().unwrap().phase = phase;
    }

    /// `text` stored once per day under its hash, which the record names instead.
    fn blob(&self, state: &mut State, text: &str) -> String {
        let id = hash(text);
        if state.blobs.insert(id.clone()) {
            let mut text = Value::String(text.to_string());
            state.redactions.json(&mut text);
            let path = self.dir.join("blobs").join(format!("{id}.txt"));
            if !path.exists() {
                if let Ok(mut file) = open(&self.dir.join("blobs"), &format!("{id}.txt")) {
                    let _ = file.write_all(text.as_str().unwrap_or_default().as_bytes());
                }
            }
        }
        id
    }

    fn request(&self, request: &ModelRequest) {
        let mut state = self.state.lock().unwrap();
        let system = self.blob(&mut state, &request.system_prompt);
        let tools = self.blob(&mut state, &serde_json::to_string_pretty(&request.tools).unwrap_or_default());
        let messages: Vec<String> = request.messages.iter().map(|message| serde_json::to_string(message).unwrap_or_default()).collect();
        let same = state.sent.iter().zip(&messages).take_while(|(a, b)| a == b).count();
        let added: Vec<Value> = messages[same..].iter().filter_map(|message| serde_json::from_str(message).ok()).collect();
        let phase = state.phase;
        if phase == "loop" {
            state.sent = messages.clone();
        }
        self.write(&mut state, "request", json!({
            "phase": phase, "system": system, "system_chars": request.system_prompt.chars().count(),
            "tools": tools, "tool_count": request.tools.len(), "messages": messages.len(), "same_as_before": same,
            "added": added, "cache_points": request.cache_points, "max_tokens": request.max_tokens,
        }));
    }

    fn call_end(&self, phase: &'static str, started: Instant, first: Option<std::time::Duration>, event: &AssistantEvent) {
        let (stop_reason, usage, error) = match event {
            AssistantEvent::Done { stop_reason, usage } => (json!(stop_reason), json!(usage), Value::Null),
            AssistantEvent::Error { message, aborted } => (json!(if *aborted { "aborted" } else { "error" }), Value::Null, json!(message)),
            _ => return,
        };
        self.record("call_end", json!({
            "phase": phase, "ms": started.elapsed().as_millis() as u64, "first_event_ms": first.map(|d| d.as_millis() as u64),
            "stop_reason": stop_reason, "usage": usage, "error": error,
        }));
    }

    /// What the loop did, from its events.
    pub fn event(&self, event: &AgentEvent) {
        let mut state = self.state.lock().unwrap();
        match event {
            AgentEvent::TurnStart => {
                state.step += 1;
                state.step_terminated.clear();
            }
            AgentEvent::MessageEnd { message: AgentMessage::Assistant(reply) } => {
                self.write(&mut state, "response", json!({
                    "content": reply.content, "stop_reason": reply.stop_reason, "error": reply.error_message, "usage": reply.usage,
                }));
            }
            AgentEvent::MessageEnd { message: AgentMessage::Custom { kind, data, .. } } if kind == crate::turns::STEERING_MESSAGE_KIND => {
                let text = data["body"]["text"].clone();
                self.write(&mut state, "steering", json!({ "message_id": data["id"], "text": text }));
            }
            AgentEvent::ToolExecutionStart { tool_call_id, args, .. } => {
                state.tool_starts.insert(tool_call_id.clone(), (Instant::now(), args.clone()));
            }
            AgentEvent::ToolExecutionEnd { tool_call_id, tool_name, result, is_error } => {
                let (started, args) = state.tool_starts.remove(tool_call_id).unwrap_or((Instant::now(), Value::Null));
                state.step_terminated.push(result.terminate);
                let text: Vec<&str> = result.content.iter().filter_map(lorca_agent::ContentPart::as_text).collect();
                let images: Vec<Value> = result
                    .content
                    .iter()
                    .filter_map(|part| match part {
                        lorca_agent::ContentPart::Image { data, mime_type } => Some(json!({ "mime_type": mime_type, "base64_bytes": data.len() })),
                        _ => None,
                    })
                    .collect();
                let fields = json!({
                    "call_id": tool_call_id, "name": tool_name, "args": args, "result": text.join("\n"), "images": images, "details": result.details,
                    "is_error": is_error, "terminate": result.terminate, "ms": started.elapsed().as_millis() as u64,
                });
                self.write(&mut state, "tool", fields);
            }
            AgentEvent::Retry { attempt, max_attempts, delay_ms, error } => {
                self.write(&mut state, "retry", json!({ "attempt": attempt, "max_attempts": max_attempts, "delay_ms": delay_ms, "error": error }));
            }
            AgentEvent::TurnEnd { message: AgentMessage::Assistant(reply), .. } => {
                state.last_reply = Some(reply.clone());
            }
            _ => {}
        }
    }

    /// A call `before_tool_call` refused, which check refused it, and for a call a codemode
    /// script made, the script's call.
    pub fn hold(&self, call: &ToolCall, parent: Option<&ToolCall>, by: &str, reason: Option<&str>, terminate: bool) {
        let mut state = self.state.lock().unwrap();
        // Only the model's own calls end its step; a script's refused call is the script's.
        if parent.is_none() {
            state.last_hold = Some(by.to_string());
        }
        let parent = parent.map(|parent| parent.id.clone());
        self.write(&mut state, "hold", json!({ "call_id": call.id, "name": call.name, "parent": parent, "by": by, "reason": reason, "terminate": terminate }));
    }

    /// The turn asked the bot once more for the work its reply announced.
    pub fn follow_up(&self, reply: &str) {
        let mut state = self.state.lock().unwrap();
        state.followed_up = true;
        self.write(&mut state, "follow_up", json!({ "reply": reply }));
    }

    /// The bot's profile changed mid-turn, so the next step has a new system prompt, and its
    /// usual tools when the change finished its setup.
    pub fn prompt_refresh(&self, changed: &[&str], system_chars: usize, tools: bool) {
        self.record("prompt_refresh", json!({ "changed": changed, "system_chars": system_chars, "tools_replaced": tools }));
    }

    pub fn compaction(&self, tokens_before: u64, when: &str) {
        self.record("compaction", json!({ "tokens_before": tokens_before, "when": when }));
    }

    /// Why the turn ended, and how. `over_budget` is a limit of the turn's used up, which also
    /// refuses a model call before it is sent, ending the turn on an error.
    pub fn finish(&self, outcome: &str, cancelled: bool, over_budget: bool) {
        let mut state = self.state.lock().unwrap();
        let reply = state.last_reply.clone();
        let end_reason = match &reply {
            _ if cancelled => "stopped",
            None if over_budget => "budget",
            None => "no_reply",
            Some(reply) => match reply.stop_reason {
                StopReason::Error if over_budget => "budget",
                StopReason::Error => "error",
                StopReason::Aborted => "stopped",
                StopReason::Length if reply.tool_calls().is_empty() => "length",
                _ if !reply.tool_calls().is_empty() && state.last_hold.as_deref() == Some("limits") => "budget",
                _ if !state.step_terminated.is_empty() && state.step_terminated.iter().all(|t| *t) => "tool_terminate",
                _ if state.followed_up && crate::turns::is_pass(&reply.text()) => "follow_up_pass",
                _ => "no_tool_call",
            },
        };
        let last_text = reply.as_ref().map(|reply| reply.text()).unwrap_or_default();
        let (steps, followed_up) = (state.step, state.followed_up);
        self.write(&mut state, "job_end", json!({ "outcome": outcome, "end_reason": end_reason, "steps": steps, "followed_up": followed_up, "last_text": last_text }));
        if let Some(file) = state.file.as_mut() {
            let _ = file.flush();
        }
    }

    /// Reloads the saved secrets the records are scrubbed of, after the turn saved one.
    pub fn reload_secrets(&self, app: &App) {
        self.state.lock().unwrap().redactions = crate::secrets::Redactions::load(app);
    }
}

/// Opens `name` in `dir` to append, readable only by this user.
fn open(dir: &Path, name: &str) -> std::io::Result<std::fs::File> {
    std::fs::create_dir_all(dir)?;
    let mut options = std::fs::OpenOptions::new();
    options.create(true).append(true);
    #[cfg(unix)]
    std::os::unix::fs::OpenOptionsExt::mode(&mut options, 0o600);
    options.open(dir.join(name))
}

/// Removes the days past `KEEP_DAYS`, then the oldest until what is left fits `KEEP_BYTES`.
pub fn prune(root: &Path, now: f64) {
    let oldest = day(now - (KEEP_DAYS * 86_400) as f64);
    let mut days: Vec<(String, PathBuf, u64)> = Vec::new();
    for bot in std::fs::read_dir(root).into_iter().flatten().flatten() {
        for entry in std::fs::read_dir(bot.path()).into_iter().flatten().flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            let path = entry.path();
            if name < oldest {
                let _ = std::fs::remove_dir_all(&path);
            } else {
                days.push((name, path.clone(), size(&path)));
            }
        }
    }
    days.sort();
    let mut total: u64 = days.iter().map(|(_, _, bytes)| bytes).sum();
    for (_, path, bytes) in days {
        if total <= KEEP_BYTES {
            break;
        }
        let _ = std::fs::remove_dir_all(&path);
        total -= bytes;
    }
}

fn size(path: &Path) -> u64 {
    std::fs::read_dir(path)
        .into_iter()
        .flatten()
        .flatten()
        .map(|entry| match entry.file_type() {
            Ok(kind) if kind.is_dir() => size(&entry.path()),
            _ => entry.metadata().map(|meta| meta.len()).unwrap_or(0),
        })
        .sum()
}

/// The turn's provider, with each request and how it ended put in the trace.
pub struct TracedProvider {
    pub inner: Arc<dyn Provider>,
    pub trace: Arc<Trace>,
}

impl TracedProvider {
    pub fn wrap(inner: Arc<dyn Provider>, trace: Option<&Arc<Trace>>) -> Arc<dyn Provider> {
        match trace {
            Some(trace) => Arc::new(TracedProvider { inner, trace: trace.clone() }),
            None => inner,
        }
    }
}

#[async_trait]
impl Provider for TracedProvider {
    fn provider_id(&self) -> &str {
        self.inner.provider_id()
    }
    fn model_id(&self) -> &str {
        self.inner.model_id()
    }
    fn supports_images(&self) -> bool {
        self.inner.supports_images()
    }
    fn model_info(&self) -> Option<&'static lorca_agent::models::ModelInfo> {
        self.inner.model_info()
    }
    fn default_max_output_tokens(&self) -> Option<u64> {
        self.inner.default_max_output_tokens()
    }
    async fn stream(&self, request: ModelRequest, cancel: CancellationToken) -> AssistantEventStream {
        self.trace.request(&request);
        let phase = self.trace.state.lock().unwrap().phase;
        let started = Instant::now();
        let trace = self.trace.clone();
        let mut first = None;
        let stream = self.inner.stream(request, cancel).await;
        Box::pin(stream.inspect(move |event| {
            if first.is_none() && !matches!(event, AssistantEvent::Start) {
                first = Some(started.elapsed());
            }
            if matches!(event, AssistantEvent::Done { .. } | AssistantEvent::Error { .. }) {
                trace.call_end(phase, started, first, event);
            }
        }))
    }
}

// MARK: - Reading traces

/// One trace file's start and end.
#[derive(Debug, Clone)]
pub struct Summary {
    pub path: PathBuf,
    pub start: Value,
    pub end: Option<Value>,
    pub requests: usize,
    pub tools: usize,
    pub usage: (u64, u64, u64),
}

fn lines(path: &Path) -> Vec<Value> {
    std::fs::read_to_string(path)
        .unwrap_or_default()
        .lines()
        .filter_map(|line| serde_json::from_str::<Value>(line).ok())
        .filter(|record| record["v"].as_u64().is_some_and(|v| v <= VERSION as u64))
        .collect()
}

/// The traces of the last `days` days, newest first, of one bot or all.
pub fn list(home: &Path, bot_id: Option<&str>, days: i64) -> Vec<Summary> {
    let since = day(crate::config::now_secs() - (days.max(1) - 1) as f64 * 86_400.0);
    let mut out = Vec::new();
    for bot in std::fs::read_dir(root(home)).into_iter().flatten().flatten() {
        if bot_id.is_some_and(|id| bot.file_name().to_string_lossy() != id) {
            continue;
        }
        for day_dir in std::fs::read_dir(bot.path()).into_iter().flatten().flatten() {
            if day_dir.file_name().to_string_lossy().to_string() < since {
                continue;
            }
            for file in std::fs::read_dir(day_dir.path()).into_iter().flatten().flatten() {
                let path = file.path();
                if path.extension().and_then(|e| e.to_str()) != Some("jsonl") {
                    continue;
                }
                let records = lines(&path);
                let Some(start) = records.iter().find(|r| r["type"] == "job_start").cloned() else { continue };
                let end = records.iter().rev().find(|r| r["type"] == "job_end").cloned();
                let mut usage = (0, 0, 0);
                for r in records.iter().filter(|r| r["type"] == "response") {
                    usage.0 += r["usage"]["input"].as_u64().unwrap_or(0);
                    usage.1 += r["usage"]["output"].as_u64().unwrap_or(0);
                    usage.2 += r["usage"]["cache_read"].as_u64().unwrap_or(0);
                }
                let requests = records.iter().filter(|r| r["type"] == "request").count();
                let tools = records.iter().filter(|r| r["type"] == "tool").count();
                out.push(Summary { path, start, end, requests, tools, usage });
            }
        }
    }
    out.sort_by(|a, b| b.start["ts"].as_f64().unwrap_or(0.0).total_cmp(&a.start["ts"].as_f64().unwrap_or(0.0)));
    out
}

/// The trace file of `job_id`.
pub fn find(home: &Path, job_id: &str) -> Option<PathBuf> {
    let name = format!("{job_id}.jsonl");
    for bot in std::fs::read_dir(root(home)).ok()?.flatten() {
        for day_dir in std::fs::read_dir(bot.path()).into_iter().flatten().flatten() {
            let path = day_dir.path().join(&name);
            if path.is_file() {
                return Some(path);
            }
        }
    }
    None
}

fn when(record: &Value) -> String {
    chrono::DateTime::from_timestamp(record["ts"].as_f64().unwrap_or(0.0) as i64, 0).map(|t| t.format("%Y-%m-%d %H:%M:%S UTC").to_string()).unwrap_or_default()
}

/// One line per trace for `lorca traces list`.
pub fn list_text(summaries: &[Summary], end_reason: Option<&str>) -> String {
    let mut out = String::new();
    for s in summaries {
        let reason = s.end.as_ref().and_then(|e| e["end_reason"].as_str()).unwrap_or("unfinished");
        if end_reason.is_some_and(|wanted| wanted != reason) {
            continue;
        }
        out.push_str(&format!(
            "{}  {}  {:<10} {:<8} {:<16} steps {:<3} tools {:<3} in {} out {} cached {}  {}\n",
            when(&s.start), s.start["job_id"].as_str().unwrap_or(""), s.start["kind"].as_str().unwrap_or(""),
            s.start["bot"].as_str().unwrap_or(""), reason, s.requests, s.tools, s.usage.0, s.usage.1, s.usage.2,
            s.end.as_ref().and_then(|e| e["last_text"].as_str()).map(|t| crate::turns::excerpt(t, 60)).unwrap_or_default(),
        ));
    }
    out
}

/// Counts over traces for `lorca traces stats`.
pub fn stats_text(summaries: &[Summary]) -> String {
    let mut reasons: std::collections::BTreeMap<String, usize> = Default::default();
    let (mut followed, mut followed_pass) = (0, 0);
    let (mut steps, mut input, mut output, mut cached) = (0, 0, 0, 0);
    for s in summaries {
        let reason = s.end.as_ref().and_then(|e| e["end_reason"].as_str()).unwrap_or("unfinished").to_string();
        if s.end.as_ref().is_some_and(|e| e["followed_up"] == true) {
            followed += 1;
            followed_pass += usize::from(reason == "follow_up_pass");
        }
        *reasons.entry(reason).or_default() += 1;
        steps += s.requests;
        input += s.usage.0;
        output += s.usage.1;
        cached += s.usage.2;
    }
    let n = summaries.len().max(1);
    let mut out = format!("{} turns\n\nHow they ended:\n", summaries.len());
    for (reason, count) in &reasons {
        out.push_str(&format!("  {reason:<16} {count}\n"));
    }
    out.push_str(&format!(
        "\nFollow-ups: {followed} (answered PASS: {followed_pass}, went on: {})\nRequests per turn: {:.1}\nTokens: {input} in ({cached} from cache, {:.0}%), {output} out\n",
        followed - followed_pass,
        steps as f64 / n as f64,
        if input + cached > 0 { cached as f64 * 100.0 / (input + cached) as f64 } else { 0.0 },
    ));
    out
}

/// A trace as Markdown, step by step, for `lorca traces show` and `export`.
pub fn markdown(path: &Path, with_prompts: bool) -> String {
    let records = lines(path);
    let blobs = path.parent().map(|dir| dir.join("blobs")).unwrap_or_default();
    let mut out = String::new();
    let mut shown_system = None;
    for r in &records {
        let block = |value: &Value| format!("```json\n{}\n```\n", serde_json::to_string_pretty(value).unwrap_or_default());
        match r["type"].as_str().unwrap_or("") {
            "job_start" => out.push_str(&format!(
                "# {} · {}\n\n- bot: {} ({})\n- chat: {} ({})\n- job: {}{}\n- model: {} / {}\n- started: {}\n\n",
                r["job_id"].as_str().unwrap_or(""), r["kind"].as_str().unwrap_or(""), r["bot"].as_str().unwrap_or(""), r["bot_id"].as_str().unwrap_or(""),
                r["chat_id"].as_str().unwrap_or(""), r["chat_kind"].as_str().unwrap_or(""), r["kind"].as_str().unwrap_or(""),
                if r["unattended"] == true { ", unattended" } else { "" }, r["provider"].as_str().unwrap_or(""), r["model"].as_str().unwrap_or(""), when(r),
            )),
            "request" => {
                out.push_str(&format!(
                    "## Step {} · request ({})\n\n{} messages, {} as before; {} tools; system prompt {} ({} chars)\n\n",
                    r["step"], r["phase"].as_str().unwrap_or(""), r["messages"], r["same_as_before"], r["tool_count"], r["system"].as_str().unwrap_or(""), r["system_chars"],
                ));
                let system = r["system"].as_str().unwrap_or("").to_string();
                if with_prompts && shown_system.as_deref() != Some(system.as_str()) {
                    let text = std::fs::read_to_string(blobs.join(format!("{system}.txt"))).unwrap_or_default();
                    out.push_str(&format!("<details><summary>System prompt</summary>\n\n```\n{text}\n```\n\n</details>\n\n"));
                    shown_system = Some(system);
                }
                for message in r["added"].as_array().into_iter().flatten() {
                    let role = message["role"].as_str().unwrap_or("");
                    let text: Vec<String> = message["content"].as_array().into_iter().flatten().filter_map(|part| part["text"].as_str().map(str::to_string)).collect();
                    out.push_str(&format!("**{role}**\n\n{}\n\n", if text.is_empty() { block(&message["content"]) } else { text.join("\n") }));
                }
            }
            "response" => {
                out.push_str(&format!("### Reply · {}\n\n", r["stop_reason"].as_str().unwrap_or("")));
                for part in r["content"].as_array().into_iter().flatten() {
                    match part["type"].as_str().unwrap_or("") {
                        "text" => out.push_str(&format!("{}\n\n", part["text"].as_str().unwrap_or(""))),
                        "thinking" => out.push_str(&format!("<details><summary>Thinking</summary>\n\n{}\n\n</details>\n\n", part["thinking"].as_str().unwrap_or(""))),
                        "tool_call" => out.push_str(&format!("**Tool call: {}**\n\n{}", part["name"].as_str().unwrap_or(""), block(&part["arguments"]))),
                        _ => {}
                    }
                }
                if let Some(error) = r["error"].as_str() {
                    out.push_str(&format!("Error: {error}\n\n"));
                }
            }
            "call_end" => out.push_str(&format!("_{} ms, first event at {} ms_\n\n", r["ms"], r["first_event_ms"])),
            "tool" => out.push_str(&format!(
                "### Tool result · {}{}{} · {} ms\n\n```\n{}\n```\n\n",
                r["name"].as_str().unwrap_or(""), if r["is_error"] == true { " · error" } else { "" }, if r["terminate"] == true { " · ends the turn" } else { "" },
                r["ms"], r["result"].as_str().unwrap_or(""),
            )),
            "hold" => out.push_str(&format!("### Held · {} by {}\n\n{}\n\n", r["name"].as_str().unwrap_or(""), r["by"].as_str().unwrap_or(""), r["reason"].as_str().unwrap_or(""))),
            "steering" => out.push_str(&format!("### The user wrote meanwhile\n\n{}\n\n", r["text"].as_str().unwrap_or(""))),
            "follow_up" => out.push_str("### Follow-up: the reply announced work and made no call\n\n"),
            "prompt_refresh" => out.push_str(&format!(
                "### The profile changed ({}): a new system prompt{}\n\n",
                r["changed"].as_array().into_iter().flatten().filter_map(Value::as_str).collect::<Vec<_>>().join(", "),
                if r["tools_replaced"] == true { " and the usual tools" } else { "" },
            )),
            "retry" => out.push_str(&format!("_Retry {} of {} in {} ms: {}_\n\n", r["attempt"], r["max_attempts"], r["delay_ms"], r["error"].as_str().unwrap_or(""))),
            "compaction" => out.push_str(&format!("### Compacted ({}), {} tokens before\n\n", r["when"].as_str().unwrap_or(""), r["tokens_before"])),
            "job_end" => out.push_str(&format!(
                "## End · {} · {}\n\n{} steps{}\n",
                r["outcome"].as_str().unwrap_or(""), r["end_reason"].as_str().unwrap_or(""), r["steps"], if r["followed_up"] == true { ", followed up once" } else { "" },
            )),
            _ => {}
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_record_keeps_the_head_and_tail_of_long_text_and_the_size_of_an_image() {
        let long = "a".repeat(MAX_TEXT * 2);
        let mut value = json!({ "result": long, "content": [{ "type": "image", "data": "QUJD", "mime_type": "image/png" }] });
        clip(&mut value);
        let result = value["result"].as_str().unwrap();
        assert!(result.len() < MAX_TEXT && result.contains(&format!("[… {} bytes cut …]", MAX_TEXT * 2 - MAX_TEXT / 2 - MAX_TEXT / 4)), "{}", result.len());
        assert_eq!(value["content"][0]["data"], "[image, 4 bytes of base64]");

        // Clipped by bytes at char boundaries: Chinese text fits the budget too.
        let mut chinese = json!("中".repeat(MAX_TEXT));
        clip(&mut chinese);
        let chinese = chinese.as_str().unwrap();
        assert!(chinese.len() < MAX_TEXT && chinese.starts_with('中') && chinese.ends_with('中'), "{}", chinese.len());
    }

    /// A saved secret is replaced before the clip, so a cut through it leaves nothing of it.
    #[test]
    fn a_secret_across_the_clip_is_scrubbed_whole() {
        let secret = "s3cr3t-token-value-0123456789";
        let mut text = "a".repeat(MAX_TEXT / 2 - 10);
        text.push_str(secret);
        text.push_str(&"b".repeat(MAX_TEXT * 2));
        let mut value = json!({ "result": text });
        clean(&crate::secrets::Redactions::of(&[(secret, "{{secret:api_token}}")]), &mut value);
        let result = value["result"].as_str().unwrap();
        assert!(!result.contains("s3cr3t") && !result.contains("0123456789"), "no part of the secret is kept");
    }

    #[test]
    fn pruning_removes_old_days_then_the_oldest_past_the_size() {
        let root = std::env::temp_dir().join(format!("lorca-traces-{}", uuid::Uuid::new_v4()));
        let now = 1_800_000_000.0;
        let old = root.join("b1").join(day(now - 20.0 * 86_400.0));
        let recent = root.join("b1").join(day(now - 86_400.0));
        for dir in [&old, &recent] {
            std::fs::create_dir_all(dir).unwrap();
            std::fs::write(dir.join("job.jsonl"), "{}\n").unwrap();
        }
        prune(&root, now);
        assert!(!old.exists() && recent.exists());
        let _ = std::fs::remove_dir_all(&root);
    }
}
