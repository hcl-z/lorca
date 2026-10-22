//! The Runner's `agents.json`: the coding agents a bot can start, each with how it starts over
//! ACP (`{ "agents": { "claude": { … } } }`), beside `mcp.json` in Lorca's folder. The file is
//! the truth: Lorca knows no agent it does not list. When there is no file, `lorca serve` (or the
//! first `lorca agent`) writes one with the agents installed here, from [`TEMPLATES`]; a file that
//! exists is never written over, so an agent the user removed stays removed. The Runner reads it
//! again on `agents.reload`, and `lorca agent` edits it, keeping the fields Lorca does not write.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use serde_json::{json, Map, Value};

use super::acp::{self, Adapter, Spec};
use super::process;
use crate::app::App;

/// What `agents.json` starts as for an agent whose program is installed here: the one place
/// Lorca knows an agent by name, read once, when the file is first written.
struct Template {
    /// The program found on the login shell's `PATH`.
    program: &'static str,
    kind: &'static str,
    name: &'static str,
    bins: &'static [&'static str],
    args: &'static [&'static str],
    /// `(package, binary_env)` of the `npx` adapter that wraps a CLI without an ACP mode.
    adapter: Option<(&'static str, &'static str)>,
    safe_mode: Option<&'static str>,
}

const TEMPLATES: &[Template] = &[
    Template {
        program: "claude",
        kind: "claude",
        name: "Claude Code",
        bins: &["claude"],
        args: &[],
        adapter: Some(("@agentclientprotocol/claude-agent-acp@0.94.0", "CLAUDE_CODE_EXECUTABLE")),
        safe_mode: Some("default"),
    },
    Template { program: "codex", kind: "codex", name: "Codex", bins: &["codex"], args: &[], adapter: Some(("@agentclientprotocol/codex-acp@2.2.2", "CODEX_PATH")), safe_mode: Some("workspace-write") },
    Template { program: "gemini", kind: "gemini", name: "Gemini CLI", bins: &["gemini"], args: &["--acp"], adapter: None, safe_mode: None },
    Template { program: "opencode", kind: "opencode", name: "OpenCode", bins: &["opencode"], args: &["acp"], adapter: None, safe_mode: None },
    Template { program: "kimi", kind: "kimi", name: "Kimi Code", bins: &["kimi"], args: &["acp"], adapter: None, safe_mode: None },
    Template { program: "copilot", kind: "copilot", name: "GitHub Copilot", bins: &["copilot"], args: &["--acp", "--stdio"], adapter: None, safe_mode: None },
    Template { program: "cursor-agent", kind: "cursor", name: "Cursor", bins: &["cursor-agent", "agent"], args: &["acp"], adapter: None, safe_mode: None },
    Template { program: "kiro-cli", kind: "kiro", name: "Kiro", bins: &["kiro-cli"], args: &["acp"], adapter: None, safe_mode: None },
    // pi has no ACP mode of its own; its adapter runs `pi --mode rpc`. pi asks before nothing, so
    // none of its commands or edits reaches Lorca to decide.
    Template { program: "pi", kind: "pi", name: "pi", bins: &["pi"], args: &[], adapter: Some(("pi-acp@0.0.34", "PI_ACP_PI_COMMAND")), safe_mode: None },
    Template { program: "dsh", kind: "dsh", name: "dsh", bins: &["dsh"], args: &["--profile", "acp"], adapter: None, safe_mode: None },
];

impl Template {
    /// The entry the file gets for it.
    fn entry(&self) -> Value {
        json!({
            "name": self.name,
            "bins": self.bins,
            "args": self.args,
            "adapter": self.adapter.map(|(package, binary_env)| json!({ "package": package, "binary_env": binary_env, "env": {} })),
            "safe_mode": self.safe_mode,
        })
    }
}

/// The spec a template's entry reads as, for tests that drive an agent without a file.
#[cfg(test)]
pub(crate) fn template_spec(kind: &str) -> Arc<Spec> {
    let template = TEMPLATES.iter().find(|template| template.kind == kind).unwrap();
    Arc::new(read_entry(kind, &template.entry()).unwrap())
}

#[cfg(test)]
pub(crate) fn template_specs() -> Vec<Arc<Spec>> {
    TEMPLATES.iter().map(|template| Arc::new(read_entry(template.kind, &template.entry()).unwrap())).collect()
}

