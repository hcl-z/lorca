//! Skills are folders. A skill is a directory holding a `SKILL.md` (agentskills.io) and any files
//! it needs; saving writes the folder and the bots use it from their next turn. Three places hold
//! them: `<home>/skills` for every bot on the Runner, `<home>/workspaces/<bot id>/skills` for one
//! bot, and `<home>/groups/<chat id>/skills` for one group.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

use crate::app::App;

/// What the skill list may take of a turn's system prompt.
pub const PROMPT_BYTES: usize = 6_000;
pub const SKILL_FILE: &str = "SKILL.md";
/// Where a removed skill goes, inside its scope's skills folder.
pub const TRASH: &str = ".trash";
/// Marks a folder Lorca installed with itself; an upgrade rewrites it.
pub const BUILTIN_MARK: &str = ".lorca-builtin";
/// Where an imported skill came from, for checking it for updates.
pub const SOURCE_FILE: &str = ".lorca-source.json";
/// The skills Lorca ships. A folder of the user's cannot take these names.
pub const BUILTIN_NAMES: [&str; 3] = ["lorca", "write-skill", "save-skill"];
/// A text file longer than this is listed but not sent to an app to edit.
const TEXT_LIMIT: u64 = 2 * 1024 * 1024;
const TRASH_KEEP_SECS: u64 = 30 * 24 * 3600;

/// Where a skill lives: for every bot on this Runner, for one bot, or for one group.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Scope {
    pub kind: String,
    #[serde(default)]
    pub id: String,
}

fn safe_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 128
        && !id.starts_with('.')
        && id.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'))
}

impl Scope {
    pub fn shared() -> Self {
        Self { kind: "shared".into(), id: String::new() }
    }
    pub fn bot(id: &str) -> Self {
        Self { kind: "bot".into(), id: id.into() }
    }
    pub fn project(id: &str) -> Self {
        Self { kind: "project".into(), id: id.into() }
    }

    pub fn validate(&self, app: &App) -> Result<(), String> {
        match self.kind.as_str() {
            "shared" => Ok(()),
            "bot" if app.bot(&self.id).is_some() => Ok(()),
            "project" if app.chat(&self.id).is_some_and(|chat| chat.meta.is_group()) => Ok(()),
            _ => Err("A skill's scope must be shared, an existing bot, or an existing group".into()),
        }
    }

    /// The folder holding this scope's skills. Ids never carry a path separator.
    pub fn root(&self, app: &App) -> Result<PathBuf, String> {
        let home = &app.config.home;
        match self.kind.as_str() {
            "shared" => Ok(home.join("skills")),
            "bot" if safe_id(&self.id) => Ok(home.join("workspaces").join(&self.id).join("skills")),
            "project" if safe_id(&self.id) => Ok(home.join("groups").join(&self.id).join("skills")),
            _ => Err("A skill's scope must be shared, an existing bot, or an existing group".into()),
        }
    }

    /// Lower is looked up first: a bot's own skill, then its group's, then the shared one.
    fn rank(&self) -> u8 {
        match self.kind.as_str() {
            "bot" => 0,
            "project" => 1,
            _ => 2,
        }
    }
}

/// A skill's name is its folder's: a lowercase slug, which the agentskills.io spec requires.
pub fn check_name(name: &str) -> Result<(), String> {
    const RESERVED: [&str; 6] = ["con", "prn", "aux", "nul", "com1", "lpt1"];
    let slug = !name.is_empty()
        && name.len() <= 64
        && name.bytes().all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
        && !name.starts_with('-')
        && !name.ends_with('-')
        && !name.contains("--");
    if !slug || RESERVED.contains(&name) {
        return Err("A skill's name is a lowercase slug of 1–64 letters, numbers, and single hyphens".into());
    }
    Ok(())
}

/// A path inside a skill folder that an app or a bot may write.
pub fn safe_relative(path: &str) -> bool {
    !path.is_empty()
        && path.len() <= 200
        && path != SKILL_FILE
        && !path.starts_with('/')
        && !path.contains(['\\', '\0'])
        && path.split('/').all(|part| {
            !part.is_empty() && part != "." && part != ".." && !part.starts_with(".lorca-") && part != TRASH && part != ".git"
        })
}

/// Pattern scrubbing also removes exact account credential values with an opaque format.
/// Only secret fields are inspected; provider metadata never enters a skill or export.
pub fn scrub(app: &App, text: &str) -> String {
    fn collect(value: &Value, values: &mut Vec<String>) {
        match value {
            Value::Object(fields) => {
                for (key, value) in fields {
                    if matches!(key.as_str(), "api_key" | "access_token" | "refresh_token" | "id_token" | "client_secret" | "token") {
                        if let Some(secret) = value.as_str().filter(|s| !s.is_empty()) {
                            values.push(secret.into());
                        }
                    } else {
                        collect(value, values);
                    }
                }
            }
            Value::Array(items) => items.iter().for_each(|item| collect(item, values)),
            _ => {}
        }
    }
    let mut secrets = Vec::new();
    collect(&serde_json::to_value(&*app.credentials.lock().unwrap()).unwrap_or(Value::Null), &mut secrets);
    secrets.sort_by_key(|secret| std::cmp::Reverse(secret.len()));
    secrets.dedup();
    let mut text = crate::memory::scrub(text);
    for secret in secrets {
        text = text.replace(&secret, "«redacted credential»");
    }
    text
}

/// The text between `---` fences at the top, and what follows.
fn split_fence(text: &str) -> Option<(&str, &str)> {
    let rest = text.strip_prefix("---\n")?;
    let mut offset = 0;
    for line in rest.split_inclusive('\n') {
        if line.trim_end() == "---" {
            return Some((&rest[..offset], &rest[offset + line.len()..]));
        }
        offset += line.len();
    }
    None
}

