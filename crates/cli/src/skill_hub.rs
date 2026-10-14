//! Skills from public communities. A skill is a folder with a `SKILL.md` (agentskills.io); ClawHub
//! serves it as an archive, and from GitHub the folder's own files are read, or the repository's
//! archive when GitHub does not list its tree. An import copies the whole folder, binaries and
//! scripts included, into the scope's skills folder, and the bots use it from their next turn. A
//! note in the folder says where it came from.

use std::io::Read;

use std::sync::Arc;
use std::time::Duration;

use reqwest::header::USER_AGENT;
use serde_json::{json, Value};

use crate::app::App;
use crate::skills::{self, parse_skill_md, Scope};

const CLAWHUB_URL: &str = "https://clawhub.ai";
const GITHUB_ARCHIVE_URL: &str = "https://codeload.github.com/{owner}/{repo}/zip/{ref}";
const GITHUB_API_URL: &str = "https://api.github.com";
const GITHUB_RAW_URL: &str = "https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}";
const TIMEOUT: Duration = Duration::from_secs(30);
/// The most a download may be, so a wrong link cannot fill memory. A repository's archive, read
/// only when GitHub does not list its tree, holds every skill in it, so it may be this large.
const MAX_ARCHIVE_BYTES: usize = 128 * 1024 * 1024;
/// The most files one skill's folder may hold.
const MAX_FOLDER_FILES: usize = 500;
const SKILLS_SH_URL: &str = "https://skills.sh";
/// As many as skills.sh's own search page asks for, which also ranks the results differently for
/// a smaller limit.
const SEARCH_LIMIT: usize = 100;
/// skills.sh refuses a shorter query.
const MIN_QUERY: usize = 2;

/// ClawHub's address, or a mirror that `LORCA_CLAWHUB_URL` names.
fn clawhub_url() -> String {
    std::env::var("LORCA_CLAWHUB_URL").unwrap_or_else(|_| CLAWHUB_URL.into()).trim_end_matches('/').to_string()
}

/// skills.sh's address, where the store searches, or a mirror that `LORCA_SKILLS_SH_URL` names.
fn skills_sh_url() -> String {
    std::env::var("LORCA_SKILLS_SH_URL").unwrap_or_else(|_| SKILLS_SH_URL.into()).trim_end_matches('/').to_string()
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
    get_from(&app.http, url, query).await
}

async fn get_from(client: &reqwest::Client, url: &str, query: &[(&str, &str)]) -> Result<reqwest::Response, String> {
    let response = client
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
    let response = get(app, url, query).await?;
    read_body(response).await
}

async fn download_from(client: &reqwest::Client, url: &str) -> Result<Vec<u8>, String> {
    read_body(get_from(client, url, &[]).await?).await
}