/// Writes every template into the file, for tests that start agents by name.
#[cfg(test)]
pub(crate) fn write_templates(app: &App) {
    let agents = Map::from_iter(TEMPLATES.iter().map(|template| (template.kind.to_string(), template.entry())));
    write(app, &Map::from_iter([("agents".to_string(), Value::Object(agents))])).unwrap();
}

/// The keys an entry may have.
const KEYS: [&str; 5] = ["name", "bins", "args", "adapter", "safe_mode"];
const ADAPTER_KEYS: [&str; 3] = ["package", "binary_env", "env"];

/// One entry of the file, usable or not.
#[derive(Debug, Clone)]
pub struct Entry {
    pub kind: String,
    /// What the file says, as it says it.
    pub raw: Value,
    /// The agent it describes, or why it cannot start.
    pub spec: Result<Arc<Spec>, String>,
}

/// `agents.json` as the Runner last read it.
#[derive(Debug, Clone, Default)]
pub struct AgentsFile {
    /// The file's object, with its keys in the file's order.
    pub doc: Map<String, Value>,
    pub entries: Vec<Entry>,
    /// Why the file does not read; it lists no agent then.
    pub error: Option<String>,
    /// There is no file yet.
    pub missing: bool,
}

impl AgentsFile {
    pub fn read(path: &Path) -> AgentsFile {
        match std::fs::read(path) {
            Ok(bytes) => AgentsFile::parse(&bytes),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => AgentsFile { missing: true, ..AgentsFile::default() },
            Err(error) => AgentsFile { error: Some(format!("Cannot read agents.json: {error}.")), ..AgentsFile::default() },
        }
    }

    pub fn parse(bytes: &[u8]) -> AgentsFile {
        let failed = |error: String| AgentsFile { error: Some(error), ..AgentsFile::default() };
        let text = String::from_utf8_lossy(bytes);
        if text.trim().is_empty() {
            return failed("agents.json is empty. It is {\"agents\": {…}}.".into());
        }
        let doc = match serde_json::from_str::<Value>(&text) {
            Ok(Value::Object(doc)) => doc,
            Ok(_) => return failed("agents.json is not an object. It is {\"agents\": {…}}.".into()),
            Err(error) => return failed(format!("agents.json is not valid JSON: {error}.")),
        };
        let entries = match doc.get("agents") {
            Some(Value::Object(agents)) => agents.iter().map(|(kind, raw)| Entry { kind: kind.clone(), raw: raw.clone(), spec: read_entry(kind, raw).map(Arc::new) }).collect(),
            Some(_) => return failed("agents.json's \"agents\" is not an object of agents by name.".into()),
            None => return failed("agents.json has no \"agents\" object.".into()),
        };
        AgentsFile { doc, entries, error: None, missing: false }
    }

    pub fn entry(&self, kind: &str) -> Option<&Entry> {
        self.entries.iter().find(|entry| entry.kind == kind)
    }

    /// The agents that can start, in the file's order.
    pub fn specs(&self) -> impl Iterator<Item = &Arc<Spec>> {
        self.entries.iter().filter_map(|entry| entry.spec.as_ref().ok())
    }

    pub fn spec(&self, kind: &str) -> Option<Arc<Spec>> {
        self.entry(kind).and_then(|entry| entry.spec.as_ref().ok()).cloned()
    }

    /// The file with `kind` saved as `entry`, in its own place or at the end.
    fn with_entry(&self, kind: &str, entry: Value) -> Map<String, Value> {
        let mut doc = self.doc.clone();
        let agents = doc.entry("agents").or_insert_with(|| Value::Object(Map::new()));
        if let Value::Object(agents) = agents {
            agents.insert(kind.to_string(), entry);
        }
        doc
    }

    fn without_entry(&self, kind: &str) -> Map<String, Value> {
        let mut doc = self.doc.clone();
        if let Some(Value::Object(agents)) = doc.get_mut("agents") {
            agents.shift_remove(kind);
        }
        doc
    }
}

/// Why `kind` cannot name an agent.
fn kind_problem(kind: &str) -> Option<String> {
    if kind.is_empty() {
        return Some("An agent's name is not empty.".into());
    }
    if kind.chars().any(|c| c.is_whitespace() || c.is_control()) {
        return Some(format!("An agent's name is one word, not {kind:?}."));
    }
    None
}