fn unquote(value: &str) -> String {
    let value = value.trim();
    let quoted = |quote: char| value.len() >= 2 && value.starts_with(quote) && value.ends_with(quote);
    if quoted('"') {
        value[1..value.len() - 1].replace("\\\"", "\"").replace("\\n", "\n").replace("\\\\", "\\")
    } else if quoted('\'') {
        value[1..value.len() - 1].replace("''", "'")
    } else {
        value.to_string()
    }
}

/// A `SKILL.md`'s top-level frontmatter fields (plain, quoted, folded `>`, and literal `|`
/// values) and its body.
pub fn parse_skill_md(text: &str) -> (BTreeMap<String, String>, String) {
    let text = text.trim_start_matches('\u{feff}').replace("\r\n", "\n");
    let mut fields = BTreeMap::new();
    let (front, body) = match split_fence(&text) {
        Some((front, body)) => (front.to_string(), body.to_string()),
        None => return (fields, text.clone()),
    };
    let mut current: Option<(String, char, String)> = None;
    let mut finish = |current: &mut Option<(String, char, String)>| {
        if let Some((key, style, value)) = current.take() {
            let value = if style == 'p' { unquote(&value) } else { value.trim().to_string() };
            fields.insert(key, value);
        }
    };
    for line in front.lines() {
        if line.trim().is_empty() || line.starts_with(' ') || line.starts_with('\t') {
            if let Some((_, style, value)) = current.as_mut() {
                let part = line.trim();
                if !part.is_empty() {
                    if !value.is_empty() {
                        value.push(if *style == '|' { '\n' } else { ' ' });
                    }
                    value.push_str(part);
                }
            }
            continue;
        }
        finish(&mut current);
        let Some((key, rest)) = line.split_once(':') else { continue };
        let rest = rest.trim();
        current = Some(match rest {
            ">" | ">-" | ">+" => (key.trim().to_lowercase(), '>', String::new()),
            "|" | "|-" | "|+" => (key.trim().to_lowercase(), '|', String::new()),
            _ => (key.trim().to_lowercase(), 'p', rest.to_string()),
        });
    }
    finish(&mut current);
    (fields, body)
}

pub fn slugify(text: &str) -> String {
    let mut slug = String::new();
    for c in text.chars().flat_map(char::to_lowercase) {
        if c.is_ascii_lowercase() || c.is_ascii_digit() {
            slug.push(c);
        } else if !slug.ends_with('-') {
            slug.push('-');
        }
    }
    slug.truncate(64);
    slug.trim_matches('-').to_string()
}

/// The frontmatter lines other than `name` and `description`, kept as they were so a save does
/// not drop a skill's `license`, `compatibility`, `allowed-tools`, or `metadata`.
fn other_frontmatter(text: &str) -> String {
    let text = text.trim_start_matches('\u{feff}').replace("\r\n", "\n");
    let Some((front, _)) = split_fence(&text) else { return String::new() };
    let mut kept: Vec<&str> = Vec::new();
    let mut keeping = false;
    for line in front.lines() {
        if line.starts_with(' ') || line.starts_with('\t') || line.trim().is_empty() {
            if keeping {
                kept.push(line);
            }
            continue;
        }
        let key = line.split_once(':').map(|(key, _)| key.trim().to_lowercase()).unwrap_or_default();
        keeping = !key.is_empty() && key != "name" && key != "description";
        if keeping {
            kept.push(line);
        }
    }
    kept.join("\n")
}

/// A `SKILL.md`: the name and description, the frontmatter fields `previous` had besides those,
/// and the instructions.
fn compose(name: &str, description: &str, instructions: &str, previous: &str) -> String {
    let description = serde_json::to_string(&description.split_whitespace().collect::<Vec<_>>().join(" ")).unwrap_or_default();
    let extra = other_frontmatter(previous);
    let extra = if extra.is_empty() { String::new() } else { format!("{extra}\n") };
    format!("---\nname: {name}\ndescription: {description}\n{extra}---\n\n{}\n", instructions.trim())
}

/// A file of a skill folder: its path inside it, with `/`, and where it is.
pub(crate) struct Entry {
    pub(crate) rel: String,
    pub(crate) path: PathBuf,
}

/// The files of a skill folder, sorted. Links are not followed, and Lorca's marker files and a
/// repository's `.git` are left out, so none of them reach a hash, an export, or a copy.
pub(crate) fn walk(dir: &Path) -> Vec<Entry> {
    fn visit(base: &Path, dir: &Path, top: bool, out: &mut Vec<Entry>) {
        let Ok(entries) = std::fs::read_dir(dir) else { return };
        for entry in entries.filter_map(Result::ok) {
            let name = entry.file_name().to_string_lossy().to_string();
            let Ok(kind) = entry.file_type() else { continue };
            if kind.is_symlink() || name == ".git" || (top && name.starts_with(".lorca-")) {
                continue;
            }
            let path = entry.path();
            if kind.is_dir() {
                visit(base, &path, false, out);
            } else if kind.is_file() {
                let rel = path.strip_prefix(base).map(|p| p.to_string_lossy().replace('\\', "/")).unwrap_or_default();
                out.push(Entry { rel, path });
            }
        }
    }
    let mut out = Vec::new();
    visit(dir, dir, true, &mut out);
    out.sort_by(|a, b| a.rel.cmp(&b.rel));
    out
}

/// What a skill folder holds, for a save to check that nothing changed under it.
pub fn hash_dir(dir: &Path) -> String {
    let mut hasher = Sha256::new();
    for entry in walk(dir) {
        hasher.update(entry.rel.as_bytes());
        hasher.update([0]);
        let bytes = std::fs::read(&entry.path).unwrap_or_default();
        hasher.update((bytes.len() as u64).to_le_bytes());
        hasher.update(&bytes);
    }
    hasher.finalize().iter().map(|byte| format!("{byte:02x}")).collect()
}

