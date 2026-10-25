//! The user's profile as the account holds it: what every Runner's `PROFILE.md` has said, one
//! entry per fact, so a fact learned on one Runner reaches the others. It rides the roster, which
//! the account key encrypts and the relay already carries, as `RosterBlob::profile`. A build that
//! does not know the field leaves it out and keeps its own `PROFILE.md`.
//!
//! An entry is keyed by its fact (`memory::fact_of`, lowercased: the line without bullet, date,
//! source, case, or extra spaces), so the same fact written on two Runners is one entry. A fact
//! that left a profile (removed, superseded, or deleted in the editor) stays as `gone`, so a
//! Runner that has not heard of it does not bring it back. The later change of an entry wins.
//!
//! `PROFILE.md` stays the record a Runner's bots read and write. Every Device keeps the entries in
//! `memory/profile/account.json`; a Runner also keeps which facts its file held when the two last
//! matched, so a fact that left the file since is told from one it has yet to receive.

use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

use serde::{Deserialize, Serialize};

use crate::memory::{self, MemoryStore};

/// One fact of the account's profile.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Entry {
    /// The fact's line as a profile had it: `- 2026-10-25 · from … · the user lives in Hangzhou`.
    pub line: String,
    /// When it last changed, written or gone, in Unix seconds.
    pub changed_at: f64,
    /// The fact left the profile.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub gone: bool,
    /// It left because the user asked to forget it, so no Runner learns it again.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub forgotten: bool,
}

/// The account's profile: entries by fact key.
pub type Entries = BTreeMap<String, Entry>;

/// What a Device keeps of the account's profile between runs.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct Kept {
    entries: Entries,
    /// The facts `PROFILE.md` held when it and the entries last matched; `None` on a Runner
    /// that has not matched them yet.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    seen: Option<BTreeSet<String>>,
}

/// One Device's account.json is read and written by one thread at a time.
static KEPT: Mutex<()> = Mutex::new(());

/// Where a Device keeps the account's entries. Unpairing deletes it with the rest of the
/// account; `PROFILE.md` stays, as this Runner's bots' memory does.
pub fn path(home: &Path) -> PathBuf {
    memory::root(home).join("profile").join("account.json")
}

fn load(home: &Path) -> Kept {
    std::fs::read_to_string(path(home)).ok().and_then(|text| serde_json::from_str(&text).ok()).unwrap_or_default()
}

fn save(home: &Path, kept: &Kept) {
    let path = path(home);
    let saved = (|| -> std::io::Result<()> {
        std::fs::create_dir_all(path.parent().unwrap_or(Path::new(".")))?;
        let tmp = path.with_extension("json.tmp");
        std::fs::write(&tmp, serde_json::to_vec(kept)?)?;
        crate::config::set_private(&tmp).map_err(std::io::Error::other)?;
        std::fs::rename(&tmp, &path)
    })();
    if let Err(error) = saved {
        tracing::warn!(%error, "saving the account's profile");
    }
}

/// The key an entry is held under.
pub fn key(line: &str) -> Option<String> {
    let fact = memory::fact_of(line).to_lowercase();
    (!fact.is_empty()).then_some(fact)
}

/// A file's live facts by key, each with its line: entry lines, without headings, blank lines,
/// or the entries a supersede struck through.
fn live(text: &str) -> BTreeMap<String, String> {
    text.lines()
        .map(str::trim)
        .filter(|line| line.starts_with("- ") && !line.starts_with("- ~~"))
        .filter_map(|line| key(line).map(|key| (key, line.to_string())))
        .collect()
}

/// Whether `theirs` should replace `ours`: the later change, and on the same second the same one
/// on every Device, so two Devices never keep different entries for good.
fn later(theirs: &Entry, ours: &Entry) -> bool {
    match theirs.changed_at.partial_cmp(&ours.changed_at) {
        Some(std::cmp::Ordering::Greater) => true,
        Some(std::cmp::Ordering::Equal) => (theirs.gone, &theirs.line) > (ours.gone, &ours.line),
        _ => false,
    }
}

/// Takes another Device's entries into `ours`, entry by entry, the later change winning.
fn merge(ours: &mut Entries, theirs: Entries) {
    for (key, entry) in theirs {
        if ours.get(&key).is_none_or(|held| later(&entry, held)) {
            ours.insert(key, entry);
        }
    }
}

