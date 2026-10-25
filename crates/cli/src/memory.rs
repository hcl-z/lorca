//! A bot's long-term memory on its Runner, in Grok Bot's shape of a curated profile over
//! append-only logs: `MEMORY.md` opens every turn under a load budget, `memory/<topic>.md`
//! files are read on demand, and `memory/log/YYYY-MM-DD.md` is the bot's diary of what
//! happened, never shown but searchable. Everything is plain markdown the user can open and
//! edit. Every write goes through the credential scrubber first: memory is bot-authored as
//! often as person-authored, and a key the bot read from a file must not end up on disk.
//!
//! Memory lives under `<LORCA_HOME>/memory/`, outside every bot's working directory, so a
//! bot's own file and shell tools cannot rewrite it: `memory_update` is the only writer, which
//! keeps one dated fact per entry and a supersede history.

use std::fmt;
use std::path::{Path, PathBuf};
use std::sync::LazyLock;

use regex::Regex;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

/// How much of `MEMORY.md` opens every turn: the first lines or bytes, whichever cuts first.
pub const MEMORY_MAX_LINES: usize = 200;
pub const MEMORY_MAX_BYTES: usize = 24_000;
/// The most a memory file may hold at all, from the app or a tool.
pub const MEMORY_FILE_MAX_BYTES: usize = 256 * 1024;
const SEP: &str = " · ";

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MemoryError {
    Invalid(String),
    /// The passage to change is not in the file.
    NotFound,
    /// The passage appears more than once, so the change is not unique.
    Ambiguous(usize),
    /// The file changed since the caller read it.
    Conflict { hash: String },
    TooLarge,
    Io(String),
}

impl fmt::Display for MemoryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            MemoryError::Invalid(why) => write!(f, "{why}"),
            MemoryError::NotFound => write!(f, "That passage is not in the file. Read it again and pass the exact text."),
            MemoryError::Ambiguous(n) => write!(f, "That passage appears {n} times. Pass a longer, unique passage."),
            MemoryError::Conflict { .. } => write!(f, "The file changed since you opened it. Reload it and apply only your change."),
            MemoryError::TooLarge => write!(f, "The file would be over {} KB.", MEMORY_FILE_MAX_BYTES / 1024),
            MemoryError::Io(why) => write!(f, "{why}"),
        }
    }
}

impl From<std::io::Error> for MemoryError {
    fn from(error: std::io::Error) -> Self {
        MemoryError::Io(error.to_string())
    }
}

/// A fact that stops being true on a date, as a `YYYY-MM-DD` day, and the line as it was saved.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct Expiry {
    pub until: String,
    /// The entry line, so the fact can be matched back to it.
    #[serde(default)]
    pub line: String,
}

impl Expiry {
    /// The date as a day, when it is one.
    pub fn day(&self) -> Option<&str> {
        DATE.is_match(&self.until).then_some(self.until.as_str())
    }
}

/// The key a fact is held under when it expires: what `fact_of` keeps, lowercased.
pub fn expiry_key(line: &str) -> String {
    fact_of(line).to_lowercase()
}

/// What a change to `MEMORY.md` did.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Change {
    /// The fact was already there, word for word.
    Duplicate,
    Appended { line: String },
    Replaced,
    Removed,
    Superseded { line: String },
}

/// The part of `MEMORY.md` a turn sees, and what was left out.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoadedIndex {
    pub text: String,
    pub truncated: bool,
    pub lines: usize,
    pub bytes: usize,
}

/// One line found by `recall`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Hit {
    /// Unix seconds, when the line carries a time.
    pub at: Option<i64>,
    /// Where it came from: `MEMORY.md`, `memory/clients.md`, `log 2026-09-17`.
    pub source: String,
    pub text: String,
}

/// A local calendar date and clock time.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LocalTime {
    pub date: String,
    pub clock: String,
}

/// The machine's local time, the way a person would note it.
pub fn local_time(unix: i64) -> LocalTime {
    let tm = local_tm(unix);
    LocalTime {
        date: format!("{:04}-{:02}-{:02}", tm.tm_year + 1900, tm.tm_mon + 1, tm.tm_mday),
        clock: format!("{:02}:{:02}", tm.tm_hour, tm.tm_min),
    }
}

pub(crate) fn local_tm(unix: i64) -> libc::tm {
    let mut tm: libc::tm = unsafe { std::mem::zeroed() };
    let t = unix as libc::time_t;
    #[cfg(unix)]
    unsafe { libc::localtime_r(&t, &mut tm) };
    // Windows' `localtime_s` takes the same two arguments the other way round.
    #[cfg(windows)]
    unsafe { libc::localtime_s(&mut tm, &t) };
    tm
}

#[cfg(unix)]
use libc::mktime;

// The CRT's `mktime` is an inline wrapper around `_mktime64`, so libc has no binding for it.
#[cfg(windows)]
extern "C" {
    #[link_name = "_mktime64"]
    fn mktime(tm: *mut libc::tm) -> i64;
}

/// How far the machine's local time is ahead of UTC at `unix`, in seconds: the local wall clock
/// read as if it were UTC, less the instant.
pub fn utc_offset_secs(unix: i64) -> i64 {
    let tm = local_tm(unix);
    let (year, month) = (tm.tm_year as i64 + 1900, tm.tm_mon as i64 + 1);
    // Days since 1970-01-01 of the civil date (Howard Hinnant's days_from_civil).
    let y = if month <= 2 { year - 1 } else { year };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let doy = (153 * ((month + 9) % 12) + 2) / 5 + tm.tm_mday as i64 - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    let days = era * 146_097 + doe - 719_468;
    days * 86_400 + tm.tm_hour as i64 * 3_600 + tm.tm_min as i64 * 60 + tm.tm_sec as i64 - unix
}

