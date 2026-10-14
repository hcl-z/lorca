//! The tools a bot makes and manages group chats with: `create_group` and `edit_group`. They
//! work through the same `App` methods as the apps' `chats.*` requests, so a group made by a
//! bot follows the rules of one made with the + button.

use std::sync::Arc;

use async_trait::async_trait;
use lorca_agent::agent_loop::ToolExecutionMode;
use lorca_agent::{Tool, ToolError, ToolResult, ToolUpdateFn};
use serde_json::{json, Value};
use tokio_util::sync::CancellationToken;

use crate::app::App;
use crate::model::{Bot, ChatMeta, MAX_GROUP_BOTS};

/// A list of bot ids from a tool call, trimmed and without repeats.
fn ids(args: &Value, key: &str) -> Result<Vec<String>, ToolError> {
    let mut out: Vec<String> = Vec::new();
    match args.get(key) {
        None | Some(Value::Null) => {}
        Some(Value::Array(items)) => {
            for item in items {
                let id = item.as_str().map(str::trim).filter(|id| !id.is_empty()).ok_or_else(|| ToolError(format!("{key} holds bot ids")))?;
                if !out.iter().any(|seen| seen == id) {
                    out.push(id.to_string());
                }
            }
        }
        Some(_) => return Err(ToolError(format!("{key} is a list of bot ids"))),
    }
    Ok(out)
}

fn field<'a>(args: &'a Value, key: &str) -> Option<&'a str> {
    args[key].as_str().map(str::trim)
}

/// The bot with this id, else an error that lists every bot as "Name (id)".
fn member(app: &App, id: &str) -> Result<Bot, ToolError> {
    app.bot(id).ok_or_else(|| {
        let bots = app.state.lock().unwrap().bots.iter().map(|b| format!("{} ({})", b.name, b.id)).collect::<Vec<_>>().join(", ");
        ToolError(format!("No bot with id {id}. Bots: {bots}"))
    })
}

/// A bot that can join a group: one still setting itself up has no job to bring yet.
fn joinable(app: &App, id: &str) -> Result<Bot, ToolError> {
    let bot = member(app, id)?;
    if bot.setting_up {
        return Err(ToolError(format!("{} is still setting up its job with the user. Add it once it has.", bot.name)));
    }
    Ok(bot)
}

fn names(app: &App, ids: &[String]) -> String {
    ids.iter().map(|id| format!("{} (id {id})", crate::runtime::name_of(app, id))).collect::<Vec<_>>().join(", ")
}

pub struct CreateGroup {
    pub app: Arc<App>,
    pub chat_id: String,
    pub bot: Bot,
    pub unattended: bool,
}

#[async_trait]
impl Tool for CreateGroup {
    fn name(&self) -> &str {
        "create_group"
    }
    fn description(&self) -> &str {
        "Create a group chat of teammates, with yourself in it, when several of them should work on something together in \
         one place the user can watch, or when the user asks for a group. Name it, say in the description what it is for \
         and what done looks like (every member reads it), and pick members by id from your team. Hand a single piece of \
         work to one teammate with message_bot instead. The group starts empty: the user writes there to begin, and its \
         owner (you, unless you name another member) splits the work between the members."
    }
    fn parameters(&self) -> Value {
        json!({ "type": "object", "properties": {
            "title": { "type": "string", "description": "The group's name" },
            "description": { "type": "string", "description": "What the group is for and what done looks like, in the user's words" },
            "bot_ids": { "type": "array", "items": { "type": "string" }, "description": "The teammates to bring in, by id from your team or list_teammates; you are added yourself" },
            "owner_bot_id": { "type": "string", "description": "The member who holds the work and splits it between the others. Defaults to you" }
        }, "required": ["title", "bot_ids"], "additionalProperties": false })
    }
    fn execution_mode(&self) -> Option<ToolExecutionMode> {
        Some(ToolExecutionMode::Sequential)
    }
    async fn execute(&self, _id: &str, args: Value, _cancel: CancellationToken, _on_update: ToolUpdateFn) -> Result<ToolResult, ToolError> {
        if self.unattended {
            return Err("Nobody is watching this run. Report that a group would help and let the user make it.".into());
        }
        let title = field(&args, "title").filter(|title| !title.is_empty()).ok_or("A group needs a title")?;
        let description = field(&args, "description").filter(|text| !text.is_empty());
        let mut members = vec![self.bot.id.clone()];
        for id in ids(&args, "bot_ids")? {
            if !members.contains(&id) {
                joinable(&self.app, &id)?;
                members.push(id);
            }
        }
        if members.len() < 2 {
            return Err("A group needs at least one teammate besides you".into());
        }
        if members.len() > MAX_GROUP_BOTS {
            return Err(ToolError(format!("A group holds at most {MAX_GROUP_BOTS} bots, you included; pick {} or fewer teammates", MAX_GROUP_BOTS - 1)));
        }
        let owner = match field(&args, "owner_bot_id").filter(|id| !id.is_empty()) {
            Some(id) if members.iter().any(|member| member == id) => id.to_string(),
            Some(id) => return Err(ToolError(format!("The owner must be a member, and {id} is not one"))),
            None => self.bot.id.clone(),
        };
        let chat = self
            .app
            .create_chat(ChatMeta {
                id: String::new(),
                kind: "group".into(),
                title: Some(title.to_string()),
                owner_bot_id: Some(owner.clone()),
                description: description.map(str::to_string),
                bot_ids: members.clone(),
                is_pinned: false,
                section_id: None,
                is_hidden: false,
                mute: None,
                created_at: 0.0,
                channel: None,
            })
            .map_err(|e| ToolError(e.to_string()))?;
        let others: Vec<String> = members.iter().filter(|id| **id != self.bot.id).cloned().collect();
        let who = others.iter().map(|id| crate::runtime::name_of(&self.app, id)).collect::<Vec<_>>().join(", ");
        self.app.notice(&self.chat_id, format!("{} created the group \"{title}\" with {who}.", self.bot.name));
        let text = format!(
            "Created the group \"{title}\" (chat id {}) with {} and you. Owner: {}. It is in the user's chat list and empty: tell \
             the user in one line that it is there and what it is for. Do not message its members to start the work; the user \
             starts it by writing in the group.",
            chat.meta.id,
            names(&self.app, &others),
            crate::runtime::name_of(&self.app, &owner)
        );
        Ok(ToolResult::text(text).with_details(json!({ "summary": format!("Created the group {title}"), "chat_id": chat.meta.id })))
    }
}

