//! Housekeeping a bot's `MEMORY.md` on its own, so what opens every turn stays what the bot uses.
//! It moves lines, never rewrites or forgets them: the struck-through history of supersedes goes
//! to `memory/history.md`, and an entry no turn was reminded of for 60 days, while the file is
//! past three quarters of its load budget, goes to `memory/archive.md`. Both are topic files, so
//! `recall` and a turn's recall note still find what moved. No model call runs.
//!
//! Each pass is a run kept under `memory/<bot id>/tidy/` with exactly what it moved, and undoing
//! one puts the lines back. Only `MEMORY.md` is tidied: the user's profile is the account's and
//! syncs to every Device, so nothing changes it in the background.

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::memory::{self, MemoryStore};

/// A tidy runs at most once a day per bot.
const EVERY_SECS: i64 = 86_400;
/// An entry no turn was reminded of for this long may be archived.
const UNUSED_SECS: i64 = 60 * 86_400;
/// Archiving starts once `MEMORY.md` holds this share of what loads every turn.
const ARCHIVE_AT: f64 = 0.75;
/// And moves at most this many entries in one run.
const MAX_ARCHIVED: usize = 20;

/// Where struck-through history goes, and where unused entries go.
pub const HISTORY: &str = "history.md";
pub const ARCHIVE: &str = "archive.md";

/// One pass, kept so the user can undo it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Run {
    pub id: String,
    /// Unix seconds.
    pub at: i64,
    /// The struck-through lines moved to `memory/history.md`.
    pub history: Vec<String>,
    /// The entries moved to `memory/archive.md`.
    pub archived: Vec<String>,
    pub undone: bool,
}

fn runs_dir(store: &MemoryStore) -> PathBuf {
    store.dir().join("tidy")
}

/// The runs kept for one bot, newest first.
pub fn runs(store: &MemoryStore) -> Vec<Run> {
    let mut runs: Vec<Run> = std::fs::read_dir(runs_dir(store))
        .map(|entries| entries.filter_map(Result::ok).filter_map(|entry| std::fs::read_to_string(entry.path()).ok()).filter_map(|text| serde_json::from_str(&text).ok()).collect())
        .unwrap_or_default();
    runs.sort_by(|a, b| b.at.cmp(&a.at));
    runs
}

/// The newest run that moved something the user has not undone: what the apps offer to undo.
pub fn latest(store: &MemoryStore) -> Option<Run> {
    runs(store).into_iter().find(|run| !run.undone && (!run.history.is_empty() || !run.archived.is_empty()))
}

fn save_run(store: &MemoryStore, run: &Run) -> Result<(), String> {
    let dir = runs_dir(store);
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let tmp = dir.join(format!("{}.json.tmp", run.id));
    std::fs::write(&tmp, serde_json::to_vec_pretty(run).map_err(|e| e.to_string())?).map_err(|e| e.to_string())?;
    std::fs::rename(&tmp, dir.join(format!("{}.json", run.id))).map_err(|e| e.to_string())
}

/// When a bot's tidy last ran, from its newest run, or never.
fn last_run_at(store: &MemoryStore) -> Option<i64> {
    std::fs::read_to_string(runs_dir(store).join("last")).ok().and_then(|text| text.trim().parse().ok())
}

fn mark_ran(store: &MemoryStore, at: i64) {
    let dir = runs_dir(store);
    if std::fs::create_dir_all(&dir).and_then(|_| std::fs::write(dir.join("last"), at.to_string())).is_err() {
        tracing::warn!("marking a memory tidy");
    }
}

/// The facts a user's message found since `since`, from the bot's memory trace, keyed as the
/// store keys facts: those its turn was given, and those it found in what the turn loaded.
fn recalled_since(home: &Path, bot_id: &str, since: i64) -> std::collections::HashSet<String> {
    crate::memory_trace::recent(home, bot_id, 10_000)
        .into_iter()
        .filter(|entry| entry.kind == crate::memory_trace::Kind::Recalled && entry.at >= since)
        .flat_map(|entry| {
            let given = entry.detail.into_iter().filter_map(|line| line.split_once(" · ").map(|(_, fact)| fact.to_string()));
            given.chain(entry.matched).collect::<Vec<_>>()
        })
        .map(|fact| memory::fact_of(&fact).to_lowercase())
        .collect()
}

/// When an entry was saved, from its date, as Unix seconds.
fn saved_at(line: &str) -> Option<i64> {
    let date = line.trim().trim_start_matches("- ").get(..10)?;
    memory::local_unix(date, None)
}

/// Appends `lines` to a topic file of the store, creating it with a heading.
fn append_topic(store: &MemoryStore, name: &str, heading: &str, lines: &[String]) -> Result<(), String> {
    let path = store.topics_dir().join(name);
    let mut text = std::fs::read_to_string(&path).unwrap_or_else(|_| format!("# {heading}\n"));
    if !text.ends_with('\n') {
        text.push('\n');
    }
    for line in lines {
        text.push_str(line);
        text.push('\n');
    }
    store.write_topic(name, &text).map_err(|e| e.to_string())
}

