use super::*;
use std::time::Duration;

/// An App over a scratch home, removed when the test ends.
struct ScratchApp(Arc<App>, std::path::PathBuf);
impl Drop for ScratchApp {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.1);
    }
}

/// A Runner with an identity, its first bot, and that bot's DM.
fn runner() -> (ScratchApp, Bot, String) {
    let home = std::env::temp_dir().join(format!("lorca-ask-{}", uuid::Uuid::new_v4()));
    let app = App::load(crate::config::Config { home: home.clone(), port: 0 }).unwrap();
    crate::identity::create(&app, Some("Workbench".into())).unwrap();
    let bot = app.state.lock().unwrap().bots[0].clone();
    let chat_id = app.dm_with(&bot.id, None).unwrap().meta.id;
    (ScratchApp(app, home), bot, chat_id)
}

fn question() -> Value {
    json!({
        "question": "Which plan should I run?",
        "options": [{ "label": "Fast", "description": "Two days, fewer checks" }, { "label": "Thorough" }, { "label": "Both" }],
        "recommended": 1,
    })
}

/// Starts `ask_user` with `args` and waits for its card; answers the card through the app's own
/// `chats.permission`, as any Device's answer arrives.
async fn ask(app: &Arc<App>, bot: &Bot, chat_id: &str, args: Value, cancel: CancellationToken) -> (tokio::task::JoinHandle<Result<ToolResult, ToolError>>, String) {
    let call = AskUser { app: app.clone(), chat_id: chat_id.into(), bot: bot.clone(), unattended: false };
    let running = tokio::spawn(async move { call.execute("call", args, cancel, Arc::new(|_| {})).await });
    let id = loop {
        let waiting = app.pending_permissions.lock().unwrap().keys().next().cloned();
        if let Some(id) = waiting.filter(|id| app.message(chat_id, id).is_some()) {
            break id;
        }
        tokio::time::sleep(Duration::from_millis(5)).await;
    };
    (running, id)
}

async fn reply(app: &Arc<App>, chat_id: &str, id: &str, decision: &str, answer: Value) -> Result<Value, String> {
    crate::api::dispatch(app, "chats.permission", json!({ "chat_id": chat_id, "message_id": id, "decision": decision, "answer": answer })).await
}

