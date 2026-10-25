//! The tools a turn has. Every turn makes the same set, and what kind of turn it is (a setup's
//! sample, an unattended run, the chief's, a bot still setting up) decides which of them it keeps
//! (`TurnShape`), so the rule reads in one place and is tested without a turn.

use std::sync::atomic::AtomicBool;
use std::sync::Arc;

use lorca_agent::codemode::{CodemodeOptions, CodemodeTool, HostFunction};
use lorca_agent::Tool;

use super::*;

/// Tools only the account's chief has: it makes bots and groups, and the other bots send those
/// requests to it.
const CHIEF_TOOLS: [&str; 3] = ["create_bot", "create_group", "edit_group"];

/// The team tools a setup's sample keeps: it cannot create teammates, hand off, install
/// integrations, or arm schedules.
const SAMPLE_TEAM_TOOLS: [&str; 2] = ["list_teammates", "search_plugins"];

/// Tools an unattended turn (a routine's run, an event) goes without: each makes a bot, a group,
/// a profile change, or a plugin card for the user, and nobody is here to ask. Their schemas stay
/// out of the run's requests.
const USER_REQUEST_TOOLS: [&str; 5] = ["create_bot", "edit_bot", "create_group", "edit_group", "recommend_plugins"];

/// All a new bot has while it works out its job with the user, settles its profile, and lists
/// its plugins.
const SETUP_TOOLS: [&str; 5] = ["ask_user", "finish_setup", "search_plugins", "recommend_plugins", "memory_update"];

/// What codemode scripts call besides plugin tools: the bot's own file, memory and output tools. `bash`
/// is the scripts' own (`shell::script_bash`); the tools that hand work to teammates, change
/// bots or routines, or install and sign in to plugins stay calls of their own, as `bash_input`
/// and `bash_output` do.
const SCRIPTABLE_TOOLS: [&str; 11] = ["read", "write", "edit", "grep", "find", "ls", "memory_update", "memory_log", "profile_update", "recall", "publish_output"];

/// What a script's `bash` does differently from the bot's own.
const SCRIPT_GUIDANCE: &str = "In a script, `bash` runs one command at a time, on pipes with nothing on stdin: a command that asks \
for input gets none, so pass answers as flags (--yes, -y) or through files. A command that exits with a nonzero code resolves too; \
check `exit_code`.";

/// Which of a turn's tools a tool is among: the team's (teammates, handoffs, routines, plugins,
/// the user's cards), or the bot's own work (attention, coding agents, memory, files, commands).
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(super) enum ToolSet {
    Team,
    Own,
}

/// What kind of turn it is, as far as its tools go.
#[derive(Clone, Copy, Default, Debug)]
pub(super) struct TurnShape {
    /// A workflow setup's sample run.
    pub(super) sample: bool,
    /// A routine's run or an event's turn: nobody is here to answer.
    pub(super) unattended: bool,
    /// The bot is the account's chief.
    pub(super) chief: bool,
    /// The bot is still setting itself up.
    pub(super) setting_up: bool,
    /// The chat has shared project context for this bot.
    pub(super) project: bool,
}

impl TurnShape {
    pub(super) fn of(app: &Arc<App>, job: &Job, bot: &Bot, chat: &Chat, unattended: bool) -> Self {
        TurnShape {
            sample: job.kind == crate::workflows::SAMPLE_JOB,
            unattended,
            chief: is_chief(app, bot),
            setting_up: bot.setting_up,
            project: crate::project_context::project_for_turn(app, &chat.meta.id, &bot.id).is_some(),
        }
    }

    /// Whether a turn of this shape keeps the tool named `name` from `set`: what `turn_tools`
    /// does in two steps, the usual tools then the setup's.
    #[cfg(test)]
    fn keeps(&self, set: ToolSet, name: &str) -> bool {
        self.offers(set, name) && self.keeps_in_setup(name)
    }

    /// Whether the tool is among the turn's usual ones, whatever its setup.
    fn offers(&self, set: ToolSet, name: &str) -> bool {
        if CHIEF_TOOLS.contains(&name) && !self.chief {
            return false;
        }
        if self.sample && (name == "coding_agent" || (set == ToolSet::Team && !SAMPLE_TEAM_TOOLS.contains(&name))) {
            return false;
        }
        if self.unattended && USER_REQUEST_TOOLS.contains(&name) {
            return false;
        }
        name != "project_context" || self.project
    }

    /// A bot setting itself up has only its setup tools; `finish_setup` is for that alone.
    fn keeps_in_setup(&self, name: &str) -> bool {
        if self.setting_up { SETUP_TOOLS.contains(&name) } else { name != "finish_setup" }
    }
}

