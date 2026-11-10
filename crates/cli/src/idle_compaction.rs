//! A chat compacted while nobody is using it, after Claude Code's idle compaction and pi's, so
//! a chat that has grown large is summarized before the user comes back to it instead of in the
//! middle of their next turn.
//!
//! A chat is due when its newest message is older than [`IDLE_SECS`], no turn of it is running
//! or starting, its context is larger than [`COMPACT_ABOVE`], and there is something past the
//! last summary to summarize. The summary is the one `chats.compact` writes
//! ([`crate::turns::compact_chat`]), so nothing new is introduced: the next turn starts from the
//! summary and the messages after it, exactly as if the user had pressed Compact.
//!
//! Compaction here costs no cache: a chat idle this long has lost its provider's prompt cache
//! already, so the request is paid for once instead of shortening every later turn. Nothing is
//! said in the chat; the inspector's Context row shows the result.

use std::collections::{HashMap, HashSet};
use std::sync::{Arc, LazyLock, Mutex};

use tokio_util::sync::CancellationToken;

use crate::app::App;
use crate::model::{Bot, Chat};

/// A chat whose newest message is older than this is idle.
pub const IDLE_SECS: i64 = 3 * 3_600;
/// What a turn of the chat sends before it is worth compacting. Below this a summary saves too
/// little to be worth the information it drops.
pub const COMPACT_ABOVE: u64 = 120_000;
/// The messages past the last summary a compaction needs; fewer is nothing to summarize.
const MIN_UNCOVERED: usize = 50;
/// How long a chat that was compacted is left alone, and one whose compaction failed.
const RECHECK_SECS: i64 = 6 * 3_600;
const FAILED_RECHECK_SECS: i64 = 30 * 60;
/// How often the check runs, in the scheduler's faster tick.
const CHECK_EVERY_SECS: i64 = 5 * 60;
/// The room a turn keeps free below the model's window for the summary and the reply, as
/// `turns::compaction_settings` does.
const RESERVE_TOKENS: u64 = 16_384;

/// When each chat may be checked again, by chat id: a chat that just compacted, or whose
/// compaction failed, is not tried again at once.
static NEXT_CHECK: LazyLock<Mutex<HashMap<String, i64>>> = LazyLock::new(Default::default);
/// The chats a compaction is running for, so a tick does not start a second one.
static RUNNING: LazyLock<Mutex<HashSet<String>>> = LazyLock::new(Default::default);
/// When the check last ran, so the scheduler's half-minute tick only looks every five minutes.
static CHECKED_AT: LazyLock<Mutex<i64>> = LazyLock::new(|| Mutex::new(0));

fn enabled() -> bool {
    std::env::var("LORCA_IDLE_COMPACTION").ok().as_deref() != Some("0")
}

/// Whether a chat is due at `now` (unix seconds). `context` is what its turns send, `window` the
/// model's context window (0 when it is not known), and `uncovered` the messages past the last
/// summary.
pub(crate) fn due(now: f64, newest_message_at: f64, context: u64, uncovered: usize, window: u64) -> bool {
    let idle = now - newest_message_at >= IDLE_SECS as f64;
    let large = context > COMPACT_ABOVE;
    // A window small enough that a turn compacts itself leaves nothing for an idle pass to save.
    let window_leaves_room = window == 0 || window > COMPACT_ABOVE + RESERVE_TOKENS;
    idle && large && uncovered >= MIN_UNCOVERED && window_leaves_room
}

/// The scheduler's hook, called on its half-minute tick: every five minutes, each idle chat of
/// this Runner is compacted once it is worth it.
pub fn tick(app: &Arc<App>) {
    if !enabled() {
        return;
    }
    let now = crate::config::now_secs();
    {
        let mut checked = CHECKED_AT.lock().unwrap();
        if now - (*checked as f64) < CHECK_EVERY_SECS as f64 {
            return;
        }
        *checked = now as i64;
    }
    let Some(this) = app.this_device_id() else { return };
    let running: HashSet<String> = app.running_chat_ids().into_iter().collect();
    let chats: Vec<Chat> = app.state.lock().unwrap().chats.clone();
    for chat in chats {
        if running.contains(&chat.meta.id) {
            continue;
        }
        if NEXT_CHECK.lock().unwrap().get(&chat.meta.id).is_some_and(|at| now < *at as f64) {
            continue;
        }
        if RUNNING.lock().unwrap().contains(&chat.meta.id) {
            continue;
        }
        // The bot that answers here, on this Runner. A group has several; its owner takes the
        // first turn, and every member's summary is its own.
        let Some(bot) = chat.meta.bot_ids.iter().filter_map(|id| app.bot(id)).find(|bot| bot.runner_id == this && !bot.setting_up) else {
            continue;
        };
        let (context, window) = chat.usage.as_ref().map(|usage| (usage.context_tokens, usage.context_window)).unwrap_or_default();
        // Nothing has run in the chat yet, so nothing says how large its turns are.
        if context == 0 {
            continue;
        }
        let Some(newest) = newest_message_at(app, &chat.meta.id) else { continue };
        let uncovered = uncovered_count(app, &chat, &bot);
        if !due(now, newest, context, uncovered, window) {
            continue;
        }
        // A turn may start between the running list and here: the chat lock is what a turn takes
        // first, so holding it means no turn can begin under the compaction.
        let Ok(guard) = app.chat_lock(&chat.meta.id).try_lock_owned() else { continue };
        later(&chat.meta.id, RECHECK_SECS);
        RUNNING.lock().unwrap().insert(chat.meta.id.clone());
        start(app.clone(), chat, bot, guard);
    }
}

