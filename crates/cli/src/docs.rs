//! The skills Lorca ships to every bot: `lorca` points at its user docs on lorca.app, so a bot
//! answers how to use Lorca from them instead of guessing; `write-skill` and `save-skill` say how
//! to keep a way of working as a skill and how to bring in skills other tools saved. They are
//! written into the shared skills folder, where every bot reads them like any skill.

use std::path::Path;

use sha2::{Digest, Sha256};

use crate::app::App;
use crate::skills::BUILTIN_MARK;

/// A skill that ships with Lorca and its files, by path in the skill's folder.
pub struct Builtin {
    pub name: &'static str,
    pub files: &'static [(&'static str, &'static str)],
}

pub const BUILTINS: [Builtin; 3] = [
    Builtin { name: "lorca", files: &[("SKILL.md", include_str!("../skills/lorca/SKILL.md"))] },
    Builtin {
        name: "write-skill",
        files: &[
            ("SKILL.md", include_str!("../skills/write-skill/SKILL.md")),
            ("references/format.md", include_str!("../skills/write-skill/references/format.md")),
        ],
    },
    Builtin {
        name: "save-skill",
        files: &[
            ("SKILL.md", include_str!("../skills/save-skill/SKILL.md")),
            ("references/locations.md", include_str!("../skills/save-skill/references/locations.md")),
        ],
    },
];

impl Builtin {
    /// What this build ships of the skill, to tell an older copy from it.
    fn digest(&self) -> String {
        let mut hasher = Sha256::new();
        for (path, text) in self.files {
            hasher.update(path.as_bytes());
            hasher.update([0]);
            hasher.update(text.as_bytes());
        }
        hasher.finalize().iter().map(|byte| format!("{byte:02x}")).collect()
    }
}

fn write(dir: &Path, builtin: &Builtin) -> std::io::Result<()> {
    for (path, text) in builtin.files {
        let file = dir.join(path);
        if let Some(parent) = file.parent() {
            std::fs::create_dir_all(parent)?;
        }
        std::fs::write(file, text)?;
    }
    std::fs::write(dir.join(BUILTIN_MARK), builtin.digest())
}

/// Writes the skills Lorca ships into the shared skills folder, and rewrites one an older build
/// wrote. A folder of that name the user made, or one whose mark the user removed to keep their
/// own version, is left alone.
pub fn install(app: &App) {
    let root = app.config.home.join("skills");
    for builtin in &BUILTINS {
        let dir = root.join(builtin.name);
        let mark = std::fs::read_to_string(dir.join(BUILTIN_MARK)).ok();
        if dir.exists() && mark.is_none() {
            continue;
        }
        let intact = builtin.files.iter().all(|(path, text)| std::fs::read_to_string(dir.join(path)).is_ok_and(|held| held == *text));
        if mark.as_deref() == Some(builtin.digest().as_str()) && intact {
            continue;
        }
        let _ = std::fs::remove_dir_all(&dir);
        if let Err(error) = write(&dir, builtin) {
            tracing::warn!(%error, skill = builtin.name, "writing a skill Lorca ships");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_shipped_skills_are_valid_and_named_as_reserved() {
        let names: Vec<&str> = BUILTINS.iter().map(|builtin| builtin.name).collect();
        assert_eq!(names, crate::skills::BUILTIN_NAMES);
        for builtin in &BUILTINS {
            let (_, text) = builtin.files.iter().find(|(path, _)| *path == "SKILL.md").unwrap();
            let (fields, body) = crate::skills::parse_skill_md(text);
            assert_eq!(fields["name"], builtin.name);
            assert!(!fields["description"].is_empty() && !body.trim().is_empty());
        }
        let lorca = BUILTINS[0].files[0].1;
        assert!(lorca.contains("https://lorca.app/llms.txt") && lorca.contains("https://lorca.app/docs/<page>.md"));
    }

    #[test]
    fn installing_writes_the_skills_and_respects_a_folder_the_user_took_over() {
        let home = std::env::temp_dir().join(format!("lorca-docs-{}", uuid::Uuid::new_v4()));
        let app = App::load(crate::config::Config { home: home.clone(), port: 0 }).unwrap();
        let shared = home.join("skills");
        install(&app);
        assert!(shared.join("lorca/SKILL.md").is_file() && shared.join("write-skill/references/format.md").is_file() && shared.join("save-skill/references/locations.md").is_file());
        assert!(crate::skills::list_scope(&app, &crate::skills::Scope::shared()).iter().all(|skill| skill.builtin));

        std::fs::write(shared.join("lorca/SKILL.md"), "changed").unwrap();
        install(&app);
        assert!(std::fs::read_to_string(shared.join("lorca/SKILL.md")).unwrap().contains("llms.txt"), "a copy the mark vouches for is rewritten");
        std::fs::remove_file(shared.join("write-skill").join(BUILTIN_MARK)).unwrap();
        std::fs::write(shared.join("write-skill/SKILL.md"), "mine").unwrap();
        install(&app);
        assert_eq!(std::fs::read_to_string(shared.join("write-skill/SKILL.md")).unwrap(), "mine", "no mark, no overwrite");
        let _ = std::fs::remove_dir_all(home);
    }
}