async fn read_body(mut response: reqwest::Response) -> Result<Vec<u8>, String> {
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

/// Searches skills.sh. It answers a query of two characters or more; a shorter one lists nothing.
pub async fn search(app: &App, query: &str) -> Result<Value, String> {
    let query = query.trim();
    if query.chars().count() < MIN_QUERY {
        return Ok(json!({ "results": [] }));
    }
    let limit = SEARCH_LIMIT.to_string();
    let text = get(app, &format!("{}/api/search", skills_sh_url()), &[("q", query), ("limit", &limit)]).await?.text().await.map_err(|e| e.to_string())?;
    let body: Value = serde_json::from_str(&text).map_err(|_| "skills.sh answered with something other than JSON".to_string())?;
    if body["error"].as_str().is_some_and(|error| !error.is_empty()) {
        return Err("skills.sh is busy; try the search again in a moment".into());
    }
    let rows = body["skills"].as_array().cloned().unwrap_or_default();
    let results: Vec<Value> = rows.iter().filter_map(listing).collect();
    Ok(json!({ "results": results }))
}

/// A row of skills.sh's search as the store shows it, with the reference `skills.import` takes:
/// a skill in a GitHub repository, `owner/repo@skill`. One from another kind of source, which is
/// not a repository, is left out.
fn listing(row: &Value) -> Option<Value> {
    let skill = row["skillId"].as_str().filter(|s| plain_name(s))?;
    let (owner, repo) = row["source"].as_str()?.split_once('/')?;
    if !plain_name(owner) || !plain_name(repo) {
        return None;
    }
    Some(json!({
        "source": "skills-sh",
        "reference": format!("{owner}/{repo}@{skill}"),
        "name": row["name"].as_str().filter(|n| !n.is_empty()).unwrap_or(skill),
        "summary": "",
        "owner": format!("{owner}/{repo}"),
        "installs": row["installs"].as_u64().unwrap_or(0),
        // skills.sh marks a copy of another repository's skill, which its page dims.
        "duplicate": row["isDuplicate"].as_bool().unwrap_or(false),
        "url": format!("https://skills.sh/{owner}/{repo}/{skill}"),
    }))
}

/// A file of a skill's folder: its path inside it, its bytes, and its Unix permissions.
struct Item {
    path: String,
    bytes: Vec<u8>,
    mode: Option<u32>,
}

/// The files of a skill's folder with their paths inside it.
struct Folder {
    files: Vec<Item>,
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
    let mut folder = Folder { files: Vec::new() };
    for (index, name) in &entries {
        let Some(relative) = name.strip_prefix(&prefix) else { continue };
        let mut entry = archive.by_index(*index).map_err(|e| e.to_string())?;
        let mode = entry.unix_mode();
        let mut bytes = Vec::new();
        entry.read_to_end(&mut bytes).map_err(|e| e.to_string())?;
        folder.files.push(Item { path: relative.to_string(), bytes, mode });
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

/// The name a folder's skill goes by: its frontmatter's, else the folder's, as a slug.
fn name_of(folder: &Folder, fallback_name: &str) -> Result<String, String> {
    let skill = folder.files.iter().find(|item| item.path == skills::SKILL_FILE).ok_or("That folder has no SKILL.md")?;
    let text = String::from_utf8(skill.bytes.clone()).map_err(|_| "SKILL.md is not UTF-8 text".to_string())?;
    let (fields, body) = parse_skill_md(&text);
    let description = fields.get("description").map(|d| d.trim().is_empty()).unwrap_or(true);
    if body.trim().is_empty() || (description && !body.lines().any(|line| !line.trim().is_empty())) {
        return Err("The skill has no description or instructions".into());
    }
    let mut name = skills::slugify(fields.get("name").map(String::as_str).unwrap_or_default());
    if name.is_empty() {
        name = skills::slugify(fallback_name);
    }
    skills::check_name(&name).map_err(|_| "The skill has no usable name".to_string())?;
    Ok(name)
}

/// The skill's folder from GitHub: the files of that folder alone when GitHub lists the
/// repository's tree, else the whole repository's archive.
async fn fetch_github(app: &App, github: &GitHub) -> Result<(Folder, String), String> {
    let (folder, root) = match fetch_github_folder(app, github).await {
        Ok(found) => found,
        // A wrong path or skill name is the answer, not a reason to download everything.
        Err(Fetch::Answer(error)) => return Err(error),
        Err(Fetch::Unlisted(error)) => {
            tracing::debug!(%error, "reading a skill from its repository's archive instead");
            let bytes = download(app, &github_archive_url(github), &[]).await?;
            read_folder(&bytes, true, github.path.as_deref(), github.skill.as_deref())?
        }
    };
    let fallback = if root.is_empty() { github.repo.clone() } else { root.rsplit('/').next().unwrap_or_default().to_string() };
    Ok((folder, fallback))
}

enum Fetch {
    /// The repository has no such skill, or it is too large: what the user is told.
    Answer(String),
    /// GitHub did not list the tree (a limit, a mirror without the API): read the archive.
    Unlisted(String),
}

/// GitHub's API, or a mirror that `LORCA_GITHUB_API_URL` names.
fn github_api_url() -> String {
    std::env::var("LORCA_GITHUB_API_URL").unwrap_or_else(|_| GITHUB_API_URL.into()).trim_end_matches('/').to_string()
}

/// A file's bytes on GitHub, or a mirror: `LORCA_GITHUB_RAW_URL` with `{owner}`, `{repo}`, `{ref}`,
/// and `{path}` in it.
fn github_raw_url(github: &GitHub, path: &str) -> String {
    let path: Vec<String> = path.split('/').map(|part| url_part(part)).collect();
    std::env::var("LORCA_GITHUB_RAW_URL")
        .unwrap_or_else(|_| GITHUB_RAW_URL.into())
        .replace("{owner}", &github.owner)
        .replace("{repo}", &github.repo)
        .replace("{ref}", github.git_ref.as_deref().unwrap_or("HEAD"))
        .replace("{path}", &path.join("/"))
}

/// A path segment as a URL holds it.
fn url_part(part: &str) -> String {
    part.bytes()
        .map(|b| if b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.' | b'~') { (b as char).to_string() } else { format!("%{b:02X}") })
        .collect()
}

/// Lists the repository's tree once, picks the skill's folder as the archive would, and downloads
/// that folder's files, eight at a time. Links and submodules are left out.
async fn fetch_github_folder(app: &App, github: &GitHub) -> Result<(Folder, String), Fetch> {
    let git_ref = github.git_ref.as_deref().unwrap_or("HEAD");
    let url = format!("{}/repos/{}/{}/git/trees/{}", github_api_url(), github.owner, github.repo, url_part(git_ref));
    let mut request = app.http.get(&url).query(&[("recursive", "1")]).timeout(TIMEOUT).header(USER_AGENT, concat!("lorca/", env!("CARGO_PKG_VERSION"))).header("Accept", "application/vnd.github+json");
    // A token in the environment lifts GitHub's limit of 60 listings an hour; it goes to GitHub's
    // API alone.
    if let Some(token) = ["GITHUB_TOKEN", "GH_TOKEN"].iter().find_map(|name| std::env::var(name).ok()).filter(|token| !token.trim().is_empty()) {
        if url.starts_with(GITHUB_API_URL) {
            request = request.bearer_auth(token.trim());
        }
    }
    let response = request.send().await.map_err(|e| Fetch::Unlisted(e.to_string()))?;
    match response.status() {
        status if status.is_success() => {}
        reqwest::StatusCode::NOT_FOUND => return Err(Fetch::Answer("That repository was not found, or it is private".into())),
        status => return Err(Fetch::Unlisted(format!("GitHub answered {status}"))),
    }
    let tree: Value = response.json().await.map_err(|e| Fetch::Unlisted(e.to_string()))?;
    if tree["truncated"].as_bool().unwrap_or(false) {
        return Err(Fetch::Unlisted("GitHub listed only part of the tree".into()));
    }
    let blobs: Vec<(String, Option<u32>, u64)> = tree["tree"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|entry| entry["type"] == "blob")
        .filter_map(|entry| {
            let path = entry["path"].as_str()?.to_string();
            let mode = entry["mode"].as_str().and_then(|mode| u32::from_str_radix(mode, 8).ok());
            // 120000 is a link, which a skill's folder never follows.
            if mode == Some(0o120000) || path.split('/').any(|part| part.is_empty() || part == "..") {
                return None;
            }
            Some((path, mode.map(|mode| mode & 0o777), entry["size"].as_u64().unwrap_or(0)))
        })
        .collect();
    let names: Vec<&str> = blobs.iter().map(|(path, _, _)| path.as_str()).collect();
    let root = choose_root(&names, github.path.as_deref(), github.skill.as_deref()).map_err(Fetch::Answer)?;
    let prefix = if root.is_empty() { String::new() } else { format!("{root}/") };
    let wanted: Vec<&(String, Option<u32>, u64)> = blobs.iter().filter(|(path, _, _)| path.starts_with(&prefix)).collect();
    if wanted.len() > MAX_FOLDER_FILES || wanted.iter().map(|(_, _, size)| *size as usize).sum::<usize>() > MAX_ARCHIVE_BYTES {
        return Err(Fetch::Answer("That skill is too large to import".into()));
    }
    // Each download owns what it needs, so the tasks can run on any worker.
    let gate = Arc::new(tokio::sync::Semaphore::new(8));
    let mut tasks = tokio::task::JoinSet::new();
    for (path, mode, _) in wanted {
        let (client, url, gate) = (app.http.clone(), github_raw_url(github, path), gate.clone());
        let (rel, mode) = (path[prefix.len()..].to_string(), *mode);
        tasks.spawn(async move {
            let _permit = gate.acquire_owned().await.map_err(|e| e.to_string())?;
            let bytes = download_from(&client, &url).await?;
            Ok::<_, String>(Item { path: rel, bytes, mode })
        });
    }
    let mut files = Vec::new();
    while let Some(done) = tasks.join_next().await {
        files.push(done.map_err(|e| Fetch::Answer(e.to_string()))?.map_err(Fetch::Answer)?);
    }
    files.sort_by(|a, b| a.path.cmp(&b.path));
    Ok((Folder { files }, root))
}

async fn fetch(app: &App, source: &Source) -> Result<(Folder, String), String> {
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
                return Ok((folder, slug.clone()));
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

/// Copies a skill's folder into `scope`'s skills folder, built beside it and renamed into place, so
/// no bot reads half of it. A name the scope already uses refuses the import. The files that did
/// not come across (a recording only the user makes) are named in the answer.
/// `reference` is the address the import was given, which the store matches its rows against.
fn install(app: &App, scope: &Scope, folder: &Folder, name: &str, source: &Source, reference: &str) -> Result<(Value, Vec<String>), String> {
    let (root, target) = skills::folder(app, scope, name)?;
    if scope.kind == "shared" && skills::BUILTIN_NAMES.contains(&name) {
        return Err("That name belongs to a skill Lorca ships".into());
    }
    if target.exists() {
        return Err("A skill with this name already exists in this scope".into());
    }
    std::fs::create_dir_all(&root).map_err(|e| e.to_string())?;
    let staging = root.join(format!(".tmp-{}", uuid::Uuid::new_v4()));
    let mut skipped = Vec::new();
    let written = (|| -> Result<(), String> {
        for item in &folder.files {
            // Lorca itself runs recorded browser steps, so only the user records them.
            if item.path == crate::browser::steps::PATH || (item.path != skills::SKILL_FILE && !skills::safe_relative(&item.path)) {
                skipped.push(item.path.clone());
                continue;
            }
            let dest = staging.join(&item.path);
            if let Some(parent) = dest.parent() {
                std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
            }
            std::fs::write(&dest, &item.bytes).map_err(|e| e.to_string())?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                if let Some(mode) = item.mode {
                    let _ = std::fs::set_permissions(&dest, std::fs::Permissions::from_mode(mode & 0o777));
                }
            }
        }
        let note = json!({ "source": source.label(), "reference": reference, "imported_at": crate::config::now_secs() });
        std::fs::write(staging.join(skills::SOURCE_FILE), note.to_string()).map_err(|e| e.to_string())?;
        std::fs::rename(&staging, &target).map_err(|e| e.to_string())
    })();
    if let Err(error) = written {
        let _ = std::fs::remove_dir_all(&staging);
        return Err(error);
    }
    skills::changed(app, scope);
    skipped.sort();
    Ok((skills::get(app, scope, name)?, skipped))
}

/// Fetches the skill `source` names into `scope`; `skipped` lists the files that did not come across.
pub async fn import(app: &Arc<App>, scope: &Scope, source: &str) -> Result<Value, String> {
    scope.validate(app)?;
    let parsed = Source::parse(source)?;
    let (folder, fallback) = fetch(app, &parsed).await?;
    let name = name_of(&folder, &fallback)?;
    let (mut view, skipped) = install(app, scope, &folder, &name, &parsed, source.trim())?;
    view["skipped"] = json!(skipped);
    Ok(view)
}

pub async fn dispatch(app: &Arc<App>, method: &str, params: Value) -> Result<Value, String> {
    match method {
        "skills.search" => search(app, params["query"].as_str().unwrap_or_default()).await,
        "skills.import" => {
            let scope: Scope = serde_json::from_value(params["scope"].clone()).map_err(|_| "An explicit skill scope is required")?;
            let source = params["source"].as_str().ok_or("A skill address is required")?;
            import(app, &scope, source).await
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
    fn a_skills_sh_row_carries_the_reference_its_import_downloads_by() {
        // skills.sh's search names a skill by its repository and its folder.
        let row = listing(&json!({"id": "anthropics/skills/pdf", "source": "anthropics/skills", "skillId": "pdf", "name": "pdf", "installs": 210224})).unwrap();
        assert_eq!(row["reference"], "anthropics/skills@pdf");
        assert_eq!(row["duplicate"], false);
        assert_eq!(listing(&json!({"source": "a/b", "skillId": "pdf", "name": "pdf", "isDuplicate": true})).unwrap()["duplicate"], true);
        assert_eq!(row["owner"], "anthropics/skills");
        assert_eq!(row["installs"], 210224);
        assert_eq!(row["url"], "https://skills.sh/anthropics/skills/pdf");
        assert_eq!(Source::parse("anthropics/skills@pdf"), Ok(github("anthropics", "skills", None, None, Some("pdf"))));

        // A skill a website publishes itself is not a repository the import reads.
        assert!(listing(&json!({"id": "mintlify.com/mintlify", "source": "mintlify.com", "skillId": "mintlify", "name": "mintlify"})).is_none());
        assert!(listing(&json!({"source": "a/b", "skillId": "../x", "name": "x"})).is_none());
        assert!(listing(&json!({"source": "a b/c", "skillId": "x", "name": "x"})).is_none());
    }

    #[test]
    fn an_import_copies_the_whole_folder_and_the_bots_use_it_at_once() {
        let home = std::env::temp_dir().join(format!("lorca-skill-hub-{}", uuid::Uuid::new_v4()));
        let app = App::load(crate::config::Config { home: home.clone(), port: 0 }).unwrap();
        crate::identity::create(&app, Some("Skill hub test".into())).unwrap();
        let bot = app.state.lock().unwrap().bots[0].id.clone();
        let scope = Scope::bot(&bot);
        let source = Source::parse("clawhub:pdf-tools").unwrap();
        let bytes = archive(&[
            ("repo-main/skills/pdf/SKILL.md", "---\nname: PDF Tools\ndescription: Fill and read PDFs\nlicense: MIT\n---\nUse the scripts."),
            ("repo-main/skills/pdf/references/forms.md", "forms"),
            ("repo-main/skills/pdf/scripts/fill.py", "print('fill')"),
            ("repo-main/skills/pdf/scripts/browser-steps.json", "[]"),
            ("repo-main/skills/pdf/assets/logo.png", "png"),
            ("repo-main/skills/pdf/LICENSE.txt", "license"),
            ("repo-main/skills/docx/SKILL.md", "---\nname: docx\ndescription: Word\n---\nWord."),
        ]);
        let (folder, root) = read_folder(&bytes, true, None, Some("pdf")).unwrap();
        assert_eq!(root, "skills/pdf");
        let name = name_of(&folder, "pdf").unwrap();
        assert_eq!(name, "pdf-tools");

        let (view, skipped) = install(&app, &scope, &folder, &name, &source, "clawhub:pdf-tools").unwrap();
        assert_eq!(view["description"], "Fill and read PDFs");
        assert_eq!(view["source"], "clawhub:pdf-tools", "the list says which address a skill came from");
        assert_eq!(view["instructions"], "Use the scripts.");
        assert_eq!(skipped, ["scripts/browser-steps.json"], "only the user's own recording is left out");
        let dir = home.join("workspaces").join(&bot).join("skills/pdf-tools");
        for file in ["references/forms.md", "scripts/fill.py", "assets/logo.png", "LICENSE.txt", skills::SOURCE_FILE] {
            assert!(dir.join(file).is_file(), "{file}");
        }
        assert!(std::fs::read_to_string(dir.join(skills::SOURCE_FILE)).unwrap().contains("ClawHub pdf-tools"));
        assert!(skills::find(&app, &skills::scopes_for_turn(&app, &bot, ""), "pdf-tools").is_some());

        let again = install(&app, &scope, &folder, &name, &source, "clawhub:pdf-tools").unwrap_err();
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
        assert_eq!(name_of(&folder, "gifgrep").unwrap(), "gifgrep");
        assert_eq!(folder.files.len(), 2);
        assert!(name_of(&Folder { files: Vec::new() }, "x").is_err(), "a folder without a SKILL.md is not a skill");
    }
}