/// Folds what changed in `PROFILE.md` since it and the entries last matched into the entries, as
/// of `now`, then makes the file say what the entries say. True when the entries changed here.
///
/// A fact the file has and did not have then was written here: a new entry, or one written again
/// after it went. A fact it had then and no longer has left it here: gone, forgotten when the
/// store's `forgotten.md` lists it. Before the first match nothing has left: a Runner's first sync
/// adds its facts and takes nobody's away, and a fact the account already holds keeps its state.
/// Then a live entry's line the file lacks is added at its end and a gone entry's line taken out,
/// through `remove` for a forgotten one so this Runner's write gate refuses it too.
fn reconcile(store: &MemoryStore, kept: &mut Kept, now: f64) -> bool {
    let text = store.read_index();
    let in_file = live(&text);
    let forgotten: BTreeSet<String> = store.forgotten_facts().iter().filter_map(|fact| key(fact)).collect();
    let mut changed = false;
    match &kept.seen {
        Some(seen) => {
            for (key, line) in &in_file {
                let new_here = !seen.contains(key) && kept.entries.get(key).is_none_or(|entry| entry.gone || entry.line != *line);
                if new_here {
                    kept.entries.insert(key.clone(), Entry { line: line.clone(), changed_at: now, gone: false, forgotten: false });
                    changed = true;
                }
            }
            for key in seen.difference(&in_file.keys().cloned().collect()) {
                if let Some(entry) = kept.entries.get_mut(key).filter(|entry| !entry.gone) {
                    entry.gone = true;
                    entry.forgotten = forgotten.contains(key);
                    entry.changed_at = now;
                    changed = true;
                }
            }
        }
        None => {
            for (key, line) in &in_file {
                if !kept.entries.contains_key(key) {
                    kept.entries.insert(key.clone(), Entry { line: line.clone(), changed_at: now, gone: false, forgotten: false });
                    changed = true;
                }
            }
        }
    }
    let mut lines: Vec<String> = text.lines().map(str::to_string).collect();
    let mut forget = Vec::new();
    lines.retain(|line| {
        let trimmed = line.trim();
        let is_entry = trimmed.starts_with("- ") && !trimmed.starts_with("- ~~");
        let gone = is_entry.then(|| key(trimmed)).flatten().and_then(|key| kept.entries.get(&key)).filter(|entry| entry.gone);
        match gone {
            Some(entry) if entry.forgotten => {
                forget.push(trimmed.to_string());
                true
            }
            Some(_) => false,
            None => true,
        }
    });
    for (key, entry) in &kept.entries {
        if !entry.gone && !in_file.contains_key(key) {
            lines.push(entry.line.clone());
        }
    }
    let mut new = lines.join("\n");
    if !new.is_empty() {
        new.push('\n');
    }
    // A bot that wrote in between makes the write refuse, and nothing more is changed here: the
    // next match takes its change in.
    let wrote = new == text || store.write_index(&new, Some(&memory::hash_text(&text))).is_ok();
    if wrote {
        for line in forget {
            let _ = store.remove(&line);
        }
        kept.seen = Some(live(&store.read_index()).into_keys().collect());
    }
    changed
}

/// The entries this Device's roster carries, with what changed in this Runner's `PROFILE.md`
/// folded in first. `runner` is false on a Device whose bots run elsewhere: it only carries the
/// account's entries on.
pub fn for_roster(home: &Path, runner: bool) -> Entries {
    let _held = KEPT.lock().unwrap();
    let mut kept = load(home);
    if runner {
        reconcile(&crate::profile::store(home), &mut kept, crate::config::now_secs());
        save(home, &kept);
    }
    kept.entries
}

/// Takes the entries a roster from another Device carries, and on a Runner writes what changed
/// into `PROFILE.md`. True when this Device's roster should go out again. A roster from a build
/// that does not know the profile carries `None` and changes nothing.
pub fn take(home: &Path, theirs: Option<Entries>, runner: bool) -> bool {
    let _held = KEPT.lock().unwrap();
    let mut kept = load(home);
    let Some(theirs) = theirs else { return !kept.entries.is_empty() };
    merge(&mut kept.entries, theirs.clone());
    if runner {
        reconcile(&crate::profile::store(home), &mut kept, crate::config::now_secs());
    }
    save(home, &kept);
    // Ahead of the roster that came: an entry it lacks or holds older, from this Device's own
    // entries or its PROFILE.md.
    kept.entries.iter().any(|(key, entry)| theirs.get(key).is_none_or(|other| later(entry, other)))
}