/// A cheap stamp of a skill folder (its files' sizes and newest change), to see that a turn
/// changed it without reading every file.
pub fn stamp(dir: &Path) -> String {
    let (mut size, mut newest, mut files) = (0u64, 0u128, 0usize);
    for entry in walk(dir) {
        if let Ok(meta) = std::fs::metadata(&entry.path) {
            size += meta.len();
            files += 1;
            let modified = meta.modified().ok().and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok()).map(|d| d.as_nanos()).unwrap_or(0);
            newest = newest.max(modified);
        }
    }
    format!("{files}-{size}-{newest}")
}

pub(crate) fn unix_secs() -> u64 {
    std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0)
}

fn text_of(bytes: Vec<u8>) -> Option<String> {
    String::from_utf8(bytes).ok().filter(|text| !text.contains('\0'))
}

/// Moves a skill folder into its scope's trash, where it stays for thirty days.
pub(crate) fn to_trash(root: &Path, dir: &Path, name: &str) -> Result<PathBuf, String> {
    let trash = root.join(TRASH);
    std::fs::create_dir_all(&trash).map_err(|e| e.to_string())?;
    let stamp = unix_secs();
    let mut target = trash.join(format!("{stamp}-{name}"));
    let mut n = 1;
    while target.exists() {
        target = trash.join(format!("{stamp}-{name}-{n}"));
        n += 1;
    }
    std::fs::rename(dir, &target).map_err(|e| format!("Could not move {name} to the trash: {e}"))?;
    Ok(target)
}

/// A skill folder as the lists and the prompt show it.
#[derive(Debug, Clone)]
pub struct Found {
    pub scope: Scope,
    pub name: String,
    pub dir: PathBuf,
    pub description: String,
    pub builtin: bool,
    /// The address an import was given, from the folder's source note; empty for a skill written here.
    pub source: String,
}

impl Found {
    pub fn skill_file(&self) -> PathBuf {
        self.dir.join(SKILL_FILE)
    }

    pub fn summary(&self) -> Value {
        let updated = std::fs::metadata(self.skill_file()).and_then(|m| m.modified()).ok().and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok()).map(|d| d.as_secs_f64());
        json!({ "scope": self.scope, "name": self.name, "description": self.description, "path": self.dir.display().to_string(), "builtin": self.builtin, "source": self.source, "updated_at": updated })
    }
}

/// The description a skill gives: its frontmatter's, else the first line of its instructions.
fn describe(fields: &BTreeMap<String, String>, body: &str) -> String {
    fields
        .get("description")
        .map(|d| d.split_whitespace().collect::<Vec<_>>().join(" "))
        .filter(|d| !d.is_empty())
        .or_else(|| body.lines().map(|l| l.trim().trim_start_matches('#').trim()).find(|l| !l.is_empty()).map(str::to_string))
        .unwrap_or_default()
}

fn read_found(scope: &Scope, dir: &Path) -> Option<(Found, String)> {
    let name = dir.file_name()?.to_string_lossy().to_string();
    if name.starts_with('.') || !std::fs::metadata(dir).ok()?.is_dir() {
        return None;
    }
    let text = std::fs::read_to_string(dir.join(SKILL_FILE)).ok()?;
    let (fields, body) = parse_skill_md(&text);
    let source = std::fs::read(dir.join(SOURCE_FILE))
        .ok()
        .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok())
        .and_then(|note| note["reference"].as_str().map(str::to_string))
        .unwrap_or_default();
    let found = Found { scope: scope.clone(), name, dir: dir.to_path_buf(), description: describe(&fields, &body), builtin: dir.join(BUILTIN_MARK).exists(), source };
    Some((found, body))
}

/// The skills in one scope's folder, by name.
pub fn list_scope(app: &App, scope: &Scope) -> Vec<Found> {
    let Ok(root) = scope.root(app) else { return Vec::new() };
    let Ok(entries) = std::fs::read_dir(&root) else { return Vec::new() };
    let mut found: Vec<Found> = entries.filter_map(Result::ok).filter_map(|entry| read_found(scope, &entry.path()).map(|(found, _)| found)).collect();
    found.sort_by(|a, b| a.name.cmp(&b.name));
    found
}

/// The skills a bot sees in a chat: the shared ones, its own, and its group's.
pub fn scopes_for_turn(app: &App, bot_id: &str, chat_id: &str) -> Vec<Scope> {
    let mut scopes = vec![Scope::shared(), Scope::bot(bot_id)];
    if app.chat(chat_id).is_some_and(|chat| chat.meta.is_group() && chat.meta.bot_ids.iter().any(|id| id == bot_id)) {
        scopes.push(Scope::project(chat_id));
    }
    scopes
}

/// The skill called `name` among `scopes`: a bot's own first, then its group's, then a shared one.
pub fn find(app: &App, scopes: &[Scope], name: &str) -> Option<Found> {
    let mut ordered: Vec<&Scope> = scopes.iter().collect();
    ordered.sort_by_key(|scope| scope.rank());
    ordered.into_iter().find_map(|scope| list_scope(app, scope).into_iter().find(|found| found.name == name))
}

/// The text of one file of a skill a turn sees, by the skill's name and the file's path in it.
pub fn resource(app: &App, scopes: &[Scope], name: &str, rel: &str) -> Result<(String, String), String> {
    let found = find(app, scopes, name.trim()).ok_or_else(|| format!("No skill {name} is available here."))?;
    if !safe_relative(rel) {
        return Err(format!("The skill {} has no {rel}.", found.name));
    }
    let bytes = std::fs::read(found.dir.join(rel)).map_err(|_| format!("The skill {} has no {rel}.", found.name))?;
    let text = text_of(bytes).ok_or_else(|| format!("{rel} is not a text file."))?;
    Ok((found.name, text))
}