fn unknown_keys(object: &Map<String, Value>, known: &[&str], what: &str) -> Result<(), String> {
    match object.keys().find(|key| !known.contains(&key.as_str())) {
        Some(key) => Err(format!("{what} has no field {key:?}. It takes {}.", known.join(", "))),
        None => Ok(()),
    }
}

fn strings(value: Option<&Value>, what: &str) -> Result<Vec<String>, String> {
    match value {
        None | Some(Value::Null) => Ok(Vec::new()),
        Some(Value::Array(items)) => items.iter().map(|item| item.as_str().map(str::to_string).ok_or_else(|| format!("{what} is a list of strings, and has {item}."))).collect(),
        Some(other) => Err(format!("{what} is a list of strings, not {other}.")),
    }
}

/// The agent an entry describes, or what is wrong with it, by field.
fn read_entry(kind: &str, raw: &Value) -> Result<Spec, String> {
    if let Some(problem) = kind_problem(kind) {
        return Err(problem);
    }
    let Value::Object(entry) = raw else { return Err(format!("{kind} is an object with name, bins, args, adapter, and safe_mode, not {raw}.")) };
    unknown_keys(entry, &KEYS, kind)?;
    let name = match entry.get("name") {
        Some(Value::String(name)) if !name.trim().is_empty() => name.trim().to_string(),
        Some(Value::String(_)) => return Err(format!("{kind}'s name is empty.")),
        Some(other) => return Err(format!("{kind}'s name is a string, not {other}.")),
        None => return Err(format!("{kind} has no name: what Lorca calls it, such as \"Claude Code\".")),
    };
    if !entry.contains_key("bins") {
        return Err(format!("{kind} has no bins: the programs it may be installed as, such as [\"{kind}\"]."));
    }
    let bins = strings(entry.get("bins"), &format!("{kind}'s bins"))?;
    if bins.is_empty() || bins.iter().any(|bin| bin.trim().is_empty()) {
        return Err(format!("{kind}'s bins names at least one program, and none empty."));
    }
    let args = strings(entry.get("args"), &format!("{kind}'s args"))?;
    let adapter = match entry.get("adapter") {
        None | Some(Value::Null) => None,
        Some(Value::Object(adapter)) => {
            let what = format!("{kind}'s adapter");
            unknown_keys(adapter, &ADAPTER_KEYS, &what)?;
            let field = |key: &str, about: &str| match adapter.get(key) {
                Some(Value::String(value)) if !value.trim().is_empty() => Ok(value.trim().to_string()),
                Some(Value::String(_)) | None => Err(format!("{what} has no {key}: {about}.")),
                Some(other) => Err(format!("{what}'s {key} is a string, not {other}.")),
            };
            let package = field("package", "the npm package npx runs, pinned to a version, such as \"pi-acp@0.0.34\"")?;
            let binary_env = field("binary_env", "the variable that tells the adapter where the CLI is, such as \"PI_ACP_PI_COMMAND\"")?;
            let env = match adapter.get("env") {
                None | Some(Value::Null) => Vec::new(),
                Some(Value::Object(env)) => env
                    .iter()
                    .map(|(name, value)| match value {
                        Value::String(value) => Ok((name.clone(), value.clone())),
                        other => Err(format!("{what}'s env gives {name} as a string, not {other}.")),
                    })
                    .collect::<Result<_, _>>()?,
                Some(other) => Err(format!("{what}'s env is an object of variables, not {other}."))?,
            };
            Some(Adapter { package, binary_env, env })
        }
        Some(other) => return Err(format!("{kind}'s adapter is an object with package, binary_env, and env, or null; not {other}.")),
    };
    let safe_mode = match entry.get("safe_mode") {
        None | Some(Value::Null) => None,
        Some(Value::String(mode)) if !mode.trim().is_empty() => Some(mode.trim().to_string()),
        Some(Value::String(_)) => return Err(format!("{kind}'s safe_mode is empty; leave it null for none.")),
        Some(other) => return Err(format!("{kind}'s safe_mode is a string or null, not {other}.")),
    };
    Ok(Spec { kind: kind.to_string(), name, bins, args, adapter, safe_mode })
}