/// A turn's tools, made once when it starts.
pub(super) struct TurnTools {
    /// What the turn's requests offer.
    pub(super) tools: Vec<Arc<dyn Tool>>,
    /// The bot's usual tools, for a turn that starts in setup and finishes it.
    pub(super) full_tools: Option<Vec<Arc<dyn Tool>>>,
    /// The plugins' tools and the bot's own scriptable ones, which codemode scripts call.
    pub(super) plugin_tools: Arc<crate::plugins::mcp::PluginCatalog>,
}

/// Makes a turn's tools for `shape`. Every tool checks the bot's Access again when it starts
/// (`permissions::guarded`); plugin tools are called from codemode scripts, so the tool list
/// stays the same for the whole turn, and so does its prompt cache.
#[allow(clippy::too_many_arguments)]
pub(super) fn turn_tools(
    app: &Arc<App>,
    job: &Job,
    bot: &Bot,
    chat: &Chat,
    shape: TurnShape,
    trigger: &Trigger,
    store: &MemoryStore,
    plugin_briefs: &[crate::plugins::mcp::PluginBrief],
    attention_handled: Arc<AtomicBool>,
) -> TurnTools {
    let chat_id = &chat.meta.id;
    let workdir = bot.working_directory(&app.config.home);
    // What the turn hands off belongs to the work it continues, which a Stop ends.
    let origin = crate::handoffs::origin_of(app, job);
    let team: Vec<Arc<dyn Tool>> = vec![
        Arc::new(ListTeammates { app: app.clone(), chat_id: chat_id.clone() }),
        Arc::new(MessageBot { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone(), hops: job.hops, task_id: job.task_id.clone(), origin: origin.clone() }),
        Arc::new(Handoffs { app: app.clone(), bot_id: bot.id.clone(), request: crate::handoffs::bound_request(app, job), origin }),
        Arc::new(FinishSetup { app: app.clone(), bot: bot.clone() }),
        Arc::new(RecommendPlugins { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone() }),
        Arc::new(EditBot { app: app.clone(), bot: bot.clone() }),
        Arc::new(Routines { app: app.clone(), bot: bot.clone() }),
        Arc::new(crate::channels::ChannelsTool { app: app.clone(), bot: bot.clone(), user_started: matches!(job.kind.as_str(), "turn" | "room_turn") }),
        Arc::new(crate::feedback::FeedbackTool { app: app.clone(), bot_id: bot.id.clone() }),
        Arc::new(crate::review_execution::StageReview { app: app.clone(), bot: bot.clone(), chat_id: chat_id.clone(), trigger: trigger.clone() }),
        Arc::new(crate::tasks::TasksTool { app: app.clone(), bot_id: bot.id.clone(), chat_id: chat_id.clone(), job_id: job.id.clone() }),
        Arc::new(crate::outputs::PublishOutputTool { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone(), workdir: workdir.clone() }),
        Arc::new(SearchPlugins { app: app.clone() }),
        Arc::new(InstallPlugin { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone(), unattended: shape.unattended }),
        Arc::new(ConnectPlugin { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone() }),
        Arc::new(crate::secrets::RequestSecret { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone(), unattended: shape.unattended }),
        Arc::new(crate::ask::AskUser { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone(), unattended: shape.unattended }),
        Arc::new(CreateBot { app: app.clone(), bot: bot.clone() }),
        Arc::new(crate::groups::CreateGroup { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone(), unattended: shape.unattended }),
        Arc::new(crate::groups::EditGroup { app: app.clone(), bot: bot.clone(), unattended: shape.unattended }),
    ];
    let mut own: Vec<Arc<dyn Tool>> = vec![
        Arc::new(crate::attention::AttentionTool {
            app: app.clone(),
            bot_id: bot.id.clone(),
            chat_id: chat_id.clone(),
            hops: job.hops,
            handled: attention_handled,
            requesting_bot_id: job.from_bot_id.clone().filter(|_| job.kind == "message"),
        }),
        // Claude Code or Codex on this Runner, which the bot starts and supervises.
        Arc::new(crate::coding::CodingAgentTool {
            app: app.clone(),
            bot: bot.clone(),
            chat_id: chat_id.clone(),
            workdir: workdir.clone(),
            trigger_message_id: trigger.message_id.clone(),
            event: trigger.event.clone(),
        }),
    ];
    own.extend(memory_tools(app, bot, store, chat));
    own.push(Arc::new(Recall { app: app.clone(), store: store.clone(), bot: bot.clone() }));
    own.push(Arc::new(crate::project_context::ProjectContextTool { app: app.clone(), chat_id: chat_id.clone(), bot: bot.clone() }));
    // Commands run in terminals of their own, kept on this Runner past the turn when they
    // wait for input.
    let sessions = Arc::new(crate::shell::TurnSessions::new(app, chat_id, &bot.id));
    let secrets = Arc::new(crate::secrets::CommandSecrets { app: app.clone(), bot_id: bot.id.clone() });
    own.extend(lorca_agent::tools::coding_tools_with_sessions(workdir.clone(), sessions, crate::shell::bot_shell_extras(app), Some(secrets)));

    let offered: Vec<Arc<dyn Tool>> = team
        .into_iter()
        .filter(|tool| shape.offers(ToolSet::Team, tool.name()))
        .chain(own.into_iter().filter(|tool| shape.offers(ToolSet::Own, tool.name())))
        .collect();
    let mut tools = crate::permissions::guarded::tools(app, bot, chat_id, offered);
    // Plugin tools are called from codemode scripts, with the bot's own file and memory tools
    // and a bash of the scripts' own, on pipes.
    let mut scriptable: Vec<Arc<dyn Tool>> = tools.iter().filter(|tool| SCRIPTABLE_TOOLS.contains(&tool.name())).cloned().collect();
    scriptable.extend(crate::permissions::guarded::tools(app, bot, chat_id, vec![crate::shell::script_bash(app, &bot.id, &workdir)]));
    let plugin_tools = crate::plugins::mcp::bot_catalog(app, bot, chat_id, scriptable);
    let script_store = Arc::new(crate::scripts::ScriptStore { app: app.clone(), chat_id: chat_id.clone(), bot_id: bot.id.clone() });
    let functions: Vec<Arc<dyn HostFunction>> =
        crate::scripts::ModelsAsk::new(app, chat_id, &bot.provider).map(|ask| Arc::new(ask) as Arc<dyn HostFunction>).into_iter().collect();
    // A routine's script has nobody to press Stop, so it gets less time.
    let timeout = std::time::Duration::from_secs(if shape.unattended { 10 * 60 } else { 30 * 60 });
    let guidance = if plugin_briefs.is_empty() { SCRIPT_GUIDANCE.to_string() } else { format!("{SCRIPT_GUIDANCE}\n{}", crate::plugins::mcp::RESOURCE_TOOLS_GUIDANCE) };
    let options = CodemodeOptions { mcp_types: !plugin_briefs.is_empty(), timeout, guidance: Some(guidance), ..CodemodeOptions::default() };
    // Codemode itself also rechecks after any asynchronous review; its children have guards.
    tools.extend(crate::permissions::guarded::tools(app, bot, chat_id, vec![Arc::new(CodemodeTool::new(plugin_tools.clone(), options).with_store(script_store).with_functions(functions))]));

    // The turn that finishes the bot's setup goes on with its usual tools.
    let full_tools = shape.setting_up.then(|| tools.iter().filter(|tool| tool.name() != "finish_setup").cloned().collect());
    tools.retain(|tool| shape.keeps_in_setup(tool.name()));
    TurnTools { tools, full_tools, plugin_tools }
}