/// Midnight at the start of the local day `unix` falls in.
pub fn start_of_local_day(unix: i64) -> i64 {
    let mut tm = local_tm(unix);
    tm.tm_hour = 0;
    tm.tm_min = 0;
    tm.tm_sec = 0;
    tm.tm_isdst = -1;
    unsafe { mktime(&mut tm) as i64 }
}

/// Unix seconds of a local `YYYY-MM-DD` at `HH:MM` (midnight when `clock` is `None`).
pub fn local_unix(date: &str, clock: Option<&str>) -> Option<i64> {
    let mut parts = date.splitn(3, '-').map(|p| p.parse::<i32>().ok());
    let (year, month, day) = (parts.next()??, parts.next()??, parts.next()??);
    let (hour, minute) = match clock {
        Some(clock) => {
            let mut hm = clock.splitn(2, ':').map(|p| p.parse::<i32>().ok());
            (hm.next()??, hm.next()??)
        }
        None => (0, 0),
    };
    let mut tm: libc::tm = unsafe { std::mem::zeroed() };
    tm.tm_year = year - 1900;
    tm.tm_mon = month - 1;
    tm.tm_mday = day;
    tm.tm_hour = hour;
    tm.tm_min = minute;
    tm.tm_isdst = -1;
    let t = unsafe { mktime(&mut tm) };
    (t >= 0).then_some(t as i64)
}

/// `since` / `until` as a bot says it: `24h`, `3d`, `2w`, `today`, `yesterday`, or a date.
pub fn parse_when(text: &str, now: i64) -> Option<i64> {
    let text = text.trim().to_lowercase();
    if text.is_empty() {
        return None;
    }
    match text.as_str() {
        "today" => return Some(start_of_local_day(now)),
        "yesterday" => return Some(start_of_local_day(now - 86_400)),
        _ => {}
    }
    if let Some(number) = text.strip_suffix('h').and_then(|n| n.parse::<i64>().ok()) {
        return Some(now - number * 3_600);
    }
    if let Some(number) = text.strip_suffix('d').and_then(|n| n.parse::<i64>().ok()) {
        return Some(now - number * 86_400);
    }
    if let Some(number) = text.strip_suffix('w').and_then(|n| n.parse::<i64>().ok()) {
        return Some(now - number * 7 * 86_400);
    }
    if DATE.is_match(&text) {
        return local_unix(&text, None);
    }
    None
}

static DATE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^\d{4}-\d{2}-\d{2}$").unwrap());
static ENTRY_PREFIX: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^(?:\d{4}-\d{2}-\d{2}\s*·?\s*)?(?:from [^·]*·\s*)?").unwrap());
static LOG_LINE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^- (\d{2}:\d{2})(?: · (.*))?$").unwrap());
static TOPIC_NAME: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^[A-Za-z0-9][A-Za-z0-9 ._-]{0,80}\.md$").unwrap());

// MARK: - Credential scrubbing

/// Whole matches that are credentials wherever they appear.
static SECRET_TOKENS: LazyLock<Vec<Regex>> = LazyLock::new(|| {
    [
        r"-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----",
        r"\bsk-[A-Za-z0-9_-]{16,}",
        r"\bAKIA[0-9A-Z]{16}\b",
        r"\bgh[pousr]_[A-Za-z0-9]{20,}",
        r"\bxox[baprs]-[A-Za-z0-9-]{10,}",
        r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}",
        r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}",
    ]
    .iter()
    .map(|p| Regex::new(p).unwrap())
    .collect()
});

/// `password: hunter2`, `API_KEY=…`: the value after the name is the secret.
static SECRET_ASSIGNMENT: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"(?i)\b((?:api[_-]?key|secret(?:[_-]?key)?|access[_-]?token|auth[_-]?token|token|password|passwd|pwd)\s*[:=]\s*["']?)([^\s"',;]{6,})"#).unwrap()
});

fn redaction(len: usize) -> String {
    format!("«redacted {len} chars»")
}

/// Replaces anything that looks like a credential with a marker of its length.
pub fn scrub(text: &str) -> String {
    let mut out = text.to_string();
    for pattern in SECRET_TOKENS.iter() {
        out = pattern.replace_all(&out, |m: &regex::Captures| redaction(m[0].chars().count())).to_string();
    }
    SECRET_ASSIGNMENT
        .replace_all(&out, |m: &regex::Captures| {
            // `{{secret:NAME}}` names a saved secret (`crate::secrets`); it isn't one.
            let placeholder = m[1].eq_ignore_ascii_case("secret:")
                && out[..m.get(0).unwrap().start()].ends_with("{{")
                && m[2].strip_suffix("}}").is_some_and(|name| name.chars().all(|c| c.is_ascii_alphanumeric() || c == '_'));
            // Already scrubbed material is read and exported again. Keep the marker intact.
            if &m[2] == "«redacted" || placeholder { m[0].to_string() }
            else { format!("{}{}", &m[1], redaction(m[2].chars().count())) }
        })
        .to_string()
}

pub fn hash_text(text: &str) -> String {
    let digest = Sha256::digest(text.as_bytes());
    digest.iter().take(16).map(|b| format!("{b:02x}")).collect()
}

