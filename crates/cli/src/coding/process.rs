//! A coding agent's process: started with the login shell's environment in a process group of
//! its own (a job object on Windows), so Stop ends what it started too, and spoken to on its stdin
//! and stdout by the driver that takes them.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use process_wrap::tokio::{ChildWrapper, CommandWrap};
use tokio::io::AsyncReadExt;
use tokio::process::{ChildStdin, ChildStdout};
use tokio::sync::{oneshot, Mutex};
use tokio_util::sync::CancellationToken;

/// What a Claude Code session started from another one would inherit: they mark the parent's
/// session, which the agent Lorca starts is not part of.
const INHERITED_MARKERS: [&str; 10] = [
    "CLAUDECODE",
    "CLAUDE_PID",
    "CLAUDE_EFFORT",
    "CLAUDE_CODE_ENTRYPOINT",
    "CLAUDE_CODE_EXECPATH",
    "CLAUDE_CODE_SESSION_ID",
    "CLAUDE_CODE_CHILD_SESSION",
    "CLAUDE_CODE_SESSION_ATTENDED",
    "CLAUDE_CODE_MESSAGING_SOCKET",
    "CLAUDE_CODE_MESSAGING_TOKEN",
];

/// How a process ended: its exit code, the last thing it said on stderr, and whether Lorca
/// stopped it.
#[derive(Debug, Clone)]
pub(crate) struct Exit {
    pub code: Option<i32>,
    pub said: Option<String>,
    pub stopped: bool,
}

pub(crate) struct Process {
    stdin: Mutex<Option<ChildStdin>>,
    kill: CancellationToken,
}

impl Process {
    /// Hands its stdin to the driver that speaks to it.
    pub async fn take_stdin(&self) -> Option<ChildStdin> {
        self.stdin.lock().await.take()
    }

    /// Ends the process and everything it started.
    pub fn stop(&self) {
        self.kill.cancel();
    }
}

/// An environment variable the agent is given: a saved secret the bot named.
pub(crate) type Variable = (std::ffi::OsString, std::ffi::OsString);

/// Starts `program` in `cwd`. Its stdout goes to the caller line by line; how it ended arrives
/// once it has.
pub(crate) async fn spawn(program: &Path, args: &[String], cwd: &Path, variables: &[Variable]) -> Result<(Arc<Process>, ChildStdout, oneshot::Receiver<Exit>), String> {
    let mut command = lorca_agent::login_shell::command(program).await;
    command.args(args).current_dir(cwd).stdin(std::process::Stdio::piped()).stdout(std::process::Stdio::piped()).stderr(std::process::Stdio::piped());
    for name in INHERITED_MARKERS {
        command.env_remove(name);
    }
    command.envs(variables.iter().map(|(name, value)| (name, value)));
    let mut wrapped = CommandWrap::from(command);
    #[cfg(unix)]
    wrapped.wrap(process_wrap::tokio::ProcessGroup::leader());
    #[cfg(windows)]
    wrapped.wrap(process_wrap::tokio::JobObject);
    wrapped.wrap(process_wrap::tokio::KillOnDrop);
    let name = program.file_name().map(|name| name.to_string_lossy().to_string()).unwrap_or_default();
    let mut child: Box<dyn ChildWrapper> = wrapped.spawn().map_err(|e| format!("Cannot start {name}: {e}"))?;
    let stdin = child.stdin().take().ok_or("No stdin")?;
    let stdout = child.stdout().take().ok_or("No stdout")?;
    let stderr = child.stderr().take();
    let kill = CancellationToken::new();
    let (tx, rx) = oneshot::channel();
    let token = kill.clone();
    tokio::spawn(async move {
        // The last lines it wrote to stderr usually say why it stopped.
        let said = tokio::spawn(async move {
            let mut tail = String::new();
            if let Some(mut stderr) = stderr {
                let mut buffer = vec![0u8; 8192];
                while let Ok(read) = stderr.read(&mut buffer).await {
                    if read == 0 {
                        break;
                    }
                    tail.push_str(&String::from_utf8_lossy(&buffer[..read]));
                    if tail.len() > 16_384 {
                        tail = tail.split_off(tail.len() - 8192);
                    }
                }
            }
            tail.lines().map(str::trim).rfind(|line| !line.is_empty()).map(|line| line.chars().take(300).collect::<String>())
        });
        let (status, stopped) = tokio::select! {
            status = child.wait() => (status.ok(), false),
            _ = token.cancelled() => {
                let _ = child.start_kill();
                (child.wait().await.ok(), true)
            }
        };
        let said = tokio::time::timeout(std::time::Duration::from_secs(1), said).await.ok().and_then(Result::ok).flatten();
        let _ = tx.send(Exit { code: status.and_then(|status| status.code()), said, stopped });
    });
    Ok((Arc::new(Process { stdin: Mutex::new(Some(stdin)), kill }), stdout, rx))
}

/// Where `name` is installed, on the login shell's `PATH`: what a terminal would run.
pub(crate) async fn find(name: &str) -> Option<PathBuf> {
    #[cfg(test)]
    if let Some(path) = tests_support::overridden(name) {
        return Some(path);
    }
    let environment = lorca_agent::login_shell::environment().await;
    let path = environment.iter().rev().find(|(key, _)| key.to_str().is_some_and(|key| key.eq_ignore_ascii_case("PATH"))).map(|(_, value)| value.clone()).or_else(|| std::env::var_os("PATH"))?;
    let names: Vec<String> = if cfg!(windows) { [".exe", ".cmd", ".bat"].iter().map(|extension| format!("{name}{extension}")).collect() } else { vec![name.to_string()] };
    std::env::split_paths(&path).filter(|folder| folder.is_absolute()).flat_map(|folder| names.iter().map(move |file| folder.join(file))).find(|file| is_program(file))
}

fn is_program(path: &Path) -> bool {
    let Ok(metadata) = std::fs::metadata(path) else { return false };
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        metadata.is_file() && metadata.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    metadata.is_file()
}

#[cfg(test)]
pub(crate) mod tests_support {
    use std::path::PathBuf;
    use std::sync::Mutex;

    static PROGRAMS: Mutex<Vec<(String, PathBuf)>> = Mutex::new(Vec::new());

    /// Has [`super::find`] answer `path` for `name`, as if it were installed.
    pub fn install(name: &str, path: PathBuf) {
        let mut programs = PROGRAMS.lock().unwrap();
        programs.retain(|(known, _)| known != name);
        programs.push((name.to_string(), path));
    }

    pub fn overridden(name: &str) -> Option<PathBuf> {
        PROGRAMS.lock().unwrap().iter().find(|(known, _)| known == name).map(|(_, path)| path.clone())
    }
}