pub(crate) fn folder(app: &App, scope: &Scope, name: &str) -> Result<(PathBuf, PathBuf), String> {
    scope.validate(app)?;
    check_name(name)?;
    let root = scope.root(app)?;
    let dir = root.join(name);
    Ok((root, dir))
}

/// One skill with its instructions, its files, and the hash a save quotes back.
pub fn get(app: &App, scope: &Scope, name: &str) -> Result<Value, String> {
    let (_, dir) = folder(app, scope, name)?;
    let (found, body) = read_found(scope, &dir).ok_or("That skill does not exist in this scope")?;
    let files: Vec<Value> = walk(&dir)
        .into_iter()
        .filter(|entry| entry.rel != SKILL_FILE)
        .map(|entry| {
            let size = std::fs::metadata(&entry.path).map(|m| m.len()).unwrap_or(0);
            let text = if size <= TEXT_LIMIT { std::fs::read(&entry.path).ok().and_then(text_of) } else { None };
            json!({ "path": entry.rel, "size": size, "text": text })
        })
        .collect();
    let mut view = found.summary();
    view["instructions"] = json!(body.trim());
    view["files"] = json!(files);
    view["hash"] = json!(hash_dir(&dir));
    Ok(view)
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct InputFile {
    pub path: String,
    pub text: String,
}

/// What an app or a drafting model gives a save: the fields of `SKILL.md` and the text files
/// beside it.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct Input {
    pub name: String,
    pub description: String,
    pub instructions: String,
    #[serde(default)]
    pub files: Vec<InputFile>,
}

fn copy_mode(from: &Path, to: &Path) {
    if let Ok(meta) = std::fs::metadata(from) {
        let _ = std::fs::set_permissions(to, meta.permissions());
    }
}

fn put(path: &Path, bytes: &[u8]) -> Result<(), String> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }
    std::fs::write(path, bytes).map_err(|e| e.to_string())
}

/// Saves a skill: a new folder, or `current` rewritten (renamed when `input.name` differs) when
/// `expected_hash` is what the folder holds now. The folder is built beside the old one and swapped
/// in, the old one going to the trash; files the editor does not show (binaries, large files, the
/// source note) carry over, and so do the frontmatter fields it does not edit.
pub fn save(app: &App, scope: &Scope, current: Option<&str>, input: Input, expected_hash: &str) -> Result<Value, String> {
    let name = input.name.trim().to_string();
    let (root, target) = folder(app, scope, &name)?;
    if scope.kind == "shared" && BUILTIN_NAMES.contains(&name.as_str()) {
        return Err("That name belongs to a skill Lorca ships".into());
    }
    let previous = match current {
        Some(current) => {
            let (_, old) = folder(app, scope, current)?;
            if !old.is_dir() {
                return Err("That skill no longer exists; reload".into());
            }
            if old.join(BUILTIN_MARK).exists() {
                return Err("A skill Lorca ships cannot be edited; save a copy under another name".into());
            }
            if hash_dir(&old) != expected_hash {
                return Err("The skill changed since you opened it; reload before saving".into());
            }
            Some(old)
        }
        None => None,
    };
    if target.exists() && previous.as_deref() != Some(target.as_path()) {
        return Err("A skill with this name already exists in this scope".into());
    }
    let description = scrub(app, input.description.trim());
    let instructions = scrub(app, input.instructions.trim());
    if description.is_empty() || instructions.is_empty() {
        return Err("A skill needs a description and instructions".into());
    }
    let mut files = BTreeMap::new();
    for file in &input.files {
        if !safe_relative(&file.path) || files.insert(file.path.clone(), scrub(app, &file.text)).is_some() {
            return Err(format!("Invalid or duplicate file path: {}", file.path));
        }
    }
    // Recorded browser steps are checked as a run reads them.
    if let Some(steps) = files.get(crate::browser::steps::PATH) {
        crate::browser::steps::parse(steps)?;
    }
    std::fs::create_dir_all(&root).map_err(|e| e.to_string())?;
    let staging = root.join(format!(".tmp-{}", uuid::Uuid::new_v4()));
    let built = build(&staging, &name, &description, &instructions, &files, previous.as_deref());
    if let Err(error) = built {
        let _ = std::fs::remove_dir_all(&staging);
        return Err(error);
    }
    let trashed = match (&previous, current) {
        (Some(old), Some(current)) => match to_trash(&root, old, current) {
            Ok(path) => Some(path),
            Err(error) => {
                let _ = std::fs::remove_dir_all(&staging);
                return Err(error);
            }
        },
        _ => None,
    };
    if let Err(error) = std::fs::rename(&staging, &target) {
        if let (Some(trashed), Some(old)) = (&trashed, &previous) {
            let _ = std::fs::rename(trashed, old);
        }
        let _ = std::fs::remove_dir_all(&staging);
        return Err(format!("Could not save {name}: {error}"));
    }
    changed(app, scope);
    get(app, scope, &name)
}

/// The new folder of a save, in `staging`.
fn build(staging: &Path, name: &str, description: &str, instructions: &str, files: &BTreeMap<String, String>, previous: Option<&Path>) -> Result<(), String> {
    std::fs::create_dir_all(staging).map_err(|e| e.to_string())?;
    let mut previous_text = String::new();
    if let Some(old) = previous {
        previous_text = std::fs::read_to_string(old.join(SKILL_FILE)).unwrap_or_default();
        for entry in walk(old) {
            if entry.rel == SKILL_FILE || files.contains_key(&entry.rel) {
                continue;
            }
            let bytes = std::fs::read(&entry.path).map_err(|e| e.to_string())?;
            let editable = bytes.len() as u64 <= TEXT_LIMIT && std::str::from_utf8(&bytes).is_ok() && !bytes.contains(&0);
            if editable {
                continue;
            }
            let dest = staging.join(&entry.rel);
            put(&dest, &bytes)?;
            copy_mode(&entry.path, &dest);
        }
        let source = old.join(SOURCE_FILE);
        if source.is_file() {
            std::fs::copy(&source, staging.join(SOURCE_FILE)).map_err(|e| e.to_string())?;
        }
    }
    put(&staging.join(SKILL_FILE), compose(name, description, instructions, &previous_text).as_bytes())?;
    for (path, text) in files {
        let dest = staging.join(path);
        put(&dest, text.as_bytes())?;
        if let Some(old) = previous {
            copy_mode(&old.join(path), &dest);
        }
    }
    Ok(())
}