// MARK: - The store

/// Where every bot's memory lives, outside the working directories bots write in: a bot's own
/// file and shell tools must not be able to rewrite what only `memory_update` may change.
pub fn root(home: &Path) -> PathBuf {
    home.join("memory")
}

/// One bot's memory folder, or the account's profile folder.
#[derive(Debug, Clone)]
pub struct MemoryStore {
    dir: PathBuf,
    /// The file the entries live in: `MEMORY.md` for a bot, `PROFILE.md` for the account.
    index: String,
    /// How much of the index opens every turn: the first lines or bytes, whichever cuts first.
    max_lines: usize,
    max_bytes: usize,
}

impl MemoryStore {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        MemoryStore::with_budget(dir, "MEMORY.md", MEMORY_MAX_LINES, MEMORY_MAX_BYTES)
    }

    /// A store whose index has its own name and load budget, as the account's profile does.
    pub fn with_budget(dir: impl Into<PathBuf>, index: &str, max_lines: usize, max_bytes: usize) -> Self {
        MemoryStore { dir: dir.into(), index: index.to_string(), max_lines, max_bytes }
    }

    /// Where a bot's memory lives: under the memory root, keyed by id so a rename or a changed
    /// working directory never moves it. A bot's earlier memory in its workspace, or a
    /// `MEMORY.md` an even earlier version left in a custom working directory, is adopted once.
    pub fn for_bot(home: &Path, bot: &crate::model::Bot) -> Self {
        let store = MemoryStore::new(root(home).join(&bot.id));
        let workspace = home.join("workspaces").join(&bot.id);
        adopt_workspace_memory(&workspace, &store.dir);
        let workdir = bot.working_directory(home);
        if workdir != workspace && workdir != store.dir {
            adopt_index(&workdir, &store);
        }
        store
    }

    pub fn dir(&self) -> &Path {
        &self.dir
    }
    pub fn index_name(&self) -> &str {
        &self.index
    }
    pub fn index_path(&self) -> PathBuf {
        self.dir.join(&self.index)
    }
    /// The load budget: lines, then bytes.
    pub fn budget(&self) -> (usize, usize) {
        (self.max_lines, self.max_bytes)
    }
    pub fn topics_dir(&self) -> PathBuf {
        self.dir.join("memory")
    }
    pub fn log_dir(&self) -> PathBuf {
        self.dir.join("memory").join("log")
    }

    pub fn read_index(&self) -> String {
        std::fs::read_to_string(self.index_path()).unwrap_or_default()
    }

    /// The index under the load budget.
    pub fn load_index(&self) -> LoadedIndex {
        let text = self.read_index();
        let lines = text.lines().count();
        let bytes = text.len();
        let mut shown = text.clone();
        let mut truncated = false;
        if lines > self.max_lines {
            shown = text.lines().take(self.max_lines).collect::<Vec<_>>().join("\n");
            truncated = true;
        }
        if shown.len() > self.max_bytes {
            let mut cut = self.max_bytes;
            while !shown.is_char_boundary(cut) {
                cut -= 1;
            }
            shown.truncate(cut);
            truncated = true;
        }
        LoadedIndex { text: shown, truncated, lines, bytes }
    }

    pub fn is_over_budget(&self) -> bool {
        self.load_index().truncated
    }

    /// The facts the index holds now: its entry lines, leaving out headings, blank lines, and
    /// entries struck through by a supersede.
    pub fn entry_count(&self) -> usize {
        self.read_index().lines().map(str::trim).filter(|line| line.starts_with("- ") && !line.starts_with("- ~~")).count()
    }

    /// The facts of the entries a supersede struck through: what changed, which a later write
    /// must not quietly bring back.
    pub fn superseded_facts(&self) -> Vec<String> {
        // The housekeeping moves struck entries out of the index into `memory/history.md`.
        let history = std::fs::read_to_string(self.topics_dir().join("history.md")).unwrap_or_default();
        self.read_index()
            .lines()
            .chain(history.lines())
            .filter_map(|line| line.trim().strip_prefix("- ~~").and_then(|rest| rest.split_once("~~")).map(|(struck, _)| fact_of(struck)))
            .filter(|fact| !fact.is_empty())
            .collect()
    }

    /// Replaces one topic file under `memory/`, scrubbed of credentials as the index is.
    pub fn write_topic(&self, name: &str, text: &str) -> Result<(), MemoryError> {
        if !TOPIC_NAME.is_match(name) {
            return Err(MemoryError::Invalid(format!("{name} is not a topic file name")));
        }
        self.write_file(&self.topics_dir().join(name), &scrub(text))
    }

    /// The facts the user removed, which `remove` records in `forgotten.md`.
    pub fn forgotten_facts(&self) -> Vec<String> {
        std::fs::read_to_string(self.dir.join("forgotten.md"))
            .unwrap_or_default()
            .lines()
            .map(fact_of)
            .filter(|fact| !fact.is_empty())
            .collect()
    }

    /// The facts that stop being true on a date, by fact key, from `expiring.json`.
    pub fn expiries(&self) -> std::collections::BTreeMap<String, Expiry> {
        std::fs::read_to_string(self.dir.join("expiring.json")).ok().and_then(|text| serde_json::from_str(&text).ok()).unwrap_or_default()
    }

    /// Records that a fact stops being true on `expiry.until`, or forgets that it does with
    /// `None`.
    pub fn set_expiry(&self, key: &str, expiry: Option<Expiry>) {
        let mut expiries = self.expiries();
        match expiry {
            Some(expiry) => {
                if expiries.get(key) == Some(&expiry) {
                    return;
                }
                expiries.insert(key.to_string(), expiry);
            }
            None => {
                if expiries.remove(key).is_none() {
                    return;
                }
            }
        }
        let path = self.dir.join("expiring.json");
        let saved = (|| -> std::io::Result<()> {
            std::fs::create_dir_all(&self.dir)?;
            let tmp = path.with_extension("json.tmp");
            std::fs::write(&tmp, serde_json::to_vec_pretty(&expiries)?)?;
            std::fs::rename(&tmp, &path)
        })();
        if let Err(error) = saved {
            tracing::warn!(%error, "saving when remembered facts expire");
        }
    }

    fn write_file(&self, path: &Path, text: &str) -> Result<(), MemoryError> {
        if text.len() > MEMORY_FILE_MAX_BYTES {
            return Err(MemoryError::TooLarge);
        }
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        // A crash mid-write leaves the old file, never a torn one.
        let tmp = path.with_extension("md.tmp");
        std::fs::write(&tmp, text)?;
        std::fs::rename(&tmp, path)?;
        Ok(())
    }

    fn save_index(&self, text: String) -> Result<(), MemoryError> {
        let text = scrub(&text);
        let text = if text.is_empty() || text.ends_with('\n') { text } else { text + "\n" };
        self.write_file(&self.index_path(), &text)
    }

    /// Replaces the whole index, refusing when it changed since `expected_hash` was read.
    pub fn write_index(&self, text: &str, expected_hash: Option<&str>) -> Result<String, MemoryError> {
        if let Some(expected) = expected_hash {
            let current = hash_text(&self.read_index());
            if current != expected {
                return Err(MemoryError::Conflict { hash: current });
            }
        }
        self.save_index(text.to_string())?;
        Ok(hash_text(&self.read_index()))
    }

    /// Adds one dated fact, unless it is already there word for word.
    pub fn append_entry(&self, text: &str, source: Option<&str>, now: i64) -> Result<Change, MemoryError> {
        let fact = normalise(text);
        if fact.is_empty() {
            return Err(MemoryError::Invalid("Pass the fact to remember.".into()));
        }
        let existing = self.read_index();
        if existing.lines().any(|line| fact_of(line).eq_ignore_ascii_case(&fact)) {
            return Ok(Change::Duplicate);
        }
        let line = entry_line(&local_time(now).date, source, &fact);
        let mut text = existing;
        if !text.is_empty() && !text.ends_with('\n') {
            text.push('\n');
        }
        text.push_str(&line);
        text.push('\n');
        self.save_index(text)?;
        Ok(Change::Appended { line })
    }

    fn locate(&self, old_text: &str) -> Result<(String, usize), MemoryError> {
        let old = old_text.trim();
        if old.is_empty() {
            return Err(MemoryError::Invalid("Pass the exact passage to change as old_text.".into()));
        }
        let text = self.read_index();
        let count = text.matches(old).count();
        match count {
            0 => Err(MemoryError::NotFound),
            1 => {
                let at = text.find(old).unwrap();
                Ok((text, at))
            }
            n => Err(MemoryError::Ambiguous(n)),
        }
    }

    /// Rewrites one unique passage in place.
    pub fn replace(&self, old_text: &str, new_text: &str) -> Result<Change, MemoryError> {
        let new = normalise(new_text);
        if new.is_empty() {
            return Err(MemoryError::Invalid("Pass the new text.".into()));
        }
        let (text, at) = self.locate(old_text)?;
        let old = old_text.trim();
        let mut updated = text.clone();
        updated.replace_range(at..at + old.len(), &new);
        self.save_index(updated)?;
        Ok(Change::Replaced)
    }

    /// Deletes one unique passage; a line left empty goes with it. A removed fact is recorded in
    /// `forgotten.md` so a later turn does not quietly add it back.
    pub fn remove(&self, old_text: &str) -> Result<Change, MemoryError> {
        let (text, at) = self.locate(old_text)?;
        let old = old_text.trim();
        let line_start = text[..at].rfind('\n').map(|i| i + 1).unwrap_or(0);
        let line_end = text[at..].find('\n').map(|i| at + i).unwrap_or(text.len());
        let mut line = text[line_start..line_end].to_string();
        line.replace_range(at - line_start..at - line_start + old.len(), "");
        let mut updated = text.clone();
        if fact_of(&line).trim_matches(|c: char| c == '~' || c == '·' || c.is_whitespace()).is_empty() {
            // Nothing but the bullet and its date is left: the line goes with the fact.
            let end = if line_end < text.len() { line_end + 1 } else { line_end };
            updated.replace_range(line_start..end, "");
        } else {
            updated.replace_range(line_start..line_end, &line);
        }
        self.save_index(updated)?;
        self.forget(&text[line_start..line_end])?;
        Ok(Change::Removed)
    }

    /// Records a line's fact as one the user asked to forget, so the write gate refuses it again.
    fn forget(&self, line: &str) -> Result<(), MemoryError> {
        let fact = fact_of(line);
        if fact.is_empty() {
            return Ok(());
        }
        let path = self.dir.join("forgotten.md");
        let mut text = std::fs::read_to_string(&path).unwrap_or_default();
        if text.lines().any(|existing| fact_of(existing).eq_ignore_ascii_case(&fact)) {
            return Ok(());
        }
        if !text.is_empty() && !text.ends_with('\n') {
            text.push('\n');
        }
        text.push_str(&format!("- {fact}\n"));
        self.write_file(&path, &text)
    }

    /// Strikes the line holding one unique passage through, dated, and adds the new fact as
    /// its own entry, so the file shows what changed.
    pub fn supersede(&self, old_text: &str, new_text: &str, source: Option<&str>, now: i64) -> Result<Change, MemoryError> {
        let new = normalise(new_text);
        if new.is_empty() {
            return Err(MemoryError::Invalid("Pass the new fact.".into()));
        }
        let (text, at) = self.locate(old_text)?;
        let date = local_time(now).date;
        let line_start = text[..at].rfind('\n').map(|i| i + 1).unwrap_or(0);
        let line_end = text[at..].find('\n').map(|i| at + i).unwrap_or(text.len());
        let line = &text[line_start..line_end];
        let body = line.trim_start_matches("- ").trim();
        let struck = if body.starts_with("~~") { line.to_string() } else { format!("- ~~{body}~~{SEP}superseded {date}") };
        let mut updated = text.clone();
        updated.replace_range(line_start..line_end, &struck);
        let line = entry_line(&date, source, &new);
        if !updated.ends_with('\n') {
            updated.push('\n');
        }
        updated.push_str(&line);
        updated.push('\n');
        self.save_index(updated)?;
        Ok(Change::Superseded { line })
    }

    /// Takes back a supersede: the entry `line` names, struck through since, reads as `line`
    /// again. False when no struck entry of it is left (the user changed it by hand).
    pub fn restore(&self, line: &str) -> Result<bool, MemoryError> {
        let body = line.trim().trim_start_matches("- ").trim();
        let struck = format!("- ~~{body}~~");
        let text = self.read_index();
        let Some(found) = text.lines().find(|candidate| candidate.trim().starts_with(&struck)) else { return Ok(false) };
        self.save_index(text.replacen(found, &format!("- {body}"), 1))?;
        Ok(true)
    }

    /// One timestamped line in today's log: what happened, not what is true.
    pub fn append_log(&self, text: &str, source: Option<&str>, now: i64) -> Result<String, MemoryError> {
        let what = normalise(text);
        if what.is_empty() {
            return Err(MemoryError::Invalid("Pass what happened.".into()));
        }
        let time = local_time(now);
        let mut line = format!("- {}", time.clock);
        if let Some(source) = source.map(str::trim).filter(|s| !s.is_empty()) {
            line.push_str(SEP);
            line.push_str(source);
        }
        line.push_str(SEP);
        line.push_str(&scrub(&what));
        let path = self.log_dir().join(format!("{}.md", time.date));
        let mut current = std::fs::read_to_string(&path).unwrap_or_default();
        if !current.is_empty() && !current.ends_with('\n') {
            current.push('\n');
        }
        current.push_str(&line);
        current.push('\n');
        self.write_file(&path, &current)?;
        Ok(line)
    }

    /// Topic file names under `memory/`, sorted.
    pub fn topics(&self) -> Vec<String> {
        let mut names: Vec<String> = std::fs::read_dir(self.topics_dir())
            .map(|entries| {
                entries
                    .filter_map(Result::ok)
                    .filter(|e| e.file_type().map(|t| t.is_file()).unwrap_or(false))
                    .filter_map(|e| e.file_name().into_string().ok())
                    .filter(|name| TOPIC_NAME.is_match(name))
                    .collect()
            })
            .unwrap_or_default();
        names.sort();
        names
    }

    /// Days with a log, oldest first.
    pub fn log_days(&self) -> Vec<String> {
        let mut days: Vec<String> = std::fs::read_dir(self.log_dir())
            .map(|entries| {
                entries
                    .filter_map(Result::ok)
                    .filter_map(|e| e.file_name().into_string().ok())
                    .filter_map(|name| name.strip_suffix(".md").map(str::to_string))
                    .filter(|day| DATE.is_match(day))
                    .collect()
            })
            .unwrap_or_default();
        days.sort();
        days
    }

    /// Lines of the index, the topics, and the logs matching `query` and the time range. Log
    /// lines carry a time; other lines match on words only, so a search with no query is a
    /// search of the logs.
    pub fn search(&self, query: Option<&Regex>, since: Option<i64>, until: Option<i64>) -> Vec<Hit> {
        let mut hits = Vec::new();
        let in_range = |at: i64| since.map(|s| at >= s).unwrap_or(true) && until.map(|u| at <= u).unwrap_or(true);
        for day in self.log_days() {
            if let (Some(since), Some(day_start)) = (since, local_unix(&day, None)) {
                if day_start + 86_400 < since {
                    continue;
                }
            }
            if let (Some(until), Some(day_start)) = (until, local_unix(&day, None)) {
                if day_start > until {
                    continue;
                }
            }
            let text = std::fs::read_to_string(self.log_dir().join(format!("{day}.md"))).unwrap_or_default();
            for line in text.lines() {
                let Some(caps) = LOG_LINE.captures(line) else { continue };
                let at = local_unix(&day, Some(&caps[1]));
                if let Some(at) = at {
                    if !in_range(at) {
                        continue;
                    }
                }
                let body = caps.get(2).map(|m| m.as_str()).unwrap_or("");
                if query.map(|q| q.is_match(body)).unwrap_or(true) {
                    hits.push(Hit { at, source: format!("log {day} {}", &caps[1]), text: body.to_string() });
                }
            }
        }
        if let Some(query) = query {
            let mut files = vec![(self.index.clone(), self.index_path())];
            files.extend(self.topics().into_iter().map(|name| (format!("memory/{name}"), self.topics_dir().join(&name))));
            for (label, path) in files {
                let text = std::fs::read_to_string(path).unwrap_or_default();
                for line in text.lines() {
                    if line.trim().is_empty() || !query.is_match(line) {
                        continue;
                    }
                    let at = line.trim_start_matches("- ").get(..10).filter(|d| DATE.is_match(d)).and_then(|d| local_unix(d, None));
                    if let Some(at) = at {
                        if !in_range(at) {
                            continue;
                        }
                    }
                    hits.push(Hit { at, source: label.clone(), text: line.trim().to_string() });
                }
            }
        }
        hits
    }

    /// What the app shows: the index with its budget, and the other files by name.
    pub fn overview(&self) -> Value {
        let text = self.read_index();
        let loaded = self.load_index();
        json!({
            "path": self.dir.display().to_string(),
            "index_name": self.index,
            "index": {
                "text": text,
                "hash": hash_text(&text),
                "lines": loaded.lines,
                "bytes": loaded.bytes,
                "truncated": loaded.truncated,
                "max_lines": self.max_lines,
                "max_bytes": self.max_bytes,
            },
            "topics": self.topics(),
            "logs": self.log_days(),
        })
    }
}