/// Tidies one bot's `MEMORY.md` as of `now`, when it is due. `None` when nothing was due or
/// nothing moved.
pub fn tidy(home: &Path, bot_id: &str, store: &MemoryStore, now: i64) -> Result<Option<Run>, String> {
    if last_run_at(store).is_some_and(|last| now - last < EVERY_SECS) {
        return Ok(None);
    }
    mark_ran(store, now);
    let text = store.read_index();
    let (max_lines, max_bytes) = store.budget();
    let full = text.lines().count() as f64 >= max_lines as f64 * ARCHIVE_AT || text.len() as f64 >= max_bytes as f64 * ARCHIVE_AT;
    let recalled = if full { recalled_since(home, bot_id, now - UNUSED_SECS) } else { Default::default() };
    let mut history = Vec::new();
    let mut archived = Vec::new();
    let mut kept = Vec::new();
    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.starts_with("- ~~") {
            history.push(trimmed.to_string());
            continue;
        }
        let unused = full
            && archived.len() < MAX_ARCHIVED
            && trimmed.starts_with("- ")
            && saved_at(trimmed).is_some_and(|at| now - at >= UNUSED_SECS)
            && !recalled.contains(&memory::fact_of(trimmed).to_lowercase());
        if unused {
            archived.push(trimmed.to_string());
            continue;
        }
        kept.push(line);
    }
    if history.is_empty() && archived.is_empty() {
        return Ok(None);
    }
    // The topics first, so a crash in between leaves a line in both files, never in neither.
    if !history.is_empty() {
        append_topic(store, HISTORY, "What changed (superseded entries, moved from MEMORY.md)", &history)?;
    }
    if !archived.is_empty() {
        append_topic(store, ARCHIVE, "Archived (no turn was reminded of these for 60 days)", &archived)?;
    }
    let mut new = kept.join("\n");
    if !new.is_empty() {
        new.push('\n');
    }
    store.write_index(&new, Some(&memory::hash_text(&text))).map_err(|e| e.to_string())?;
    let run = Run { id: format!("tidy-{}", uuid::Uuid::new_v4()), at: now, history, archived, undone: false };
    save_run(store, &run)?;
    Ok(Some(run))
}

/// Takes `lines` out of a topic file, each once.
fn take_from_topic(store: &MemoryStore, name: &str, lines: &[String]) -> Result<(), String> {
    let path = store.topics_dir().join(name);
    let Ok(text) = std::fs::read_to_string(&path) else { return Ok(()) };
    let mut left: Vec<&str> = text.lines().collect();
    for line in lines {
        if let Some(at) = left.iter().position(|kept| kept.trim() == line.trim()) {
            left.remove(at);
        }
    }
    let mut new = left.join("\n");
    new.push('\n');
    store.write_topic(name, &new).map_err(|e| e.to_string())
}

/// Puts back what a run moved: its lines return to the end of `MEMORY.md`, archived entries
/// first, and leave the topic files. Answers how many lines came back.
pub fn undo(store: &MemoryStore, run_id: &str) -> Result<usize, String> {
    let mut run = runs(store).into_iter().find(|run| run.id == run_id).ok_or("No such tidy run.")?;
    if run.undone {
        return Ok(0);
    }
    let text = store.read_index();
    let mut new = text.clone();
    if !new.is_empty() && !new.ends_with('\n') {
        new.push('\n');
    }
    let back: Vec<&String> = run.archived.iter().chain(&run.history).filter(|line| !text.lines().any(|kept| kept.trim() == line.trim())).collect();
    for line in &back {
        new.push_str(line);
        new.push('\n');
    }
    store.write_index(&new, Some(&memory::hash_text(&text))).map_err(|e| e.to_string())?;
    take_from_topic(store, HISTORY, &run.history)?;
    take_from_topic(store, ARCHIVE, &run.archived)?;
    run.undone = true;
    save_run(store, &run)?;
    Ok(back.len())
}

