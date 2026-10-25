//! Questions one Device asks a Runner through the relay: a `request` envelope the asking Device
//! signs and seals to the Runner, answered with a `response` envelope the Runner signs and seals
//! back (`envelope.rs`). The verbs read and change what lives on the Runner (a bot's memory, its
//! plugins and MCP servers, its secrets, a command it runs), so the desktop app and phone manage
//! a bot that runs on another machine. A turn's `job` / `job_result` pair works the same way; this
//! is the same idea for questions with an answer.

use std::sync::Arc;
use std::time::Duration;

use serde_json::{json, Value};

use crate::app::App;
use crate::memory::MemoryStore;
use crate::model::{Request, Response};

/// How long a question may wait for its answer. A Runner that is online answers in a few
/// seconds; past this the relay is slow or the Runner just went away.
pub const REQUEST_TIMEOUT: Duration = Duration::from_secs(20);

/// Asks `runner_id` to run `verb` and waits for the answer. Refuses up front when the Runner is
/// unknown, unreachable, or offline, so an editor never spins on a machine that is asleep.
pub async fn ask(app: &Arc<App>, runner_id: &str, verb: &str, body: Value) -> Result<Value, String> {
    ask_within(app, runner_id, verb, body, REQUEST_TIMEOUT).await
}

/// `ask` for a verb the Runner takes longer to answer, waiting up to `timeout`.
pub async fn ask_within(app: &Arc<App>, runner_id: &str, verb: &str, body: Value, timeout: Duration) -> Result<Value, String> {
    let remote = app.remote.lock().unwrap().clone();
    remote.ask(app, runner_id, verb, body, timeout).await
}

/// Where a verb about something on Runner `runner_id` runs: `here` on this Device when it is
/// that Runner, else on the Runner, which answers the same verb from its `answer`, waiting up to
/// `wait`.
pub async fn on_runner(
    app: &Arc<App>,
    runner_id: &str,
    verb: &str,
    body: Value,
    wait: Duration,
    here: impl AsyncFnOnce(Value) -> Result<Value, String>,
) -> Result<Value, String> {
    if app.this_device_id().as_deref() == Some(runner_id) {
        return here(body).await;
    }
    ask_within(app, runner_id, verb, body, wait).await
}

/// How this Device reaches another Runner with a question: over the relay (`Relay`), or, in
/// tests, straight to another `App` (`tests::Direct`).
#[async_trait::async_trait]
pub trait Remote: Send + Sync {
    async fn ask(&self, app: &Arc<App>, runner_id: &str, verb: &str, body: Value, timeout: Duration) -> Result<Value, String>;
}

/// A `request` envelope signed and sealed to the Runner through the relay, and its `response`.
pub struct Relay;

#[async_trait::async_trait]
impl Remote for Relay {
    async fn ask(&self, app: &Arc<App>, runner_id: &str, verb: &str, body: Value, timeout: Duration) -> Result<Value, String> {
        let runner = app.device(runner_id).ok_or("That bot is assigned to a Runner this Device does not know yet.")?;
        if runner.box_pubkey.is_empty() {
            return Err(format!("{} has not shared its key yet.", runner.name));
        }
        if app.relay_url().is_none() {
            return Err(format!("No relay is configured, so this Device cannot reach {}.", runner.name));
        }
        if !app.device_is_online(runner_id) {
            return Err(format!("{} is offline.", runner.name));
        }
        let request = Request { id: format!("req-{}", uuid::Uuid::new_v4()), verb: verb.to_string(), body };
        let ciphertext = crate::envelope::seal(app, "request", &runner, &request).map_err(|e| e.to_string())?;
        let (tx, rx) = tokio::sync::oneshot::channel();
        app.pending_responses.lock().unwrap().insert(request.id.clone(), (runner.id.clone(), tx));
        app.push_blob("request", Some(runner.id.clone()), ciphertext);
        let answer = tokio::time::timeout(timeout, rx).await;
        app.pending_responses.lock().unwrap().remove(&request.id);
        match answer {
            Ok(Ok(Response { error: Some(error), .. })) => Err(error),
            Ok(Ok(Response { body, .. })) => Ok(body),
            Ok(Err(_)) | Err(_) => Err(format!("{} did not answer in time.", runner.name)),
        }
    }
}