/// Moves a bot's memory from the workspace it used to live in into the memory root, once: the
/// index and the `memory/` tree, each on its own, so a part whose move failed is tried again on
/// the next start while the part that moved stays where it went. Nothing is overwritten.
fn adopt_workspace_memory(workspace: &Path, dir: &Path) {
    for name in ["MEMORY.md", "memory"] {
        let from = workspace.join(name);
        let to = dir.join(name);
        if std::fs::symlink_metadata(&from).is_err() || std::fs::symlink_metadata(&to).is_ok() {
            continue;
        }
        if let Err(error) = std::fs::create_dir_all(dir).and_then(|_| move_path(&from, &to)) {
            tracing::warn!(%error, from = %from.display(), "moving a bot's memory out of its workspace");
        }
    }
}

/// A rename, or across volumes a copy into a staging name, a rename of the copy into place, and
/// only then the removal of the original, so an interrupted move leaves the original whole.
fn move_path(from: &Path, to: &Path) -> std::io::Result<()> {
    if std::fs::rename(from, to).is_ok() {
        return Ok(());
    }
    let staging = to.with_extension("moving");
    if std::fs::symlink_metadata(&staging).is_ok() {
        remove_path(&staging)?;
    }
    copy_tree(from, &staging)?;
    std::fs::rename(&staging, to)?;
    remove_path(from)
}

