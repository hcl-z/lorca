//! What a Device does with turns: sends the user's message, runs a group's room exchange,
//! starts jobs here or on the bot's Runner through the relay, and takes their results. The
//! turn itself, with its model, tools, and memory, lives in `turns` under the `runner` feature.

use std::sync::Arc;

use tokio_util::sync::CancellationToken;

use crate::app::{App, RunningJob, SentJob};
use crate::config::now_secs;
use crate::model::*;

/// How a chat is named in a bot's memory: `your chat with the user`, `group "Standup"`.
pub fn chat_source(app: &App, chat: &Chat) -> String {
    if chat.meta.is_group() {
        let title = chat.meta.title.clone().filter(|t| !t.trim().is_empty()).unwrap_or_else(|| {
            chat.meta.bot_ids.iter().map(|id| name_of(app, id)).collect::<Vec<_>>().join(", ")
        });
        format!("group \"{title}\"")
    } else {
        "your chat with the user".into()
    }
}

// MARK: - Sending

/// Appends the user's message and admits the turn it calls for. If another turn owns the chat
/// lock, that turn can consume the message as steering at its next safe model boundary, and a
/// question it waits on is dismissed; the admitted replacement job then exits when it reaches
/// the lock.
///
/// A direct chat's bot always answers. A group runs a room exchange: every member is offered a
/// turn in order and sends or passes, in rounds, until a round goes by with nobody speaking.
pub fn send_user_message(
    app: Arc<App>,
    chat_id: &str,
    text: &str,
    message_id: Option<String>,
    attachments: Vec<Attachment>,
    mentions: Vec<String>,
    reply_to: Option<String>,
) -> anyhow::Result<Message> {
    send_user_message_with_agents(app, chat_id, text, message_id, attachments, mentions, Vec::new(), reply_to)
}

/// A user message that may address coding agents. The picked ids and the `@Name`s in `text` are
/// resolved against the chat's open agent cards; a message with any target goes to those agents
/// as an `agent_message` job and starts no bot turn. Everything else is the ordinary path.
pub fn send_user_message_with_agents(
    app: Arc<App>,
    chat_id: &str,
    text: &str,
    message_id: Option<String>,
    attachments: Vec<Attachment>,
    mentions: Vec<String>,
    agents: Vec<String>,
    reply_to: Option<String>,
) -> anyhow::Result<Message> {
    let text = text.trim();
    if text.is_empty() && attachments.is_empty() {
        anyhow::bail!("Empty message");
    }
    let mentions = resolve_mentions(&app, text, mentions);
    let chat = app.chat(chat_id).ok_or_else(|| anyhow::anyhow!("Unknown chat"))?;
    let reply_to = match reply_to.filter(|id| !id.is_empty()) {
        Some(id) => Some(
            app.message(chat_id, &id)
                .as_ref()
                .and_then(ReplyTo::quoting)
                .ok_or_else(|| anyhow::anyhow!("The message you replied to is no longer in this chat"))?,
        ),
        None => None,
    };
    // The bytes go out ahead of the message that names them.
    for attachment in &attachments {
        if let Err(error) = crate::files::push_blob(&app, Some(chat_id), attachment) {
            tracing::warn!(%error, name = %attachment.name, "uploading an attachment");
        }
    }
    let replied_bot = match &reply_to {
        Some(ReplyTo { author: Author::Bot { bot_id }, .. }) => Some(bot_id.clone()),
        _ => None,
    };
    let targets = agent_targets(&app, &chat, text, &agents)?;
    if !targets.is_empty() {
        if text.is_empty() {
            anyhow::bail!("Write a message for the coding agent");
        }
        for target in &targets {
            let bot = app.bot(&target.bot_id).ok_or_else(|| anyhow::anyhow!("The agent's bot is no longer in this chat"))?;
            if app.this_device_id().as_deref() != Some(bot.runner_id.as_str()) {
                let runner = app
                    .device(&bot.runner_id)
                    .ok_or_else(|| anyhow::anyhow!("{} is assigned to a Runner this Device does not know yet.", bot.name))?;
                if !runner.agent_messages {
                    anyhow::bail!("Update {} to talk to coding agents.", runner.name);
                }
            }
        }
        let agent_ids: Vec<String> = targets.iter().map(|target| target.id.clone()).collect();
        let mut message = Message::new(
            chat_id,
            Author::You,
            Body::Text { agents: agent_ids, text: text.to_string(), attachments, mentions: mentions.clone(), reply_to },
        );
        if let Some(id) = message_id.filter(|id| !id.is_empty()) {
            message.id = id;
        }
        app.upsert_message(message.clone(), true);
        // No hear_user_message: this is not the bot's turn to steer, dismiss a question for, or
        // stop. The agent reads it through its own job.
        for target in targets {
            dispatch_job(&app, agent_message_job(&app, &chat.meta.id, &target.bot_id, &message.id, &target.id, text));
        }
        return Ok(message);
    }
    let mut message = Message::new(
        chat_id,
        Author::You,
        Body::Text { agents: Vec::new(), text: text.to_string(), attachments, mentions: mentions.clone(), reply_to },
    );
    if let Some(id) = message_id.filter(|id| !id.is_empty()) {
        message.id = id;
    }
    admit_user_message(app, &chat, message, &mentions, replied_bot.as_deref())
}

/// A coding agent a message addresses, and the bot that runs it.
struct AgentTarget {
    id: String,
    bot_id: String,
}

/// Resolve picked agent ids and `@Name`s against the chat's open agent cards. A picked id that
/// is not an open card is refused; a typed name that is not one is left for the bot, as before.
fn agent_targets(app: &App, chat: &Chat, text: &str, picked: &[String]) -> anyhow::Result<Vec<AgentTarget>> {
    let mut cards: Vec<(String, String, String, f64, bool)> = Vec::new();
    for message in app.store.all(&chat.meta.id).unwrap_or_default() {
        let Body::Tool { agent: Some(run), .. } = &message.body else { continue };
        let Author::Bot { bot_id } = &message.author else { continue };
        if !chat.meta.bot_ids.contains(bot_id) {
            continue;
        }
        cards.push((run.id.clone(), run.display_name().to_string(), bot_id.clone(), run.started_at.unwrap_or(0.0), run.is_open()));
    }
    let mut targets: Vec<AgentTarget> = Vec::new();
    for id in picked {
        // A pick that raced the agent's end still keeps the message: the Runner has no open
        // agent to take it, so the job is a no-op rather than a bot turn.
        let Some((_, _, bot_id, _, _)) = cards.iter().find(|(card_id, _, _, _, _)| card_id == id) else {
            anyhow::bail!("Unknown coding agent");
        };
        if !targets.iter().any(|target| target.id == *id) {
            targets.push(AgentTarget { id: id.clone(), bot_id: bot_id.clone() });
        }
    }
    let mut typed: Vec<&(String, String, String, f64, bool)> = cards
        .iter()
        .filter(|(_, name, _, _, open)| *open && text.match_indices('@').any(|(at, _)| mention_at(text, at, name)))
        .collect();
    typed.sort_by(|a, b| b.3.total_cmp(&a.3));
    for (id, _, bot_id, _, _) in typed {
        if !targets.iter().any(|target| target.id == *id) {
            targets.push(AgentTarget { id: id.clone(), bot_id: bot_id.clone() });
        }
    }
    Ok(targets)
}

/// The job that hands one user message to one coding agent. It carries the text as typed; the
/// Runner strips the agent's `@Name` before it sends it.
pub(crate) fn agent_message_job(app: &Arc<App>, chat_id: &str, bot_id: &str, trigger_message_id: &str, agent_id: &str, text: &str) -> Job {
    let mut job = Job::local(crate::model::AGENT_MESSAGE_JOB, chat_id, bot_id, &app.this_device_id().unwrap_or_default());
    job.trigger_message_id = trigger_message_id.to_string();
    job.agent_message = Some(crate::model::AgentMessage { id: agent_id.to_string(), text: text.to_string() });
    job
}

fn admit_user_message(app: Arc<App>, chat: &Chat, message: Message, mentions: &[String], replied_bot: Option<&str>) -> anyhow::Result<Message> {
    app.upsert_message(message.clone(), true);
    #[cfg(feature = "runner")]
    crate::turns::hear_user_message(&app, &message);

    if chat.meta.is_group() {
        let members = turn_order(&chat.meta, &app, mentions, replied_bot);
        start_room(app.clone(), chat.meta.id.clone(), message.id.clone(), members);
    } else if let Some(bot) = chat.meta.bot_ids.first().and_then(|id| app.bot(id)) {
        start_turn(&app, user_turn_job(&app, &chat.meta.id, &bot.id, &message.id));
    }
    Ok(message)
}

