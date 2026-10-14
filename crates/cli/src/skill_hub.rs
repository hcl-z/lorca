//! Skills from public communities. A skill is a folder with a `SKILL.md` (agentskills.io); ClawHub
//! serves it as an archive and GitHub serves the repository holding it as one. An import becomes an
//! inactive playbook draft that the user reviews and saves, so a third party's text never reaches a
//! turn before then.

use std::collections::BTreeMap;
use std::io::Read;
use std::sync::Arc;
use std::time::Duration;

use reqwest::header::USER_AGENT;
use serde_json::{json, Value};

use crate::app::App;
use crate::playbooks::{self, PlaybookContent, Provenance, Resource, Scope};

const CLAWHUB_URL: &str = "https://clawhub.ai";
const GITHUB_ARCHIVE_URL: &str = "https://codeload.github.com/{owner}/{repo}/zip/{ref}";
const TIMEOUT: Duration = Duration::from_secs(30);
const MAX_ARCHIVE_BYTES: usize = 32 * 1024 * 1024;
const MAX_FILE_BYTES: u64 = 256 * 1024;
const MAX_TOTAL_BYTES: usize = 4 * 1024 * 1024;
const MAX_RESOURCES: usize = 16;
/// What a skill's text may take of the playbook's 64 KiB, leaving room for its JSON framing.
const CONTENT_BUDGET: usize = 62 * 1024;
const SEARCH_LIMIT: usize = 20;
/// How many of the most downloaded skills the store lists before a search.
const BROWSE_LIMIT: usize = 30;

/// ClawHub's address, or a mirror that `LORCA_CLAWHUB_URL` names.
fn clawhub_url() -> String {
    std::env::var("LORCA_CLAWHUB_URL").unwrap_or_else(|_| CLAWHUB_URL.into()).trim_end_matches('/').to_string()
}