/// Moves a skill to its scope's trash. `expected_hash`, when given, must be what the folder holds.
pub fn remove(app: &App, scope: &Scope, name: &str, expected_hash: Option<&str>) -> Result<Value, String> {
    let (root, dir) = folder(app, scope, name)?;
    if !dir.is_dir() {
        return Err("That skill does not exist in this scope".into());
    }
    if dir.join(BUILTIN_MARK).exists() {
        return Err("A skill Lorca ships cannot be removed".into());
    }
    if expected_hash.is_some_and(|expected| hash_dir(&dir) != expected) {
        return Err("The skill changed since you opened it; reload before removing".into());
    }
    to_trash(&root, &dir, name)?;
    changed(app, scope);
    Ok(json!({ "name": name, "removed": true }))
}

/// Called after a skill folder changed through the CLI's own calls, for what follows a change.
pub fn changed(app: &App, scope: &Scope) {
    #[cfg(feature = "cli")]
    crate::skills_sync::push_scope(app, scope);
    #[cfg(not(feature = "cli"))]
    let _ = (app, scope);
}

/// The skills folders a bot's turns use: the shared one, its own, and its groups'.
pub fn readable_roots(app: &App, bot_id: &str) -> Vec<PathBuf> {
    let home = &app.config.home;
    let mut roots = vec![home.join("skills")];
    if safe_id(bot_id) {
        roots.push(home.join("workspaces").join(bot_id).join("skills"));
    }
    let groups: Vec<String> = {
        let state = app.state.lock().unwrap();
        state.chats.iter().filter(|chat| chat.meta.is_group() && chat.meta.bot_ids.iter().any(|id| id == bot_id)).map(|chat| chat.meta.id.clone()).collect()
    };
    roots.extend(groups.iter().filter(|id| safe_id(id)).map(|id| home.join("groups").join(id).join("skills")));
    roots
}

/// Deletes what has sat in a trash for thirty days.
pub fn prune_trash(root: &Path) {
    let Ok(entries) = std::fs::read_dir(root.join(TRASH)) else { return };
    let now = unix_secs();
    for entry in entries.filter_map(Result::ok) {
        let name = entry.file_name().to_string_lossy().to_string();
        let stamp = name.split('-').next().and_then(|secs| secs.parse::<u64>().ok());
        if stamp.is_some_and(|stamp| now.saturating_sub(stamp) > TRASH_KEEP_SECS) {
            let _ = std::fs::remove_dir_all(entry.path());
        }
    }
}

/// A skill as a zip a person can keep or give away: the folder under its name, with the files'
/// permissions, scrubbed of credentials as text. The answer carries the archive as base64.
#[cfg(feature = "cli")]
pub fn export(app: &App, scope: &Scope, name: &str) -> Result<Value, String> {
    use base64::Engine;
    use std::io::Write;

    let (_, dir) = folder(app, scope, name)?;
    if !dir.is_dir() {
        return Err("That skill does not exist in this scope".into());
    }
    let mut writer = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
    for entry in walk(&dir) {
        let bytes = std::fs::read(&entry.path).map_err(|e| e.to_string())?;
        let bytes = match text_of(bytes.clone()) {
            Some(text) => scrub(app, &text).into_bytes(),
            None => bytes,
        };
        let mut options = zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Deflated);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            if let Ok(meta) = std::fs::metadata(&entry.path) {
                options = options.unix_permissions(meta.permissions().mode() & 0o777);
            }
        }
        writer.start_file(format!("{name}/{}", entry.rel), options).map_err(|e| e.to_string())?;
        writer.write_all(&bytes).map_err(|e| e.to_string())?;
    }
    let archive = writer.finish().map_err(|e| e.to_string())?.into_inner();
    Ok(json!({ "name": name, "format": "zip", "data": base64::engine::general_purpose::STANDARD.encode(archive) }))
}

/// What the inspector lists for a bot in a chat: the skills of each scope it sees, and the
/// folders they live in.
pub fn list_for(app: &App, bot_id: &str, chat_id: &str) -> Result<Value, String> {
    app.bot(bot_id).ok_or("Unknown bot")?;
    let mut skills = Vec::new();
    let mut folders = serde_json::Map::new();
    for scope in scopes_for_turn(app, bot_id, chat_id) {
        skills.extend(list_scope(app, &scope).iter().map(Found::summary));
        if let Ok(root) = scope.root(app) {
            folders.insert(scope.kind.clone(), json!(root.display().to_string()));
        }
    }
    Ok(json!({ "skills": skills, "folders": folders }))
}