/// Runs the latest exchange of a direct chat again (`chats.rerun`), on the bot's Runner: what
/// came after the user's latest message (the bot's reply, its tool rows and cards, notices) leaves
/// the chat on every Device, the message takes `text` when the user edited it, and a new turn
/// answers it, from a context that no longer holds the old reply. What the old reply did stays
/// done: a command it ran is not undone. So a reply that left something the user still has to
/// act on or that lives on elsewhere (a draft, a published output, work handed to another bot) is
/// not run again, and neither is one under a compaction summary.
pub fn rerun_user_message(app: &Arc<App>, chat_id: &str, message_id: &str, text: Option<&str>) -> Result<Message, String> {
    let chat = app.chat(chat_id).ok_or("Unknown chat")?;
    if chat.meta.is_group() || chat.meta.channel.is_some() {
        return Err("Only a direct chat's reply can run again.".into());
    }
    let bot = chat.meta.bot_ids.first().and_then(|id| app.bot(id)).ok_or("The chat has no bot")?;
    if app.this_device_id().as_deref() != Some(bot.runner_id.as_str()) {
        return Err("This runs on the bot's Runner.".into());
    }
    if app.running_chat_ids().iter().any(|id| id == chat_id) {
        return Err("Stop the reply first.".into());
    }
    let mut message = app.message(chat_id, message_id).ok_or("Unknown message")?;
    if message.author != Author::You || message.queued || !message.is_complete() {
        return Err("Only a message of yours runs again.".into());
    }
    if message.body.addresses_agent() {
        return Err("A message to a coding agent cannot run again.".into());
    }
    let after = app.store.messages_after(chat_id, message_id).map_err(|error| error.to_string())?;
    if after.iter().any(|later| later.author == Author::You) {
        return Err("Only your latest message runs again.".into());
    }
    let compacted = chat.compactions.iter().any(|c| c.after_message_id == message_id || after.iter().any(|later| later.id == c.after_message_id));
    if compacted {
        return Err("The chat was compacted after this message, so it cannot run again.".into());
    }
    for later in &after {
        let keeps = match &later.body {
            Body::Draft { .. } => Some("a draft"),
            Body::Tool { target_bot_id: Some(_), .. } => Some("work handed to another bot"),
            Body::Handoff { .. } if later.id.starts_with(crate::handoffs::SENT_MARKER_PREFIX) => Some("work handed to another bot"),
            _ if later.output.is_some() => Some("a published output"),
            _ => None,
        };
        if let Some(what) = keeps {
            return Err(format!("The reply left {what}, which running it again would lose."));
        }
    }
    if let Some(text) = text {
        let text = text.trim();
        let Body::Text { text: current, attachments, mentions, .. } = &mut message.body else {
            return Err("Only a message of yours runs again.".into());
        };
        if text.is_empty() && attachments.is_empty() {
            return Err("Empty message".into());
        }
        if text != current {
            *mentions = resolve_mentions(app, text, std::mem::take(mentions));
            *current = text.to_string();
            message.edited_at = Some(crate::config::now_secs());
        }
    }
    for later in &after {
        app.remove_message(chat_id, &later.id, true);
    }
    app.upsert_message(message.clone(), true);
    start_turn(app, user_turn_job(app, chat_id, &bot.id, &message.id));
    Ok(message)
}

/// The user's first message to a bot just added from a marketplace template, and the turn that
/// answers it: the bot sets itself up from `setup` first (`turns::setup_cue`), then says hello.
pub fn greet_new_bot(app: &Arc<App>, chat_id: &str, bot_id: &str, text: &str, setup: TemplateSetup) {
    let message = Message::new(chat_id, Author::You, Body::text(text.trim()));
    let mut job = user_turn_job(app, chat_id, bot_id, &message.id);
    job.setup = Some(setup);
    app.upsert_message(message, true);
    start_turn(app, job);
}

/// The first turn of a bot that sets itself up with the user (`Bot::setting_up`).
pub const SETUP_JOB: &str = "setup";

/// Starts a new bot's setup in its DM, wherever it runs. Its creator's brief, when a bot made it,
/// opens the chat as a "Message from" marker the new bot reads; otherwise a notice does.
pub fn start_setup(app: &Arc<App>, bot: &Bot, chat_id: &str, creator: Option<&Bot>, brief: Option<&str>) {
    let body = match (creator, brief.map(str::trim).filter(|b| !b.is_empty())) {
        (Some(creator), Some(brief)) => Body::Handoff { from: creator.id.clone(), to: bot.id.clone(), reason: brief.to_string() },
        (Some(creator), None) => Body::Notice { text: format!("{} created a new bot", creator.name), routine_id: None },
        (None, _) => Body::Notice { text: "New bot".into(), routine_id: None },
    };
    let author = match (&body, creator) {
        (Body::Handoff { .. }, Some(creator)) => Author::Bot { bot_id: creator.id.clone() },
        _ => Author::System,
    };
    let mut marker = Message::new(chat_id, author, body);
    marker.id = format!("setup-{}", bot.id);
    app.upsert_message(marker.clone(), true);
    let mut job = user_turn_job(app, chat_id, &bot.id, &marker.id);
    job.kind = SETUP_JOB.into();
    start_turn(app, job);
}

/// Stops work in a chat on this Device and forwards job-specific cancellations to every other
/// Runner currently doing that work. This is the hard Stop path; ordinary messages steer and
/// do not call it.
pub fn cancel_chat(app: &Arc<App>, chat_id: &str) {
    // A command left waiting for input in this chat stops too, turn or no turn, and so do the
    // coding agents its bots run.
    #[cfg(feature = "runner")]
    {
        app.shell_sessions.stop_chat(chat_id);
        app.coding_agents.stop_chat(app, chat_id);
    }
    for (job_id, runner_id) in app.cancel_chat(chat_id) {
        let Some(runner) = app.device(&runner_id).filter(|runner| !runner.box_pubkey.is_empty()) else { continue };
        match crate::envelope::seal(app, "job_cancel", &runner, &JobCancel { job_id: job_id.clone() }) {
            Ok(ciphertext) => {
                app.push_blob("job_cancel", Some(runner.id), ciphertext);
            }
            Err(error) => tracing::warn!(%error, %job_id, "sealing job cancellation"),
        }
    }
}

pub(crate) fn user_turn_job(app: &Arc<App>, chat_id: &str, bot_id: &str, trigger_message_id: &str) -> Job {
    Job {
        agent_message: None,
        id: format!("job-{}", uuid::Uuid::new_v4()),
        chat_id: chat_id.to_string(),
        bot_id: bot_id.to_string(),
        kind: "turn".into(),
        task_id: None,
        task_context: None,
        trigger_message_id: trigger_message_id.to_string(),
        handoff: None,
        requested_by: app.this_device_id().unwrap_or_default(),
        routine_id: None,
        check: None,
        from_bot_id: None,
        hops: 0,
        round: 0,
        is_winding_down: false,
        setup: None,
        created_at: now_secs(),
    }
}

/// The turn in which a bot hears that a command it left running ended (`shell::wake_job`):
/// `trigger_message_id` is the command's card.
pub fn command_job(app: &App, chat_id: &str, bot_id: &str, card_id: &str) -> Job {
    Job {
        agent_message: None,
        id: format!("job-{}", uuid::Uuid::new_v4()),
        chat_id: chat_id.to_string(),
        bot_id: bot_id.to_string(),
        kind: "command".into(),
        task_id: None,
        task_context: None,
        trigger_message_id: card_id.to_string(),
        handoff: None,
        requested_by: app.this_device_id().unwrap_or_default(),
        check: None,
        routine_id: None,
        from_bot_id: None,
        hops: 0,
        round: 0,
        is_winding_down: false,
        setup: None,
        created_at: now_secs(),
    }
}

/// The turn in which a bot hears what a coding agent it runs did (`coding::wake`):
/// `trigger_message_id` is the agent's card.
pub fn agent_job(app: &App, chat_id: &str, bot_id: &str, card_id: &str) -> Job {
    Job {
        agent_message: None, kind: "agent".into(), ..command_job(app, chat_id, bot_id, card_id) }
}

/// Members in chat order, with the ones the message mentions first, then the one whose message
/// it replies to.
pub fn turn_order(chat: &ChatMeta, app: &Arc<App>, mentions: &[String], replied: Option<&str>) -> Vec<Bot> {
    let members: Vec<Bot> = chat.bot_ids.iter().filter_map(|id| app.bot(id)).collect();
    let (mentioned, rest): (Vec<Bot>, Vec<Bot>) = members.into_iter().partition(|bot| mentions.contains(&bot.id));
    let (answered, rest): (Vec<Bot>, Vec<Bot>) = rest.into_iter().partition(|bot| Some(bot.id.as_str()) == replied);
    mentioned.into_iter().chain(answered).chain(rest).collect()
}

/// The bots a message mentions, by id: the ones the user picked from the `@` menu, then each
/// other `@Name` in the text that only one bot answers to. A name two bots share stays as typed
/// unless the user picked which.
fn resolve_mentions(app: &App, text: &str, picked: Vec<String>) -> Vec<String> {
    let bots = app.state.lock().unwrap().bots.clone();
    let mut mentions: Vec<String> = Vec::new();
    for id in picked {
        if bots.iter().any(|bot| bot.id == id) && !mentions.contains(&id) {
            mentions.push(id);
        }
    }
    for bot in &bots {
        let shared = bots.iter().filter(|other| other.name.eq_ignore_ascii_case(&bot.name)).count() > 1;
        if !shared && !mentions.contains(&bot.id) && text.match_indices('@').any(|(at, _)| mention_at(text, at, &bot.name)) {
            mentions.push(bot.id.clone());
        }
    }
    mentions
}