/// The entry `lorca agent add` writes from its flags.
pub fn entry_of(name: Option<&str>, kind: &str, bins: &[String], args: &[String], package: Option<&str>, binary_env: Option<&str>, env: &[(String, String)], safe_mode: Option<&str>) -> Result<Value, String> {
    let adapter = match (package, binary_env) {
        (Some(package), Some(binary_env)) => json!({ "package": package, "binary_env": binary_env, "env": Map::from_iter(env.iter().map(|(name, value)| (name.clone(), Value::String(value.clone())))) }),
        (Some(_), None) => return Err("An adapter needs --adapter-binary-env too: the variable that tells it where the CLI is.".into()),
        (None, Some(_)) => return Err("--adapter-binary-env goes with --adapter-package.".into()),
        (None, None) if !env.is_empty() => return Err("--adapter-env sets an adapter's environment; give --adapter-package too.".into()),
        (None, None) => Value::Null,
    };
    let bins = if bins.is_empty() { vec![kind.to_string()] } else { bins.to_vec() };
    Ok(json!({ "name": name.unwrap_or(kind), "bins": bins, "args": args, "adapter": adapter, "safe_mode": safe_mode }))
}

fn path(app: &App) -> PathBuf {
    app.config.agents_path()
}

/// Reads the file at `lorca serve`'s start, or writes it when there is none: an entry for each
/// agent whose program is installed here, or none.
pub async fn ensure(app: &Arc<App>) {
    let path = path(app);
    if path.exists() {
        reload(app);
        return;
    }
    let mut agents = Map::new();
    for template in TEMPLATES {
        if process::find(template.program).await.is_some() {
            agents.insert(template.kind.to_string(), template.entry());
        }
    }
    let found: Vec<&String> = agents.keys().collect();
    tracing::info!(?found, "writing agents.json with the coding agents installed here");
    let bytes = pretty(&Map::from_iter([("agents".to_string(), Value::Object(agents))]));
    if let Err(error) = write_new(&path, &bytes) {
        // Another process wrote it first: that one stands.
        if error.kind() != std::io::ErrorKind::AlreadyExists {
            tracing::warn!(%error, "writing agents.json");
        }
    }
    reload(app);
}

fn pretty(doc: &Map<String, Value>) -> Vec<u8> {
    let mut bytes = serde_json::to_vec_pretty(doc).unwrap_or_default();
    bytes.push(b'\n');
    bytes
}

/// Writes a file that is not there, and fails when it is.
fn write_new(path: &Path, bytes: &[u8]) -> std::io::Result<()> {
    use std::io::Write;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let mut options = std::fs::OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    let mut file = options.open(path)?;
    file.write_all(bytes)?;
    file.sync_all()
}

/// Reads the file again.
pub fn reload(app: &App) {
    let file = AgentsFile::read(&path(app));
    if let Some(error) = &file.error {
        tracing::warn!(%error, "agents.json");
    }
    *app.agents.lock().unwrap() = file;
}

/// The file as it is on disk now, so an edit by hand since the last read is kept, or why it
/// cannot be changed.
fn current(app: &App) -> Result<AgentsFile, String> {
    let file = AgentsFile::read(&path(app));
    match &file.error {
        Some(error) => Err(format!("{error} Fix the file first.")),
        None => Ok(file),
    }
}

fn write(app: &App, doc: &Map<String, Value>) -> Result<(), String> {
    let bytes = pretty(doc);
    crate::config::write_private(&path(app), &bytes).map_err(|e| format!("Cannot write agents.json: {e}"))?;
    *app.agents.lock().unwrap() = AgentsFile::parse(&bytes);
    Ok(())
}

/// Adds agent `kind` as `entry`; the entry is checked before the file changes.
pub fn add(app: &App, kind: &str, entry: &Value) -> Result<(), String> {
    let kind = kind.trim();
    read_entry(kind, entry)?;
    let file = current(app)?;
    if file.entry(kind).is_some() {
        return Err(format!("agents.json already has {kind}. Remove it first with `lorca agent remove {kind}`, or edit the file."));
    }
    write(app, &file.with_entry(kind, entry.clone()))
}

pub fn remove(app: &App, kind: &str) -> Result<(), String> {
    let file = current(app)?;
    if file.entry(kind).is_none() {
        return Err(format!("No agent named {kind} in agents.json."));
    }
    write(app, &file.without_entry(kind))
}

