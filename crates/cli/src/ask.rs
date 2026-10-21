//! Questions a bot asks the user with options (`ask_user`): a card in the chat with two to six
//! options, one pick or several, and a line of the user's own words when the bot allows it. The
//! call waits for the answer as a permission card's does (`mcp::await_answer`); the answer is kept
//! on the card, so every Device shows what was picked, and comes back as the call's result.

use std::sync::Arc;

use async_trait::async_trait;
use lorca_agent::agent_loop::ToolExecutionMode;
use lorca_agent::{Tool, ToolError, ToolResult, ToolUpdateFn};
use serde_json::{json, Value};
use tokio_util::sync::CancellationToken;

use crate::app::App;
use crate::model::{AskAnswer, AskOption, AskQuestion, Author, Body, Bot, Message};
use crate::plugins::mcp::{self, Decision};

/// The card's `tool`.
pub const TOOL: &str = "ask";

const MIN_OPTIONS: usize = 2;
const MAX_OPTIONS: usize = 6;
const MAX_LABEL: usize = 80;
const MAX_DESCRIPTION: usize = 200;
const MAX_QUESTION: usize = 300;
const MAX_OTHER: usize = 2000;

const DESCRIPTION: &str = "Ask the user to choose on a card in this chat, when you need a decision that a few options cover: which approach, \
     which account, which of several results. Give 2 to 6 short options in the user's language, each with an optional one-line description, \
     and mark the one you recommend. Set multiple when several may apply, and allow_other when the user may want something you did not list. \
     Waits for the answer, which comes back as this call's result. Do not use it for yes/no confirmations of an action (those are asked \
     for you) or for passwords and keys (request_secret).";

/// `ask_user`: the bot asks a question with options on a card, and its call waits for the pick. A
/// turn nobody watches cannot ask.
pub struct AskUser {
    pub app: Arc<App>,
    pub chat_id: String,
    pub bot: Bot,
    pub unattended: bool,
}

#[async_trait]
impl Tool for AskUser {
    fn name(&self) -> &str {
        "ask_user"
    }
    fn description(&self) -> &str {
        DESCRIPTION
    }
    fn parameters(&self) -> Value {
        json!({
            "type": "object",
            "properties": {
                "question": { "type": "string", "description": "The question, one sentence in the user's language" },
                // Two to six, which `parse` checks with a reason the model acts on: a model that
                // sent none under a schema bound repeated the call as it was.
                "options": {
                    "type": "array",
                    "description": "2 to 6 options, each { label, description? }",
                    "items": {
                        "type": "object",
                        "properties": {
                            "label": { "type": "string", "description": "The option, a few words" },
                            "description": { "type": "string", "description": "What choosing it means, one line" }
                        },
                        "required": ["label"],
                        "additionalProperties": false
                    }
                },
                "multiple": { "type": "boolean", "description": "The user may pick several" },
                "allow_other": { "type": "boolean", "description": "The user may write their own answer instead" },
                "recommended": { "type": "integer", "minimum": 0, "description": "The place (from 0) of the option you recommend" }
            },
            "required": ["question", "options"],
            "additionalProperties": false
        })
    }
    fn execution_mode(&self) -> Option<ToolExecutionMode> {
        Some(ToolExecutionMode::Sequential)
    }
    async fn execute(&self, _id: &str, args: Value, cancel: CancellationToken, _on_update: ToolUpdateFn) -> Result<ToolResult, ToolError> {
        if self.unattended {
            return Err("Nobody is here to answer a question now. Decide yourself, and say what you chose and why.".into());
        }
        let question = parse(&args).map_err(ToolError)?;
        let message = Message::new(
            &self.chat_id,
            Author::Bot { bot_id: self.bot.id.clone() },
            Body::Permission {
                plugin_id: String::new(),
                plugin_name: String::new(),
                tool: TOOL.into(),
                summary: question.question.clone(),
                arguments: Value::Null,
                decision: "pending".into(),
                reason: None,
                rule: None,
                command: None,
                link: None,
                code: None,
                secret: None,
                ask: Some(question.clone()),
                agent_ask: None,
            },
        );
        let decision = mcp::await_answer(&self.app, &self.chat_id, &message.id, None, &cancel, || {
            self.app.upsert_message(message.clone(), true);
            crate::push::permission(&self.app, &message);
        })
        .await;
        // The answer was kept on the card before the wait ended (`answer`).
        let card = self.app.message(&self.chat_id, &message.id);
        let answer = card.as_ref().and_then(|card| match &card.body {
            Body::Permission { ask: Some(ask), .. } => ask.answer.clone(),
            _ => None,
        });
        if let Some(mut card) = card {
            if let Body::Permission { decision: shown, .. } = &mut card.body {
                *shown = match decision {
                    Decision::Allowed | Decision::Always => "allowed",
                    Decision::Denied if cancel.is_cancelled() => "dismissed",
                    decision => decision.as_str(),
                }
                .into();
            }
            self.app.upsert_message(card, true);
        }
        match (decision, answer) {
            (Decision::Allowed | Decision::Always, Some(answer)) => {
                let text = answer_text(&question, &answer);
                Ok(ToolResult::text(format!("The user answered: {text}")).with_details(json!({ "summary": text, "answer": answer })))
            }
            (Decision::Allowed | Decision::Always, None) => Err("The answer did not arrive. Ask again in a message.".into()),
            (Decision::Denied, _) if cancel.is_cancelled() => Err("Stopped before the user answered.".into()),
            (Decision::Denied, _) => Err("The user skipped the question. Go on with what you think best, and say what you chose.".into()),
            (Decision::Expired, _) => Err("Nobody answered in time. Say what you need decided and stop.".into()),
            (Decision::Dismissed, _) => Ok(mcp::dismissed_call("The user wrote a message instead of picking an option. Follow that message.".into())),
        }
    }
}