/// Whether `@name` starts at byte `at` of `text` as a word of its own, ignoring ASCII case: not
/// inside an email address, and not the start of a longer name.
pub(crate) fn mention_at(text: &str, at: usize, name: &str) -> bool {
    let before = text[..at].chars().next_back();
    let Some(rest) = text[at..].strip_prefix('@') else { return false };
    !name.is_empty()
        && !before.is_some_and(char::is_alphanumeric)
        && rest.get(..name.len()).is_some_and(|word| word.eq_ignore_ascii_case(name))
        && !rest[name.len()..].chars().next().is_some_and(|c| c.is_alphanumeric() || c == '_')
}

// MARK: - Rooms

/// Rounds of turns after one user message before the exchange winds down.
pub const MAX_ROOM_ROUNDS: u32 = 4;
/// How long a member's turn on another Runner may take before the room moves on.
const REMOTE_TURN_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(300);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TurnOutcome {
    /// The member posted at least one message.
    Sent,
    /// The member had nothing to add.
    Pass,
    /// The turn could not run (offline Runner, no provider) or failed.
    Skipped,
}

impl TurnOutcome {
    pub(crate) fn as_str(self) -> &'static str {
        match self {
            TurnOutcome::Sent => "sent",
            TurnOutcome::Pass => "pass",
            TurnOutcome::Skipped => "error",
        }
    }
    fn parse(text: &str) -> TurnOutcome {
        match text {
            "sent" => TurnOutcome::Sent,
            "pass" => TurnOutcome::Pass,
            _ => TurnOutcome::Skipped,
        }
    }
}

fn begin_job(
    app: &App,
    job_id: &str,
    chat_id: &str,
    bot_id: &str,
    routine_id: Option<String>,
    runner_id: Option<String>,
    cancel: CancellationToken,
) -> bool {
    let mut running = app.running_jobs.lock().unwrap();
    if running.contains_key(job_id) {
        return false;
    }
    // A job on another Runner outlives this process; `resume_sent_jobs` picks up its wait.
    if let Some(runner_id) = &runner_id {
        let sent = SentJob {
            id: job_id.to_string(),
            chat_id: chat_id.to_string(),
            bot_id: bot_id.to_string(),
            routine_id: routine_id.clone(),
            runner_id: runner_id.clone(),
            sent_at: now_secs(),
        };
        if let Err(error) = app.store.insert_sent_job(&sent) {
            tracing::error!(%error, %job_id, "keeping a job sent to another Runner");
        }
    }
    running.insert(
        job_id.to_string(),
        RunningJob {
            chat_id: chat_id.to_string(),
            bot_id: bot_id.to_string(),
            routine_id,
            runner_id,
            cancel,
            activity: None,
        },
    );
    drop(running);
    app.local_turns_changed();
    true
}

fn finish_job(app: &App, job_id: &str) {
    let job = app.running_jobs.lock().unwrap().remove(job_id);
    if job.is_some_and(|job| job.runner_id.is_some()) {
        if let Err(error) = app.store.remove_sent_job(job_id) {
            tracing::error!(%error, %job_id, "dropping a job sent to another Runner");
        }
    }
    app.local_turns_changed();
}

/// Registers a room before it waits for the chat lock. A later message is admitted behind it
/// and takes over at the next member boundary.
fn start_room(app: Arc<App>, chat_id: String, trigger: String, members: Vec<Bot>) {
    if members.is_empty() {
        return;
    }
    let room_id = format!("room-{}", uuid::Uuid::new_v4());
    let cancel = CancellationToken::new();
    begin_job(&app, &room_id, &chat_id, "", None, None, cancel.clone());
    tokio::spawn(run_room(app, chat_id, trigger, members, room_id, cancel));
}

/// Whether another user instruction follows this room's trigger in the durable transcript.
fn has_newer_user_message(app: &App, chat_id: &str, trigger: &str) -> bool {
    if app.chat(chat_id).is_none() {
        return false;
    }
    app.store
        .messages_after(chat_id, trigger)
        .unwrap_or_default()
        .iter()
        .any(|message| message.author == Author::You && message.is_complete())
}

/// One group exchange: each member gets a turn in order and either speaks or passes; the room
/// goes another round while anyone spoke, and the last round is marked as winding down. The
/// room holds the chat lock, so turns in one group never overlap. Stop cancels it immediately;
/// a new user message lets the current member settle, then hands the lock to its replacement.
async fn run_room(
    app: Arc<App>,
    chat_id: String,
    trigger: String,
    members: Vec<Bot>,
    room_id: String,
    cancel: CancellationToken,
) {
    let lock = app.chat_lock(&chat_id);
    let _guard = lock.lock().await;
    if has_newer_user_message(&app, &chat_id, &trigger) {
        finish_job(&app, &room_id);
        return;
    }

    // What each member had seen from others when it last took a turn: a member with nothing
    // new is not offered another turn.
    let mut seen: std::collections::HashMap<String, usize> = std::collections::HashMap::new();
    'rounds: for round in 1..=MAX_ROOM_ROUNDS {
        let mut any_sent = false;
        for bot in &members {
            if cancel.is_cancelled() || has_newer_user_message(&app, &chat_id, &trigger) {
                break 'rounds;
            }
            let Some(chat) = app.chat(&chat_id) else { break 'rounds };
            if !chat.meta.bot_ids.contains(&bot.id) {
                continue;
            }
            if round > 1 && seen.get(&bot.id) == Some(&heard_count(&app, &chat.meta.id, &bot.id)) {
                continue;
            }
            let job = Job {
                agent_message: None,
                id: format!("job-{}", uuid::Uuid::new_v4()),
                chat_id: chat_id.clone(),
                bot_id: bot.id.clone(),
                kind: "room_turn".into(),
                check: None,
                task_id: None,
                task_context: None,
                trigger_message_id: trigger.clone(),
                handoff: None,
                routine_id: None,
                requested_by: app.this_device_id().unwrap_or_default(),
                from_bot_id: None,
                hops: 0,
                round,
                is_winding_down: round == MAX_ROOM_ROUNDS,
                setup: None,
                created_at: now_secs(),
            };
            let outcome = run_member_turn(&app, job, &cancel).await;
            if has_newer_user_message(&app, &chat_id, &trigger) {
                break 'rounds;
            }
            if let Some(chat) = app.chat(&chat_id) {
                seen.insert(bot.id.clone(), heard_count(&app, &chat.meta.id, &bot.id));
            }
            if outcome == TurnOutcome::Sent {
                any_sent = true;
            }
        }
        if !any_sent {
            break;
        }
    }

    finish_job(&app, &room_id);
}

/// Messages in the chat that `bot_id` did not write itself.
fn heard_count(app: &App, chat_id: &str, bot_id: &str) -> usize {
    app.store.heard_count(chat_id, bot_id).unwrap_or(0)
}

/// Runs one member's turn here or on its Runner and waits for the outcome.
async fn run_member_turn(app: &Arc<App>, job: Job, room_cancel: &CancellationToken) -> TurnOutcome {
    let Some(bot) = app.bot(&job.bot_id) else { return TurnOutcome::Skipped };
    if app.this_device_id().as_deref() == Some(bot.runner_id.as_str()) {
        return run_job_here(app, job, room_cancel.child_token()).await;
    }
    remote_turn(app, job, bot.runner_id, room_cancel.child_token()).await
}

/// Seals a job to the bot's Runner and waits for its `job_result`. The job counts as running
/// here meanwhile, so the app shows the bot at work and can steer or stop it.
async fn remote_turn(
    app: &Arc<App>,
    job: Job,
    runner_id: String,
    cancel: CancellationToken,
) -> TurnOutcome {
    begin_job(
        app,
        &job.id,
        &job.chat_id,
        &job.bot_id,
        job.routine_id.clone(),
        Some(runner_id),
        cancel.clone(),
    );
    remote_turn_started(app, job, cancel).await
}

/// A remote turn whose running record already exists. `start_turn` registers synchronously so
/// a hard Stop immediately afterwards can still cancel it before dispatch.
async fn remote_turn_started(
    app: &Arc<App>,
    job: Job,
    cancel: CancellationToken,
) -> TurnOutcome {
    let result = expect_result(app, &job.id);
    let job_id = job.id.clone();
    let outcome = if cancel.is_cancelled() {
        TurnOutcome::Skipped
    } else {
        match dispatch_job(app, job) {
            Dispatch::Sent => wait_for_result(result, REMOTE_TURN_TIMEOUT, &cancel).await,
            Dispatch::Ran | Dispatch::Deferred => TurnOutcome::Skipped,
        }
    };
    app.pending_results.lock().unwrap().remove(&job_id);
    finish_job(app, &job_id);
    outcome
}

/// Registers the wait for a job's `job_result` before anything can deliver it.
fn expect_result(app: &App, job_id: &str) -> tokio::sync::oneshot::Receiver<String> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    app.pending_results.lock().unwrap().insert(job_id.to_string(), tx);
    rx
}