#[cfg(test)]
mod tests {
    use super::{ToolSet, TurnShape};

    fn kept(shape: TurnShape, tools: &[(ToolSet, &'static str)]) -> Vec<&'static str> {
        tools.iter().filter(|(set, name)| shape.keeps(*set, name)).map(|(_, name)| *name).collect()
    }

    #[test]
    fn each_kind_of_turn_keeps_its_tools() {
        use ToolSet::{Own, Team};
        let tools = [
            (Team, "list_teammates"),
            (Team, "message_bot"),
            (Team, "finish_setup"),
            (Team, "recommend_plugins"),
            (Team, "edit_bot"),
            (Team, "search_plugins"),
            (Team, "ask_user"),
            (Team, "create_bot"),
            (Own, "coding_agent"),
            (Own, "memory_update"),
            (Own, "project_context"),
            (Own, "bash"),
            (Own, "codemode"),
        ];
        let usual = TurnShape::default();
        assert_eq!(kept(usual, &tools), ["list_teammates", "message_bot", "recommend_plugins", "edit_bot", "search_plugins", "ask_user", "coding_agent", "memory_update", "bash", "codemode"]);
        assert!(kept(TurnShape { chief: true, ..usual }, &tools).contains(&"create_bot"), "only the chief makes bots");
        assert!(kept(TurnShape { project: true, ..usual }, &tools).contains(&"project_context"));
        assert_eq!(
            kept(TurnShape { chief: true, unattended: true, ..usual }, &tools),
            ["list_teammates", "message_bot", "search_plugins", "ask_user", "coding_agent", "memory_update", "bash", "codemode"],
            "nobody is here for a bot, a profile change, or a plugin card"
        );
        assert_eq!(
            kept(TurnShape { sample: true, chief: true, ..usual }, &tools),
            ["list_teammates", "search_plugins", "memory_update", "bash", "codemode"],
            "a sample hands nothing off and starts no agent, but works with its own tools"
        );
        assert_eq!(kept(TurnShape { setting_up: true, ..usual }, &tools), ["finish_setup", "recommend_plugins", "search_plugins", "ask_user", "memory_update"]);
        assert!(!kept(TurnShape { setting_up: true, unattended: true, ..usual }, &tools).contains(&"recommend_plugins"));
    }
}