fn card(app: &Arc<App>, chat_id: &str, id: &str) -> (String, AskQuestion) {
    match app.message(chat_id, id).unwrap().body {
        Body::Permission { tool, decision, ask: Some(ask), summary, .. } => {
            assert_eq!((tool.as_str(), summary.as_str()), (TOOL, ask.question.as_str()));
            (decision, ask)
        }
        other => panic!("not a question: {other:?}"),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_pick_is_kept_on_the_card_and_comes_back_as_the_result() {
    let (scratch, bot, chat_id) = runner();
    let app = &scratch.0;
    let (running, id) = ask(app, &bot, &chat_id, question(), CancellationToken::new()).await;
    let (decision, asked) = card(app, &chat_id, &id);
    assert_eq!((decision.as_str(), asked.options.len(), asked.recommended, asked.multiple), ("pending", 3, Some(1), false));
    assert_eq!(asked.options[0].description.as_deref(), Some("Two days, fewer checks"));

    // One question takes one pick, and only of its options; a wrong answer keeps it waiting.
    assert_eq!(reply(app, &chat_id, &id, "allow", json!({ "choices": [0, 1] })).await.unwrap_err(), "Pick one option.");
    assert_eq!(reply(app, &chat_id, &id, "allow", json!({ "choices": [7] })).await.unwrap_err(), "Pick one of the options.");
    assert_eq!(reply(app, &chat_id, &id, "allow", json!({ "other": "Neither" })).await.unwrap_err(), "This question takes only its options.");
    assert_eq!(reply(app, &chat_id, &id, "allow", json!({})).await.unwrap_err(), "Pick an option.");
    assert!(app.pending_permissions.lock().unwrap().contains_key(&id), "still waiting");

    reply(app, &chat_id, &id, "allow", json!({ "choices": [1] })).await.unwrap();
    let result = running.await.unwrap().unwrap();
    assert_eq!(result.text_content(), "The user answered: \"Thorough\"");
    let (decision, asked) = card(app, &chat_id, &id);
    assert_eq!((decision.as_str(), asked.answer), ("allowed".into(), Some(AskAnswer { choices: vec![1], other: None })));
    assert_eq!(reply(app, &chat_id, &id, "allow", json!({ "choices": [0] })).await.unwrap_err(), "This question is no longer waiting for an answer.");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn several_picks_and_the_users_own_words() {
    let (scratch, bot, chat_id) = runner();
    let app = &scratch.0;
    let mut args = question();
    args["multiple"] = json!(true);
    args["allow_other"] = json!(true);
    let (running, id) = ask(app, &bot, &chat_id, args, CancellationToken::new()).await;
    reply(app, &chat_id, &id, "allow", json!({ "choices": [2, 0, 2], "other": "  and a dry run first " })).await.unwrap();
    let result = running.await.unwrap().unwrap();
    assert_eq!(result.text_content(), "The user answered: \"Fast\", \"Both\", in their own words: \"and a dry run first\"");
    assert_eq!(card(app, &chat_id, &id).1.answer, Some(AskAnswer { choices: vec![0, 2], other: Some("and a dry run first".into()) }));

    // Other alone answers a single-pick question that allows it.
    let mut args = question();
    args["allow_other"] = json!(true);
    let (running, id) = ask(app, &bot, &chat_id, args, CancellationToken::new()).await;
    assert_eq!(reply(app, &chat_id, &id, "allow", json!({ "choices": [0], "other": "x" })).await.unwrap_err(), "Pick an option or write your own, not both.");
    reply(app, &chat_id, &id, "allow", json!({ "other": "Next week" })).await.unwrap();
    assert_eq!(running.await.unwrap().unwrap().text_content(), "The user answered: in their own words: \"Next week\"");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn skip_stop_and_a_new_message_end_the_wait() {
    let (scratch, bot, chat_id) = runner();
    let app = &scratch.0;

    let (running, id) = ask(app, &bot, &chat_id, question(), CancellationToken::new()).await;
    reply(app, &chat_id, &id, "deny", Value::Null).await.unwrap();
    assert!(running.await.unwrap().unwrap_err().0.starts_with("The user skipped the question."));
    let (decision, asked) = card(app, &chat_id, &id);
    assert_eq!((decision.as_str(), asked.answer), ("denied", None));

    let cancel = CancellationToken::new();
    let (running, id) = ask(app, &bot, &chat_id, question(), cancel.clone()).await;
    cancel.cancel();
    assert_eq!(running.await.unwrap().unwrap_err().0, "Stopped before the user answered.");
    assert_eq!(card(app, &chat_id, &id).0, "dismissed");

    let (running, id) = ask(app, &bot, &chat_id, question(), CancellationToken::new()).await;
    mcp::dismiss_questions(app, &chat_id);
    assert!(running.await.unwrap().unwrap().terminate, "a new message dismisses it like any question");
    assert_eq!(card(app, &chat_id, &id).0, "dismissed");
}

#[test]
fn a_question_needs_distinct_options_and_an_audience() {
    let bad = |args: Value| parse(&args).unwrap_err();
    assert_eq!(bad(json!({ "question": " ", "options": [] })), "question is required");
    assert_eq!(bad(json!({ "question": "Q", "options": [{ "label": "One" }] })), "Give 2 to 6 options.");
    assert_eq!(bad(json!({ "question": "Q", "options": [{ "label": "One" }, { "label": " one " }] })), "Each option needs a label of its own.");
    assert_eq!(bad(json!({ "question": "Q", "options": [{ "label": "A" }, { "label": "B" }], "recommended": 2 })), "recommended is the place of one of the options, from 0.");
    assert!(parse(&question()).is_ok());

    let (scratch, bot, chat_id) = runner();
    let call = AskUser { app: scratch.0.clone(), chat_id, bot, unattended: true };
    let refused = tokio::runtime::Runtime::new().unwrap().block_on(call.execute("call", question(), CancellationToken::new(), Arc::new(|_| {})));
    assert!(refused.unwrap_err().0.starts_with("Nobody is here to answer"));
}