/// How a job on another Runner ended: its `job_result`, or skipped when the wait runs out or
/// Stop cancels it.
async fn wait_for_result(
    result: tokio::sync::oneshot::Receiver<String>,
    timeout: std::time::Duration,
    cancel: &CancellationToken,
) -> TurnOutcome {
    tokio::select! {
        result = result => result.map(|text| TurnOutcome::parse(&text)).unwrap_or(TurnOutcome::Skipped),
        _ = tokio::time::sleep(timeout) => TurnOutcome::Skipped,
        _ = cancel.cancelled() => TurnOutcome::Skipped,
    }
}

/// Picks up the jobs this Device sent to other Runners before it last stopped: each counts as
/// running again until its `job_result` arrives, the rest of its wait runs out, or Stop cancels
/// it, so the apps show the bot at work across a restart. Called before the sync loop starts,
/// since the first pull may carry a result that landed meanwhile. A group exchange this Device
/// was running does not resume; only the member turn in flight shows.
pub fn resume_sent_jobs(app: &Arc<App>) {
    if let Err(error) = crate::handoffs::resume(app) {
        tracing::error!(%error, "recovering handoffs");
    }
    let jobs = match app.store.sent_jobs() {
        Ok(jobs) => jobs,
        Err(error) => {
            tracing::error!(%error, "reading the jobs sent to other Runners");
            return;
        }
    };
    let now = now_secs();
    for job in jobs {
        let left = job.sent_at + REMOTE_TURN_TIMEOUT.as_secs_f64() - now;
        if left <= 0.0 || app.chat(&job.chat_id).is_none() {
            if let Err(error) = app.store.remove_sent_job(&job.id) {
                tracing::error!(%error, job_id = %job.id, "dropping a job sent to another Runner");
            }
            continue;
        }
        let cancel = CancellationToken::new();
        app.running_jobs.lock().unwrap().insert(
            job.id.clone(),
            RunningJob {
                chat_id: job.chat_id.clone(),
                bot_id: job.bot_id.clone(),
                routine_id: job.routine_id.clone(),
                runner_id: Some(job.runner_id.clone()),
                cancel: cancel.clone(),
                activity: None,
            },
        );
        let result = expect_result(app, &job.id);
        let app = app.clone();
        tokio::spawn(async move {
            wait_for_result(result, std::time::Duration::from_secs_f64(left), &cancel).await;
            app.pending_results.lock().unwrap().remove(&job.id);
            finish_job(&app, &job.id);
        });
    }
    app.turns_changed();
}

/// A `job_result` from another Runner reached this Device.
pub fn deliver_job_result(app: &Arc<App>, from: &str, result: JobResult) {
    // Only the Runner the job went to says how it ended.
    let sent_there = app.running_jobs.lock().unwrap().get(&result.job_id).is_some_and(|job| job.runner_id.as_deref() == Some(from));
    if !sent_there {
        tracing::warn!(job_id = %result.job_id, "a job result from a Device the job was not sent to");
        return;
    }
    if let Some(tx) = app.pending_results.lock().unwrap().remove(&result.job_id) {
        let _ = tx.send(result.outcome);
    }
}

// MARK: - Jobs

/// Starts a turn wherever the bot runs: here in the background, or on its Runner with the
/// wait for the result tracked here.
pub fn start_turn(app: &Arc<App>, job: Job) {
    let Some(bot) = app.bot(&job.bot_id) else { return };
    if app.this_device_id().as_deref() == Some(bot.runner_id.as_str()) {
        spawn_local_job(app.clone(), job, None);
    } else {
        let cancel = CancellationToken::new();
        begin_job(
            app,
            &job.id,
            &job.chat_id,
            &job.bot_id,
            job.routine_id.clone(),
            Some(bot.runner_id),
            cancel.clone(),
        );
        let app = app.clone();
        tokio::spawn(async move {
            remote_turn_started(&app, job, cancel).await;
        });
    }
}

pub enum Dispatch {
    /// Runs on this Device.
    Ran,
    /// Sealed to an online Runner through the relay.
    Sent,
    /// Parked on the relay for an offline Runner, or could not be addressed (a notice says why).
    Deferred,
}

/// Runs the job here when the bot's Runner is this Device; otherwise seals it to that Runner.
pub fn dispatch_job(app: &Arc<App>, job: Job) -> Dispatch {
    let Some(bot) = app.bot(&job.bot_id) else { return Dispatch::Deferred };
    if app.this_device_id().as_deref() == Some(bot.runner_id.as_str()) {
        spawn_local_job(app.clone(), job, None);
        return Dispatch::Ran;
    }
    match app.device(&bot.runner_id) {
        Some(runner) if !runner.box_pubkey.is_empty() => match crate::envelope::seal(app, "job", &runner, &job) {
            Ok(ciphertext) => {
                app.push_blob("job", Some(runner.id.clone()), ciphertext);
                if app.relay_url().is_none() {
                    app.notice(&job.chat_id, format!("{} runs on {}, but no relay is configured, so this turn cannot leave this Device.", bot.name, runner.name));
                    Dispatch::Deferred
                } else if !app.device_is_online(&runner.id) {
                    app.notice(&job.chat_id, format!("{} runs on {}, which is offline. This turn waits on the relay until it reconnects.", bot.name, runner.name));
                    Dispatch::Deferred
                } else {
                    Dispatch::Sent
                }
            }
            Err(error) => {
                app.notice(&job.chat_id, format!("Could not address the job to {}: {error}", runner.name));
                Dispatch::Deferred
            }
        },
        _ => {
            app.notice(&job.chat_id, format!("{} is assigned to a Runner this Device does not know yet.", bot.name));
            Dispatch::Deferred
        }
    }
}

/// Runs a job on this Runner in the background. Turns in one chat run one at a time; the
/// outcome goes back to the requesting Device when the job came from another one. It registers
/// before taking the lock, so hard Stop also cancels jobs waiting behind another turn.
pub fn spawn_local_job(app: Arc<App>, job: Job, remote_blob_id: Option<String>) {
    #[cfg(feature = "runner")]
    if job.kind == crate::model::AGENT_MESSAGE_JOB {
        crate::coding::from_job(&app, &job);
        if let Some(id) = remote_blob_id {
            tokio::spawn(async move { crate::sync::delete_remote_blob(&app, &id).await });
        }
        return;
    }
    if job.kind == "task" {
        match crate::tasks::claim(&app, &job) {
            Ok(true) => {},
            result => {
                if let Err(error) = result { app.notice(&job.chat_id, format!("Task run did not start: {error}")); }
                if let Some(id) = remote_blob_id { tokio::spawn(async move { crate::sync::delete_remote_blob(&app, &id).await; }); }
                return;
            }
        }
    }
    if let Err(error) = crate::handoffs::stage_job(&app, &job) {
        tracing::error!(%error, job_id = %job.id, "persisting handoff admission");
        return;
    }
    let cancel = CancellationToken::new();
    if !begin_job(
        &app,
        &job.id,
        &job.chat_id,
        &job.bot_id,
        job.routine_id.clone(),
        None,
        cancel.clone(),
    ) {
        if let Some(id) = remote_blob_id {
            tokio::spawn(async move { crate::sync::delete_remote_blob(&app, &id).await });
        }
        return;
    }
    tokio::spawn(async move {
        let lock = app.chat_lock(&job.chat_id);
        let _guard = lock.lock().await;
        if job.kind == "task" {
            match crate::tasks::admit(&app, &job).await {
                Ok(true) => {},
                result => {
                    if let Err(error) = result { app.notice(&job.chat_id, format!("Task run did not start: {error}")); }
                    finish_job(&app, &job.id);
                    crate::tasks::finished(&app, &job, TurnOutcome::Skipped).await;
                    if let Some(id) = remote_blob_id { crate::sync::delete_remote_blob(&app, &id).await; }
                    return;
                }
            }
        }
        let outcome = run_job_started(&app, job.clone(), cancel.clone()).await;
        if job.kind == "task" { crate::tasks::finished(&app, &job, if cancel.is_cancelled() { TurnOutcome::Skipped } else { outcome }).await; }
        // An event turn settles its delivery; it is not a run of the routine it targets.
        if job.kind == "event" {
            // The event turn of a routine on events is its run; the delivery's event is read
            // before it settles and drops its payload.
            #[cfg(feature = "runner")]
            let watched = job.routine_id.as_ref().and_then(|_| crate::event_triggers::delivery_event(&app, &job));
            #[cfg(feature = "runner")]
            if let Err(error) = crate::event_triggers::finished(&app, &job, outcome, cancel.is_cancelled() && !crate::handoffs::stopped_at_limits(&app, &job)) {
                tracing::error!(%error, "recording the event turn outcome");
            }
            #[cfg(feature = "runner")]
            if let (Some((subscription_id, payload)), Some(routine_id)) = (watched, job.routine_id.as_deref()) {
                if app.routine(routine_id).and_then(|routine| routine.events).is_some_and(|events| events.subscription_id == subscription_id) {
                    crate::routine_events::event_finished(&app, routine_id, &payload, outcome);
                }
            }
        } else if let Some(id) = &job.routine_id {
            crate::routines::finished(&app, id, outcome);
            if !cancel.is_cancelled() { crate::feedback::routine_outcome(&app, &job, outcome == TurnOutcome::Skipped || job.check.as_ref().is_some_and(|check|check.error.is_some())); }
        }
        if let Some(id) = remote_blob_id {
            crate::sync::delete_remote_blob(&app, &id).await;
        }
        if app.this_device_id().as_deref() != Some(job.requested_by.as_str()) {
            report_outcome(&app, &job, outcome);
        }
    });
}

