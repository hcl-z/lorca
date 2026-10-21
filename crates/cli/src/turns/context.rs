//! What a turn sends the model besides the chat's transcript: its system prompt, made from the
//! same inputs when the turn starts, when the bot's profile changes mid-turn, and for the apps'
//! preview, and the notes every request ends with.

use std::sync::Arc;

use serde_json::{json, Value};

use super::{context_notes, system_sections, turn_plugins, Section};
use crate::app::App;
use crate::config::now_secs;
use crate::memory::MemoryStore;
use crate::model::{Bot, Chat, Job};

/// What a turn's system prompt is made of besides the bot and the chat as they stand, fixed for
/// the turn: its job, the bot's memory, the plugins it names, and whether anyone attends it.
pub(super) struct PromptInputs {
    /// The chat as the turn started, for a prompt made after the chat is gone.
    pub(super) chat: Chat,
    pub(super) job: Job,
    pub(super) store: MemoryStore,
    pub(super) plugin_briefs: Arc<Vec<crate::plugins::mcp::PluginBrief>>,
    pub(super) unattended: bool,
}

impl PromptInputs {
    pub(super) fn for_turn(app: &Arc<App>, bot: &Bot, chat: &Chat, job: &Job, unattended: bool) -> Self {
        PromptInputs {
            chat: chat.clone(),
            job: job.clone(),
            store: MemoryStore::for_bot(&app.config.home, bot),
            plugin_briefs: Arc::new(turn_plugins(app, bot)),
            unattended,
        }
    }

    /// A turn the user's message would start in `chat`, which the preview shows.
    fn for_preview(app: &Arc<App>, bot: &Bot, chat: &Chat) -> Self {
        let kind = if chat.meta.is_group() { "room_turn" } else { "turn" };
        Self::for_turn(app, bot, chat, &Job::local(kind, &chat.meta.id, &bot.id, "preview"), false)
    }

    pub(super) fn sections(&self, app: &Arc<App>, chat: &Chat, bot: &Bot) -> Vec<Section> {
        system_sections(app, chat, bot, &self.job, &self.store, self.unattended, &self.plugin_briefs)
    }

    /// The system prompt as one string, the way a provider takes it.
    pub(super) fn system_prompt(&self, app: &Arc<App>, chat: &Chat, bot: &Bot) -> String {
        self.sections(app, chat, bot).into_iter().map(|section| section.text).collect()
    }
}

/// The notes every request of a turn ends with, before its brief, made fresh for each request:
/// the chat's durable tasks, the attention items, then the bot's memory, the user's profile,
/// the group's shared project context, and the clock. They are kept in no message, so a fact
/// saved mid-turn reaches the next step, a compaction never drops them, and the transcript before
/// them stays as the provider cached it.
pub(super) fn request_notes(app: &Arc<App>, bot: &Bot, chat_id: &str, now: i64) -> Vec<String> {
    let mut notes: Vec<String> = crate::tasks::context(app, &bot.id, chat_id).into_iter().collect();
    notes.extend(crate::attention::items_note(app, &bot.id));
    notes.extend(context_notes(app, bot, chat_id, now));
    notes
}

/// What a turn sends as its system prompt, section by section, and the notes after the
/// transcript, for the apps and `lorca prompt`. `chat_id` picks the chat whose place, team, and
/// skills apply; without one, the bot's DM. A turn's brief belongs to what started it, so a
/// preview has none.
pub fn prompt_preview(app: &Arc<App>, bot_id: &str, chat_id: Option<&str>) -> Result<Value, String> {
    let bot = app.bot(bot_id).ok_or("Unknown bot")?;
    let chat = match chat_id {
        Some(id) => app.chat(id).ok_or("Unknown chat")?,
        None => app.state.lock().unwrap().chats.iter().find(|chat| chat.meta.is_dm_of(&bot.id)).cloned().ok_or("That bot has no direct chat.")?,
    };
    let sections = PromptInputs::for_preview(app, &bot, &chat).sections(app, &chat, &bot);
    let notes = request_notes(app, &bot, &chat.meta.id, now_secs() as i64);
    let size = |texts: &mut dyn Iterator<Item = &str>| texts.map(str::len).sum::<usize>();
    let prompt_bytes = size(&mut sections.iter().map(|section| section.text.as_str()));
    let notes_bytes = size(&mut notes.iter().map(String::as_str));
    Ok(json!({
        "bot_id": bot.id,
        "chat_id": chat.meta.id,
        "sections": sections.iter().map(|section| json!({ "id": section.id, "title": section.title, "text": section.text, "bytes": section.text.len() })).collect::<Vec<_>>(),
        "total_bytes": prompt_bytes,
        "approx_tokens": prompt_bytes / 4,
        "notes": notes,
        "notes_bytes": notes_bytes,
    }))
}