/// Whether `PROFILE.md` changed since it and the entries last matched: a bot wrote it, the capture
/// did, or the user edited it. The scheduler's tick sends the roster again when it did.
pub fn changed_here(home: &Path) -> bool {
    let _held = KEPT.lock().unwrap();
    let kept = load(home);
    let file: BTreeSet<String> = live(&crate::profile::store(home).read_index()).into_keys().collect();
    kept.seen.as_ref() != Some(&file)
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Runner(PathBuf);
    impl Drop for Runner {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    impl Runner {
        fn new() -> Self {
            Runner(std::env::temp_dir().join(format!("lorca-profile-sync-{}", uuid::Uuid::new_v4())))
        }
        fn store(&self) -> MemoryStore {
            crate::profile::store(&self.0)
        }
        fn facts(&self) -> Vec<String> {
            live(&self.store().read_index()).into_keys().collect()
        }
    }

    /// One Device's roster reaching the other: its entries go there and are taken in.
    fn send(from: &Runner, to: &Runner) -> bool {
        take(&to.0, Some(for_roster(&from.0, true)), true)
    }

    #[test]
    fn a_fact_learned_on_one_runner_reaches_the_other() {
        let (mac, linux) = (Runner::new(), Runner::new());
        mac.store().append_entry("the user lives in Hangzhou", None, 0).unwrap();
        linux.store().append_entry("the user prefers short replies", None, 0).unwrap();
        assert!(send(&mac, &linux), "linux holds the reply style the mac lacks, so linux sends again");
        assert!(!send(&linux, &mac), "the mac has everything now");
        let both = ["the user lives in hangzhou", "the user prefers short replies"];
        assert_eq!(mac.facts(), both);
        assert_eq!(linux.facts(), both);
        assert!(!send(&mac, &linux), "nothing left to tell");
    }

    #[test]
    fn a_change_on_one_runner_replaces_the_fact_on_the_other() {
        let (mac, linux) = (Runner::new(), Runner::new());
        mac.store().append_entry("the user lives in Shanghai", None, 0).unwrap();
        send(&mac, &linux);
        assert_eq!(linux.facts(), ["the user lives in shanghai"]);
        mac.store().supersede("the user lives in Shanghai", "the user lives in Hangzhou", None, 0).unwrap();
        assert!(changed_here(&mac.0));
        send(&mac, &linux);
        assert_eq!(linux.facts(), ["the user lives in hangzhou"]);
        assert!(!linux.store().read_index().contains("Shanghai"), "{}", linux.store().read_index());
    }

    #[test]
    fn a_removed_fact_stays_removed_and_a_forgotten_one_is_refused_everywhere() {
        let (mac, linux) = (Runner::new(), Runner::new());
        mac.store().append_entry("the user ships on Fridays", None, 0).unwrap();
        mac.store().append_entry("the user is called Xiao Hong", None, 0).unwrap();
        send(&mac, &linux);
        // An edit that drops a line, and a remove that forgets one.
        let edited = mac.store().read_index().lines().filter(|line| !line.contains("Fridays")).collect::<Vec<_>>().join("\n");
        mac.store().write_index(&edited, None).unwrap();
        mac.store().remove("the user is called Xiao Hong").unwrap();
        send(&mac, &linux);
        assert!(linux.facts().is_empty(), "{}", linux.store().read_index());
        // Linux has not heard anything since: its own roster does not bring them back.
        send(&linux, &mac);
        assert!(mac.facts().is_empty(), "{}", mac.store().read_index());
        assert!(crate::memory_check::check("the user is called Xiao Hong", &linux.store()).is_err(), "forgotten on linux too");
        assert!(crate::memory_check::check("the user ships on Fridays", &linux.store()).is_ok(), "only removed, so it may be learned again");
    }

    #[test]
    fn a_runners_first_sync_adds_its_facts_and_takes_none_away() {
        let (mac, linux) = (Runner::new(), Runner::new());
        mac.store().append_entry("the user lives in Hangzhou", None, 0).unwrap();
        send(&mac, &linux);
        // A Runner set up before profiles synced, with a profile of its own.
        let old = Runner::new();
        old.store().append_entry("the user writes in Chinese", None, 0).unwrap();
        send(&mac, &old);
        assert_eq!(old.facts(), ["the user lives in hangzhou", "the user writes in chinese"]);
        send(&old, &mac);
        assert_eq!(mac.facts(), ["the user lives in hangzhou", "the user writes in chinese"]);
    }

    #[test]
    fn a_roster_without_the_profile_changes_nothing() {
        let mac = Runner::new();
        mac.store().append_entry("the user lives in Hangzhou", None, 0).unwrap();
        for_roster(&mac.0, true);
        assert!(take(&mac.0, None, true), "an older build's roster lacks it: send ours again");
        assert_eq!(mac.facts(), ["the user lives in hangzhou"]);
    }

    #[test]
    fn a_phone_carries_the_entries_on_and_writes_no_file() {
        let (mac, phone, linux) = (Runner::new(), Runner::new(), Runner::new());
        mac.store().append_entry("the user lives in Hangzhou", None, 0).unwrap();
        take(&phone.0, Some(for_roster(&mac.0, true)), false);
        assert!(!phone.store().index_path().exists());
        take(&linux.0, Some(for_roster(&phone.0, false)), true);
        assert_eq!(linux.facts(), ["the user lives in hangzhou"]);
    }

    #[test]
    fn the_same_second_settles_the_same_way_on_both_sides() {
        let a = Entry { line: "- x".into(), changed_at: 5.0, gone: false, forgotten: false };
        let b = Entry { line: "- x".into(), changed_at: 5.0, gone: true, forgotten: false };
        assert!(later(&b, &a) && !later(&a, &b));
    }
}