/// Runs a member job inside an active room. The room already holds the chat lock.
async fn run_job_here(app: &Arc<App>, job: Job, cancel: CancellationToken) -> TurnOutcome {
    begin_job(
        app,
        &job.id,
        &job.chat_id,
        &job.bot_id,
        job.routine_id.clone(),
        None,
        cancel.clone(),
    );
    run_job_started(app, job, cancel).await
}

/// Runs a job with its working record already installed.
async fn run_job_started(app: &Arc<App>, job: Job, cancel: CancellationToken) -> TurnOutcome {
    crate::workflows::sample_started(app, &job);
    let admitted = match crate::handoffs::begin_job(app, &job) {
        Ok(admitted) => admitted,
        Err(error) => {
            app.notice(&job.chat_id, format!("Could not start handoff: {error}"));
            false
        }
    };
    let report_cancel = cancel.clone();
    #[cfg(feature = "runner")]
    let outcome = if !admitted || cancel.is_cancelled()
        || (job.kind == "turn"
            && app.take_steering_message(&job.chat_id, &job.trigger_message_id))
    {
        TurnOutcome::Skipped
    } else {
        // A message a turn held and never read: this turn reads it now.
        if job.kind == "turn" {
            app.set_queued(&job.chat_id, &job.trigger_message_id, false);
        }
        crate::turns::run_job(app, &job, cancel).await
    };
    #[cfg(not(feature = "runner"))]
    let outcome = {
        let _ = admitted;
        let _ = cancel;
        app.notice(&job.chat_id, "This Device does not run bots; assign the bot to a Runner.");
        TurnOutcome::Skipped
    };
    crate::workflows::sample_finished(app, &job, outcome);
    if let Err(error) = crate::handoffs::finish_job(app, &job, outcome, report_cancel.is_cancelled()) {
        tracing::error!(%error, job_id = %job.id, "reporting handoff outcome");
    }
    if report_cancel.is_cancelled() {
        crate::handoffs::stop_handed_off(app, &job);
    }
    finish_job(app, &job.id);
    outcome
}

fn report_outcome(app: &Arc<App>, job: &Job, outcome: TurnOutcome) {
    let Some(requester) = app.device(&job.requested_by).filter(|d| !d.box_pubkey.is_empty()) else { return };
    let result = JobResult { job_id: job.id.clone(), chat_id: job.chat_id.clone(), bot_id: job.bot_id.clone(), outcome: outcome.as_str().into() };
    match crate::envelope::seal(app, "job_result", &requester, &result) {
        Ok(ciphertext) => {
            app.push_blob("job_result", Some(requester.id.clone()), ciphertext);
        }
        Err(error) => tracing::warn!(%error, "sealing the job result"),
    }
}

/// What a turn running here is doing that no message says: it goes into this Device's machine
/// blob, so every app shows it, and the local app hears it at once.
pub fn report_activity(app: &App, job: &Job, activity: JobActivity) {
    app.set_job_activity(&job.id, activity);
}