/// Copies a file or a folder's regular files and folders; a link is left behind, as it may point
/// anywhere.
fn copy_tree(from: &Path, to: &Path) -> std::io::Result<()> {
    let meta = std::fs::symlink_metadata(from)?;
    if meta.is_dir() {
        std::fs::create_dir_all(to)?;
        for entry in std::fs::read_dir(from)? {
            let entry = entry?;
            if !entry.file_type()?.is_symlink() {
                copy_tree(&entry.path(), &to.join(entry.file_name()))?;
            }
        }
        Ok(())
    } else {
        std::fs::copy(from, to).map(|_| ())
    }
}

fn remove_path(path: &Path) -> std::io::Result<()> {
    if std::fs::symlink_metadata(path)?.is_dir() {
        std::fs::remove_dir_all(path)
    } else {
        std::fs::remove_file(path)
    }
}

/// Adopts a `MEMORY.md` from a custom working directory, once, as earlier versions did.
fn adopt_index(workdir: &Path, store: &MemoryStore) {
    let legacy = workdir.join("MEMORY.md");
    if legacy.is_file() && !store.index_path().is_file() {
        if let Err(error) = std::fs::create_dir_all(store.dir()).and_then(|_| std::fs::rename(&legacy, store.index_path())) {
            tracing::warn!(%error, "adopting a MEMORY.md from the working directory");
        }
    }
}

