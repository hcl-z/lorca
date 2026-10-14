//! A template's skills are the bot's skill folders: each goes into the template as its
//! instructions and the text files in `references/` and `scripts/`, and a new bot's are written as
//! folders of its own. Other files stay out, since a template's link is small.

use super::format::{Resource, Skill};
use crate::app::App;
use crate::skills::{self, Input, InputFile, Scope};

/// The names of the bot's own skills, not the ones Lorca ships.
pub fn list(app: &App, bot_id: &str) -> Vec<String> {
    skills::list_scope(app, &Scope::bot(bot_id)).into_iter().filter(|skill| !skill.builtin).map(|skill| skill.name).collect()
}

/// A skill's content, scrubbed as the export scrubs it.
pub fn export(app: &App, bot_id: &str, name: &str) -> Result<Skill, String> {
    let view = skills::get(app, &Scope::bot(bot_id), name)?;
    let (mut references, mut scripts) = (Vec::new(), Vec::new());
    for file in view["files"].as_array().into_iter().flatten() {
        let (Some(path), Some(text)) = (file["path"].as_str(), file["text"].as_str()) else { continue };
        let resource = Resource { path: path.into(), text: skills::scrub(app, text) };
        if path.starts_with("references/") {
            references.push(resource);
        } else if path.starts_with("scripts/") {
            scripts.push(resource);
        }
    }
    Ok(Skill {
        name: name.into(),
        description: skills::scrub(app, view["description"].as_str().unwrap_or_default()),
        instructions: skills::scrub(app, view["instructions"].as_str().unwrap_or_default()),
        examples: String::new(),
        references,
        scripts,
    })
}

/// Writes a template's skill as a folder of the bot, and answers its name, which `remove` takes back.
pub fn save(app: &App, bot_id: &str, skill: &Skill) -> Result<String, String> {
    let mut instructions = skill.instructions.clone();
    if !skill.examples.trim().is_empty() {
        instructions.push_str(&format!("\n\n## Examples\n\n{}", skill.examples.trim()));
    }
    let files = skill.references.iter().chain(&skill.scripts).map(|file| InputFile { path: file.path.clone(), text: file.text.clone() }).collect();
    let input = Input { name: skill.name.clone(), description: skill.description.clone(), instructions, files };
    skills::save(app, &Scope::bot(bot_id), None, input, "")?;
    Ok(skill.name.clone())
}

pub fn remove(app: &App, bot_id: &str, name: &str) -> Result<(), String> {
    skills::remove(app, &Scope::bot(bot_id), name, None).map(|_| ())
}
