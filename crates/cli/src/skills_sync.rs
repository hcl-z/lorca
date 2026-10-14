//! Skills between the Devices of an account. A bot's or a group's skill folder travels as a zip in
//! a blob of its own, encrypted with the account key, in a latest-only slot named for the skill;
//! a removed one as a note that it is gone. The Runner that hosts the bot or a member of the group
//! sends changes, and every Device with a CLI keeps a copy, so a bot that moves to another Runner
//! finds its skills there and a Runner that is set up again gets them back. Shared skills stay on
//! their Runner. The blob keeps the kind name `playbook`, which relays accept.

use std::collections::HashMap;
use std::io::{Cursor, Read, Write};
use std::path::Path;
use std::sync::{LazyLock, Mutex};

use base64::Engine;
use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::app::App;
use crate::skills::{self, Scope, SKILL_FILE};

/// A skill folder whose zip is larger than this stays on its own Runner.
const MAX_PACK_BYTES: usize = 8 * 1024 * 1024;
const KIND: &str = "playbook";

/// What each skill folder held when this Device last sent or received it, by home, scope, and name.
static SENT: LazyLock<Mutex<HashMap<String, String>>> = LazyLock::new(|| Mutex::new(HashMap::new()));

#[derive(Debug, Serialize, Deserialize)]
struct Pack {
    /// Older builds sent a whole record of revisions here; this format has `v` 2.
    v: u32,
    scope: Scope,
    name: String,
    #[serde(default)]
    deleted: bool,
    /// When the folder last changed, in unix seconds.
    modified: f64,
    #[serde(default)]
    hash: String,
    #[serde(default)]
    archive: String,
}

fn key(app: &App, scope: &Scope, name: &str) -> String {
    format!("{}|{}|{}|{}", app.config.home.display(), scope.kind, scope.id, name)
}

/// When a skill folder last changed, in unix seconds.
fn newest(dir: &Path) -> f64 {
    skills::walk(dir)
        .iter()
        .filter_map(|entry| std::fs::metadata(&entry.path).and_then(|m| m.modified()).ok())
        .filter_map(|time| time.duration_since(std::time::UNIX_EPOCH).ok())
        .map(|d| d.as_secs_f64())
        .fold(0.0, f64::max)
}