pub struct EditGroup {
    pub app: Arc<App>,
    pub bot: Bot,
    pub unattended: bool,
}

#[async_trait]
impl Tool for EditGroup {
    fn name(&self) -> &str {
        "edit_group"
    }
    fn description(&self) -> &str {
        "Change a group you are in: rename it, rewrite what it is for, bring teammates in or take them out, or hand its \
         ownership to another member. Only the fields you pass change. You cannot take yourself out; the user can."
    }
    fn parameters(&self) -> Value {
        json!({ "type": "object", "properties": {
            "chat_id": { "type": "string", "description": "The group's chat id, or its title" },
            "title": { "type": "string" },
            "description": { "type": "string", "description": "The complete account of what the group is for; an empty string clears it" },
            "add_bot_ids": { "type": "array", "items": { "type": "string" } },
            "remove_bot_ids": { "type": "array", "items": { "type": "string" } },
            "owner_bot_id": { "type": "string", "description": "A member of the group after these changes" }
        }, "required": ["chat_id"], "additionalProperties": false })
    }
    fn execution_mode(&self) -> Option<ToolExecutionMode> {
        Some(ToolExecutionMode::Sequential)
    }
    async fn execute(&self, _id: &str, args: Value, _cancel: CancellationToken, _on_update: ToolUpdateFn) -> Result<ToolResult, ToolError> {
        if self.unattended {
            return Err("Nobody is watching this run. Report what the group needs and let the user change it.".into());
        }
        let wanted = field(&args, "chat_id").filter(|id| !id.is_empty()).ok_or("chat_id is required")?;
        let chat = self.app.find_group(wanted).map_err(ToolError)?;
        let chat_id = chat.meta.id.clone();
        if !chat.meta.bot_ids.contains(&self.bot.id) {
            return Err("You are not in this group, so you cannot change it".into());
        }
        let (add, remove) = (ids(&args, "add_bot_ids")?, ids(&args, "remove_bot_ids")?);
        let mut after = chat.meta.bot_ids.clone();
        for id in &remove {
            if *id == self.bot.id {
                return Err("You cannot take yourself out of the group; the user can".into());
            }
            if !after.contains(id) {
                return Err(ToolError(format!("{} is not in this group", crate::runtime::name_of(&self.app, id))));
            }
            after.retain(|member| member != id);
        }
        let mut joining = Vec::new();
        for id in &add {
            if after.contains(id) {
                continue;
            }
            joinable(&self.app, id)?;
            after.push(id.clone());
            joining.push(id.clone());
        }
        if after.len() > MAX_GROUP_BOTS {
            return Err(ToolError(format!("A group holds at most {MAX_GROUP_BOTS} bots; it would have {}", after.len())));
        }
        let owner = field(&args, "owner_bot_id").filter(|id| !id.is_empty());
        if let Some(owner) = owner.filter(|owner| !after.iter().any(|member| member == owner)) {
            return Err(ToolError(format!("The owner must be a member, and {owner} is not one")));
        }
        let title = field(&args, "title");
        if title == Some("") {
            return Err("A group needs a title".into());
        }
        // Everything checked: now change it.
        for id in &remove {
            self.app.remove_group_bot(&chat_id, id).map_err(ToolError)?;
            self.app.notice(&chat_id, format!("{} left the chat.", crate::runtime::name_of(&self.app, id)));
        }
        for id in &joining {
            self.app.add_group_bot(&chat_id, id).map_err(ToolError)?;
            self.app.notice(&chat_id, format!("{} joined the chat.", crate::runtime::name_of(&self.app, id)));
        }
        if let Some(owner) = owner {
            self.app.set_group_owner(&chat_id, owner).map_err(ToolError)?;
        }
        if let Some(title) = title {
            self.app.rename_chat(&chat_id, Some(title.to_string())).map_err(|e| ToolError(e.to_string()))?;
        }
        if let Some(description) = field(&args, "description") {
            self.app.describe_chat(&chat_id, Some(description.to_string()).filter(|text| !text.is_empty())).map_err(|e| ToolError(e.to_string()))?;
        }
        let now = self.app.chat(&chat_id).ok_or("The group is gone")?;
        let title = self.app.chat_title(&now.meta);
        let text = format!(
            "The group \"{title}\" (chat id {chat_id}) has {}. Owner: {}.",
            names(&self.app, &now.meta.bot_ids),
            now.meta.owner().map(|owner| crate::runtime::name_of(&self.app, owner)).unwrap_or_default(),
        );
        Ok(ToolResult::text(text).with_details(json!({ "summary": format!("Updated the group {title}"), "chat_id": chat_id })))
    }
}