/// GitHub's repository archives, or a mirror: `LORCA_GITHUB_ARCHIVE_URL` with `{owner}`, `{repo}`,
/// and `{ref}` in it.
fn github_archive_url(github: &GitHub) -> String {
    std::env::var("LORCA_GITHUB_ARCHIVE_URL")
        .unwrap_or_else(|_| GITHUB_ARCHIVE_URL.into())
        .replace("{owner}", &github.owner)
        .replace("{repo}", &github.repo)
        .replace("{ref}", github.git_ref.as_deref().unwrap_or("HEAD"))
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct GitHub {
    owner: String,
    repo: String,
    git_ref: Option<String>,
    /// The skill's folder in the repository.
    path: Option<String>,
    /// A skill named by its folder, wherever it sits (`owner/repo@skill`, as skills.sh writes it).
    skill: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Source {
    /// A ClawHub skill. Several publishers can use one slug, so ClawHub downloads it by slug and
    /// publisher.
    ClawHub { owner: Option<String>, slug: String },
    GitHub(GitHub),
}

const WHERE_FROM: &str = "Use a ClawHub, skills.sh, or GitHub link, or owner/repo";

fn plain_name(text: &str) -> bool {
    !text.is_empty() && text.len() <= 100 && text.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'))
}

impl Source {
    /// A ClawHub or skills.sh page, a GitHub link (a folder or a `SKILL.md` in it included),
    /// `clawhub:owner/slug`, `github:owner/repo/path`, or `owner/repo[/path][@skill]`.
    fn parse(input: &str) -> Result<Source, String> {
        let input = input.trim();
        if input.starts_with("http://") || input.starts_with("https://") {
            return Self::from_url(input);
        }
        if let Some(rest) = input.strip_prefix("clawhub:") {
            let rest = rest.trim();
            return match rest.split_once('/') {
                Some((owner, slug)) => Self::clawhub(Some(owner), slug),
                None => Self::clawhub(None, rest),
            };
        }
        let (location, skill) = match input.strip_prefix("github:").unwrap_or(input).split_once('@') {
            Some((location, skill)) => (location, Some(skill.trim().to_string()).filter(|s| !s.is_empty())),
            None => (input.strip_prefix("github:").unwrap_or(input), None),
        };
        let segments: Vec<&str> = location.split('/').map(str::trim).filter(|s| !s.is_empty()).collect();
        if segments.len() < 2 {
            return Err(WHERE_FROM.into());
        }
        let path = (segments.len() > 2).then(|| segments[2..].join("/"));
        Self::github(segments[0], segments[1], None, path, skill)
    }

    fn from_url(input: &str) -> Result<Source, String> {
        let url = reqwest::Url::parse(input).map_err(|_| WHERE_FROM.to_string())?;
        let host = url.host_str().unwrap_or_default().trim_start_matches("www.").to_ascii_lowercase();
        let segments: Vec<String> = url.path_segments().map(|s| s.filter(|p| !p.is_empty()).map(str::to_string).collect()).unwrap_or_default();
        match host.as_str() {
            "clawhub.ai" | "hub.openclaw.ai" => {
                // A skill ClawHub lists from skills.sh is `/skills-sh/owner/repo/skill`.
                if segments.first().map(String::as_str) == Some("skills-sh") && segments.len() >= 4 {
                    return Self::github(&segments[1], &segments[2], None, None, Some(segments[3].clone()));
                }
                let at = segments.iter().position(|s| s == "skills").ok_or(WHERE_FROM)?;
                let slug = segments.get(at + 1).ok_or(WHERE_FROM)?;
                Self::clawhub((at > 0).then(|| segments[at - 1].as_str()), slug)
            }
            "skills.sh" => {
                if segments.len() < 2 {
                    return Err(WHERE_FROM.into());
                }
                Self::github(&segments[0], &segments[1], None, None, segments.get(2).cloned())
            }
            "github.com" => {
                if segments.len() < 2 {
                    return Err(WHERE_FROM.into());
                }
                let (git_ref, path) = match segments.get(2).map(String::as_str) {
                    Some("tree") | Some("blob") if segments.len() > 3 => {
                        (Some(segments[3].clone()), (segments.len() > 4).then(|| segments[4..].join("/")))
                    }
                    _ => (None, None),
                };
                Self::github(&segments[0], &segments[1], git_ref, path, None)
            }
            _ => Err(WHERE_FROM.into()),
        }
    }

    fn clawhub(owner: Option<&str>, slug: &str) -> Result<Source, String> {
        if !plain_name(slug) || owner.is_some_and(|owner| !plain_name(owner)) {
            return Err(WHERE_FROM.into());
        }
        Ok(Source::ClawHub { owner: owner.map(str::to_string), slug: slug.into() })
    }

    fn github(owner: &str, repo: &str, git_ref: Option<String>, path: Option<String>, skill: Option<String>) -> Result<Source, String> {
        let repo = repo.strip_suffix(".git").unwrap_or(repo);
        let path = path.map(|p| p.trim_matches('/').to_string()).filter(|p| !p.is_empty());
        let path = path.map(|p| p.strip_suffix("SKILL.md").map(|dir| dir.trim_end_matches('/').to_string()).unwrap_or(p));
        let bad_ref = git_ref.as_deref().is_some_and(|r| r.is_empty() || r.contains("..") || r.contains(|c: char| c.is_whitespace() || matches!(c, '?' | '#')));
        if !plain_name(owner) || !plain_name(repo) || bad_ref || skill.as_deref().is_some_and(|s| !plain_name(s)) {
            return Err(WHERE_FROM.into());
        }
        Ok(Source::GitHub(GitHub { owner: owner.into(), repo: repo.into(), git_ref, path, skill }))
    }

    fn label(&self) -> String {
        match self {
            Source::ClawHub { owner: Some(owner), slug } => format!("ClawHub {owner}/{slug}"),
            Source::ClawHub { owner: None, slug } => format!("ClawHub {slug}"),
            Source::GitHub(g) => {
                let mut label = format!("github.com/{}/{}", g.owner, g.repo);
                if let Some(path) = g.path.as_ref().or(g.skill.as_ref()) {
                    label.push('/');
                    label.push_str(path);
                }
                label
            }
        }
    }
}

async fn get(app: &App, url: &str, query: &[(&str, &str)]) -> Result<reqwest::Response, String> {
    let response = app
        .http
        .get(url)
        .query(query)
        .timeout(TIMEOUT)
        .header(USER_AGENT, concat!("lorca/", env!("CARGO_PKG_VERSION")))
        .send()
        .await
        .map_err(|e| format!("Could not reach {url}: {e}"))?;
    match response.status() {
        status if status.is_success() => Ok(response),
        reqwest::StatusCode::NOT_FOUND => Err("That skill was not found".into()),
        reqwest::StatusCode::TOO_MANY_REQUESTS => Err("The skill site is limiting requests; try again in a minute".into()),
        reqwest::StatusCode::CONFLICT => Err("Several publishers use that skill name; use the skill's link on clawhub.ai, or clawhub:publisher/name".into()),
        status => {
            let body = response.text().await.unwrap_or_default();
            let body: String = body.trim().chars().take(240).collect();
            Err(if body.is_empty() { format!("{url} answered {status}") } else { format!("{url} answered {status}: {body}") })
        }
    }
}

async fn download(app: &App, url: &str, query: &[(&str, &str)]) -> Result<Vec<u8>, String> {
    let mut response = get(app, url, query).await?;
    if response.content_length().is_some_and(|len| len as usize > MAX_ARCHIVE_BYTES) {
        return Err("That download is too large to import".into());
    }
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|e| e.to_string())? {
        bytes.extend_from_slice(&chunk);
        if bytes.len() > MAX_ARCHIVE_BYTES {
            return Err("That download is too large to import".into());
        }
    }
    Ok(bytes)
}

/// Searches ClawHub, or lists its most downloaded skills when `query` is empty.
pub async fn search(app: &App, query: &str) -> Result<Value, String> {
    let query = query.trim();
    let limit = (if query.is_empty() { BROWSE_LIMIT } else { SEARCH_LIMIT }).to_string();
    let limit = limit.as_str();
    let (path, params): (&str, Vec<(&str, &str)>) = if query.is_empty() {
        ("/api/v1/skills", vec![("sort", "downloads"), ("limit", limit), ("nonSuspiciousOnly", "true")])
    } else {
        ("/api/v1/search", vec![("q", query), ("limit", limit), ("nonSuspiciousOnly", "true")])
    };
    let text = get(app, &format!("{}{path}", clawhub_url()), &params).await?.text().await.map_err(|e| e.to_string())?;
    let body: Value = serde_json::from_str(&text).map_err(|_| "ClawHub answered with something other than JSON".to_string())?;
    let rows = body["results"].as_array().or_else(|| body["items"].as_array()).cloned().unwrap_or_default();
    let results: Vec<Value> = rows.iter().filter_map(listing).collect();
    Ok(json!({ "results": results }))
}

/// A row of ClawHub's search or list as the store shows it, with the reference `skills.import`
/// takes. ClawHub's search also lists skills it indexes from skills.sh, which are GitHub folders
/// (`owner/repo@skill`); a row that is not installable, or from another source, is left out.
fn listing(row: &Value) -> Option<Value> {
    let slug = row["slug"].as_str().filter(|s| plain_name(s))?;
    if row["trust"]["installability"].as_str().is_some_and(|state| state != "installable") {
        return None;
    }
    let owner = row["ownerHandle"].as_str().filter(|o| plain_name(o));
    let source = row["source"].as_str().unwrap_or("clawhub");
    let (reference, url) = match source {
        "clawhub" => (
            match owner {
                Some(owner) => format!("clawhub:{owner}/{slug}"),
                None => format!("clawhub:{slug}"),
            },
            match owner {
                Some(owner) => format!("https://clawhub.ai/{owner}/skills/{slug}"),
                None => format!("https://clawhub.ai/skills/{slug}"),
            },
        ),
        "skills-sh" => {
            let from = &row["sourceIdentity"];
            let (owner, repo) = (from["owner"].as_str().filter(|o| plain_name(o))?, from["repo"].as_str().filter(|r| plain_name(r))?);
            (format!("{owner}/{repo}@{slug}"), row["links"]["source"].as_str().map(str::to_string).unwrap_or_else(|| format!("https://skills.sh/{owner}/{repo}/{slug}")))
        }
        _ => return None,
    };
    let version = row["version"].as_str().or_else(|| row["latestVersion"]["version"].as_str()).unwrap_or_default();
    Some(json!({
        "source": source,
        "reference": reference,
        "name": row["displayName"].as_str().filter(|n| !n.is_empty()).unwrap_or(slug),
        "summary": row["summary"].as_str().unwrap_or_default(),
        "owner": owner.unwrap_or_default(),
        "version": version,
        "url": url,
    }))
}

/// The files of a skill's folder with their paths inside it, and what was left behind.
struct Folder {
    files: Vec<(String, Vec<u8>)>,
    skipped: Vec<String>,
}

/// Reads the archive's skill folder. A GitHub archive wraps the repository in one folder, which
/// `strip_top` removes.
fn read_folder(bytes: &[u8], strip_top: bool, path: Option<&str>, skill: Option<&str>) -> Result<(Folder, String), String> {
    let mut archive = zip::ZipArchive::new(std::io::Cursor::new(bytes)).map_err(|e| format!("That is not a skill archive: {e}"))?;
    let mut entries = Vec::new();
    for index in 0..archive.len() {
        let entry = archive.by_index(index).map_err(|e| e.to_string())?;
        if !entry.is_file() {
            continue;
        }
        let name = entry.name().replace('\\', "/");
        let name = if strip_top { name.split_once('/').map(|(_, rest)| rest.to_string()).unwrap_or_default() } else { name };
        if name.is_empty() || name.split('/').any(|part| part.is_empty() || part == "..") {
            continue;
        }
        entries.push((index, name));
    }
    let names: Vec<&str> = entries.iter().map(|(_, name)| name.as_str()).collect();
    let root = choose_root(&names, path, skill)?;
    let prefix = if root.is_empty() { String::new() } else { format!("{root}/") };
    let mut folder = Folder { files: Vec::new(), skipped: Vec::new() };
    let mut total = 0;
    for (index, name) in &entries {
        let Some(relative) = name.strip_prefix(&prefix) else { continue };
        let mut entry = archive.by_index(*index).map_err(|e| e.to_string())?;
        let mut data = Vec::new();
        entry.by_ref().take(MAX_FILE_BYTES + 1).read_to_end(&mut data).map_err(|e| e.to_string())?;
        total += data.len();
        if data.len() as u64 > MAX_FILE_BYTES || total > MAX_TOTAL_BYTES {
            folder.skipped.push(relative.to_string());
            continue;
        }
        folder.files.push((relative.to_string(), data));
    }
    Ok((folder, root))
}

/// The folder holding the skill: the one the link names, the one called `skill`, or the only one.
fn choose_root(names: &[&str], path: Option<&str>, skill: Option<&str>) -> Result<String, String> {
    let roots: Vec<String> = names
        .iter()
        .filter_map(|name| {
            let dir = name.strip_suffix("SKILL.md")?;
            if dir.is_empty() { Some(String::new()) } else { dir.strip_suffix('/').map(str::to_string) }
        })
        .collect();
    let last = |root: &str| root.rsplit('/').next().unwrap_or_default().to_string();
    if let Some(path) = path {
        return roots.iter().find(|root| root.as_str() == path).cloned().ok_or_else(|| format!("No SKILL.md in {path}"));
    }
    if let Some(skill) = skill {
        return roots
            .iter()
            .filter(|root| last(root).eq_ignore_ascii_case(skill))
            .min_by_key(|root| root.len())
            .cloned()
            .ok_or_else(|| format!("No skill named {skill} here"));
    }
    match roots.as_slice() {
        [] => Err("No SKILL.md found there".into()),
        [only] => Ok(only.clone()),
        _ if roots.iter().any(String::is_empty) => Ok(String::new()),
        _ => {
            let mut listed: Vec<String> = roots.iter().map(|root| last(root)).collect();
            listed.sort();
            listed.dedup();
            let more = listed.len().saturating_sub(8);
            listed.truncate(8);
            Err(format!(
                "That holds {} skills ({}{}); name one, for example owner/repo@{}",
                roots.len(),
                listed.join(", "),
                if more > 0 { format!(", and {more} more") } else { String::new() },
                listed[0]
            ))
        }
    }
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
fn parse_skill_md(text: &str) -> (BTreeMap<String, String>, String) {
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

fn slugify(text: &str) -> String {
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

fn truncate_bytes(text: &str, max: usize) -> String {
    let mut end = max.min(text.len());
    while !text.is_char_boundary(end) {
        end -= 1;
    }
    text[..end].trim().to_string()
}

/// Playbook content from a skill folder. The skill's text and the text files under `references/`
/// and `scripts/` come across; every other file is named in the skipped list.
fn content_from(folder: Folder, fallback_name: &str) -> Result<(PlaybookContent, Vec<String>), String> {
    let Folder { files, mut skipped } = folder;
    let skill = files.iter().find(|(path, _)| path == "SKILL.md").ok_or("That folder has no SKILL.md")?;
    let text = String::from_utf8(skill.1.clone()).map_err(|_| "SKILL.md is not UTF-8 text".to_string())?;
    let (fields, body) = parse_skill_md(&text);
    let mut name = slugify(fields.get("name").map(String::as_str).unwrap_or_default());
    if name.is_empty() {
        name = slugify(fallback_name);
    }
    if name.is_empty() {
        return Err("The skill has no usable name".into());
    }
    let instructions = body.trim().to_string();
    let described = fields.get("description").cloned().filter(|d| !d.trim().is_empty()).or_else(|| {
        instructions.lines().map(|l| l.trim().trim_start_matches('#').trim()).find(|l| !l.is_empty()).map(str::to_string)
    });
    let description = truncate_bytes(&described.unwrap_or_default().split_whitespace().collect::<Vec<_>>().join(" "), 512);
    if description.is_empty() || instructions.is_empty() {
        return Err("The skill has no description or instructions".into());
    }
    let mut budget = CONTENT_BUDGET.saturating_sub(instructions.len() + description.len());
    let mut content = PlaybookContent { name, description, instructions, ..Default::default() };
    let mut sorted: Vec<&(String, Vec<u8>)> = files.iter().filter(|(path, _)| path != "SKILL.md").collect();
    sorted.sort_by(|a, b| a.0.cmp(&b.0));
    for (path, data) in sorted {
        let kind = ["references", "scripts"].into_iter().find(|kind| playbooks::safe_resource_path(path, kind));
        let text = String::from_utf8(data.clone()).ok().filter(|t| !t.contains('\0'));
        let (Some(kind), Some(text)) = (kind, text) else {
            skipped.push(path.clone());
            continue;
        };
        let target = if kind == "references" { &mut content.references } else { &mut content.scripts };
        let cost = path.len() + text.len() + 48;
        // Lorca itself runs recorded browser steps, so only the user records them.
        if path == crate::browser::steps::PATH || target.len() >= MAX_RESOURCES || cost > budget {
            skipped.push(path.clone());
            continue;
        }
        budget -= cost;
        target.push(Resource { path: path.clone(), text });
    }
    skipped.sort();
    Ok((content, skipped))
}

async fn fetch_github(app: &App, github: &GitHub) -> Result<(PlaybookContent, Vec<String>), String> {
    let bytes = download(app, &github_archive_url(github), &[]).await?;
    let (folder, root) = read_folder(&bytes, true, github.path.as_deref(), github.skill.as_deref())?;
    let fallback = if root.is_empty() { github.repo.clone() } else { root.rsplit('/').next().unwrap_or_default().to_string() };
    content_from(folder, &fallback)
}

async fn fetch(app: &App, source: &Source) -> Result<(PlaybookContent, Vec<String>), String> {
    match source {
        Source::GitHub(github) => fetch_github(app, github).await,
        Source::ClawHub { owner, slug } => {
            let mut query = vec![("slug", slug.as_str())];
            if let Some(owner) = owner {
                query.push(("ownerHandle", owner.as_str()));
            }
            let bytes = download(app, &format!("{}/api/v1/download", clawhub_url()), &query).await?;
            if bytes.starts_with(b"PK") {
                let (folder, _) = read_folder(&bytes, false, None, None)?;
                return content_from(folder, slug);
            }
            // A skill ClawHub keeps on GitHub is answered with where it is.
            let handoff: Value = serde_json::from_slice(&bytes).map_err(|_| "ClawHub answered with neither an archive nor a GitHub address".to_string())?;
            let repo = handoff["repo"].as_str().unwrap_or_default().trim_start_matches("https://github.com/").trim_end_matches(".git").to_string();
            let (owner, repo) = repo.split_once('/').ok_or("ClawHub answered with neither an archive nor a GitHub address")?;
            match Source::github(owner, repo, handoff["commit"].as_str().map(str::to_string), handoff["path"].as_str().map(str::to_string), None)? {
                Source::GitHub(github) => fetch_github(app, &github).await,
                Source::ClawHub { .. } => unreachable!("github() builds GitHub sources"),
            }
        }
    }
}

/// Writes imported content into `scope`: a saved skill the bots use at once when `activate`, else
/// an inactive draft for the user to review.
fn keep(app: &App, scope: &Scope, content: PlaybookContent, source: &Source, activate: bool) -> Result<Value, String> {
    let provenance = Provenance { kind: "import".into(), note: format!("Imported from {}", source.label()), ..Default::default() };
    if activate {
        playbooks::save(app, scope, None, content, 0, "", provenance)
    } else {
        playbooks::draft(app, scope, content, provenance)
    }
}

/// Fetches the skill `source` names into `scope`; `skipped` lists the files that did not come across.
pub async fn import(app: &Arc<App>, scope: &Scope, source: &str, activate: bool) -> Result<Value, String> {
    scope.validate(app)?;
    let source = Source::parse(source)?;
    let (content, skipped) = fetch(app, &source).await?;
    let mut record = keep(app, scope, content, &source, activate)?;
    record["skipped"] = json!(skipped);
    Ok(record)
}

pub async fn dispatch(app: &Arc<App>, method: &str, params: Value) -> Result<Value, String> {
    match method {
        "skills.search" => search(app, params["query"].as_str().unwrap_or_default()).await,
        "skills.import" => {
            let scope: Scope = serde_json::from_value(params["scope"].clone()).map_err(|_| "Explicit playbook scope is required")?;
            let source = params["source"].as_str().ok_or("A skill address is required")?;
            import(app, &scope, source, params["activate"].as_bool().unwrap_or(false)).await
        }
        _ => Err("Unknown skills method".into()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn archive(files: &[(&str, &str)]) -> Vec<u8> {
        let mut writer = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
        let options = zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Stored);
        for (path, text) in files {
            writer.start_file(*path, options).unwrap();
            writer.write_all(text.as_bytes()).unwrap();
        }
        writer.finish().unwrap().into_inner()
    }

    fn github(owner: &str, repo: &str, git_ref: Option<&str>, path: Option<&str>, skill: Option<&str>) -> Source {
        Source::GitHub(GitHub {
            owner: owner.into(),
            repo: repo.into(),
            git_ref: git_ref.map(str::to_string),
            path: path.map(str::to_string),
            skill: skill.map(str::to_string),
        })
    }

    #[test]
    fn sources_are_read_from_links_and_short_forms() {
        let parse = |input: &str| Source::parse(input);
        let clawhub = |owner: Option<&str>, slug: &str| Source::ClawHub { owner: owner.map(str::to_string), slug: slug.into() };
        assert_eq!(parse("clawhub:gifgrep"), Ok(clawhub(None, "gifgrep")));
        assert_eq!(parse("clawhub:steipete/gifgrep"), Ok(clawhub(Some("steipete"), "gifgrep")));
        assert_eq!(parse("https://clawhub.ai/steipete/skills/gifgrep"), Ok(clawhub(Some("steipete"), "gifgrep")));
        assert_eq!(parse("https://clawhub.ai/skills/gifgrep"), Ok(clawhub(None, "gifgrep")));
        assert_eq!(parse("https://clawhub.ai/skills-sh/lobehub/lobehub/react"), Ok(github("lobehub", "lobehub", None, None, Some("react"))));
        assert_eq!(parse("https://skills.sh/vercel-labs/agent-skills/web-design-guidelines"), Ok(github("vercel-labs", "agent-skills", None, None, Some("web-design-guidelines"))));
        assert_eq!(parse("anthropics/skills@pdf"), Ok(github("anthropics", "skills", None, None, Some("pdf"))));
        assert_eq!(parse("github:anthropics/skills/skills/pdf"), Ok(github("anthropics", "skills", None, Some("skills/pdf"), None)));
        assert_eq!(
            parse("https://github.com/anthropics/skills/tree/main/skills/pdf"),
            Ok(github("anthropics", "skills", Some("main"), Some("skills/pdf"), None))
        );
        assert_eq!(
            parse("https://github.com/anthropics/skills/blob/main/skills/pdf/SKILL.md"),
            Ok(github("anthropics", "skills", Some("main"), Some("skills/pdf"), None))
        );
        assert_eq!(parse("https://github.com/owner/repo.git"), Ok(github("owner", "repo", None, None, None)));
        for bad in ["", "pdf", "https://example.com/a/b", "https://skills.sh/only", "clawhub:", "a b/c", "owner/repo@bad name"] {
            assert!(parse(bad).is_err(), "{bad:?} should be refused");
        }
    }

    #[test]
    fn frontmatter_values_are_plain_quoted_folded_or_literal() {
        let (fields, body) = parse_skill_md(
            "---\r\nname: pdf-tools\r\ndescription: >\r\n  Work with PDFs.\r\n  Use for forms.\r\nlicense: 'MIT'\r\nmetadata:\r\n  name: nested\r\n---\r\n\r\n# Body\r\nText\r\n",
        );
        assert_eq!(fields["name"], "pdf-tools");
        assert_eq!(fields["description"], "Work with PDFs. Use for forms.");
        assert_eq!(fields["license"], "MIT");
        assert!(body.contains("# Body"));

        let (fields, body) = parse_skill_md("---\nname: \"quoted \\\"x\\\"\"\ndescription: |\n  one\n  two\n---\nBody");
        assert_eq!(fields["name"], "quoted \"x\"");
        assert_eq!(fields["description"], "one\ntwo");
        assert_eq!(body, "Body");

        let (fields, body) = parse_skill_md("No frontmatter here");
        assert!(fields.is_empty());
        assert_eq!(body, "No frontmatter here");
    }

    #[test]
    fn a_github_archive_yields_the_named_skill_with_its_text_files() {
        let bytes = archive(&[
            ("repo-main/README.md", "readme"),
            ("repo-main/skills/pdf/SKILL.md", "---\nname: PDF Tools\ndescription: Fill and read PDFs\n---\nUse the scripts."),
            ("repo-main/skills/pdf/references/forms.md", "forms"),
            ("repo-main/skills/pdf/scripts/fill.py", "print('fill')"),
            ("repo-main/skills/pdf/scripts/browser-steps.json", "[]"),
            ("repo-main/skills/pdf/assets/logo.png", "png"),
            ("repo-main/skills/pdf/LICENSE.txt", "license"),
            ("repo-main/skills/docx/SKILL.md", "---\nname: docx\ndescription: Word\n---\nWord."),
        ]);
        let (folder, root) = read_folder(&bytes, true, None, Some("pdf")).unwrap();
        assert_eq!(root, "skills/pdf");
        let (content, skipped) = content_from(folder, "pdf").unwrap();
        assert_eq!(content.name, "pdf-tools");
        assert_eq!(content.description, "Fill and read PDFs");
        assert_eq!(content.instructions, "Use the scripts.");
        assert_eq!(content.references, vec![Resource { path: "references/forms.md".into(), text: "forms".into() }]);
        assert_eq!(content.scripts, vec![Resource { path: "scripts/fill.py".into(), text: "print('fill')".into() }]);
        assert_eq!(skipped, ["LICENSE.txt", "assets/logo.png", "scripts/browser-steps.json"]);
    }

    #[test]
    fn listed_skills_carry_the_reference_their_source_downloads_by() {
        // ClawHub downloads by slug and publisher: a slug alone is refused with a 409 when several
        // publishers use it.
        let clawhub = json!({"slug": "pdf", "displayName": "Pdf", "summary": "PDF tools", "ownerHandle": "awspace", "source": "clawhub", "version": null, "trust": {"installability": "installable"}});
        let row = listing(&clawhub).unwrap();
        assert_eq!(row["reference"], "clawhub:awspace/pdf");
        assert_eq!(row["url"], "https://clawhub.ai/awspace/skills/pdf");
        assert_eq!(Source::parse("clawhub:awspace/pdf"), Ok(Source::ClawHub { owner: Some("awspace".into()), slug: "pdf".into() }));

        // A skill ClawHub indexes from skills.sh is a folder in a GitHub repository.
        let indexed = json!({"slug": "react", "displayName": "react", "ownerHandle": "lobehub", "source": "skills-sh",
            "sourceIdentity": {"owner": "lobehub", "repo": "lobehub"}, "links": {"source": "https://www.skills.sh/lobehub/lobehub/react"}, "trust": {"installability": "installable"}});
        let row = listing(&indexed).unwrap();
        assert_eq!(row["reference"], "lobehub/lobehub@react");
        assert_eq!(row["url"], "https://www.skills.sh/lobehub/lobehub/react");
        assert_eq!(Source::parse("lobehub/lobehub@react"), Ok(github("lobehub", "lobehub", None, None, Some("react"))));

        // The list of most downloaded skills has no source field, and the version is in latestVersion.
        let listed = json!({"slug": "self-improving-agent", "ownerHandle": "pskoett", "displayName": "self-improving agent", "latestVersion": {"version": "4.0.3"}});
        let row = listing(&listed).unwrap();
        assert_eq!(row["reference"], "clawhub:pskoett/self-improving-agent");
        assert_eq!(row["version"], "4.0.3");

        assert!(listing(&json!({"slug": "x", "ownerHandle": "o", "source": "clawhub", "trust": {"installability": "blocked"}})).is_none());
        assert!(listing(&json!({"slug": "x", "ownerHandle": "o", "source": "somewhere-else"})).is_none());
        assert!(listing(&json!({"slug": "../x", "ownerHandle": "o", "source": "clawhub"})).is_none());
    }

    #[test]
    fn an_import_is_a_draft_or_a_skill_the_bot_uses_at_once() {
        let home = std::env::temp_dir().join(format!("lorca-skill-hub-{}", uuid::Uuid::new_v4()));
        let app = App::load(crate::config::Config { home: home.clone(), port: 0 }).unwrap();
        crate::identity::create(&app, Some("Skill hub test".into())).unwrap();
        let bot = app.state.lock().unwrap().bots[0].id.clone();
        let scope = Scope::bot(&bot);
        let source = Source::parse("clawhub:pdf-tools").unwrap();
        let content = |name: &str| PlaybookContent {
            name: name.into(),
            description: "Fill PDFs".into(),
            instructions: "Use the scripts.".into(),
            ..Default::default()
        };

        let draft = keep(&app, &scope, content("pdf-tools"), &source, false).unwrap();
        assert_eq!(draft["status"], "draft");
        assert_eq!(draft["provenance"]["kind"], "import");
        assert_eq!(draft["provenance"]["note"], "Imported from ClawHub pdf-tools");

        let saved = keep(&app, &scope, content("pdf-tools-2"), &source, true).unwrap();
        assert_eq!(saved["status"], "saved");
        assert_eq!(saved["revision"], 1);
        let catalog = playbooks::catalog(&app, &[scope.clone()], "", playbooks::PROMPT_BYTES);
        assert!(catalog.to_string().contains("pdf-tools-2"), "{catalog}");
        assert!(!catalog.to_string().contains("\"pdf-tools\""), "a draft stays out of the catalog: {catalog}");

        let again = keep(&app, &scope, content("pdf-tools-2"), &source, true).unwrap_err();
        assert!(again.contains("already exists"), "{again}");
        let _ = std::fs::remove_dir_all(home);
    }

    #[test]
    fn a_repository_of_several_skills_asks_which() {
        let bytes = archive(&[
            ("repo-main/a/SKILL.md", "---\nname: a\ndescription: A\n---\nA"),
            ("repo-main/b/SKILL.md", "---\nname: b\ndescription: B\n---\nB"),
        ]);
        let error = read_folder(&bytes, true, None, None).err().unwrap();
        assert!(error.contains("2 skills") && error.contains("owner/repo@a"), "{error}");
        assert!(read_folder(&bytes, true, Some("a"), None).is_ok());
        assert!(read_folder(&bytes, true, Some("c"), None).is_err());
        assert!(read_folder(&bytes, true, None, Some("c")).is_err());
    }

    #[test]
    fn a_clawhub_archive_holds_the_skill_at_its_root() {
        let bytes = archive(&[("SKILL.md", "# Gif search\n\nFind GIFs."), ("references/api.md", "api")]);
        let (folder, root) = read_folder(&bytes, false, None, None).unwrap();
        assert_eq!(root, "");
        let (content, skipped) = content_from(folder, "gifgrep").unwrap();
        assert_eq!(content.name, "gifgrep");
        assert_eq!(content.description, "Gif search");
        assert!(skipped.is_empty());
    }

    #[test]
    fn resources_past_the_playbook_limits_are_left_out_not_failed() {
        let many: Vec<(String, String)> = (0..20).map(|i| (format!("references/r{i:02}.md"), "x".into())).collect();
        let mut files = vec![("SKILL.md".to_string(), "---\nname: big\ndescription: Big\n---\nBody".to_string())];
        files.extend(many);
        let folder = Folder { files: files.into_iter().map(|(p, t)| (p, t.into_bytes())).collect(), skipped: Vec::new() };
        let (content, skipped) = content_from(folder, "big").unwrap();
        assert_eq!(content.references.len(), MAX_RESOURCES);
        assert_eq!(skipped.len(), 4);
    }
}
