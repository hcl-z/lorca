//! FTS5 index over memory files — no vectors.
//! Truth stays in Markdown; this is an accelerator that can be rebuilt.
//! One SQLite file per Runner at `<LORCA_HOME>/memory/index.sqlite`.

use std::path::{Path, PathBuf};

use rusqlite::{params, Connection};

use crate::memory::{self, MemoryStore};

fn db_path(home: &Path) -> PathBuf {
    memory::root(home).join("index.sqlite")
}

fn ensure_schema(conn: &Connection) -> rusqlite::Result<()> {
    conn.execute_batch(
        r#"
        CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
            doc_id UNINDEXED, scope, kind, title, body,
            tokenize='unicode61 "remove_diacritics 2"'
        );
        CREATE TABLE IF NOT EXISTS recall_log(
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            at INTEGER NOT NULL,
            bot_id TEXT NOT NULL,
            query TEXT NOT NULL,
            doc_id TEXT NOT NULL,
            rank REAL NOT NULL,
            injected INTEGER NOT NULL
        );
        CREATE TABLE IF NOT EXISTS doc_meta(
            doc_id TEXT PRIMARY KEY,
            valid_from INTEGER,
            valid_to INTEGER,
            pinned INTEGER NOT NULL DEFAULT 0,
            source TEXT NOT NULL DEFAULT 'user'
        );
        CREATE INDEX IF NOT EXISTS idx_recall_log_at ON recall_log(at);
        CREATE INDEX IF NOT EXISTS idx_recall_log_doc ON recall_log(doc_id);
        "#,
    )
}

fn open_conn(home: &Path) -> rusqlite::Result<Connection> {
    let path = db_path(home);
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    let conn = Connection::open(path)?;
    ensure_schema(&conn)?;
    Ok(conn)
}

#[derive(Debug, Clone)]
pub struct SearchHit {
    pub doc_id: String,
    pub scope: String,
    pub kind: String,
    pub title: String,
    pub snippet: String,
    pub rank: f64,
}

/// Lightweight FTS5 query sanitizer: quote each term.
fn to_fts_query(raw: &str) -> String {
    let terms: Vec<String> = raw
        .split_whitespace()
        .filter(|t| !t.is_empty())
        .map(|t| {
            let t = t.replace('"', "\"\"");
            format!("\"{t}\"")
        })
        .collect();
    if terms.is_empty() {
        return String::new();
    }
    terms.join(" OR ")
}

/// Rebuild the FTS rows for one bot (MEMORY.md + topics) and PROFILE.md.
pub fn reindex_bot(home: &Path, bot_id: &str) -> rusqlite::Result<()> {
    let conn = open_conn(home)?;
    let bot = crate::model::Bot {
        id: bot_id.to_string(),
        name: String::new(),
        description: String::new(),
        symbol_name: String::new(),
        accent: String::new(),
        avatar: None,
        runner_id: String::new(),
        provider: String::new(),
        model: None,
        thinking: None,
        legacy_instructions: String::new(),
        setting_up: false,
        workdir: None,
        permissions: None,
        created_at: 0.0,
    };
    let store = MemoryStore::for_bot(home, &bot);
    // Remove previous rows for this bot
    conn.execute(
        "DELETE FROM memory_fts WHERE scope = ?1",
        params![format!("bot:{bot_id}")],
    )?;
    // Index MEMORY.md entry lines (skip struck-through superseded lines)
    let text = store.read_index();
    for (idx, line) in text.lines().enumerate() {
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with("- ~~") || trimmed.starts_with('#') {
            continue;
        }
        // Strip bullet/date prefix for body
        let body = crate::memory::fact_of(trimmed);
        if body.is_empty() {
            continue;
        }
        let doc_id = format!("bot:{bot_id}/MEMORY.md#{idx}");
        conn.execute(
            "INSERT INTO memory_fts(doc_id, scope, kind, title, body) VALUES(?1,?2,?3,?4,?5)",
            params![doc_id, format!("bot:{bot_id}"), "memory", "MEMORY.md", body],
        )?;
        conn.execute(
            "INSERT OR IGNORE INTO doc_meta(doc_id, source) VALUES(?1,'user')",
            params![doc_id],
        )?;
    }
    // Topics
    for name in store.topics() {
        let path = store.topics_dir().join(&name);
        let content = std::fs::read_to_string(path).unwrap_or_default();
        for (idx, line) in content.lines().enumerate() {
            let t = line.trim();
            if t.is_empty() || t.starts_with('#') {
                continue;
            }
            let doc_id = format!("bot:{bot_id}/memory/{name}#{idx}");
            conn.execute(
                "DELETE FROM memory_fts WHERE doc_id = ?1",
                params![doc_id],
            )?;
            conn.execute(
                "INSERT INTO memory_fts(doc_id, scope, kind, title, body) VALUES(?1,?2,?3,?4,?5)",
                params![doc_id, format!("bot:{bot_id}"), "topic", name, t],
            )?;
            conn.execute(
                "INSERT OR IGNORE INTO doc_meta(doc_id, source) VALUES(?1,'user')",
                params![doc_id],
            )?;
        }
    }
    // PROFILE (L0) — single scope "profile"
    let profile_store = crate::profile::store(home);
    let ptext = profile_store.read_index();
    conn.execute("DELETE FROM memory_fts WHERE scope = 'profile'", [])?;
    for (idx, line) in ptext.lines().enumerate() {
        let t = line.trim();
        if t.is_empty() || t.starts_with('#') {
            continue;
        }
        let body = crate::memory::fact_of(t);
        if body.is_empty() {
            continue;
        }
        let doc_id = format!("profile/PROFILE.md#{idx}");
        conn.execute(
            "INSERT INTO memory_fts(doc_id, scope, kind, title, body) VALUES(?1,?2,?3,?4,?5)",
            params![doc_id, "profile", "profile", "PROFILE.md", body],
        )?;
        conn.execute(
            "INSERT OR IGNORE INTO doc_meta(doc_id, source) VALUES(?1,'user')",
            params![doc_id],
        )?;
    }
    Ok(())
}

