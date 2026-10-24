//! Memory trace — desktop only, always on in dev.
//! Tiny, human-readable timeline. 3 kinds only.

use std::path::{Path, PathBuf};

use crate::memory;

fn trace_dir(home: &Path) -> PathBuf {
    memory::root(home).join(".trace")
}

#[derive(Debug, Clone, serde::Serialize)]
struct Entry {
    at: i64,
    bot_id: String,
    kind: String,
    text: String,
}

fn append(home: &Path, entry: Entry) {
    let dir = trace_dir(home);
    let _ = std::fs::create_dir_all(&dir);
    let day = memory::local_time(entry.at).date;
    let path = dir.join(format!("{day}.jsonl"));
    let line = serde_json::to_string(&entry).unwrap_or_default();
    use std::io::Write;
    if let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(&path) {
        let _ = writeln!(f, "{line}");
    }
    // Keep last 30 days (best-effort)
    if let Ok(rd) = std::fs::read_dir(&dir) {
        let mut names: Vec<String> = rd
            .flatten()
            .filter_map(|e| e.file_name().into_string().ok())
            .filter(|n| n.ends_with(".jsonl"))
            .collect();
        names.sort();
        while names.len() > 30 {
            let oldest = names.remove(0);
            let _ = std::fs::remove_file(dir.join(oldest));
        }
    }
}

fn now_secs() -> i64 {
    crate::config::now_secs() as i64
}

/// What was recalled and injected for the turn.
pub fn recalled(home: &Path, bot_id: &str, query: &str, injected: &[String]) {
    let text = if injected.is_empty() {
        format!("\"{query}\" — no hits")
    } else {
        format!("\"{}\" → {}", query, injected.join(" · "))
    };
    append(home, Entry { at: now_secs(), bot_id: bot_id.into(), kind: "recalled".into(), text });
}

/// A write to MEMORY.md / PROFILE.md (or a topic).
pub fn saved(home: &Path, bot_id: &str, summary: &str) {
    append(home, Entry { at: now_secs(), bot_id: bot_id.into(), kind: "saved".into(), text: summary.chars().take(160).collect() });
}

/// A supersede (old → new).
pub fn superseded(home: &Path, bot_id: &str, from: &str, to: &str) {
    let text = format!("{} → {}", from.chars().take(60).collect::<String>(), to.chars().take(60).collect::<String>());
    append(home, Entry { at: now_secs(), bot_id: bot_id.into(), kind: "saved".into(), text });
}

pub fn recent(home: &Path, bot_id: Option<&str>, limit: usize) -> Vec<serde_json::Value> {
    let dir = trace_dir(home);
    let mut files: Vec<PathBuf> = std::fs::read_dir(&dir)
        .map(|rd| rd.flatten().map(|e| e.path()).collect())
        .unwrap_or_default();
    files.sort();
    files.reverse();
    let mut out = Vec::new();
    for path in files {
        if out.len() >= limit {
            break;
        }
        if let Ok(text) = std::fs::read_to_string(&path) {
            for line in text.lines().rev() {
                if out.len() >= limit {
                    break;
                }
                if let Ok(v) = serde_json::from_str::<serde_json::Value>(line) {
                    if let Some(want) = bot_id {
                        if v.get("bot_id").and_then(|x| x.as_str()) != Some(want) {
                            continue;
                        }
                    }
                    out.push(v);
                }
            }
        }
    }
    out.reverse();
    out
}