/// The question as the card shows it, from the call's arguments.
fn parse(args: &Value) -> Result<AskQuestion, String> {
    let question = trimmed(&args["question"]).ok_or("question is required")?;
    if question.chars().count() > MAX_QUESTION {
        return Err(format!("Keep the question under {MAX_QUESTION} characters."));
    }
    let options: Vec<AskOption> = args["options"]
        .as_array()
        .ok_or("options is required")?
        .iter()
        .map(|option| {
            let label = trimmed(&option["label"]).ok_or("Each option needs a label.")?;
            if label.chars().count() > MAX_LABEL {
                return Err(format!("Keep each label under {MAX_LABEL} characters; put the rest in its description."));
            }
            let description = trimmed(&option["description"]).map(|text| text.chars().take(MAX_DESCRIPTION).collect());
            Ok(AskOption { label, description })
        })
        .collect::<Result<_, String>>()?;
    if !(MIN_OPTIONS..=MAX_OPTIONS).contains(&options.len()) {
        return Err(format!(
            "Give {MIN_OPTIONS} to {MAX_OPTIONS} options, each {{ \"label\": \"…\" }} in `options`; this call had {}. A question with no answers \
             to offer is not for ask_user: write it in your reply instead, and the user's answer comes as their next message.",
            options.len()
        ));
    }
    let mut labels: Vec<String> = options.iter().map(|option| option.label.to_lowercase()).collect();
    labels.sort();
    labels.dedup();
    if labels.len() != options.len() {
        return Err("Each option needs a label of its own.".into());
    }
    let recommended = args["recommended"].as_u64().map(|place| place as usize);
    if recommended.is_some_and(|place| place >= options.len()) {
        return Err("recommended is the place of one of the options, from 0.".into());
    }
    Ok(AskQuestion {
        question,
        options,
        multiple: args["multiple"].as_bool().unwrap_or(false),
        allow_other: args["allow_other"].as_bool().unwrap_or(false),
        recommended,
        answer: None,
    })
}

fn trimmed(value: &Value) -> Option<String> {
    value.as_str().map(str::trim).filter(|text| !text.is_empty()).map(str::to_string)
}

/// What the user answered, in words: the labels picked, then their own.
fn answer_text(question: &AskQuestion, answer: &AskAnswer) -> String {
    let mut parts: Vec<String> = answer.choices.iter().filter_map(|&place| question.options.get(place)).map(|option| format!("\"{}\"", option.label)).collect();
    if let Some(other) = &answer.other {
        parts.push(format!("in their own words: \"{other}\""));
    }
    parts.join(", ")
}

/// `permission.answer` for a question, on the bot's Runner: checks the answer against the card,
/// keeps it there, and hands the waiting call an Allowed. A skip is answered without one.
pub fn answer(app: &Arc<App>, chat_id: &str, message_id: &str, decision: Decision, body: &Value) -> Result<Value, String> {
    if !app.pending_permissions.lock().unwrap().contains_key(message_id) {
        return Err("This question is no longer waiting for an answer.".into());
    }
    if decision == Decision::Denied {
        return match mcp::answer(app, message_id, Decision::Denied) {
            true => Ok(json!({ "answered": true })),
            false => Err("This question is no longer waiting for an answer.".into()),
        };
    }
    let mut message = app.message(chat_id, message_id).ok_or("Unknown message")?;
    let Body::Permission { ask: Some(question), .. } = &mut message.body else {
        return Err("That is not a question.".into());
    };
    let given = &body["answer"];
    let mut choices: Vec<usize> = given["choices"].as_array().map(|places| places.iter().filter_map(Value::as_u64).map(|place| place as usize).collect()).unwrap_or_default();
    choices.sort_unstable();
    choices.dedup();
    if choices.iter().any(|&place| place >= question.options.len()) {
        return Err("Pick one of the options.".into());
    }
    let other = trimmed(&given["other"]);
    if other.is_some() && !question.allow_other {
        return Err("This question takes only its options.".into());
    }
    if other.as_ref().is_some_and(|text| text.chars().count() > MAX_OTHER) {
        return Err(format!("Keep your answer under {MAX_OTHER} characters."));
    }
    match (choices.len(), &other) {
        (0, None) => return Err("Pick an option.".into()),
        (n, _) if n > 1 && !question.multiple => return Err("Pick one option.".into()),
        (n, Some(_)) if n > 0 && !question.multiple => return Err("Pick an option or write your own, not both.".into()),
        _ => {}
    }
    question.answer = Some(AskAnswer { choices, other });
    app.upsert_message(message, true);
    match mcp::answer(app, message_id, Decision::Allowed) {
        true => Ok(json!({ "answered": true })),
        false => Err("This question is no longer waiting for an answer.".into()),
    }
}

#[cfg(test)]
mod tests;
