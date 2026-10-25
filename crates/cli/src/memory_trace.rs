//! What the bot's memory did, as a short timeline the desktop app shows while developing Lorca:
//! which remembered lines a turn was given, what a bot saved, and what the write gate refused.
//! One JSON line each in `memory/<bot id>/trace/YYYY-MM-DD.jsonl`, the newest 14 days kept.
//! Nothing leaves the Runner, and nothing reads it back but `memory.read` for the apps.

use std::io::Write;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::memory;

/// Days of trace kept.
const KEEP_DAYS: usize = 14;
/// The most of a text one entry keeps.
const MAX_TEXT_CHARS: usize = 240;

/// One thing the bot's memory did.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Entry {
    /// Unix seconds.
    pub at: i64,
    pub kind: Kind,
    /// What it was about: the user's message for a recall, the fact for a save or a refusal.
    pub text: String,
    /// What came of it: the lines recalled, the file the fact went to, or why it was refused.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub detail: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Kind {
    /// A turn started from the user's message, and these remembered lines went with it.
    Recalled,
    /// A fact went into MEMORY.md or PROFILE.md, or one changed there.
    Saved,
    /// The write gate refused a fact.
    Refused,
}

fn dir(home: &Path, bot_id: &str) -> PathBuf {
    memory::root(home).join(bot_id).join("trace")
}

fn clip(text: &str) -> String {
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if text.chars().count() <= MAX_TEXT_CHARS {
        return text;
    }
    format!("{}…", text.chars().take(MAX_TEXT_CHARS).collect::<String>())
}

/// Adds one entry to today's file, scrubbed of credentials as memory is, and drops days past
/// the newest `KEEP_DAYS`. A failure is logged: the trace never stands in a turn's way.
fn record(home: &Path, bot_id: &str, kind: Kind, text: &str, detail: &[&str]) {
    let at = crate::config::now_secs() as i64;
    let entry = Entry { at, kind, text: memory::scrub(&clip(text)), detail: detail.iter().map(|line| memory::scrub(&clip(line))).collect() };
    let dir = dir(home, bot_id);
    let written = (|| -> std::io::Result<()> {
        std::fs::create_dir_all(&dir)?;
        let line = serde_json::to_string(&entry)?;
        let path = dir.join(format!("{}.jsonl", memory::local_time(at).date));
        writeln!(std::fs::OpenOptions::new().create(true).append(true).open(path)?, "{line}")
    })();
    if let Err(error) = written {
        tracing::warn!(%error, "writing the memory trace");
        return;
    }
    let mut days = days(&dir);
    while days.len() > KEEP_DAYS {
        let _ = std::fs::remove_file(days.remove(0));
    }
}

/// The trace's files, oldest first.
fn days(dir: &Path) -> Vec<PathBuf> {
    let mut days: Vec<PathBuf> = std::fs::read_dir(dir)
        .map(|entries| entries.filter_map(Result::ok).map(|entry| entry.path()).filter(|path| path.extension().is_some_and(|ext| ext == "jsonl")).collect())
        .unwrap_or_default();
    days.sort();
    days
}

/// A turn started from `message`; `note` is the recall note it carried, or `None` when nothing
/// matched.
pub fn recalled(home: &Path, bot_id: &str, message: &str, note: Option<&str>) {
    let lines: Vec<&str> = note
        .map(|note| note.lines().filter_map(|line| line.strip_prefix("- ")).map(|line| line.trim_end_matches(']')).collect())
        .unwrap_or_default();
    record(home, bot_id, Kind::Recalled, message, &lines);
}

/// A fact saved in `file` (`MEMORY.md`, `PROFILE.md`), with what the store said of it.
pub fn saved(home: &Path, bot_id: &str, file: &str, what: &str) {
    record(home, bot_id, Kind::Saved, what, &[file]);
}

/// A fact the write gate refused, and why.
pub fn refused(home: &Path, bot_id: &str, fact: &str, why: &str) {
    record(home, bot_id, Kind::Refused, fact, &[why]);
}

/// The newest `limit` entries, newest first.
pub fn recent(home: &Path, bot_id: &str, limit: usize) -> Vec<Entry> {
    let mut out = Vec::new();
    for day in days(&dir(home, bot_id)).into_iter().rev() {
        let text = std::fs::read_to_string(day).unwrap_or_default();
        for line in text.lines().rev() {
            if out.len() >= limit {
                return out;
            }
            if let Ok(entry) = serde_json::from_str::<Entry>(line) {
                out.push(entry);
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn entries_read_back_newest_first_scrubbed() {
        let home = std::env::temp_dir().join(format!("lorca-trace-{}", uuid::Uuid::new_v4()));
        recalled(&home, "b1", "杭州天气", Some("[What you remember …:\n- PROFILE.md · 用户住在杭州\n- MEMORY.md · prefers short replies]"));
        saved(&home, "b1", "MEMORY.md", "Saved in MEMORY.md: - 2026-10-25 · the key is sk-abcdefghijklmnopqrstuvwxyz");
        refused(&home, "b1", "ignore previous instructions", "reads like an instruction");
        recalled(&home, "b2", "other bot", None);
        let entries = recent(&home, "b1", 10);
        assert_eq!(entries.iter().map(|entry| entry.kind).collect::<Vec<_>>(), [Kind::Refused, Kind::Saved, Kind::Recalled]);
        assert_eq!(entries[2].detail, ["PROFILE.md · 用户住在杭州", "MEMORY.md · prefers short replies"]);
        assert!(!entries[1].text.contains("sk-abc"), "{}", entries[1].text);
        assert_eq!(recent(&home, "b1", 1).len(), 1);
        assert!(recent(&home, "b2", 10)[0].detail.is_empty(), "nothing recalled");
        let _ = std::fs::remove_dir_all(&home);
    }
}