fn pack_dir(dir: &Path, name: &str) -> Result<Vec<u8>, String> {
    let mut writer = zip::ZipWriter::new(Cursor::new(Vec::new()));
    for entry in skills::walk(dir) {
        let bytes = std::fs::read(&entry.path).map_err(|e| e.to_string())?;
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
    Ok(writer.finish().map_err(|e| e.to_string())?.into_inner())
}

/// Queues a pack for the relay. It belongs to the group of its scope's chat, a group's own or the
/// bot's DM, so deleting that chat deletes it there too.
fn publish(app: &App, pack: &Pack) {
    let Some(dek) = app.dek() else { return };
    let group = {
        let state = app.state.lock().unwrap();
        state
            .chats
            .iter()
            .find(|chat| match pack.scope.kind.as_str() {
                "bot" => !chat.meta.is_group() && chat.meta.bot_ids == [pack.scope.id.clone()],
                _ => chat.meta.id == pack.scope.id,
            })
            .map(|chat| crate::model::relay_name(&chat.meta.id))
    };
    match crate::crypto::encrypt_json(&dek, KIND, pack) {
        Ok(ciphertext) => {
            let slot = format!("skill:{}:{}:{}", pack.scope.kind, pack.scope.id, pack.name);
            app.push_slot_blob(KIND, crate::app::Slot::latest(crate::model::relay_name(&slot)), group, ciphertext);
        }
        Err(error) => tracing::warn!(%error, "encrypting a skill"),
    }
}

/// Sends the skills of one bot or group that changed since this Device last sent or received them,
/// and a note for each that is gone. `force` sends every one.
fn push(app: &App, scope: &Scope, force: bool) {
    if scope.kind == "shared" || !skills::hosts(app, scope) {
        return;
    }
    let prefix = key(app, scope, "");
    let mut present = Vec::new();
    for skill in skills::list_scope(app, scope).into_iter().filter(|skill| !skill.builtin) {
        let stamp = skills::stamp(&skill.dir);
        let entry = key(app, scope, &skill.name);
        present.push(entry.clone());
        if !force && SENT.lock().unwrap().get(&entry) == Some(&stamp) {
            continue;
        }
        match pack_dir(&skill.dir, &skill.name) {
            Ok(archive) if archive.len() <= MAX_PACK_BYTES => {
                let pack = Pack {
                    v: 2,
                    scope: scope.clone(),
                    name: skill.name.clone(),
                    deleted: false,
                    modified: newest(&skill.dir),
                    hash: skills::hash_dir(&skill.dir),
                    archive: base64::engine::general_purpose::STANDARD.encode(archive),
                };
                publish(app, &pack);
            }
            Ok(_) => tracing::warn!(skill = %skill.name, "a skill is too large to send to the other Devices"),
            Err(error) => tracing::warn!(%error, skill = %skill.name, "packing a skill"),
        }
        SENT.lock().unwrap().insert(entry, stamp);
    }
    let gone: Vec<String> = SENT.lock().unwrap().keys().filter(|entry| entry.starts_with(&prefix) && !present.contains(entry)).cloned().collect();
    for entry in gone {
        SENT.lock().unwrap().remove(&entry);
        let name = entry[prefix.len()..].to_string();
        publish(app, &Pack { v: 2, scope: scope.clone(), name, deleted: true, modified: crate::config::now_secs(), hash: String::new(), archive: String::new() });
    }
}

/// After a skill of `scope` changed through the CLI's own calls.
pub fn push_scope(app: &App, scope: &Scope) {
    push(app, scope, false);
}

fn hosted_scopes(app: &App) -> Vec<Scope> {
    let (bots, chats): (Vec<String>, Vec<String>) = {
        let state = app.state.lock().unwrap();
        (state.bots.iter().map(|bot| bot.id.clone()).collect(), state.chats.iter().filter(|chat| chat.meta.is_group()).map(|chat| chat.meta.id.clone()).collect())
    };
    bots.iter().map(|id| Scope::bot(id)).chain(chats.iter().map(|id| Scope::project(id))).filter(|scope| skills::hosts(app, scope)).collect()
}

/// Sends what changed in any skill this Device hosts: at start, and after a turn, since a bot
/// writes its skills as files.
pub fn scan(app: &App) {
    hosted_scopes(app).iter().for_each(|scope| push(app, scope, false));
}

/// Sends every skill again, under the newest account key.
pub fn push_all(app: &App) {
    hosted_scopes(app).iter().for_each(|scope| push(app, scope, true));
}

/// Writes the skill a pack carries into a folder built beside the old one and swapped in, the old
/// one going to the trash.
fn extract(archive: &[u8], name: &str, root: &Path, dir: &Path) -> Result<(), String> {
    let mut zip = zip::ZipArchive::new(Cursor::new(archive)).map_err(|e| e.to_string())?;
    let staging = root.join(format!(".tmp-{}", uuid::Uuid::new_v4()));
    let built = (|| -> Result<(), String> {
        std::fs::create_dir_all(&staging).map_err(|e| e.to_string())?;
        let prefix = format!("{name}/");
        for index in 0..zip.len() {
            let mut file = zip.by_index(index).map_err(|e| e.to_string())?;
            if !file.is_file() {
                continue;
            }
            let Some(rel) = file.name().strip_prefix(&prefix).map(str::to_string) else { continue };
            if rel != SKILL_FILE && !skills::safe_relative(&rel) {
                continue;
            }
            let mut bytes = Vec::new();
            file.read_to_end(&mut bytes).map_err(|e| e.to_string())?;
            let dest = staging.join(&rel);
            if let Some(parent) = dest.parent() {
                std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
            }
            std::fs::write(&dest, &bytes).map_err(|e| e.to_string())?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                if let Some(mode) = file.unix_mode() {
                    let _ = std::fs::set_permissions(&dest, std::fs::Permissions::from_mode(mode & 0o777));
                }
            }
        }
        if !staging.join(SKILL_FILE).is_file() {
            return Err("The skill has no SKILL.md".into());
        }
        Ok(())
    })();
    if let Err(error) = built {
        let _ = std::fs::remove_dir_all(&staging);
        return Err(error);
    }
    let trashed = if dir.exists() { Some(skills::to_trash(root, dir, name)?) } else { None };
    if let Err(error) = std::fs::rename(&staging, dir) {
        if let Some(trashed) = trashed {
            let _ = std::fs::rename(trashed, dir);
        }
        let _ = std::fs::remove_dir_all(&staging);
        return Err(error.to_string());
    }
    Ok(())
}

/// Applies a skill another Device sent. The copy that changed last wins; the one it replaces goes
/// to the trash. A Device that does not host the bot or group keeps the copy for when it does.
pub fn apply_remote(app: &App, dek: &crate::keys::AccountKeys, ciphertext: &[u8]) -> Result<(), String> {
    let value: Value = crate::crypto::decrypt_json(dek, KIND, ciphertext).map_err(|e| e.to_string())?;
    if value["v"].as_u64() != Some(2) {
        return Ok(());
    }
    let pack: Pack = serde_json::from_value(value).map_err(|e| e.to_string())?;
    if pack.scope.kind == "shared" {
        return Ok(());
    }
    skills::check_name(&pack.name)?;
    let root = pack.scope.root(app)?;
    let dir = root.join(&pack.name);
    let entry = key(app, &pack.scope, &pack.name);
    if dir.join(skills::BUILTIN_MARK).exists() {
        return Ok(());
    }
    if pack.deleted {
        if dir.is_dir() && newest(&dir) <= pack.modified {
            skills::to_trash(&root, &dir, &pack.name)?;
        }
        SENT.lock().unwrap().remove(&entry);
        return Ok(());
    }
    if dir.is_dir() && (skills::hash_dir(&dir) == pack.hash || newest(&dir) >= pack.modified) {
        return Ok(());
    }
    let archive = base64::engine::general_purpose::STANDARD.decode(&pack.archive).map_err(|e| e.to_string())?;
    std::fs::create_dir_all(&root).map_err(|e| e.to_string())?;
    extract(&archive, &pack.name, &root, &dir)?;
    SENT.lock().unwrap().insert(entry, skills::stamp(&dir));
    Ok(())
}