/// The Runner that holds the skills a call names, when that is not this Device: a bot's Runner,
/// a group's (this one when it runs a member), and for shared skills the bot's the call names.
fn remote_runner(app: &App, params: &Value) -> Option<String> {
    let here = app.this_device_id();
    let hint = params["bot_id"].as_str().and_then(|id| app.bot(id));
    let scope: Option<Scope> = params.get("scope").and_then(|scope| serde_json::from_value(scope.clone()).ok());
    let runner = match scope.as_ref().map(|scope| scope.kind.as_str()) {
        Some("bot") => scope.as_ref().and_then(|scope| app.bot(&scope.id)).map(|bot| bot.runner_id),
        Some("project") => {
            let members: Vec<crate::model::Bot> = scope.as_ref().and_then(|scope| app.chat(&scope.id)).map(|chat| chat.meta.bot_ids.iter().filter_map(|id| app.bot(id)).collect()).unwrap_or_default();
            if members.iter().any(|bot| Some(bot.runner_id.as_str()) == here.as_deref()) {
                here.clone()
            } else {
                hint.as_ref().filter(|bot| members.iter().any(|member| member.id == bot.id)).or(members.first()).map(|bot| bot.runner_id.clone())
            }
        }
        _ => hint.map(|bot| bot.runner_id),
    };
    runner.filter(|runner| Some(runner.as_str()) != here.as_deref())
}

/// Runs a `skills.*` call where the skills are: here for this Runner's, and through a sealed
/// request to the Runner that holds them for another Device's bot.
pub async fn dispatch(app: &std::sync::Arc<App>, method: &str, params: Value) -> Result<Value, String> {
    if let Some(runner) = remote_runner(app, &params) {
        let wait = if method == "skills.generate" || method == "skills.import" { std::time::Duration::from_secs(150) } else { std::time::Duration::from_secs(30) };
        return crate::requests::ask_within(app, &runner, method, params, wait).await;
    }
    serve(app, method, params).await
}

fn scope_of(params: &Value) -> Result<Scope, String> {
    serde_json::from_value(params["scope"].clone()).map_err(|_| "A skill's scope is required".to_string())
}

fn name_of(params: &Value) -> Result<&str, String> {
    params["name"].as_str().filter(|name| !name.is_empty()).ok_or_else(|| "A skill's name is required".to_string())
}

/// A `skills.*` call on the skills of this Device.
#[cfg(feature = "runner")]
pub async fn serve(app: &std::sync::Arc<App>, method: &str, params: Value) -> Result<Value, String> {
    match method {
        "skills.list" => list_for(app, params["bot_id"].as_str().unwrap_or_default(), params["chat_id"].as_str().unwrap_or_default()),
        "skills.get" => get(app, &scope_of(&params)?, name_of(&params)?),
        "skills.save" => {
            let input: Input = serde_json::from_value(params["skill"].clone()).map_err(|e| e.to_string())?;
            save(app, &scope_of(&params)?, params["current"].as_str().filter(|c| !c.is_empty()), input, params["expected_hash"].as_str().unwrap_or_default())
        }
        "skills.remove" => remove(app, &scope_of(&params)?, name_of(&params)?, params["expected_hash"].as_str()),
        #[cfg(feature = "cli")]
        "skills.export" => export(app, &scope_of(&params)?, name_of(&params)?),
        "skills.generate" => crate::skills_turn::generate(app, &scope_of(&params)?, &params).await,
        #[cfg(feature = "cli")]
        "skills.search" | "skills.import" => crate::skill_hub::dispatch(app, method, params).await,
        _ => Err("Unknown skills method".into()),
    }
}

#[cfg(not(feature = "runner"))]
pub async fn serve(_app: &std::sync::Arc<App>, _method: &str, _params: Value) -> Result<Value, String> {
    Err("This Device keeps no skills; they live on a Runner".into())
}

#[derive(Deserialize)]
struct LegacyLibrary {
    #[serde(default)]
    records: BTreeMap<String, LegacyRecord>,
}

#[derive(Deserialize)]
struct LegacyRecord {
    scope: Scope,
    revisions: Vec<LegacyRevision>,
}

#[derive(Deserialize)]
struct LegacyRevision {
    id: String,
    revision: u64,
    created_at: f64,
    status: String,
    content: Option<LegacyContent>,
}

#[derive(Deserialize)]
struct LegacyContent {
    name: String,
    description: String,
    instructions: String,
    #[serde(default)]
    examples: String,
    #[serde(default)]
    references: Vec<InputFile>,
    #[serde(default)]
    scripts: Vec<InputFile>,
}

/// Whether this Device runs the bot or a member of the group that `scope` names.
pub(crate) fn hosts(app: &App, scope: &Scope) -> bool {
    let here = app.this_device_id();
    let runs = |bot_id: &str| app.bot(bot_id).is_some_and(|bot| Some(bot.runner_id.as_str()) == here.as_deref());
    match scope.kind.as_str() {
        "bot" => runs(&scope.id),
        "project" => app.chat(&scope.id).is_some_and(|chat| chat.meta.bot_ids.iter().any(|id| runs(id))),
        _ => false,
    }
}

/// Writes the skills of an older build's encrypted library as folders, for the bots and groups
/// this Device runs, and sets the library aside. Drafts and removed skills are not carried over.
pub fn migrate(app: &App) {
    let path = app.config.home.join("playbooks.enc");
    if !path.exists() {
        return;
    }
    let Some(dek) = app.dek() else { return };
    let library: LegacyLibrary = match std::fs::read(&path).map_err(anyhow::Error::from).and_then(|bytes| crate::crypto::decrypt_json(&dek, "playbooks", &bytes)) {
        Ok(library) => library,
        Err(error) => {
            tracing::warn!(%error, "reading playbooks.enc to move its skills into folders");
            return;
        }
    };
    for record in library.records.values() {
        let Some(head) = record.revisions.iter().max_by(|a, b| a.revision.cmp(&b.revision).then(a.created_at.total_cmp(&b.created_at)).then(a.id.cmp(&b.id))) else { continue };
        let Some(content) = head.content.as_ref().filter(|_| head.status == "saved") else { continue };
        if !hosts(app, &record.scope) {
            continue;
        }
        let mut instructions = content.instructions.clone();
        if !content.examples.trim().is_empty() {
            instructions.push_str(&format!("\n\n## Examples\n\n{}", content.examples.trim()));
        }
        let files = content.references.iter().chain(&content.scripts).map(|file| InputFile { path: file.path.clone(), text: file.text.clone() }).collect();
        let input = Input { name: content.name.clone(), description: content.description.clone(), instructions, files };
        if let Err(error) = save(app, &record.scope, None, input, "") {
            tracing::warn!(%error, skill = %content.name, "moving a skill out of playbooks.enc");
        }
    }
    let _ = std::fs::rename(&path, app.config.home.join("playbooks.enc.migrated"));
}