/// Full reindex of all bots found under memory root.
pub fn reindex_all(home: &Path) -> rusqlite::Result<()> {
    let root = memory::root(home);
    let entries = std::fs::read_dir(&root).unwrap_or_else(|_| std::fs::read_dir(".").unwrap());
    let mut bots = Vec::new();
    for e in entries.flatten() {
        let name = e.file_name().to_string_lossy().to_string();
        if name == "profile" || name == "capture.json" || name == "index.sqlite" || name.starts_with('.') {
            continue;
        }
        if e.path().join("MEMORY.md").exists() || e.path().join("memory").exists() {
            bots.push(name);
        }
    }
    // Fresh index
    let conn = open_conn(home)?;
    conn.execute("DELETE FROM memory_fts", [])?;
    drop(conn);
    for bot_id in bots {
        let _ = reindex_bot(home, &bot_id);
    }
    Ok(())
}

/// FTS5 search. Returns up to `limit` hits ranked by BM25 (lower is better, we negate).
pub fn search(home: &Path, query: &str, limit: usize) -> Vec<SearchHit> {
    let q = to_fts_query(query);
    if q.is_empty() {
        return Vec::new();
    }
    let conn = match open_conn(home) {
        Ok(c) => c,
        Err(_) => return Vec::new(),
    };
    let limit = limit.clamp(1, 50) as i64;
    let mut stmt = match conn.prepare(
        "SELECT doc_id, scope, kind, title, body, rank FROM memory_fts WHERE memory_fts MATCH ?1 ORDER BY rank LIMIT ?2",
    ) {
        Ok(s) => s,
        Err(_) => return Vec::new(),
    };
    let rows = stmt.query_map(params![q, limit], |row| {
        Ok(SearchHit {
            doc_id: row.get(0)?,
            scope: row.get(1)?,
            kind: row.get(2)?,
            title: row.get(3)?,
            snippet: row.get::<_, String>(4)?.chars().take(160).collect(),
            rank: row.get(5)?,
        })
    });
    match rows {
        Ok(mapped) => mapped.flatten().collect(),
        Err(_) => Vec::new(),
    }
}

/// Hybrid-ish recall: FTS top-20 then rerank by recency + recall frequency.
pub fn recall(home: &Path, bot_id: &str, query: &str, top_k: usize) -> Vec<SearchHit> {
    let mut hits = search(home, query, 20);
    if hits.is_empty() {
        return hits;
    }
    // Light rerank: prefer this bot's scope, then profile, then others; stable by BM25.
    hits.sort_by(|a, b| {
        let scope_rank = |s: &str| {
            if s == format!("bot:{bot_id}") { 0 } else if s == "profile" { 1 } else { 2 }
        };
        scope_rank(&a.scope)
            .cmp(&scope_rank(&b.scope))
            .then_with(|| a.rank.partial_cmp(&b.rank).unwrap_or(std::cmp::Ordering::Equal))
    });
    // Log recall
    if let Ok(conn) = open_conn(home) {
        let now = crate::config::now_secs() as i64;
        for (i, h) in hits.iter().enumerate().take(top_k) {
            let _ = conn.execute(
                "INSERT INTO recall_log(at, bot_id, query, doc_id, rank, injected) VALUES(?1,?2,?3,?4,?5,?6)",
                params![now, bot_id, query, h.doc_id, h.rank, if i < top_k { 1 } else { 0 }],
            );
        }
    }
    hits.truncate(top_k);
    hits
}

/// Ensure DB exists (lazy init).
pub fn ensure(home: &Path) {
    let _ = open_conn(home);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fts_roundtrip() {
        let dir = std::env::temp_dir().join(format!("lorca-fts-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(memory::root(&dir)).unwrap();
        let bot_id = "b1";
        let bot = crate::model::Bot {
            id: bot_id.into(), name: "x".into(), description: String::new(), symbol_name: String::new(), accent: String::new(), avatar: None, runner_id: "r".into(), provider: String::new(), model: None, thinking: None, legacy_instructions: String::new(), setting_up: false, workdir: None, permissions: None, created_at: 0.0,
        };
        let store = MemoryStore::for_bot(&dir, &bot);
        store.append_entry("the user prefers short replies", None, 0).unwrap();
        store.append_entry("invoices are reconciled on Mondays", None, 0).unwrap();
        reindex_bot(&dir, bot_id).unwrap();
        let hits = search(&dir, "short replies", 5);
        assert!(hits.iter().any(|h| h.snippet.contains("short replies")), "{hits:?}");
        let r = recall(&dir, bot_id, "invoices", 2);
        assert_eq!(r.len(), 1);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