/// One agent as `lorca agent` shows it: its entry, where each of its programs is, and whether
/// it can start here.
async fn agent_out(entry: &Entry) -> Value {
    let mut bins = Vec::new();
    for bin in strings(entry.raw.get("bins"), "").unwrap_or_default() {
        let found = process::find(&bin).await;
        bins.push(json!({ "bin": bin, "path": found }));
    }
    let (program, npx) = match &entry.spec {
        Ok(spec) => (acp::find(spec).await, spec.adapter.is_some()),
        Err(_) => (None, false),
    };
    json!({
        "kind": entry.kind,
        "config": entry.raw,
        "problem": entry.spec.as_ref().err(),
        "bins": bins,
        "adapter": npx,
        "program": program,
        "can_start": program.is_some(),
    })
}

async fn list(app: &App) -> Value {
    let file = app.agents.lock().unwrap().clone();
    let mut agents = Vec::new();
    for entry in &file.entries {
        agents.push(agent_out(entry).await);
    }
    json!({ "path": path(app), "error": file.error, "agents": agents })
}

/// The `agents.*` verbs, from `lorca agent`.
pub async fn serve_request(app: &Arc<App>, verb: &str, body: &Value) -> Result<Value, String> {
    let kind = || body["kind"].as_str().map(str::trim).filter(|kind| !kind.is_empty()).map(str::to_string).ok_or_else(|| "missing kind".to_string());
    match verb {
        "agents.list" => {
            ensure_file(app).await;
            Ok(list(app).await)
        }
        "agents.get" => {
            ensure_file(app).await;
            let kind = kind()?;
            let entry = app.agents.lock().unwrap().entry(&kind).cloned().ok_or_else(|| format!("No agent named {kind} in agents.json."))?;
            Ok(json!({ "path": path(app), "agent": agent_out(&entry).await }))
        }
        "agents.add" => {
            ensure_file(app).await;
            let kind = kind()?;
            add(app, &kind, &body["config"])?;
            let entry = app.agents.lock().unwrap().entry(&kind).cloned().ok_or("agents.json changed meanwhile")?;
            Ok(json!({ "path": path(app), "agent": agent_out(&entry).await }))
        }
        "agents.remove" => {
            remove(app, &kind()?)?;
            Ok(json!({ "path": path(app) }))
        }
        // The file as it is now, after an edit made outside Lorca.
        "agents.reload" => {
            reload(app);
            Ok(list(app).await)
        }
        other => Err(format!("Unknown request {other}")),
    }
}