/// The scheduler's hook: each bot on this Runner whose tidy is due is tidied, while none of its
/// turns runs. `LORCA_MEMORY_TIDY=0` turns it off.
#[cfg(feature = "runner")]
pub fn tick(app: &std::sync::Arc<crate::app::App>) {
    if std::env::var("LORCA_MEMORY_TIDY").ok().as_deref() == Some("0") {
        return;
    }
    let Some(this) = app.this_device_id() else { return };
    let running: std::collections::HashSet<String> = app.running_chat_ids().into_iter().collect();
    let (bots, busy): (Vec<crate::model::Bot>, std::collections::HashSet<String>) = {
        let state = app.state.lock().unwrap();
        let busy = state.chats.iter().filter(|chat| running.contains(&chat.meta.id)).flat_map(|chat| chat.meta.bot_ids.clone()).collect();
        (state.bots.clone(), busy)
    };
    let now = crate::config::now_secs() as i64;
    for bot in bots.iter().filter(|bot| bot.runner_id == this && !bot.setting_up && !busy.contains(&bot.id)) {
        let store = MemoryStore::for_bot(&app.config.home, bot);
        match tidy(&app.config.home, &bot.id, &store, now) {
            Ok(Some(run)) => {
                let moved = run.history.len() + run.archived.len();
                crate::memory_trace::saved(&app.config.home, &bot.id, "memory/", &format!("tidied {moved} lines out of MEMORY.md"));
            }
            Ok(None) => {}
            Err(error) => tracing::warn!(%error, bot = %bot.name, "tidying memory"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Scratch(PathBuf);
    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn scratch() -> (Scratch, MemoryStore) {
        let home = std::env::temp_dir().join(format!("lorca-tidy-{}", uuid::Uuid::new_v4()));
        let store = MemoryStore::with_budget(memory::root(&home).join("b1"), "MEMORY.md", 8, 24_000);
        (Scratch(home), store)
    }

    fn day(date: &str) -> i64 {
        memory::local_unix(date, Some("12:00")).unwrap()
    }

    #[test]
    fn history_moves_out_and_comes_back_on_undo() {
        let (scratch, store) = scratch();
        store.append_entry("the office is in Pune", None, day("2026-01-01")).unwrap();
        store.append_entry("the user ships on Fridays", None, day("2026-01-01")).unwrap();
        store.supersede("the office is in Pune", "the office is in Goa", None, day("2026-02-01")).unwrap();
        let before = store.read_index();
        let run = tidy(&scratch.0, "b1", &store, day("2026-02-02")).unwrap().unwrap();
        assert_eq!(run.history.len(), 1);
        assert!(run.archived.is_empty(), "the file is far from its budget");
        assert!(!store.read_index().contains("~~"), "{}", store.read_index());
        let history = std::fs::read_to_string(store.topics_dir().join(HISTORY)).unwrap();
        assert!(history.contains("~~") && history.contains("Pune"), "{history}");
        assert!(crate::memory_check::check("the office is in Pune", &store).is_err(), "the gate still knows it was replaced");
        assert_eq!(tidy(&scratch.0, "b1", &store, day("2026-02-02") + 3_600).unwrap(), None, "once a day");

        assert_eq!(undo(&store, &run.id).unwrap(), 1);
        let after = store.read_index();
        let back: std::collections::BTreeSet<&str> = after.lines().collect();
        assert_eq!(back, before.lines().collect(), "every line is back");
        assert!(!std::fs::read_to_string(store.topics_dir().join(HISTORY)).unwrap().contains("Pune"));
    }

    #[test]
    fn an_unused_entry_is_archived_only_when_memory_is_nearly_full() {
        let (scratch, store) = scratch();
        let old = day("2026-01-01");
        for fact in ["the launch is on Friday", "invoices go out on Mondays", "the user likes tea", "the repo is lorca", "Acme pays net 30"] {
            store.append_entry(fact, None, old).unwrap();
        }
        let now = day("2026-04-01");
        assert_eq!(tidy(&scratch.0, "b1", &store, now).unwrap(), None, "five of eight lines: not full");
        store.append_entry("standups are at nine", None, now).unwrap();
        // The launch line loads every turn, so a message about it finds it there.
        crate::memory_trace::recalled(&scratch.0, "b1", "when is the launch", None, &["the launch is on Friday".into()]);
        let run = tidy(&scratch.0, "b1", &store, now + EVERY_SECS).unwrap().unwrap();
        let archived: Vec<String> = run.archived.iter().map(|line| memory::fact_of(line)).collect();
        assert_eq!(archived, ["invoices go out on Mondays", "the user likes tea", "the repo is lorca", "Acme pays net 30"]);
        let left = store.read_index();
        assert!(left.contains("the launch is on Friday"), "a turn was reminded of it lately");
        assert!(left.contains("standups are at nine"), "saved lately");
        let archive = std::fs::read_to_string(store.topics_dir().join(ARCHIVE)).unwrap();
        assert!(archive.contains("the user likes tea"), "{archive}");
        assert!(store.topics().contains(&ARCHIVE.to_string()), "a topic file, so recall finds it");
        assert_eq!(undo(&store, &run.id).unwrap(), 4);
        assert!(store.read_index().contains("the user likes tea"));
    }

    #[test]
    fn an_edit_in_between_refuses_the_tidy() {
        let (_scratch, store) = scratch();
        store.append_entry("the office is in Pune", None, 0).unwrap();
        store.supersede("the office is in Pune", "the office is in Goa", None, 0).unwrap();
        let text = store.read_index();
        // A bot writes between the read and the write: the write is refused and nothing moved.
        std::fs::write(store.index_path(), format!("{text}- a newer fact\n")).unwrap();
        let hash = memory::hash_text(&text);
        assert!(store.write_index("", Some(&hash)).is_err(), "the guard this tidy relies on");
    }
}