/// Bot names are not in the chat struct; they come from this App's roster as it is now, and a
/// bot no longer on it is named by its id.
pub fn name_of(app: &App, bot_id: &str) -> String {
    app.state.lock().unwrap().bots.iter().find(|b| b.id == bot_id).map(|b| b.name.clone()).unwrap_or_else(|| bot_id.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::Config;
    use crate::events::Event;

    struct ScratchApp(Arc<App>, std::path::PathBuf);

    impl Drop for ScratchApp {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.1);
        }
    }

    fn scratch_app() -> ScratchApp {
        let home = std::env::temp_dir().join(format!("lorca-runtime-{}", uuid::Uuid::new_v4()));
        let app = App::load(Config { home: home.clone(), port: 0 }).unwrap();
        ScratchApp(app, home)
    }

    fn empty_chat(id: &str) -> Chat {
        Chat {
            meta: ChatMeta {
                id: id.into(),
                kind: "dm".into(),
                title: None,
                bot_ids: Vec::new(),
                owner_bot_id: None,
                description: None,
                is_pinned: false,
                section_id: None,
                is_hidden: false,
                mute: None,
                created_at: 1.0, channel: None,
            },
            unread_count: 0,
            usage: None,
            compactions: Vec::new(),
        }
    }

    fn bot(id: &str, name: &str) -> Bot {
        Bot {
            id: id.into(),
            name: name.into(),
            description: String::new(),
            symbol_name: String::new(),
            accent: String::new(),
            avatar: None,
            runner_id: "dev".into(),
            provider: "deepseek".into(),
            model: None,
            thinking: None,
            legacy_instructions: String::new(), setting_up: false,
            workdir: None,
            permissions: None,
            created_at: 0.0,
        }
    }

    /// A Runner with an identity, its first bot, and that bot's DM with an exchange in it: the
    /// user's message, then the bot's reply and a notice.
    fn exchange() -> (ScratchApp, Bot, String, Message) {
        let scratch = scratch_app();
        let app = &scratch.0;
        crate::identity::create(app, Some("Workbench".into())).unwrap();
        let bot = app.state.lock().unwrap().bots[0].clone();
        let chat_id = app.dm_with(&bot.id, None).unwrap().meta.id;
        let asked = Message::new(&chat_id, Author::You, Body::text("Plan the launch"));
        app.upsert_message(asked.clone(), false);
        app.upsert_message(Message::new(&chat_id, Author::Bot { bot_id: bot.id.clone() }, Body::text("Here is a plan.")), false);
        app.upsert_message(Message::new(&chat_id, Author::System, Body::Notice { text: "Used 2k tokens".into(), routine_id: None }), false);
        (scratch, bot, chat_id, asked)
    }

    /// Regenerate takes the reply and everything after the user's message out, and starts a turn
    /// on that message; a second one waits for that turn.
    #[tokio::test]
    async fn a_reply_runs_again_without_the_old_one() {
        let (scratch, _bot, chat_id, asked) = exchange();
        let app = &scratch.0;
        let message = rerun_user_message(app, &chat_id, &asked.id, None).unwrap();
        assert_eq!((message.id.as_str(), message.edited_at), (asked.id.as_str(), None));
        let ids: Vec<String> = app.messages(&chat_id).into_iter().map(|message| message.id).collect();
        assert_eq!(ids.first(), Some(&asked.id));
        assert!(app.messages(&chat_id).iter().all(|message| !matches!(&message.body, Body::Text { text, .. } if text == "Here is a plan.")), "the old reply is gone");
        assert!(app.running_chat_ids().contains(&chat_id), "a new turn answers it");
        assert_eq!(rerun_user_message(app, &chat_id, &asked.id, None).unwrap_err(), "Stop the reply first.");
    }

    /// Edit and send again: the message takes the new words and says when; the reply goes.
    #[tokio::test]
    async fn an_edited_message_is_sent_again() {
        let (scratch, _bot, chat_id, asked) = exchange();
        let app = &scratch.0;
        let message = rerun_user_message(app, &chat_id, &asked.id, Some("  Plan the launch for Friday ")).unwrap();
        assert!(matches!(&message.body, Body::Text { text, .. } if text == "Plan the launch for Friday"));
        assert!(message.edited_at.is_some());
        let kept = app.message(&chat_id, &asked.id).unwrap();
        assert_eq!(kept.edited_at, message.edited_at);
        assert!(app.messages(&chat_id).iter().all(|message| !matches!(message.body, Body::Notice { .. })));
    }

    /// Only the latest message of the user's runs again, and not when the reply left something
    /// the user still has to act on, or in a group.
    #[tokio::test]
    async fn what_cannot_run_again() {
        let (scratch, bot, chat_id, asked) = exchange();
        let app = &scratch.0;
        let later = Message::new(&chat_id, Author::You, Body::text("And the release notes"));
        app.upsert_message(later.clone(), false);
        assert_eq!(rerun_user_message(app, &chat_id, &asked.id, None).unwrap_err(), "Only your latest message runs again.");
        let reply = Message::new(&chat_id, Author::Bot { bot_id: bot.id.clone() }, Body::text("Noted."));
        assert_eq!(rerun_user_message(app, &chat_id, &reply.id, None).unwrap_err(), "Unknown message");
        app.upsert_message(reply.clone(), false);
        assert_eq!(rerun_user_message(app, &chat_id, &reply.id, None).unwrap_err(), "Only a message of yours runs again.");
        let mut tool = Message::new(&chat_id, Author::Bot { bot_id: bot.id.clone() }, Body::text("Asked Scout."));
        tool.body = serde_json::from_value(serde_json::json!({ "kind": "tool", "name": "message_bot", "summary": "Messaged Scout", "detail": "", "is_running": false, "call_id": "call-1", "arguments": null, "is_error": false, "target_bot_id": "bot-scout" })).unwrap();
        app.upsert_message(tool, false);
        assert_eq!(rerun_user_message(app, &chat_id, &later.id, None).unwrap_err(), "The reply left work handed to another bot, which running it again would lose.");
        assert_eq!(rerun_user_message(app, &chat_id, &later.id, Some(" ")).unwrap_err(), "The reply left work handed to another bot, which running it again would lose.");
        assert!(app.message(&chat_id, &reply.id).is_some(), "nothing was taken out");
    }

    #[test]
    fn a_message_mentions_bots_by_id() {
        let scratch = scratch_app();
        let app = &scratch.0;
        app.state.lock().unwrap().bots = vec![bot("b1", "Chef"), bot("b2", "Chef"), bot("b3", "Scout"), bot("b4", "Sc")];

        // A typed name that one bot answers to resolves; a shared one only when the user picked
        // which. Neither an email address nor the start of a longer name is a mention.
        assert_eq!(resolve_mentions(app, "@chef and @Scout, mail x@sc.com", Vec::new()), ["b3"]);
        assert_eq!(resolve_mentions(app, "@Chef and @Scout", vec!["b2".into(), "gone".into(), "b2".into()]), ["b2", "b3"]);

        let group = ChatMeta { kind: "group".into(), bot_ids: vec!["b1".into(), "b2".into(), "b3".into()], ..empty_chat("g").meta };
        let order: Vec<String> = turn_order(&group, app, &["b2".into()], None).into_iter().map(|bot| bot.id).collect();
        assert_eq!(order, ["b2", "b1", "b3"]);
        // The bot whose message the user answers comes after the ones the user mentions.
        let order: Vec<String> = turn_order(&group, app, &["b2".into()], Some("b3")).into_iter().map(|bot| bot.id).collect();
        assert_eq!(order, ["b2", "b3", "b1"]);
        let order: Vec<String> = turn_order(&group, app, &["b3".into()], Some("b3")).into_iter().map(|bot| bot.id).collect();
        assert_eq!(order, ["b3", "b1", "b2"]);
    }

    /// A reply keeps a one-line quote of the message it answers, and only a text message can be
    /// answered; a message that is not in the chat refuses the send.
    #[test]
    fn a_reply_quotes_the_message_it_answers() {
        let scratch = scratch_app();
        let app = &scratch.0;
        app.state.lock().unwrap().chats.push(empty_chat("chat"));
        let mut original = Message::new("chat", Author::Bot { bot_id: "b1".into() }, Body::text("The build   passes.\n\nShip it?"));
        original.id = "msg-original".into();
        app.upsert_message(original, false);

        let reply = send_user_message(app.clone(), "chat", "yes, ship it", None, Vec::new(), Vec::new(), Some("msg-original".into())).unwrap();
        let Body::Text { reply_to: Some(quote), .. } = &reply.body else { panic!("no quote: {:?}", reply.body) };
        assert_eq!(quote, &ReplyTo { message_id: "msg-original".into(), author: Author::Bot { bot_id: "b1".into() }, text: "The build passes. Ship it?".into(), agent: None });
        assert_eq!(app.message("chat", &reply.id).map(|stored| stored.body), Some(reply.body.clone()));

        let error = send_user_message(app.clone(), "chat", "and this?", None, Vec::new(), Vec::new(), Some("msg-gone".into())).unwrap_err();
        assert_eq!(error.to_string(), "The message you replied to is no longer in this chat");

        let list = Message::new("chat", Author::You, Body::text("The checklist:\n\n- **Onboarding** done\n- `site` [checked](https://x.y)\n\n> ship it"));
        assert_eq!(ReplyTo::quoting(&list).unwrap().text, "The checklist: Onboarding done site checked ship it");
        let long = Message::new("chat", Author::You, Body::text("word ".repeat(100)));
        let quote = ReplyTo::quoting(&long).unwrap();
        assert_eq!(quote.text.chars().count(), REPLY_QUOTE_CHARS);
        assert!(quote.text.ends_with('…'));
        let notice = Message::new("chat", Author::System, Body::Notice { text: "Compacted".into(), routine_id: None });
        assert_eq!(ReplyTo::quoting(&notice), None);
        let file = Message::new(
            "chat",
            Author::You,
            Body::Text {
                agents: Vec::new(),
                text: String::new(),
                attachments: vec![Attachment { id: "f".into(), name: "plan.pdf".into(), mime: "application/pdf".into(), size: 1, width: None, height: None, sha256: None }],
                mentions: Vec::new(),
                reply_to: None,
            },
        );
        assert_eq!(ReplyTo::quoting(&file).unwrap().text, "plan.pdf");
    }

    #[test]
    fn each_app_names_bots_from_its_own_roster() {
        let (one, two) = (scratch_app(), scratch_app());
        one.0.state.lock().unwrap().bots.push(bot("b2", "Scout"));
        two.0.state.lock().unwrap().bots.push(bot("b2", "Critic"));
        let group = Chat { meta: ChatMeta { kind: "group".into(), bot_ids: vec!["b2".into(), "gone".into()], ..empty_chat("g").meta }, ..empty_chat("g") };
        assert_eq!(chat_source(&one.0, &group), "group \"Scout, gone\"", "a bot off the roster is named by its id");
        assert_eq!(chat_source(&two.0, &group), "group \"Critic, gone\"");
    }

    #[test]
    fn a_new_user_message_does_not_abort_the_job_in_flight() {
        let scratch = scratch_app();
        let app = &scratch.0;
        app.state.lock().unwrap().chats.push(empty_chat("chat"));
        let cancel = CancellationToken::new();
        app.running_jobs.lock().unwrap().insert(
            "old".into(),
            RunningJob {
                chat_id: "chat".into(),
                bot_id: "bot".into(),
                routine_id: None,
                runner_id: None,
                cancel: cancel.clone(),
                activity: None,
            },
        );

        let message =
            send_user_message(app.clone(), "chat", "change course", None, Vec::new(), Vec::new(), None).unwrap();

        assert!(!cancel.is_cancelled());
        assert_eq!(message.author, Author::You);
        assert_eq!(
            app.messages("chat")
                .last()
                .map(|message| message.id.as_str()),
            Some(message.id.as_str())
        );
    }

    #[test]
    fn cancellation_is_sealed_to_the_remote_runner() {
        let scratch = scratch_app();
        let app = &scratch.0;
        // The cancellation is signed as this Device.
        crate::identity::create(app, None).unwrap();
        let runner_keys = crate::keys::Machine::generate();
        app.state.lock().unwrap().devices.push(Device {
            agent_messages: false,
            id: "runner".into(),
            name: "Runner".into(),
            model: String::new(),
            os: "macos".into(),
            os_version: String::new(),
            box_pubkey: runner_keys.box_pubkey(),
            plugins: Vec::new(), channels: Vec::new(),
            version: String::new(),
            update: None,
            updated_at: 1,
        });
        let cancel = CancellationToken::new();
        app.running_jobs.lock().unwrap().insert(
            "remote-job".into(),
            RunningJob {
                chat_id: "chat".into(),
                bot_id: "bot".into(),
                routine_id: None,
                runner_id: Some("runner".into()),
                cancel: cancel.clone(),
                activity: None,
            },
        );

        cancel_chat(app, "chat");

        assert!(cancel.is_cancelled());
        let queued = app.store.last_outbox().unwrap().unwrap();
        assert_eq!(queued.kind, "job_cancel");
        assert_eq!(queued.recipient.as_deref(), Some("runner"));
        let ciphertext = queued.ciphertext;
        let payload: JobCancel =
            crate::envelope::body_of(&runner_keys.box_secret, &ciphertext);
        assert_eq!(payload.job_id, "remote-job");
    }

    fn turn_on_mac(job_id: &str, chat_id: &str) -> LiveTurn {
        LiveTurn { job_id: job_id.into(), chat_id: chat_id.into(), bot_id: "bot".into(), routine_id: None, activity: Some(JobActivity::Thinking) }
    }

    #[test]
    fn another_devices_turns_show_while_the_relay_lists_it_online() {
        let scratch = scratch_app();
        let app = &scratch.0;
        let mut events = app.events.subscribe();
        let turn = turn_on_mac("mac-job", "chat");

        // Listed while the Mac is not online: nothing shows.
        app.set_device_turns("mac", vec![turn.clone()]);
        assert!(app.running_turns().is_empty());
        assert!(events.try_recv().is_err());

        app.state.lock().unwrap().turns_online.insert("mac".into());
        app.turns_changed();
        assert!(matches!(events.try_recv(), Ok(Event::JobStarted { job_id, chat_id, bot_id, .. }) if job_id == "mac-job" && chat_id == "chat" && bot_id == "bot"));
        assert!(matches!(events.try_recv(), Ok(Event::JobThinking { chat_id, bot_id }) if chat_id == "chat" && bot_id == "bot"));
        assert_eq!(app.running_turns().len(), 1);

        // The same list again says nothing new; another activity does.
        app.set_device_turns("mac", vec![turn.clone()]);
        assert!(events.try_recv().is_err());
        let retry = JobActivity::Retry { attempt: 1, max_attempts: 3, delay_ms: 2000, error: "overloaded".into() };
        app.set_device_turns("mac", vec![LiveTurn { activity: Some(retry), ..turn.clone() }]);
        assert!(matches!(events.try_recv(), Ok(Event::JobRetry { attempt: 1, max_attempts: 3, delay_ms: 2000, .. })));

        // It ends when the Mac lists it no more.
        app.set_device_turns("mac", Vec::new());
        assert!(matches!(events.try_recv(), Ok(Event::JobFinished { job_id, .. }) if job_id == "mac-job"));

        // The list outlives a restart, and shows again once the relay says the Mac is online.
        app.set_device_turns("mac", vec![turn.clone()]);
        let restarted = App::load(Config { home: scratch.1.clone(), port: 0 }).unwrap();
        assert!(restarted.running_turns().is_empty());
        restarted.state.lock().unwrap().turns_online.insert("mac".into());
        assert_eq!(restarted.running_turns().len(), 1);

        // Offline, it ends.
        while events.try_recv().is_ok() {}
        app.state.lock().unwrap().turns_online.clear();
        app.turns_changed();
        assert!(matches!(events.try_recv(), Ok(Event::JobFinished { job_id, .. }) if job_id == "mac-job"));
        assert!(app.running_turns().is_empty());
    }

    #[test]
    fn a_job_sent_to_a_runner_that_lists_it_is_one_turn_until_both_let_go() {
        let scratch = scratch_app();
        let app = &scratch.0;
        app.state.lock().unwrap().chats.push(empty_chat("chat"));
        let mut events = app.events.subscribe();
        begin_job(app, "job", "chat", "bot", None, Some("mac".into()), CancellationToken::new());
        assert!(matches!(events.try_recv(), Ok(Event::JobStarted { job_id, .. }) if job_id == "job"));
        // A job sent elsewhere is that Runner's to list.
        assert!(app.turns_here().is_empty());

        app.state.lock().unwrap().turns_online.insert("mac".into());
        app.set_device_turns("mac", vec![turn_on_mac("job", "chat")]);
        assert!(matches!(events.try_recv(), Ok(Event::JobThinking { .. })));
        assert_eq!(app.running_turns().len(), 1);

        // The wait here runs out while the Runner still lists the turn: it is still working.
        finish_job(app, "job");
        assert!(events.try_recv().is_err());
        assert_eq!(app.running_turns().len(), 1);
        app.set_device_turns("mac", Vec::new());
        assert!(matches!(events.try_recv(), Ok(Event::JobFinished { job_id, .. }) if job_id == "job"));
        assert!(app.running_turns().is_empty());
    }

    #[test]
    fn stop_is_sealed_to_the_device_that_lists_the_turn() {
        let scratch = scratch_app();
        let app = &scratch.0;
        // The cancellation is signed as this Device.
        crate::identity::create(app, None).unwrap();
        let mac_keys = crate::keys::Machine::generate();
        app.state.lock().unwrap().devices.push(Device {
            agent_messages: false,
            id: "mac".into(),
            name: "Mac".into(),
            model: String::new(),
            os: "macos".into(),
            os_version: String::new(),
            box_pubkey: mac_keys.box_pubkey(),
            plugins: Vec::new(), channels: Vec::new(),
            version: String::new(),
            update: None,
            updated_at: 1,
        });
        app.state.lock().unwrap().turns_online.insert("mac".into());
        app.set_device_turns("mac", vec![turn_on_mac("mac-job", "chat"), turn_on_mac("elsewhere", "other-chat")]);

        cancel_chat(app, "chat");

        let cancels: Vec<_> = app.store.outbox().unwrap().into_iter().filter(|item| item.kind == "job_cancel").collect();
        let [cancel] = cancels.try_into().unwrap();
        assert_eq!(cancel.recipient.as_deref(), Some("mac"));
        let payload: JobCancel = crate::envelope::body_of(&mac_keys.box_secret, &cancel.ciphertext);
        assert_eq!(payload.job_id, "mac-job");
    }

    #[test]
    fn a_machine_blob_that_only_lists_other_turns_leaves_the_roster_alone() {
        let scratch = scratch_app();
        let app = &scratch.0;
        crate::identity::create(app, Some("Phone".into())).unwrap();
        let machine_file = app.machine_file().unwrap();
        let dek = machine_file.dek().unwrap();
        let mac = Device {
            agent_messages: true,
            id: "mac".into(),
            name: "Mac".into(),
            model: "MacBook Air (M5)".into(),
            os: "macos".into(),
            os_version: "26.0".into(),
            box_pubkey: String::new(),
            plugins: Vec::new(), channels: Vec::new(),
            version: String::new(),
            update: None,
            updated_at: 1,
        };
        app.state.lock().unwrap().device_seen.insert("mac".into(), 1);
        app.state.lock().unwrap().turns_online.insert("mac".into());
        let blob = |seq: i64, device: Device, turns: Vec<LiveTurn>| crate::relay::BlobIn {
            id: format!("blob-{seq}"),
            kind: "machine".into(),
            recipient_machine_pubkey: None,
            seq,
            ciphertext: crate::keys::b64(&crate::crypto::encrypt_json(&dek, "machine", &MachineBlob { device, turns, budgets: Vec::new(), watching: None }).unwrap()),
            created_at: 0,
        };
        let mut events = app.events.subscribe();

        crate::sync::apply_blob(app, &machine_file, &blob(1, mac.clone(), Vec::new()));
        assert!(std::iter::from_fn(|| events.try_recv().ok()).any(|event| matches!(event, Event::RosterChanged { .. })));

        crate::sync::apply_blob(app, &machine_file, &blob(2, Device { updated_at: 2, ..mac.clone() }, vec![turn_on_mac("mac-job", "chat")]));
        let heard: Vec<Event> = std::iter::from_fn(|| events.try_recv().ok()).collect();
        assert!(!heard.iter().any(|event| matches!(event, Event::RosterChanged { .. })));
        assert!(heard.iter().any(|event| matches!(event, Event::JobStarted { job_id, .. } if job_id == "mac-job")));
    }

    #[tokio::test]
    async fn a_replacement_job_exits_after_its_message_was_steered() {
        let scratch = scratch_app();
        let app = &scratch.0;
        let chat = empty_chat("chat");
        let mut message = Message::new("chat", Author::You, Body::text("steer"));
        message.id = "message".into();
        app.state.lock().unwrap().chats.push(chat);
        app.upsert_message(message, false);
        assert!(app.claim_steering_message("chat", "message"));
        let job = Job {
            agent_message: None,
            id: "replacement".into(),
            chat_id: "chat".into(),
            bot_id: "bot".into(),
            check: None,
            kind: "turn".into(),
            task_id: None,
            task_context: None,
            trigger_message_id: "message".into(),
            handoff: None,
            routine_id: None,
            requested_by: String::new(),
            from_bot_id: None,
            hops: 0,
            round: 0,
            is_winding_down: false,
            setup: None,
            created_at: 1.0,
        };

        let outcome = run_job_started(app, job, CancellationToken::new()).await;

        assert_eq!(outcome, TurnOutcome::Skipped);
        assert!(app.take_steering_message("chat", "message"));
    }

    #[tokio::test]
    async fn a_job_sent_to_another_runner_runs_again_after_a_restart() {
        let scratch = scratch_app();
        let app = &scratch.0;
        app.state.lock().unwrap().chats.push(empty_chat("chat"));
        app.save_state_now();
        begin_job(app, "remote-job", "chat", "bot", None, Some("runner".into()), CancellationToken::new());

        // The process ends with the job in flight, and the next one opens the same folder.
        let restarted = App::load(Config { home: scratch.1.clone(), port: 0 }).unwrap();
        assert!(restarted.running_turns().is_empty());
        resume_sent_jobs(&restarted);
        let turns = restarted.running_turns();
        assert_eq!(turns.len(), 1);
        assert_eq!((turns[0]["job_id"].as_str(), turns[0]["chat_id"].as_str(), turns[0]["bot_id"].as_str()), (Some("remote-job"), Some("chat"), Some("bot")));

        // The result that lands later ends it there, and nothing is left to resume.
        let mut events = restarted.events.subscribe();
        deliver_job_result(&restarted, "runner", JobResult { job_id: "remote-job".into(), chat_id: "chat".into(), bot_id: "bot".into(), outcome: "sent".into() });
        let finished = tokio::time::timeout(std::time::Duration::from_secs(5), events.recv()).await.unwrap().unwrap();
        assert!(matches!(finished, Event::JobFinished { job_id, .. } if job_id == "remote-job"));
        assert!(restarted.running_turns().is_empty());
        assert!(restarted.store.sent_jobs().unwrap().is_empty());
    }

    #[tokio::test]
    async fn a_sent_job_past_its_wait_or_chat_does_not_come_back() {
        let scratch = scratch_app();
        let app = &scratch.0;
        app.state.lock().unwrap().chats.push(empty_chat("chat"));
        let expired = SentJob {
            id: "expired".into(),
            chat_id: "chat".into(),
            bot_id: "bot".into(),
            routine_id: None,
            runner_id: "runner".into(),
            sent_at: now_secs() - REMOTE_TURN_TIMEOUT.as_secs_f64() - 1.0,
        };
        app.store.insert_sent_job(&expired).unwrap();
        app.store.insert_sent_job(&SentJob { id: "orphan".into(), chat_id: "deleted".into(), sent_at: now_secs(), ..expired.clone() }).unwrap();

        resume_sent_jobs(app);

        assert!(app.running_turns().is_empty());
        assert!(app.store.sent_jobs().unwrap().is_empty());
    }
}