/// Writes the file from what is installed when there is none yet, as `lorca serve` does.
async fn ensure_file(app: &Arc<App>) {
    if app.agents.lock().unwrap().missing {
        ensure(app).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(text: &str) -> AgentsFile {
        AgentsFile::parse(text.as_bytes())
    }

    #[test]
    fn the_file_reads_as_its_agents_in_order() {
        let file = parse(
            r#"{ "agents": {
                "pi": { "name": "pi", "bins": ["pi"], "args": [], "adapter": { "package": "pi-acp@0.0.34", "binary_env": "PI_ACP_PI_COMMAND", "env": { "A": "1" } }, "safe_mode": null },
                "dsh": { "name": "dsh", "bins": ["dsh"], "args": ["--profile", "acp"] }
            } }"#,
        );
        assert_eq!(file.error, None);
        let kinds: Vec<&str> = file.specs().map(|spec| spec.kind.as_str()).collect();
        assert_eq!(kinds, ["pi", "dsh"]);
        let pi = file.spec("pi").unwrap();
        assert_eq!(pi.adapter.as_ref().unwrap().env, [("A".to_string(), "1".to_string())]);
        assert_eq!(file.spec("dsh").unwrap().args, ["--profile", "acp"]);
        assert_eq!(file.spec("dsh").unwrap().adapter, None);
    }

    #[test]
    fn a_file_that_does_not_read_says_why() {
        assert!(parse("").error.unwrap().contains("empty"));
        assert!(parse("  \n").error.unwrap().contains("empty"));
        assert!(parse("{").error.unwrap().contains("not valid JSON"));
        assert!(parse("[]").error.unwrap().contains("not an object"));
        assert!(parse("{}").error.unwrap().contains("no \"agents\""));
        assert!(parse(r#"{"agents": []}"#).error.unwrap().contains("not an object"));
        let empty = parse(r#"{"agents": {}}"#);
        assert_eq!((empty.error, empty.entries.len()), (None, 0));
    }

    #[test]
    fn an_entry_that_cannot_start_says_which_field() {
        let problem = |entry: &str| parse(&format!(r#"{{"agents": {{"x": {entry}}}}}"#)).entries[0].spec.clone().unwrap_err();
        assert!(problem(r#"{"bins": ["x"]}"#).contains("has no name"));
        assert!(problem(r#"{"name": "X"}"#).contains("has no bins"));
        assert!(problem(r#"{"name": "X", "bins": []}"#).contains("at least one program"));
        assert!(problem(r#"{"name": "X", "bins": ["x"], "args": "acp"}"#).contains("args is a list"));
        assert!(problem(r#"{"name": "X", "bins": ["x"], "adapter": {"binary_env": "X"}}"#).contains("adapter has no package"));
        assert!(problem(r#"{"name": "X", "bins": ["x"], "adapter": {"package": "x@1"}}"#).contains("adapter has no binary_env"));
        assert!(problem(r#"{"name": "X", "bins": ["x"], "adapter": {"package": "x@1", "binary_env": "X", "env": {"A": 1}}}"#).contains("as a string"));
        assert!(problem(r#"{"name": "X", "bins": ["x"], "safemode": "default"}"#).contains("no field \"safemode\""));
        assert!(problem(r#"{"name": "X", "bins": ["x"], "safe_mode": 3}"#).contains("safe_mode is a string"));
        assert!(problem(r#""claude""#).contains("is an object"));
        // One bad entry leaves the others usable.
        let file = parse(r#"{"agents": {"bad": {}, "good": {"name": "Good", "bins": ["good"]}}}"#);
        assert_eq!(file.specs().map(|spec| spec.kind.clone()).collect::<Vec<_>>(), ["good"]);
    }

    #[test]
    fn every_template_reads_as_an_agent() {
        let specs = template_specs();
        assert_eq!(specs.len(), 10);
        let claude = template_spec("claude");
        assert_eq!((claude.name.as_str(), claude.safe_mode.as_deref()), ("Claude Code", Some("default")));
        assert_eq!(template_spec("dsh").args, ["--profile", "acp"]);
        assert!(specs.iter().filter(|spec| spec.adapter.is_some()).map(|spec| spec.kind.as_str()).eq(["claude", "codex", "pi"]));
        let kinds: std::collections::HashSet<_> = specs.iter().map(|spec| spec.kind.clone()).collect();
        assert_eq!(kinds.len(), specs.len());
    }

    #[test]
    fn edits_keep_the_files_order_and_fields() {
        let file = parse(r#"{"note": "mine", "agents": {"b": {"name": "B", "bins": ["b"]}, "a": {"name": "A", "bins": ["a"]}}}"#);
        let added = file.with_entry("c", json!({ "name": "C", "bins": ["c"] }));
        assert_eq!(serde_json::to_string(&added).unwrap(), r#"{"note":"mine","agents":{"b":{"name":"B","bins":["b"]},"a":{"name":"A","bins":["a"]},"c":{"name":"C","bins":["c"]}}}"#);
        let removed = file.without_entry("b");
        assert_eq!(serde_json::to_string(&removed).unwrap(), r#"{"note":"mine","agents":{"a":{"name":"A","bins":["a"]}}}"#);
    }

    #[test]
    fn flags_make_an_entry() {
        let entry = entry_of(None, "x", &[], &["acp".into()], None, None, &[], None).unwrap();
        assert_eq!(entry, json!({ "name": "x", "bins": ["x"], "args": ["acp"], "adapter": null, "safe_mode": null }));
        let adapter = entry_of(Some("X"), "x", &["xx".into()], &[], Some("x-acp@1"), Some("X_PATH"), &[("A".into(), "1".into())], Some("default")).unwrap();
        assert_eq!(adapter["adapter"], json!({ "package": "x-acp@1", "binary_env": "X_PATH", "env": { "A": "1" } }));
        assert!(read_entry("x", &adapter).is_ok());
        assert!(entry_of(None, "x", &[], &[], Some("x-acp@1"), None, &[], None).is_err());
        assert!(entry_of(None, "x", &[], &[], None, None, &[("A".into(), "1".into())], None).is_err());
    }

    #[test]
    fn a_new_file_is_never_written_over() {
        let path = std::env::temp_dir().join(format!("lorca-agents-{}.json", uuid::Uuid::new_v4()));
        write_new(&path, b"{\"agents\": {}}").unwrap();
        assert_eq!(write_new(&path, b"other").unwrap_err().kind(), std::io::ErrorKind::AlreadyExists);
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{\"agents\": {}}");
        let _ = std::fs::remove_file(&path);
        assert!(AgentsFile::read(&path).missing);
    }
}