/// On start: the older library becomes folders, and old trash is emptied.
pub fn startup(app: &App) {
    migrate(app);
    #[cfg(feature = "runner")]
    crate::docs::install(app);
    let mut roots = vec![app.config.home.join("skills")];
    let (bots, chats): (Vec<String>, Vec<String>) = {
        let state = app.state.lock().unwrap();
        (state.bots.iter().map(|bot| bot.id.clone()).collect(), state.chats.iter().filter(|chat| chat.meta.is_group()).map(|chat| chat.meta.id.clone()).collect())
    };
    roots.extend(bots.iter().filter(|id| safe_id(id)).map(|id| app.config.home.join("workspaces").join(id).join("skills")));
    roots.extend(chats.iter().filter(|id| safe_id(id)).map(|id| app.config.home.join("groups").join(id).join("skills")));
    roots.iter().for_each(|root| prune_trash(root));
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::Config;
    use std::sync::Arc;

    struct Fixture {
        app: Arc<App>,
        home: PathBuf,
        bot: String,
        group: String,
    }

    impl Fixture {
        fn new() -> Self {
            let home = std::env::temp_dir().join(format!("lorca-skills-{}", uuid::Uuid::new_v4()));
            let app = App::load(Config { home: home.clone(), port: 0 }).unwrap();
            crate::identity::create(&app, Some("Skills test".into())).unwrap();
            let bot = app.state.lock().unwrap().bots[0].id.clone();
            let dm = app.state.lock().unwrap().chats.iter().find(|c| c.meta.bot_ids.contains(&bot)).unwrap().clone();
            let mut meta = dm.meta;
            meta.id = format!("chat-{}", uuid::Uuid::new_v4());
            meta.kind = "group".into();
            meta.title = Some("Project".into());
            let group = meta.id.clone();
            app.create_chat(meta).unwrap();
            Self { app, home, bot, group }
        }
        fn scope(&self) -> Scope {
            Scope::bot(&self.bot)
        }
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.home);
        }
    }

    fn input(name: &str) -> Input {
        Input {
            name: name.into(),
            description: "Use for a weekly report".into(),
            instructions: "Compare the evidence and report the changes.".into(),
            files: vec![InputFile { path: "scripts/compare.sh".into(), text: "printf 'example'".into() }],
        }
    }

    #[test]
    fn a_skill_is_a_folder_in_its_scope_and_saving_it_guards_what_it_replaces() {
        let f = Fixture::new();
        let created = save(&f.app, &f.scope(), None, input("weekly-report"), "").unwrap();
        let dir = f.home.join("workspaces").join(&f.bot).join("skills/weekly-report");
        assert!(dir.join("SKILL.md").is_file() && dir.join("scripts/compare.sh").is_file());
        assert_eq!(created["name"], "weekly-report");
        assert_eq!(created["instructions"], "Compare the evidence and report the changes.");
        assert_eq!(list_scope(&f.app, &f.scope()).len(), 1);

        let hash = created["hash"].as_str().unwrap().to_string();
        assert!(save(&f.app, &f.scope(), None, input("weekly-report"), "").unwrap_err().contains("already exists"));
        assert!(save(&f.app, &f.scope(), Some("weekly-report"), input("weekly-report"), "stale").unwrap_err().contains("changed since"));
        let mut edit = input("weekly-report");
        edit.instructions = "Use the corrected comparison.".into();
        let edited = save(&f.app, &f.scope(), Some("weekly-report"), edit, &hash).unwrap();
        assert_ne!(edited["hash"], created["hash"]);
        assert!(std::fs::read_to_string(dir.join("SKILL.md")).unwrap().contains("Use the corrected comparison."));
        assert_eq!(std::fs::read_dir(f.home.join("workspaces").join(&f.bot).join("skills/.trash")).unwrap().count(), 1, "the replaced folder is in the trash");

        let removed = remove(&f.app, &f.scope(), "weekly-report", edited["hash"].as_str()).unwrap();
        assert_eq!(removed["removed"], true);
        assert!(list_scope(&f.app, &f.scope()).is_empty());
        assert!(remove(&f.app, &f.scope(), "weekly-report", None).is_err());
    }

    #[test]
    fn a_save_keeps_what_the_editor_does_not_show() {
        let f = Fixture::new();
        let created = save(&f.app, &f.scope(), None, input("pdf-tools"), "").unwrap();
        let dir = f.home.join("workspaces").join(&f.bot).join("skills/pdf-tools");
        std::fs::create_dir_all(dir.join("assets")).unwrap();
        std::fs::write(dir.join("assets/logo.bin"), [0u8, 159, 146, 150]).unwrap();
        std::fs::write(dir.join(SOURCE_FILE), "{}").unwrap();
        std::fs::write(dir.join("SKILL.md"), "---\nname: pdf-tools\ndescription: Use for PDFs\nlicense: MIT\nmetadata:\n  author: acme\n---\n\nBody").unwrap();
        let hash = hash_dir(&dir);
        let _ = created;
        let mut edit = input("pdf-tools");
        edit.files.clear();
        save(&f.app, &f.scope(), Some("pdf-tools"), edit, &hash).unwrap();
        assert_eq!(std::fs::read(dir.join("assets/logo.bin")).unwrap(), [0u8, 159, 146, 150]);
        assert!(dir.join(SOURCE_FILE).is_file());
        assert!(!dir.join("scripts/compare.sh").exists(), "a text file the editor dropped is gone");
        let skill = std::fs::read_to_string(dir.join("SKILL.md")).unwrap();
        assert!(skill.contains("license: MIT") && skill.contains("metadata:\n  author: acme"), "{skill}");
    }

    #[test]
    fn names_and_paths_that_could_leave_the_folder_are_refused() {
        let f = Fixture::new();
        for name in ["", "Upper", "a/b", "..", "-lead", "double--hyphen", "con"] {
            assert!(save(&f.app, &f.scope(), None, input(name), "").is_err(), "{name:?}");
        }
        for path in ["../x", "/etc/passwd", "a//b", "SKILL.md", ".lorca-builtin", "scripts/../../x", "a\\b"] {
            let mut bad = input("ok-name");
            bad.files = vec![InputFile { path: path.into(), text: "x".into() }];
            assert!(save(&f.app, &f.scope(), None, bad, "").is_err(), "{path:?}");
        }
        assert!(save(&f.app, &Scope { kind: "bot".into(), id: "../x".into() }, None, input("ok-name"), "").is_err());
        assert!(save(&f.app, &Scope::shared(), None, input("lorca"), "").unwrap_err().contains("Lorca ships"));
        assert!(list_scope(&f.app, &Scope::shared()).iter().all(|skill| skill.builtin), "only the skills Lorca ships are shared so far");
    }

    #[test]
    fn a_turn_sees_shared_bot_and_group_skills_and_the_nearest_wins_a_name() {
        let f = Fixture::new();
        save(&f.app, &Scope::shared(), None, input("report"), "").unwrap();
        save(&f.app, &f.scope(), None, input("report"), "").unwrap();
        save(&f.app, &Scope::project(&f.group), None, input("standup"), "").unwrap();
        assert!(f.home.join(format!("groups/{}/skills/standup/SKILL.md", f.group)).is_file());
        assert!(f.home.join("skills/report/SKILL.md").is_file());

        let dm = f.app.state.lock().unwrap().chats.iter().find(|c| !c.meta.is_group()).unwrap().meta.id.clone();
        assert_eq!(scopes_for_turn(&f.app, &f.bot, &dm), vec![Scope::shared(), f.scope()]);
        let scopes = scopes_for_turn(&f.app, &f.bot, &f.group);
        assert_eq!(scopes.len(), 3);
        assert_eq!(find(&f.app, &scopes, "report").unwrap().scope, f.scope());
        assert_eq!(find(&f.app, &scopes, "standup").unwrap().scope, Scope::project(&f.group));
        let listed = list_for(&f.app, &f.bot, &f.group).unwrap();
        let shipped = if cfg!(feature = "runner") { BUILTIN_NAMES.len() } else { 0 };
        assert_eq!(listed["skills"].as_array().unwrap().len(), 3 + shipped, "the three saved here, and the ones Lorca ships");
        let (name, script) = resource(&f.app, &scopes, "report", "scripts/compare.sh").unwrap();
        assert_eq!((name.as_str(), script.as_str()), ("report", "printf 'example'"));
        assert!(resource(&f.app, &scopes, "report", "../../identity.json").is_err());
    }

    #[test]
    fn frontmatter_values_are_plain_quoted_folded_or_literal() {
        let (fields, body) = parse_skill_md("---\r\nname: pdf-tools\r\ndescription: >\r\n  Work with PDFs.\r\n  Use for forms.\r\nlicense: 'MIT'\r\nmetadata:\r\n  name: nested\r\n---\r\n\r\n# Body\r\nText\r\n");
        assert_eq!(fields["name"], "pdf-tools");
        assert_eq!(fields["description"], "Work with PDFs. Use for forms.");
        assert_eq!(fields["license"], "MIT");
        assert!(body.contains("# Body"));
        let (fields, body) = parse_skill_md("No frontmatter here");
        assert!(fields.is_empty());
        assert_eq!(body, "No frontmatter here");
    }

    #[test]
    fn an_older_library_becomes_folders_and_is_set_aside() {
        let f = Fixture::new();
        let dek = f.app.dek().unwrap();
        let library = json!({ "records": {
            "playbook-1": { "id": "playbook-1", "scope": { "kind": "bot", "id": f.bot }, "revisions": [
                { "id": "r1", "revision": 1, "created_at": 1.0, "status": "saved", "hash": "", "content": { "name": "old-skill", "description": "From the old library", "instructions": "Do it.", "examples": "An example", "references": [{ "path": "references/a.md", "text": "ref" }], "scripts": [] } },
                { "id": "r2", "revision": 2, "created_at": 2.0, "status": "saved", "hash": "", "content": { "name": "old-skill", "description": "From the old library", "instructions": "Do it better.", "examples": "", "references": [], "scripts": [] } } ] },
            "playbook-2": { "id": "playbook-2", "scope": { "kind": "bot", "id": f.bot }, "revisions": [
                { "id": "r3", "revision": 1, "created_at": 1.0, "status": "draft", "hash": "", "content": { "name": "only-a-draft", "description": "d", "instructions": "x", "examples": "", "references": [], "scripts": [] } } ] } } });
        let ciphertext = crate::crypto::encrypt_json(&dek, "playbooks", &library).unwrap();
        std::fs::write(f.home.join("playbooks.enc"), ciphertext).unwrap();
        migrate(&f.app);
        let skills = list_scope(&f.app, &f.scope());
        assert_eq!(skills.iter().map(|s| s.name.as_str()).collect::<Vec<_>>(), ["old-skill"]);
        assert!(std::fs::read_to_string(skills[0].skill_file()).unwrap().contains("Do it better."));
        assert!(skills[0].dir.join("references/a.md").exists() == false, "the newest revision has no reference");
        assert!(!f.home.join("playbooks.enc").exists() && f.home.join("playbooks.enc.migrated").exists());
    }
}