#[cfg(test)]
mod agent_send_tests {
    use super::*;
    use crate::model::{AgentMessage, AgentRun, Body, Device, AGENT_MESSAGE_JOB};
    use lorca_agent::{AgentMessageQueue, QueueMode};

    struct Scratch(Arc<App>, std::path::PathBuf);

    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.1);
        }
    }

    fn scratch() -> Scratch {
        let home = std::env::temp_dir().join(format!("lorca-agent-send-{}", uuid::Uuid::new_v4()));
        let app = App::load(crate::config::Config { home: home.clone(), port: 0 }).unwrap();
        Scratch(app, home)
    }

    fn bot(id: &str, runner_id: &str) -> crate::model::Bot {
        crate::model::Bot {
            id: id.into(),
            name: "Chef".into(),
            description: String::new(),
            symbol_name: String::new(),
            accent: String::new(),
            avatar: None,
            runner_id: runner_id.into(),
            provider: "deepseek".into(),
            model: None,
            thinking: None,
            legacy_instructions: String::new(),
            setting_up: false,
            workdir: None,
            permissions: None,
            created_at: 0.0,
        }
    }

    fn chat(bot_id: &str) -> crate::model::Chat {
        crate::model::Chat {
            meta: crate::model::ChatMeta {
                id: "chat".into(),
                kind: "dm".into(),
                title: None,
                bot_ids: vec![bot_id.into()],
                owner_bot_id: None,
                description: None,
                is_pinned: false,
                section_id: None,
                is_hidden: false,
                mute: None,
                created_at: 1.0,
                channel: None,
            },
            unread_count: 0,
            usage: None,
            compactions: Vec::new(),
        }
    }

    fn agent_card(app: &Arc<App>, bot_id: &str, id: &str, name: &str) {
        let run = AgentRun { id: id.into(), kind: "claude".into(), name: name.into(), state: "idle".into(), ..AgentRun::default() };
        let card = Message::new(
            "chat",
            Author::Bot { bot_id: bot_id.into() },
            Body::Tool {
                name: "coding_agent".into(),
                summary: "Claude Code".into(),
                detail: String::new(),
                is_running: false,
                call_id: "call-1".into(),
                arguments: serde_json::json!({}),
                result: None,
                is_error: false,
                description: None,
                target_bot_id: None,
                script_command: None,
                run: None,
                agent: Some(run),
            },
        );
        app.upsert_message(card, false);
    }

    fn setup() -> (Scratch, Arc<App>, crate::model::Bot) {
        let scratch = scratch();
        let app = scratch.0.clone();
        crate::identity::create(&app, Some("Workbench".into())).unwrap();
        let me = app.this_device_id().unwrap();
        let bot = bot("b1", &me);
        {
            let mut state = app.state.lock().unwrap();
            state.bots.push(bot.clone());
            state.chats.push(chat("b1"));
        }
        agent_card(&app, "b1", "agent-1", "Claude Code");
        (scratch, app, bot)
    }

    #[test]
    fn a_message_to_an_agent_starts_no_bot_turn() {
        let (_scratch, app, _bot) = setup();
        let message = send_user_message_with_agents(
            app.clone(),
            "chat",
            "@Claude Code status",
            None,
            Vec::new(),
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap();
        assert!(app.running_chat_ids().is_empty());
        assert_eq!(message.body.agents(), ["agent-1"]);
        assert!(app.message("chat", &message.id).is_some());
    }

    #[test]
    fn an_agent_message_job_carries_its_text_and_its_target() {
        let (_scratch, app, _bot) = setup();
        let job = agent_message_job(&app, "chat", "b1", "msg-1", "agent-1", "@Claude Code status");
        assert_eq!(job.kind, AGENT_MESSAGE_JOB);
        assert_eq!(job.trigger_message_id, "msg-1");
        assert_eq!(job.agent_message, Some(AgentMessage { id: "agent-1".into(), text: "@Claude Code status".into() }));
    }

    #[test]
    fn an_agent_directed_message_does_not_steer_the_bot_turn() {
        let (_scratch, app, _bot) = setup();
        let queue = AgentMessageQueue::new(QueueMode::All);
        app.register_steering_queue("chat", "job", queue.clone());
        let message = send_user_message_with_agents(
            app.clone(),
            "chat",
            "@Claude Code status",
            None,
            Vec::new(),
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap();
        assert!(queue.is_empty(), "the bot's turn is not steered");
        assert!(!app.message("chat", &message.id).unwrap().queued);
    }

    #[test]
    fn an_agent_directed_message_does_not_dismiss_the_bots_question() {
        let (_scratch, app, _bot) = setup();
        let (tx, _rx) = tokio::sync::oneshot::channel();
        app.pending_permissions.lock().unwrap().insert("ask-1".into(), ("chat".into(), tx));
        send_user_message_with_agents(
            app.clone(),
            "chat",
            "@Claude Code status",
            None,
            Vec::new(),
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap();
        assert!(app.pending_permissions.lock().unwrap().contains_key("ask-1"), "the question still waits");
    }

    #[test]
    fn an_agent_directed_message_leaves_the_bot_turn_to_finish() {
        let (_scratch, app, _bot) = setup();
        let queue = AgentMessageQueue::new(QueueMode::All);
        app.register_steering_queue("chat", "job", queue.clone());
        let cancel = tokio_util::sync::CancellationToken::new();
        app.running_jobs.lock().unwrap().insert(
            "job".into(),
            crate::app::RunningJob {
                chat_id: "chat".into(),
                bot_id: "b1".into(),
                routine_id: None,
                runner_id: None,
                cancel: cancel.clone(),
                activity: None,
            },
        );
        send_user_message_with_agents(
            app.clone(),
            "chat",
            "@Claude Code status",
            None,
            Vec::new(),
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap();
        assert!(!cancel.is_cancelled(), "the bot turn is not stopped");
        assert!(queue.is_empty(), "the bot turn is not steered");
        assert!(!app.messages("chat").iter().any(|message| matches!(&message.body, Body::Notice { .. })), "no “stopped without replying” notice");
    }

    #[test]
    fn a_message_to_an_agent_needs_words() {
        let (_scratch, app, _bot) = setup();
        let error = send_user_message_with_agents(
            app.clone(),
            "chat",
            "",
            None,
            vec![Attachment { id: "f".into(), name: "plan.pdf".into(), mime: "application/pdf".into(), size: 1, width: None, height: None, sha256: None }],
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap_err();
        assert!(error.to_string().contains("Write a message for the coding agent"), "{error}");
    }

    #[test]
    fn an_unknown_agent_is_refused() {
        let (_scratch, app, _bot) = setup();
        let error = send_user_message_with_agents(app, "chat", "status", None, Vec::new(), Vec::new(), vec!["agent-nope".into()], None).unwrap_err();
        assert!(error.to_string().contains("Unknown coding agent"), "{error}");
    }

    #[test]
    fn an_old_runner_is_not_sent_an_agent_message() {
        let (_scratch, app, _bot) = setup();
        app.state.lock().unwrap().devices.push(Device { id: "old".into(), name: "Old".into(), os: "macos".into(), ..Device::default() });
        {
            let mut state = app.state.lock().unwrap();
            for bot in &mut state.bots {
                if bot.id == "b1" {
                    bot.runner_id = "old".into();
                }
            }
        }
        let error = send_user_message_with_agents(app, "chat", "status", None, Vec::new(), Vec::new(), vec!["agent-1".into()], None).unwrap_err();
        assert!(error.to_string().contains("Update Old"), "{error}");
    }

    #[test]
    fn an_agent_that_ended_meanwhile_keeps_the_message() {
        let (_scratch, app, _bot) = setup();
        {
            let mut card = app.messages("chat").into_iter().find(|message| matches!(&message.body, Body::Tool { agent: Some(_), .. })).unwrap();
            if let Body::Tool { agent: Some(run), .. } = &mut card.body {
                run.state = "stopped".into();
            }
            app.upsert_message(card, false);
        }
        let message = send_user_message_with_agents(
            app.clone(),
            "chat",
            "@Claude Code status",
            None,
            Vec::new(),
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap();
        assert!(app.running_chat_ids().is_empty());
        assert_eq!(message.body.agents(), ["agent-1"]);
    }

    #[test]
    fn a_message_to_an_agent_cannot_run_again() {
        let (_scratch, app, _bot) = setup();
        let message = send_user_message_with_agents(
            app.clone(),
            "chat",
            "@Claude Code status",
            None,
            Vec::new(),
            Vec::new(),
            vec!["agent-1".into()],
            None,
        )
        .unwrap();
        let error = rerun_user_message(&app, "chat", &message.id, None).unwrap_err();
        assert_eq!(error, "A message to a coding agent cannot run again.");
    }
}
