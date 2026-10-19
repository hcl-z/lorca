//! What the user is, on this Runner: one `PROFILE.md` under the memory root that every bot
//! reads. A bot's own memory is about its work; this is about the person — their name, how they
//! like to be spoken to, the facts that hold in every chat with every bot. It is shared because a
//! second bot should not have to learn the same things again.
//!
//! Like memory it is plain markdown with one dated entry per line, written by bots only through
//! `profile_update` and the write gate, and scrubbed of credentials. Its text rides the note after
//! the transcript, so a fact added mid-turn leaves the cached system prompt alone.

use std::path::Path;

use crate::memory::{self, MemoryStore};

/// How much of `PROFILE.md` opens every turn: the first lines or bytes, whichever cuts first.
/// A bot's write that would take it past the bytes is refused, so the profile stays whole.
pub const PROFILE_MAX_LINES: usize = 120;
pub const PROFILE_MAX_BYTES: usize = 8_000;

/// The account's profile on this Runner.
pub fn store(home: &Path) -> MemoryStore {
    MemoryStore::with_budget(memory::root(home).join("profile"), "PROFILE.md", PROFILE_MAX_LINES, PROFILE_MAX_BYTES)
}

/// The system prompt's part: what the profile is and how a bot adds to it. The same on every
/// turn, whatever the profile holds.
pub fn instructions() -> &'static str {
    "\nAbout the user: PROFILE.md, in the note after the transcript, is what every bot on this Runner knows about the \
     user. When the user tells you something about themselves or how every bot should work for them (name, role, \
     timezone, language, reply style), save it with profile_update, one fact per call; supersede one that changed, remove \
     one that is wrong.\n"
}

/// The profile as the note after the transcript reads it, under its budget. `None` when the user
/// has nothing in it yet.
pub fn note(store: &MemoryStore) -> Option<String> {
    let index = store.load_index();
    if index.text.trim().is_empty() {
        return None;
    }
    let mut note = format!("[About the user (PROFILE.md):\n{}\n", index.text.trim_end());
    if index.truncated {
        note.push_str(&format!(
            "\n(PROFILE.md is {} lines and {} bytes; only the first {PROFILE_MAX_LINES} lines / {PROFILE_MAX_BYTES} bytes \
             are shown. Consolidate it with profile_update: replace, supersede, or remove older entries.)\n",
            index.lines, index.bytes
        ));
    }
    note.push(']');
    Some(note)
}

/// What the apps show: the profile with its budget, as `bots.memory` shows the index.
pub fn overview(store: &MemoryStore) -> serde_json::Value {
    let mut overview = store.overview()["index"].clone();
    overview["path"] = serde_json::json!(store.index_path().display().to_string());
    overview
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scratch() -> (std::path::PathBuf, MemoryStore) {
        let dir = std::env::temp_dir().join(format!("lorca-profile-{}", uuid::Uuid::new_v4()));
        let store = store(&dir);
        (dir, store)
    }

    #[test]
    fn a_profile_is_shared_and_bounded() {
        let (dir, store) = scratch();
        assert!(note(&store).is_none(), "no profile means no note");
        let now = memory::local_unix("2026-09-17", Some("09:05")).unwrap();
        store.append_entry("the user prefers short replies", Some("chat with the user"), now).unwrap();
        store.append_entry("the user works in Shanghai", None, now).unwrap();
        let note = note(&store).unwrap();
        assert!(note.contains("prefers short replies") && note.contains("Shanghai"), "{note}");
        assert!(!store.is_over_budget());
        assert_eq!(store.budget(), (PROFILE_MAX_LINES, PROFILE_MAX_BYTES));
        assert_eq!(overview(&store)["max_bytes"], PROFILE_MAX_BYTES);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn a_long_profile_says_what_is_cut() {
        let (dir, store) = scratch();
        for i in 0..(PROFILE_MAX_LINES + 3) {
            store.append_entry(&format!("fact {i}"), None, 0).unwrap();
        }
        assert!(store.is_over_budget());
        assert!(note(&store).unwrap().contains("only the first"));
        let _ = std::fs::remove_dir_all(&dir);
    }
}