/// A `response` from `from` reached this Device: hands it to the waiting `ask` when `from` is the
/// Runner it asked, then drops the blob from the relay.
pub fn deliver(app: Arc<App>, from: &str, response: Response, blob_id: String) {
    let waiting = {
        let mut pending = app.pending_responses.lock().unwrap();
        match pending.get(&response.request_id) {
            Some((runner, _)) if runner == from => pending.remove(&response.request_id).map(|(_, tx)| tx),
            Some(_) => {
                tracing::warn!(request_id = %response.request_id, "a response from a Device that was not asked");
                None
            }
            None => None,
        }
    };
    if let Some(tx) = waiting {
        let _ = tx.send(response);
    }
    tokio::spawn(async move { crate::sync::delete_remote_blob(&app, &blob_id).await });
}

/// A `request` that Device `from` signed reached this Runner: answers it as asked by `from`,
/// seals the answer to it, and drops the request from the relay.
pub fn serve(app: Arc<App>, from: String, request: Request, blob_id: String) {
    tokio::spawn(async move {
        let (body, error) = match answer(&app, &from, &request).await {
            Ok(body) => (body, None),
            Err(error) => (Value::Null, Some(error)),
        };
        let response = Response { request_id: request.id.clone(), body, error };
        match app.device(&from) {
            Some(requester) => match crate::envelope::seal(&app, "response", &requester, &response) {
                Ok(ciphertext) => {
                    app.push_blob("response", Some(requester.id.clone()), ciphertext);
                }
                Err(error) => tracing::warn!(%error, "sealing a response"),
            },
            None => tracing::warn!(verb = %request.verb, "the Device that asked is gone"),
        }
        crate::sync::delete_remote_blob(&app, &blob_id).await;
    });
}

/// What this Runner can be asked. The memory verbs check that the bot runs here: a request
/// that reached the wrong machine is refused, not forwarded. The plugin verbs act on this
/// Runner's own installs, the `mcp.*` verbs on its mcp.json, and the `events.*` verbs on its
/// event subscriptions; the permission verb answers a card a bot here is waiting on; the bash
/// verbs type into, stop, or background a command here;
/// Send now has a turn here read a message it holds. The update verbs install the latest release
/// of this CLI, or turn its automatic updates on or off. A request names no release and no
/// download: the Runner installs only what its own signed manifest offers.
async fn answer(app: &Arc<App>, from: &str, request: &Request) -> Result<Value, String> {
    let body = &request.body;
    match request.verb.as_str() {
        "templates.memories" => Ok(json!(crate::templates::local_memories(app, body["bot_id"].as_str().ok_or("missing bot_id")?)?)),
        "templates.export.preview" => {
            let selection = serde_json::from_value(body["selection"].clone()).map_err(|e| format!("Invalid export selection: {e}"))?;
            Ok(json!(crate::templates::export_preview(app, body["bot_id"].as_str().ok_or("missing bot_id")?, &selection).await?))
        }
        "templates.import" => crate::templates::import_text(app, body["text"].as_str().ok_or("missing text")?, body).await,
        verb if verb.starts_with("events.") => crate::event_triggers::serve(app, verb, body).map_err(|e| e.to_string()),
        verb if verb.starts_with("budgets.") => crate::budgets::serve(app, verb, body),
        #[cfg(feature = "runner")]
        verb if verb.starts_with("connector_limits.") => crate::connector_limits::serve(app, verb, body),
        verb if verb.starts_with("reviews.") => crate::review_queue::serve(app, verb, body, from).await,
        verb if verb.starts_with("tasks.") => crate::tasks::serve(app, verb, body, from),
        "memory.read" => memory_read(app, body["bot_id"].as_str().ok_or("missing bot_id")?),
        "memory.write" => {
            let text = body["text"].as_str().ok_or("missing text")?;
            memory_write(app, body["bot_id"].as_str().ok_or("missing bot_id")?, text, body["expected_hash"].as_str())
        }
        // The account's profile on this Runner, and a bot's prompt preview and capture runs.
        "profile.read" | "profile.write" | "bots.prompt" | "memory_capture.list" | "memory_capture.undo" => answer_here(app, &request.verb, body),
        verb if verb.starts_with("feedback.") => crate::feedback::serve(app, verb, body, from).await,
        #[cfg(feature = "runner")]
        verb if verb.starts_with("plugins.") || verb == "permission.answer" || verb == "permissions.catalog" => crate::plugins::serve_request(app, verb, body, Some(from)).await,
        #[cfg(feature = "runner")]
        verb if verb.starts_with("mcp.") => crate::plugins::mcp_json::serve_request(app, verb, body).await,
        #[cfg(feature = "runner")]
        verb if verb.starts_with("secrets.") => crate::secrets::serve(app, verb, body),
        #[cfg(feature = "runner")]
        "bash.stdin" | "bash.stop" | "bash.background" => crate::shell::serve(app, &request.verb, body).await,
        // A coding agent's card on another Device: its transcript, Stop, and an answer to what it
        // asks.
        #[cfg(feature = "runner")]
        "coding.transcript" | "coding.stop" | "coding.set" | "coding.answer" => crate::coding::serve(app, &request.verb, body).await,
        #[cfg(feature = "runner")]
        "routines.regenerate_key" => {
            let id = body["id"].as_str().ok_or("missing id")?;
            crate::routine_events::regenerate_key(app, id).await?;
            Ok(json!({ "routine": app.routine(id).map(|routine| app.routine_out(&routine)) }))
        }
        #[cfg(feature = "runner")]
        "routines.subscribe" => {
            let id = body["id"].as_str().ok_or("missing id")?;
            crate::routine_events::subscribe_now(app, id).await?;
            Ok(json!({ "routine": app.routine(id).map(|routine| app.routine_out(&routine)) }))
        }
        // A skill on this Runner, asked for from another Device of the account.
        verb if verb.starts_with("skills.") => crate::skills::serve(app, verb, body.clone()).await,
        // Regenerate, or edit and send again, asked from another Device of the account.
        "chats.rerun" => {
            let chat_id = body["chat_id"].as_str().ok_or("missing chat_id")?;
            let message_id = body["message_id"].as_str().ok_or("missing message_id")?;
            crate::runtime::rerun_user_message(app, chat_id, message_id, body["text"].as_str()).map(|message| json!({ "message": message.for_app() }))
        }
        #[cfg(feature = "runner")]
        "chats.send_now" => {
            let chat_id = body["chat_id"].as_str().ok_or("missing chat_id")?;
            let message_id = body["message_id"].as_str().ok_or("missing message_id")?;
            crate::turns::send_now(app, chat_id, message_id).map(|sent| json!({ "sent": sent }))
        }
        #[cfg(feature = "cli")]
        "update.install" => crate::update::install_now(app).await,
        #[cfg(feature = "cli")]
        "update.auto" => crate::update::set_auto(app, body["on"].as_bool().ok_or("missing on")?),
        #[cfg(feature = "cli")]
        "service.status" => crate::service::status_out(&app.config),
        other => Err(format!("Unknown request {other}")),
    }
}

