//! A bot's profile changing while its turn runs, and the notes its turns read where its chat
//! says the profile changed.

use lorca_agent::agent_loop::AgentContext;
use lorca_agent::{AgentMessage, UserMessage};

use super::TurnHooks;
use crate::model::{Bot, Profile, ProfileChange, ProfileNotice};

impl TurnHooks {
    /// The system prompt made again when the bot's profile changed since it was made: by the
    /// user in its details, by `edit_bot` or `finish_setup`, or on another Device. The next step
    /// reads it, after a note of the change, and a turn that finished the bot's setup goes on
    /// with its usual tools. Its tools keep the working directory they were made with until the
    /// next turn. False when nothing changed.
    pub(super) fn refresh_profile(&self, context: &mut AgentContext) -> bool {
        let Some(mut bot) = self.app.bot(&self.bot.id) else { return false };
        let now = Profile::of(&bot);
        let before = std::mem::replace(&mut *self.profile.lock().unwrap(), now.clone());
        if before == now {
            return false;
        }
        bot.workdir = self.bot.workdir.clone();
        // The transcript the turn started from has the bot's earlier words about itself and not
        // the chat's notice of the change.
        if let Some(change) = ProfileChange::between(&before, &now) {
            context.messages.push(AgentMessage::User(UserMessage::text(profile_note(change.into(), &change.notice(&before, &now), &bot, true))));
        }
        let inputs = &self.prompt_inputs;
        let chat = self.app.chat(&self.chat_id).unwrap_or_else(|| inputs.chat.clone());
        context.system_prompt = inputs.system_prompt(&self.app, &chat, &bot);
        let finished_setup = before.setting_up && !now.setting_up;
        if let (true, Some(tools)) = (finished_setup, &self.full_tools) {
            context.tools = tools.clone();
        }
        if let Some(trace) = &self.trace {
            let changed: Vec<&str> = [("name", before.name != now.name), ("description", before.description != now.description), ("setup", before.setting_up != now.setting_up)]
                .into_iter()
                .filter_map(|(field, changed)| changed.then_some(field))
                .collect();
            trace.prompt_refresh(&changed, context.system_prompt.chars().count(), finished_setup);
        }
        true
    }
}

/// Most of a description a profile note repeats; the system prompt has it whole.
const PROFILE_NOTE_DESCRIPTION_CHARS: usize = 2_000;

/// What the bot reads where its chat has a profile notice: the latest gives its name and
/// description as they stand and marks what the chat said about it before as out of date; an
/// earlier one only marks a change.
pub(super) fn profile_note(kind: ProfileNotice, notice: &str, bot: &Bot, latest: bool) -> String {
    if !latest {
        return format!("[From Lorca: {} A later note gives your profile as it stands.]", notice.trim());
    }
    let mut description: String = bot.description.trim().chars().take(PROFILE_NOTE_DESCRIPTION_CHARS).collect();
    if bot.description.trim().chars().count() > PROFILE_NOTE_DESCRIPTION_CHARS {
        description.push_str("… (your system prompt has the rest)");
    }
    match kind {
        ProfileNotice::SetUp => format!(
            "[From Lorca: your setup finished here. You are {name} now, and this is your job, as your system prompt has it:\n{description}\nThe setup \
             questions and the placeholder name before this are done with: work as {name} from here.]",
            name = bot.name
        ),
        ProfileNotice::Changed => format!(
            "[From Lorca: the user changed your profile here ({}). You are {} now, and your description reads:\n{description}\nWhat this chat said about \
             your name or job before this is out of date: follow this description, which your system prompt has too.]",
            notice.trim().trim_end_matches('.'),
            bot.name
        ),
    }
}