fn start(app: Arc<App>, chat: Chat, bot: Bot, guard: tokio::sync::OwnedMutexGuard<()>) {
    tokio::spawn(async move {
        let _guard = guard;
        match compact(&app, &chat, &bot).await {
            Ok(Some(tokens_before)) => {
                later(&chat.meta.id, RECHECK_SECS);
                tracing::info!(bot = %bot.name, chat = %chat.meta.id, tokens_before, "compacted an idle chat");
            }
            // Nothing to summarize after all: the next turn's own rules decide, and the check
            // waits as after a compaction.
            Ok(None) => later(&chat.meta.id, RECHECK_SECS),
            // The limits refused it, or the provider failed: try again before long, but not on
            // every tick.
            Err(error) => {
                later(&chat.meta.id, FAILED_RECHECK_SECS);
                tracing::warn!(%error, bot = %bot.name, chat = %chat.meta.id, "compacting an idle chat");
            }
        }
        RUNNING.lock().unwrap().remove(&chat.meta.id);
    });
}

/// When this chat is looked at again, `secs` from now.
fn later(chat_id: &str, secs: i64) {
    NEXT_CHECK.lock().unwrap().insert(chat_id.to_string(), crate::config::now_secs() as i64 + secs);
}

async fn compact(app: &Arc<App>, chat: &Chat, bot: &Bot) -> Result<Option<u64>, String> {
    let job = crate::model::Job::local("idle_compaction", &chat.meta.id, &bot.id, "idle_compaction");
    // The call runs under the chat's limits as one of its turns would; refused, it waits for the
    // next check rather than being retried in a loop.
    let budget = crate::budgets::for_job(app, &job)?;
    let provider = crate::providers::provider_for(app, &bot.provider, bot.model.as_deref(), crate::providers::thinking_level(bot))?;
    let window = provider.model_info().map(|info| info.context_window).unwrap_or(0);
    let settings = crate::turns::compaction_settings(window);
    // `LORCA_COMPACTION=0` turns off every compaction, this one included.
    if !settings.enabled {
        return Ok(None);
    }
    let cancel = CancellationToken::new();
    budget.run(&cancel, crate::turns::compact_chat(app, chat, bot, &provider, &settings, &cancel)).await?
}

/// The newest message of a chat, by when it was made.
fn newest_message_at(app: &App, chat_id: &str) -> Option<f64> {
    app.store.page(chat_id, None, 1).ok()?.0.first().map(|message| message.created_at)
}

/// The messages past the bot's last summary, as a turn counts them.
fn uncovered_count(app: &App, chat: &Chat, bot: &Bot) -> usize {
    let after = chat.compactions.iter().find(|compaction| compaction.bot_id == bot.id).map(|compaction| compaction.after_message_id.as_str());
    app.store.count_after(&chat.meta.id, after).unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn now() -> f64 {
        1_800_000_000.0
    }

    #[test]
    fn a_chat_quiet_for_hours_with_a_large_context_is_due() {
        assert!(due(now(), now() - IDLE_SECS as f64, COMPACT_ABOVE + 1, MIN_UNCOVERED, 0));
        assert!(due(now(), now() - 10.0 * 3_600.0, 500_000, 1_000, 1_048_576));
    }

    #[test]
    fn a_chat_someone_wrote_in_recently_is_not_due() {
        assert!(!due(now(), now() - 60.0, 500_000, 1_000, 0));
        assert!(!due(now(), now() - IDLE_SECS as f64 + 1.0, 500_000, 1_000, 0));
    }

    #[test]
    fn a_small_context_or_a_short_gap_is_not_worth_a_summary() {
        assert!(!due(now(), now() - 10.0 * 3_600.0, COMPACT_ABOVE, MIN_UNCOVERED, 0));
        assert!(!due(now(), now() - 10.0 * 3_600.0, 500_000, MIN_UNCOVERED - 1, 0));
    }

    #[test]
    fn a_window_the_turn_already_compacts_leaves_nothing_to_save() {
        // A 128k window: a turn compacts past 112k, so an idle pass would only repeat it.
        assert!(!due(now(), now() - 10.0 * 3_600.0, 150_000, 1_000, 128_000));
        // A 1M window: a turn never compacts by itself, so the idle pass is what keeps it in
        // hand.
        assert!(due(now(), now() - 10.0 * 3_600.0, 150_000, 1_000, 1_048_576));
    }
}