/// How much of a bot's memory trace `memory.read` answers with.
const TRACE_ENTRIES: usize = 50;

/// A bot's memory as this Runner has it: the index with its budget and the other files by name,
/// with the account's profile, the newest capture run, and the bot's memory trace, which the
/// inspector shows beside it.
pub fn memory_read(app: &Arc<App>, bot_id: &str) -> Result<Value, String> {
    let bot = local_bot(app, bot_id)?;
    let store = MemoryStore::for_bot(&app.config.home, &bot);
    let mut overview = store.overview();
    overview["profile"] = crate::profile::overview(&crate::profile::store(&app.config.home));
    #[cfg(feature = "runner")]
    {
        overview["capture"] = crate::memory_capture::latest(&store).map(|run| json!(run)).unwrap_or(Value::Null);
    }
    overview["trace"] = json!(crate::memory_trace::recent(&app.config.home, bot_id, TRACE_ENTRIES));
    Ok(overview)
}

/// Replaces a bot's MEMORY.md on this Runner, refusing when it changed since `expected_hash`.
pub fn memory_write(app: &Arc<App>, bot_id: &str, text: &str, expected_hash: Option<&str>) -> Result<Value, String> {
    let bot = local_bot(app, bot_id)?;
    let hash = MemoryStore::for_bot(&app.config.home, &bot).write_index(text, expected_hash).map_err(|e| e.to_string())?;
    Ok(json!({ "hash": hash }))
}

/// Replaces the account's profile on this Runner, refusing when it changed since `expected_hash`.
pub fn profile_write(app: &Arc<App>, text: &str, expected_hash: Option<&str>) -> Result<Value, String> {
    let store = crate::profile::store(&app.config.home);
    let hash = store.write_index(text, expected_hash).map_err(|e| e.to_string())?;
    Ok(json!({ "hash": hash }))
}