/// One line, whitespace collapsed, without a leading bullet.
fn normalise(text: &str) -> String {
    let collapsed: String = text.split_whitespace().collect::<Vec<_>>().join(" ");
    collapsed.trim_start_matches("- ").trim().to_string()
}

/// `- 2026-09-17 · from chat "X" · fact`
fn entry_line(date: &str, source: Option<&str>, fact: &str) -> String {
    let mut line = format!("- {date}");
    if let Some(source) = source.map(str::trim).filter(|s| !s.is_empty()) {
        line.push_str(SEP);
        line.push_str("from ");
        line.push_str(source);
    }
    line.push_str(SEP);
    line.push_str(fact);
    line
}

/// The fact of an entry line without its bullet, date, or source, for telling duplicates.
pub(crate) fn fact_of(line: &str) -> String {
    let body = line.trim().trim_start_matches("- ").trim();
    let body = ENTRY_PREFIX.replace(body, "");
    normalise(&body)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A scratch folder under the system temp dir, removed when the test ends.
    struct Scratch(PathBuf);
    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn store() -> (Scratch, MemoryStore) {
        let dir = std::env::temp_dir().join(format!("lorca-memory-{}", uuid::Uuid::new_v4()));
        let store = MemoryStore::new(dir.join("bot"));
        (Scratch(dir), store)
    }

    #[test]
    fn entries_append_dedupe_replace_supersede_and_remove() {
        let (_dir, store) = store();
        let now = local_unix("2026-09-17", Some("09:05")).unwrap();
        let change = store.append_entry("the user prefers short replies", Some("chat \"Follow-up\""), now).unwrap();
        assert_eq!(change, Change::Appended { line: "- 2026-09-17 · from chat \"Follow-up\" · the user prefers short replies".into() });
        assert_eq!(store.append_entry("  The user  prefers short replies ", None, now).unwrap(), Change::Duplicate);
        store.append_entry("the office is in Pune", None, now).unwrap();

        assert_eq!(store.replace("short replies", "one-line replies").unwrap(), Change::Replaced);
        assert!(store.read_index().contains("the user prefers one-line replies"));
        assert_eq!(store.replace("nowhere", "x"), Err(MemoryError::NotFound));
        assert_eq!(store.replace("the", "x"), Err(MemoryError::Ambiguous(2)));

        let change = store.supersede("the office is in Pune", "the office is in Goa", None, now + 86_400).unwrap();
        assert_eq!(change, Change::Superseded { line: "- 2026-09-18 · the office is in Goa".into() });
        let text = store.read_index();
        assert!(text.contains("- ~~2026-09-17 · the office is in Pune~~ · superseded 2026-09-18\n"), "{text}");
        assert!(text.ends_with("- 2026-09-18 · the office is in Goa\n"));

        assert_eq!(store.remove("the office is in Goa").unwrap(), Change::Removed);
        assert!(!store.read_index().contains("Goa"));
        assert_eq!(store.read_index().lines().count(), 2, "{}", store.read_index());
    }

    #[test]
    fn a_supersede_is_taken_back() {
        let (_dir, store) = store();
        store.append_entry("the office is in Pune", None, 0).unwrap();
        let old = store.read_index().lines().next().unwrap().to_string();
        let Change::Superseded { line } = store.supersede("the office is in Pune", "the office is in Goa", None, 86_400).unwrap() else { panic!() };
        store.remove(&line).unwrap();
        assert!(store.restore(&old).unwrap());
        assert_eq!(store.read_index(), format!("{old}\n"));
        assert!(crate::memory_check::check("the office is in Goa", &store).is_err(), "the taken-back fact stays forgotten");
        assert!(!store.restore(&old).unwrap(), "nothing struck is left to restore");
    }

    #[test]
    fn the_index_loads_under_a_budget() {
        let (_dir, store) = store();
        let now = 1_700_000_000;
        for i in 0..(MEMORY_MAX_LINES + 5) {
            store.append_entry(&format!("fact number {i}"), None, now).unwrap();
        }
        let loaded = store.load_index();
        assert!(loaded.truncated);
        assert_eq!(loaded.lines, MEMORY_MAX_LINES + 5);
        assert_eq!(loaded.text.lines().count(), MEMORY_MAX_LINES);
        assert!(store.is_over_budget());

        let big = "x".repeat(MEMORY_MAX_BYTES + 10);
        store.write_index(&big, None).unwrap();
        let loaded = store.load_index();
        assert!(loaded.truncated);
        assert_eq!(loaded.text.len(), MEMORY_MAX_BYTES);
    }

    #[test]
    fn writes_are_scrubbed_and_guarded_by_hash() {
        let (_dir, store) = store();
        store.append_entry("deploy key is sk-abcdefghijklmnopqrstuvwxyz and password: hunter22", None, 0).unwrap();
        let text = store.read_index();
        assert!(!text.contains("sk-abc") && !text.contains("hunter22"), "{text}");
        assert!(text.contains("«redacted 29 chars»") && text.contains("password: «redacted 8 chars»"), "{text}");

        let hash = hash_text(&text);
        assert_eq!(store.write_index("# Memory\n- fresh\n", Some("stale")), Err(MemoryError::Conflict { hash: hash.clone() }));
        let new_hash = store.write_index("# Memory\n- fresh\n", Some(&hash)).unwrap();
        assert_eq!(new_hash, hash_text("# Memory\n- fresh\n"));
        assert_eq!(store.write_index(&"y".repeat(MEMORY_FILE_MAX_BYTES + 1), None), Err(MemoryError::TooLarge));
    }

    #[test]
    fn scrubbing_keeps_ordinary_text() {
        assert_eq!(scrub("the token budget is 20k tokens"), "the token budget is 20k tokens");
        assert_eq!(scrub("AKIAABCDEFGHIJKLMNOP is an aws key"), "«redacted 20 chars» is an aws key");
        assert_eq!(scrub("Authorization: Bearer abcdefghijklmnopqrstuvwxyz"), "Authorization: «redacted 33 chars»");
        assert_eq!(scrub("API_KEY=\"abcdef123456\""), "API_KEY=\"«redacted 12 chars»\"");
        // A saved secret's placeholder is a name, not a value.
        assert_eq!(scrub(r#"{"value":"{{secret:SHOP_PIN}}"}"#), r#"{"value":"{{secret:SHOP_PIN}}"}"#);
        assert_eq!(scrub("secret:hunter2-long"), "secret:«redacted 12 chars»");
        assert_eq!(scrub("{{secret:hunter2-long}}x"), "{{secret:«redacted 15 chars»");
    }

    #[test]
    fn logs_are_daily_and_searchable_by_time_and_words() {
        let (_dir, store) = store();
        let day1 = local_unix("2026-09-16", Some("10:00")).unwrap();
        let day2 = local_unix("2026-09-17", Some("09:05")).unwrap();
        let line = store.append_log("sent the three flagged invoices", Some("in group \"Standup\""), day1).unwrap();
        assert_eq!(line, "- 10:00 · in group \"Standup\" · sent the three flagged invoices");
        store.append_log("the deploy went out", None, day2).unwrap();
        assert_eq!(store.log_days(), vec!["2026-09-16", "2026-09-17"]);

        let all = store.search(None, None, None);
        assert_eq!(all.len(), 2);
        assert_eq!(all[0].source, "log 2026-09-16 10:00");
        assert_eq!(all[0].at, Some(day1));

        let recent = store.search(None, Some(day2 - 3_600), None);
        assert_eq!(recent.len(), 1);
        assert_eq!(recent[0].text, "the deploy went out");

        let re = Regex::new("(?i)invoice").unwrap();
        let words = store.search(Some(&re), None, None);
        assert_eq!(words.len(), 1);
        assert!(words[0].text.contains("flagged invoices"));

        store.append_entry("invoices are reconciled on Mondays", None, day2).unwrap();
        std::fs::create_dir_all(store.topics_dir()).unwrap();
        std::fs::write(store.topics_dir().join("clients.md"), "# Clients\nAcme pays invoices net 30\n").unwrap();
        let words = store.search(Some(&re), None, None);
        let sources: Vec<&str> = words.iter().map(|h| h.source.as_str()).collect();
        assert_eq!(sources, vec!["log 2026-09-16 10:00", "MEMORY.md", "memory/clients.md"]);
        assert_eq!(store.topics(), vec!["clients.md"]);
    }

    #[test]
    fn the_utc_offset_matches_the_local_clock() {
        let now = local_unix("2026-09-17", Some("12:00")).unwrap();
        let offset = utc_offset_secs(now);
        assert!(offset.abs() <= 14 * 3_600 && offset % 900 == 0, "{offset}");
        // Noon local time is noon UTC moved by the offset.
        assert_eq!((now + offset).rem_euclid(86_400), 12 * 3_600);
    }

    #[test]
    fn when_parses_relative_and_absolute() {
        let now = local_unix("2026-09-17", Some("12:00")).unwrap();
        assert_eq!(parse_when("24h", now), Some(now - 86_400));
        assert_eq!(parse_when("3d", now), Some(now - 3 * 86_400));
        assert_eq!(parse_when("1w", now), Some(now - 7 * 86_400));
        assert_eq!(parse_when("today", now), local_unix("2026-09-17", None));
        assert_eq!(parse_when("yesterday", now), local_unix("2026-09-16", None));
        assert_eq!(parse_when("2026-09-10", now), local_unix("2026-09-10", None));
        assert_eq!(parse_when("whenever", now), None);
    }

    #[test]
    fn overview_reports_the_budget() {
        let (_dir, store) = store();
        store.append_entry("a fact", None, 0).unwrap();
        let overview = store.overview();
        assert_eq!(overview["index"]["lines"], 1);
        assert_eq!(overview["index"]["max_lines"], MEMORY_MAX_LINES);
        assert_eq!(overview["index"]["truncated"], false);
        assert_eq!(overview["topics"].as_array().unwrap().len(), 0);
    }

    #[test]
    fn a_workspace_memory_moves_under_the_memory_root_once() {
        let dir = std::env::temp_dir().join(format!("lorca-memory-move-{}", uuid::Uuid::new_v4()));
        let workspace = dir.join("workspaces").join("bot-1");
        std::fs::create_dir_all(workspace.join("memory").join("log")).unwrap();
        std::fs::write(workspace.join("MEMORY.md"), "- 2026-09-17 · the user ships on Fridays\n").unwrap();
        std::fs::write(workspace.join("memory").join("clients.md"), "# Clients\nAcme\n").unwrap();

        let store = MemoryStore::new(root(&dir).join("bot-1"));
        adopt_workspace_memory(&workspace, store.dir());
        assert!(store.read_index().contains("Fridays"));
        assert_eq!(store.topics(), vec!["clients.md"]);
        assert!(!workspace.join("MEMORY.md").exists(), "the old index moved rather than copied");
        assert!(!workspace.join("memory").exists(), "the old topics moved with it");

        // A second start finds the memory already moved and leaves it alone.
        std::fs::write(store.index_path(), "- a newer fact\n").unwrap();
        adopt_workspace_memory(&workspace, store.dir());
        assert_eq!(store.read_index(), "- a newer fact\n");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn a_bot_keeps_its_memory_outside_its_working_directory() {
        let dir = std::env::temp_dir().join(format!("lorca-memory-bot-{}", uuid::Uuid::new_v4()));
        let bot = crate::model::Bot {
            id: "bot-1".into(),
            name: "Scout".into(),
            description: String::new(),
            symbol_name: String::new(),
            accent: String::new(),
            avatar: None,
            runner_id: "runner-1".into(),
            provider: String::new(),
            model: None,
            thinking: None,
            legacy_instructions: String::new(),
            setting_up: false,
            workdir: None,
            permissions: None,
            created_at: 0.0,
        };
        let store = MemoryStore::for_bot(&dir, &bot);
        assert_eq!(store.dir(), root(&dir).join("bot-1"));
        assert_ne!(store.dir(), bot.working_directory(&dir));
        assert!(store.dir().starts_with(root(&dir)));
        let _ = std::fs::remove_dir_all(&dir);
    }
}
