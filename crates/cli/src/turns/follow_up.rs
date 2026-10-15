//! A direct chat's turn asking once more for the work its bot's reply announced and never
//! started: a reply without a tool call ends the loop.

use lorca_agent::{AgentMessage, AssistantMessage, StopReason, UserMessage};

use super::is_pass;

/// What a turn adds, unseen in the chat, when the bot's reply announced work and made no call.
pub(super) const FOLLOW_UP_NOTE: &str = "[Your last message says what you are about to do, but it made no tool call, so nothing ran \
     and your turn would end here. If work is left that you can do now, do it with your tools without announcing it again. \
     If the work is done, or it waits on the user or on something already running, answer with exactly PASS.]";

/// Asks once per turn for the work a reply announced and never started ("Fetching all sources
/// now."): a reply without a tool call ends the loop, and in a routine nobody is there to ask.
#[derive(Default)]
pub(super) struct FollowUp {
    /// The text of the last step's reply, when the step ended on it with no tool call.
    reply: std::sync::Mutex<Option<String>>,
    asked: std::sync::atomic::AtomicBool,
}

impl FollowUp {
    /// A step starts: the reply before it no longer ends the turn.
    pub(super) fn step(&self) {
        *self.reply.lock().unwrap() = None;
    }

    pub(super) fn turn_ended(&self, message: &AssistantMessage) {
        let ended_on_words = message.stop_reason == StopReason::Stop && message.tool_calls().is_empty();
        *self.reply.lock().unwrap() = ended_on_words.then(|| message.text());
    }

    /// The note and the reply it answers, when the turn would end on a reply that announced work.
    pub(super) fn note(&self) -> Option<(String, AgentMessage)> {
        let reply = self.reply.lock().unwrap().take()?;
        if !announces_work(&reply) || self.asked.swap(true, std::sync::atomic::Ordering::Relaxed) {
            return None;
        }
        Some((reply, AgentMessage::User(UserMessage::text(FOLLOW_UP_NOTE))))
    }
}

/// Words that, anywhere in a reply's last sentence, promise a step rather than report one.
const ANNOUNCING: &[&str] = &[
    " i'll ", " i’ll ", " i will ", " i'm going to ", " i’m going to ", " i am going to ", " i'm about to ", " next, i ",
    "我这就", "我马上", "马上", "这就去", "接下来我", "下一步我", "现在开始", "现在就去", "现在去", "现在重新", "正在", "稍等",
];

/// Words that promise a step only at the start of a clause: "Let me check", "好，让我看看", but
/// not "…让我知道" or "当我来看时".
const ANNOUNCING_CLAUSE: &[&str] = &["let me ", "我来", "我先", "让我", "我去"];

/// A whole last sentence that promises a step.
const ANNOUNCING_SENTENCE: &[&str] = &["on it", "stand by", "one moment", "one sec"];

/// Whether a reply's last line says what the bot is about to do: a phrase that promises a step,
/// "Fetching all sources now.", or a colon or ellipsis with nothing after it. A question waits on
/// the user, and a promise for later ("如果需要，我会再提醒你") is not work left now.
pub(super) fn announces_work(reply: &str) -> bool {
    let Some(line) = reply.lines().map(str::trim).filter(|line| !line.is_empty()).last() else { return false };
    if is_pass(line) || line.ends_with(['?', '？']) {
        return false;
    }
    if line.ends_with([':', '：', '…']) || line.ends_with("...") {
        return true;
    }
    let lower = line.to_lowercase();
    let last_sentence = lower.split(['.', '!', '。', '！']).map(str::trim).filter(|sentence| !sentence.is_empty()).last().unwrap_or("");
    // A space in front, so an English phrase starts at a word.
    let spaced = format!(" {last_sentence} ");
    let doing_now = last_sentence.ends_with(" now")
        && last_sentence
            .split_whitespace()
            .next()
            .is_some_and(|word| word.ends_with("ing") && !["nothing", "everything", "something", "anything"].contains(&word));
    let clause_starts = last_sentence.split([',', '，', ';', '；']).map(str::trim).any(|clause| ANNOUNCING_CLAUSE.iter().any(|phrase| clause.starts_with(phrase)));
    doing_now || clause_starts || ANNOUNCING_SENTENCE.contains(&last_sentence) || ANNOUNCING.iter().any(|phrase| spaced.contains(phrase))
}