/// The verbs about what lives on this Runner that the local API and the relay answer alike: the
/// profile, a bot's prompt preview, and its memory capture runs. A bot verb checks the bot runs
/// here.
pub fn answer_here(app: &Arc<App>, verb: &str, body: &Value) -> Result<Value, String> {
    match verb {
        "profile.read" => Ok(crate::profile::overview(&crate::profile::store(&app.config.home))),
        "profile.write" => profile_write(app, body["text"].as_str().ok_or("missing text")?, body["expected_hash"].as_str()),
        #[cfg(feature = "runner")]
        "bots.prompt" => {
            let bot = local_bot(app, body["bot_id"].as_str().ok_or("missing bot_id")?)?;
            crate::turns::prompt_preview(app, &bot.id, body["chat_id"].as_str())
        }
        #[cfg(feature = "runner")]
        "memory_capture.list" => {
            let bot = local_bot(app, body["bot_id"].as_str().ok_or("missing bot_id")?)?;
            Ok(json!({ "runs": crate::memory_capture::runs(&MemoryStore::for_bot(&app.config.home, &bot)) }))
        }
        #[cfg(feature = "runner")]
        "memory_capture.undo" => {
            let bot = local_bot(app, body["bot_id"].as_str().ok_or("missing bot_id")?)?;
            let run_id = body["run_id"].as_str().ok_or("missing run_id")?;
            let removed = crate::memory_capture::undo(&MemoryStore::for_bot(&app.config.home, &bot), &crate::profile::store(&app.config.home), run_id)?;
            Ok(json!({ "removed": removed }))
        }
        other => Err(format!("Unknown request {other}")),
    }
}

fn local_bot(app: &Arc<App>, bot_id: &str) -> Result<crate::model::Bot, String> {
    let bot = app.bot(bot_id).ok_or("Unknown bot")?;
    if app.this_device_id().as_deref() != Some(bot.runner_id.as_str()) {
        let runner = app.device(&bot.runner_id).map(|d| d.name).unwrap_or_else(|| "another Runner".into());
        return Err(format!("{} runs on {runner}, not here.", bot.name));
    }
    Ok(bot)
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::config::Config;

    /// Another Device's question answered straight by the Runner's own `App`, as its `answer`
    /// would over the relay.
    pub(crate) struct Direct {
        pub(crate) runner_id: String,
        pub(crate) runner: Arc<App>,
        pub(crate) asked: std::sync::Mutex<Vec<String>>,
    }

    #[async_trait::async_trait]
    impl Remote for Direct {
        async fn ask(&self, _app: &Arc<App>, runner_id: &str, verb: &str, body: Value, _timeout: Duration) -> Result<Value, String> {
            self.asked.lock().unwrap().push(verb.to_string());
            if runner_id != self.runner_id {
                return Err(format!("{runner_id} is offline."));
            }
            answer(&self.runner, "asker", &Request { id: "req".into(), verb: verb.into(), body }).await
        }
    }

    struct Scratch(Arc<App>, std::path::PathBuf);
    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.1);
        }
    }

    fn scratch() -> Scratch {
        let home = std::env::temp_dir().join(format!("lorca-requests-{}", uuid::Uuid::new_v4()));
        Scratch(App::load(Config { home: home.clone(), port: 0 }).unwrap(), home)
    }

    /// A verb about another Runner goes to it and comes back with its answer; one about this
    /// Device runs here and asks nobody.
    #[tokio::test]
    async fn a_verb_runs_on_the_runner_it_is_about() {
        let (asker, runner) = (scratch(), scratch());
        crate::profile::store(&runner.0.config.home).write_index("The user ships on Fridays.\n", None).unwrap();
        let direct = Arc::new(Direct { runner_id: "workbench".into(), runner: runner.0.clone(), asked: Default::default() });
        *asker.0.remote.lock().unwrap() = direct.clone();

        let answer = crate::api::dispatch(&asker.0, "profile", json!({ "runner_id": "workbench" })).await.unwrap();
        assert!(answer["text"].as_str().unwrap().contains("ships on Fridays"), "{answer}");
        assert_eq!(answer["runner_id"], "workbench");
        assert_eq!(*direct.asked.lock().unwrap(), ["profile.read"]);

        let here = on_runner(&asker.0, "elsewhere", "profile.read", json!({}), REQUEST_TIMEOUT, async |_| Ok(json!("here"))).await;
        assert_eq!(here.unwrap_err(), "elsewhere is offline.");
        assert_eq!(direct.asked.lock().unwrap().len(), 2, "a Runner that is not this Device is asked");
    }
}
