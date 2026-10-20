//! What a coding agent's work leaves for the bot and the user, published as outputs in the chat
//! each time it is done ([Outputs and evidence](../../../../docs/architecture/outputs.md)): its
//! diff, as a new version whenever it changed, and the pull request its transcript names. The bot
//! gets their references to cite as task evidence. A saved secret in the diff goes out as its
//! placeholder.

use std::path::Path;
use std::sync::{Arc, LazyLock};

use regex::Regex;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use super::Agent;
use crate::app::App;
use crate::outputs::PublishOutput;

/// What Lorca published of an agent's work so far, so a later round publishes only what changed.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub(crate) struct Published {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub diff: Option<Version>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub links: Vec<String>,
}

/// A published diff: its name, what it held, and the message of its latest version.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub(crate) struct Version {
    pub name: String,
    pub fingerprint: String,
    pub message_id: String,
}

/// An output as the bot cites it.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub(crate) struct Reference {
    pub name: String,
    pub message_id: String,
    pub output_id: String,
    pub version: u32,
}

static PULL_REQUEST: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"https://(?:github\.com/[\w.-]+/[\w.-]+/pull/\d+|gitlab\.com/[\w./-]+/-/merge_requests/\d+)").unwrap());

/// The pull request (or GitLab merge request) a line of its transcript names.
pub(crate) fn pull_request(line: &str) -> Option<String> {
    PULL_REQUEST.find(line).map(|found| found.as_str().to_string())
}

/// Publishes what changed in the agent's work since the last round, and returns what it
/// published.
pub(crate) async fn publish(app: &Arc<App>, agent: &Arc<Agent>) -> Vec<Reference> {
    let record = agent.record();
    let mut published = record.published.clone();
    let mut references = Vec::new();
    let redactions = crate::secrets::Redactions::load(app);
    let scratch = app.config.home.join("agents").join(&record.id);

    if let Some(base) = record.base.as_deref() {
        let diff = super::workspace::diff(&record.folder, base).await;
        let diff = redactions.text(&diff).unwrap_or(diff);
        let fingerprint = hex(&Sha256::digest(diff.as_bytes()));
        if !diff.trim().is_empty() && published.diff.as_ref().is_none_or(|version| version.fingerprint != fingerprint) {
            let name = format!("{}.diff", record.branch.clone().unwrap_or_else(|| record.id.clone()));
            let path = scratch.join(&name);
            let written = std::fs::create_dir_all(&scratch).and_then(|_| std::fs::write(&path, diff.as_bytes()));
            if written.is_ok() {
                let replaces = published.diff.as_ref().map(|version| version.message_id.clone());
                let request = PublishOutput { name: name.clone(), path: Some(path.to_string_lossy().to_string()), mime: Some("text/x-diff".into()), replaces, ..Default::default() };
                if let Some(reference) = output(app, &record, &scratch, request).await {
                    published.diff = Some(Version { name, fingerprint, message_id: reference.message_id.clone() });
                    references.push(reference);
                }
            }
            let _ = std::fs::remove_file(&path);
        }
    }

    if let Some(link) = record.pull_request.clone().filter(|link| !published.links.contains(link)) {
        let number = link.rsplit('/').next().unwrap_or("");
        let request = PublishOutput { name: format!("Pull request #{number}"), url: Some(link.clone()), ..Default::default() };
        if let Some(reference) = output(app, &record, &record.folder, request).await {
            references.push(reference);
        }
        published.links.push(link);
    }

    agent.record.lock().unwrap().published = published;
    references
}

/// Publishes one output in the agent's chat as its bot's, and its reference. A version whose
/// predecessor the bot replaced meanwhile starts a series of its own.
async fn output(app: &Arc<App>, record: &super::Record, workdir: &Path, request: PublishOutput) -> Option<Reference> {
    let (app, chat_id, bot_id, workdir) = (app.clone(), record.chat_id.clone(), record.bot_id.clone(), workdir.to_path_buf());
    let name = request.name.clone();
    let published = tokio::task::spawn_blocking(move || {
        crate::outputs::publish(&app, &chat_id, &bot_id, &workdir, request.clone()).or_else(|error| match request.replaces {
            Some(_) => crate::outputs::publish(&app, &chat_id, &bot_id, &workdir, PublishOutput { replaces: None, ..request }),
            None => Err(error),
        })
    })
    .await;
    match published {
        Ok(Ok(message)) => {
            let output = message.output.as_ref()?;
            Some(Reference { name, message_id: message.id.clone(), output_id: output.id.clone(), version: output.version })
        }
        Ok(Err(error)) => {
            tracing::warn!(%error, %name, "publishing a coding agent's output");
            None
        }
        Err(error) => {
            tracing::warn!(%error, "publishing a coding agent's output");
            None
        }
    }
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_transcript_line_names_its_pull_request() {
        assert_eq!(pull_request("Opened https://github.com/egoist/lorca/pull/141 for review.").as_deref(), Some("https://github.com/egoist/lorca/pull/141"));
        assert_eq!(pull_request("  ⎿ https://gitlab.com/acme/shop/-/merge_requests/7").as_deref(), Some("https://gitlab.com/acme/shop/-/merge_requests/7"));
        assert_eq!(pull_request("See https://github.com/egoist/lorca/issues/134"), None);
    }
}
