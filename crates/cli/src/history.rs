//! Older history from the Device that still holds it: a page of a chat before what the asker
//! has, served over the signed request envelopes.

use std::sync::Arc;

use serde_json::json;

use crate::app::App;
use crate::model::Message;

/// A page an origin Device answered, oldest first, and whether it holds more below it.
#[derive(Debug, Clone)]
pub struct OriginPage {
    pub messages: Vec<Message>,
    pub has_more: bool,
}

/// The Devices to ask for a chat's older messages, in order: the bot's Runner (a group's owner
/// first, then the other members'), then the identity Devices, then any other Device. This
/// Device is left out, as are duplicates.
pub fn origin_candidates(app: &App, chat_id: &str) -> Vec<String> {
    let state = app.state.lock().unwrap();
    let this = app.this_device_id();
    let mut out: Vec<String> = Vec::new();
    if let Some(chat) = state.chats.iter().find(|chat| chat.meta.id == chat_id) {
        let mut bots: Vec<&str> = Vec::new();
        if let Some(owner) = chat.meta.owner_bot_id.as_deref() {
            bots.push(owner);
        }
        bots.extend(chat.meta.bot_ids.iter().map(String::as_str));
        for bot_id in bots {
            if let Some(runner) = state.bots.iter().find(|bot| bot.id == bot_id).map(|bot| bot.runner_id.clone()) {
                if this.as_deref() != Some(runner.as_str()) && !out.contains(&runner) {
                    out.push(runner);
                }
            }
        }
    }
    for device in state.devices.iter().filter(|device| device.is_identity_device) {
        if this.as_deref() != Some(device.id.as_str()) && !out.contains(&device.id) {
            out.push(device.id.clone());
        }
    }
    for device in &state.devices {
        if this.as_deref() != Some(device.id.as_str()) && !out.contains(&device.id) {
            out.push(device.id.clone());
        }
    }
    out
}

/// Asks the first online candidate for a page older than `before` and inserts it here. `None`
/// when no candidate answers: the chat has no more, or every origin is offline.
pub async fn fetch_origin_page(app: &Arc<App>, chat_id: &str, before: Option<(f64, String)>) -> Result<Option<OriginPage>, String> {
    for candidate in origin_candidates(app, chat_id) {
        if !app.device_is_online(&candidate) {
            continue;
        }
        let body = json!({
            "chat_id": chat_id,
            "before": before.as_ref().map(|(sort_at, message_id)| json!({ "sort_at": sort_at, "message_id": message_id })),
            "limit": 100,
            "max_bytes": 256 * 1024,
        });
        let answer = match crate::requests::ask_within(app, &candidate, "history.page", body, std::time::Duration::from_secs(5)).await {
            Ok(answer) => answer,
            Err(_) => continue,
        };
        if answer["not_here"].as_bool().unwrap_or(false) {
            continue;
        }
        let messages: Vec<Message> = serde_json::from_value(answer["messages"].clone()).unwrap_or_default();
        let has_more = answer["has_more"].as_bool().unwrap_or(false);
        if messages.is_empty() && !has_more {
            continue;
        }
        let mut newest_first = messages.clone();
        newest_first.reverse();
        app.store.insert_older(&newest_first).map_err(|error| error.to_string())?;
        return Ok(Some(OriginPage { messages, has_more }));
    }
    Ok(None)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::Config;
    use crate::model::{Author, Body, Chat, ChatMeta};
    use serde_json::Value;

    struct Scratch(Arc<App>, std::path::PathBuf);
    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.1);
        }
    }

    fn scratch() -> Scratch {
        let home = std::env::temp_dir().join(format!("lorca-history-{}", uuid::Uuid::new_v4()));
        Scratch(App::load(Config { home: home.clone(), port: 0 }).unwrap(), home)
    }

    fn group(app: &Arc<App>) -> String {
        let lead = app.state.lock().unwrap().bots[0].clone();
        let mut first = lead.clone();
        first.id = "b1".into();
        first.runner_id = "runner-1".into();
        let mut second = lead;
        second.id = "b2".into();
        second.runner_id = "runner-2".into();
        let mut identity = app.local_device().unwrap();
        identity.id = "identity-device".into();
        identity.is_identity_device = true;
        {
            let mut state = app.state.lock().unwrap();
            state.bots.retain(|bot| bot.id != "b1" && bot.id != "b2");
            state.bots.push(first);
            state.bots.push(second);
            state.chats.retain(|chat| chat.meta.id != "group");
            state.chats.push(Chat {
                meta: ChatMeta {
                    id: "group".into(),
                    kind: "group".into(),
                    title: Some("Group".into()),
                    bot_ids: vec!["b1".into(), "b2".into()],
                    owner_bot_id: Some("b1".into()),
                    description: None,
                    is_pinned: false,
                    section_id: None,
                    is_hidden: false,
                    mute: None,
                    created_at: 0.0,
                    channel: None,
                },
                unread_count: 0,
                usage: None,
                compactions: Vec::new(),
            });
            state.devices.retain(|device| device.id != "identity-device");
            state.devices.push(identity);
            state.device_online.insert("runner-1".into());
            state.device_online.insert("runner-2".into());
            state.device_online.insert("identity-device".into());
        }
        "group".into()
    }

    #[test]
    fn a_group_prefers_its_owner_bot_s_runner_then_the_identity_device() {
        let scratch = scratch();
        let app = &scratch.0;
        crate::identity::create(app, Some("Runner".into())).unwrap();
        let chat = group(app);
        assert_eq!(origin_candidates(app, &chat), ["runner-1", "runner-2", "identity-device"]);
    }

    struct Canned(Arc<std::sync::Mutex<Vec<String>>>, Message);

    #[async_trait::async_trait]
    impl crate::requests::Remote for Canned {
        async fn ask(&self, _app: &Arc<App>, runner_id: &str, _verb: &str, _body: Value, _timeout: std::time::Duration) -> Result<Value, String> {
            self.0.lock().unwrap().push(runner_id.to_string());
            match runner_id {
                "runner-1" => Ok(json!({ "not_here": true })),
                _ => Ok(json!({ "messages": [json!(self.1.clone())], "has_more": false })),
            }
        }
    }

    #[tokio::test]
    async fn an_offline_or_absent_origin_falls_through_to_the_next_candidate() {
        let scratch = scratch();
        let app = &scratch.0;
        crate::identity::create(app, Some("Runner".into())).unwrap();
        let chat = group(app);
        let message = Message::new(&chat, Author::You, Body::text("older"));
        let calls = Arc::new(std::sync::Mutex::new(Vec::new()));
        *app.remote.lock().unwrap() = Arc::new(Canned(calls.clone(), message.clone()));
        let page = fetch_origin_page(app, &chat, None).await.unwrap().unwrap();
        assert_eq!(page.messages.len(), 1);
        assert_eq!(*calls.lock().unwrap(), ["runner-1", "runner-2"]);
        assert!(app.message(&chat, &message.id).is_some());
    }
}
