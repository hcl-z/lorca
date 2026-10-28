//! The transcript as the apps draw it: what the agent was sent, what it said, each tool it used
//! with its command's output as a terminal wrote it and its edits as a diff, its plan, and Lorca's
//! notes. The driver puts each one under a key of its own and changes it as it goes (a message as
//! it streams, a command as its output comes); the agent keeps them in order, each with the change
//! it was last part of, so `coding.transcript { since }` answers only what changed. The bot reads
//! the text transcript beside it (`lines`), which says the same in words.

use std::collections::{HashMap, VecDeque};

use serde::{Deserialize, Serialize};

/// The entries kept, the latest; and how much of a command's output, from its end.
pub(crate) const KEEP_ENTRIES: usize = 1000;
pub(crate) const OUTPUT_BYTES: usize = 64 * 1024;
/// The lines of an edit's diff kept, and how many unchanged lines show around each change.
const DIFF_LINES: usize = 400;
const CONTEXT: usize = 3;

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub(crate) struct Entry {
    /// Its place in the transcript, which a later change keeps.
    #[serde(default)]
    pub seq: u64,
    /// The change it was last part of.
    #[serde(default)]
    pub rev: u64,
    /// `sent`, `said`, `thought`, `tool`, `plan`, `note` (Lorca's), `notice` (the agent's),
    /// `compaction`, or `log`: the bot's text of an agent from before entries.
    pub kind: String,
    /// What was sent, said, or thought (Markdown), the note or notice, or a compaction's summary.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub text: String,
    /// A notice's severity: `info`, `warning`, or `error`.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub level: String,
    /// How long a thought took, in seconds, while it thinks.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub seconds: Option<f64>,
    /// Who sent a `sent` entry: `user`, or empty for the bot.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub from: String,
    /// How a compaction stands: `in_progress`, `completed`, `failed`, or `cancelled`.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool: Option<Tool>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub steps: Vec<Step>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub(crate) struct Tool {
    pub title: String,
    /// The tool's own name, when the agent gives it: `Bash`, an MCP tool's.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    /// What it acts on: the command, the path, the pattern.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub detail: String,
    /// The agent's kind of tool: `execute`, `edit`, `read`, `search`, `fetch`, …
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    /// `pending`, `in_progress`, `completed`, or `failed`.
    pub status: String,
    /// A command's output as its terminal wrote it, with only its colors kept (`terminal_text`).
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub output: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub exit_code: Option<i64>,
    /// What else it gave back, as text.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub result: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub diffs: Vec<FileDiff>,
}

/// An edit: the file, and its changed lines with a few around them, each `+`, `-`, or ` ` and the
/// line, with `@@` between the parts.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub(crate) struct FileDiff {
    pub path: String,
    pub lines: Vec<String>,
    pub added: usize,
    pub removed: usize,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
pub(crate) struct Step {
    pub text: String,
    pub done: bool,
}

impl Entry {
    pub(crate) fn text(kind: &str, text: impl Into<String>) -> Entry {
        Entry { kind: kind.into(), text: text.into(), ..Entry::default() }
    }

    /// What was sent to the agent, by the user (`from` is `user`) or the bot (empty).
    pub(crate) fn sent(text: impl Into<String>, from: &str) -> Entry {
        Entry { kind: "sent".into(), text: text.into(), from: from.into(), ..Entry::default() }
    }

    /// The entry with each of its texts passed through `clean`, as everything an agent shows is
    /// before it reaches an app.
    pub(crate) fn cleaned(mut self, clean: impl Fn(String) -> String) -> Entry {
        self.text = clean(std::mem::take(&mut self.text));
        if let Some(tool) = &mut self.tool {
            tool.title = clean(std::mem::take(&mut tool.title));
            tool.detail = clean(std::mem::take(&mut tool.detail));
            tool.output = clean(std::mem::take(&mut tool.output));
            tool.result = clean(std::mem::take(&mut tool.result));
            for diff in &mut tool.diffs {
                diff.path = clean(std::mem::take(&mut diff.path));
                diff.lines = std::mem::take(&mut diff.lines).into_iter().map(&clean).collect();
            }
        }
        for step in &mut self.steps {
            step.text = clean(std::mem::take(&mut step.text));
        }
        self
    }
}
/// An agent's entries in order, with the keys its current run puts them under.
#[derive(Default)]
pub(crate) struct Entries {
    list: VecDeque<Entry>,
    keys: HashMap<String, u64>,
    next: u64,
    rev: u64,
}

impl Entries {
    /// Entries kept from an earlier run, as `coding.transcript` answered them.
    pub(crate) fn restore(list: Vec<Entry>) -> Entries {
        let next = list.last().map_or(0, |entry| entry.seq + 1);
        let rev = list.iter().map(|entry| entry.rev).max().unwrap_or(0);
        Entries { list: list.into(), keys: HashMap::new(), next, rev }
    }

    /// A new run of the agent's process: its keys start afresh.
    pub(crate) fn new_run(&mut self) {
        self.keys.clear();
    }

    /// Puts `entry` under `key`: in place of the one there, or after the rest.
    pub(crate) fn put(&mut self, key: String, mut entry: Entry) {
        self.rev += 1;
        entry.rev = self.rev;
        let first = self.list.front().map_or(self.next, |entry| entry.seq);
        if let Some(seq) = self.keys.get(&key).copied().filter(|seq| *seq >= first) {
            entry.seq = seq;
            if let Some(slot) = self.list.get_mut((seq - first) as usize) {
                *slot = entry;
            }
            return;
        }
        entry.seq = self.next;
        self.next += 1;
        self.keys.insert(key, entry.seq);
        self.list.push_back(entry);
        while self.list.len() > KEEP_ENTRIES {
            self.list.pop_front();
        }
    }

    /// The entries changed after change `since` (all of them without it), the latest change, and
    /// the first entry kept, before which an app drops what it has.
    pub(crate) fn since(&self, since: Option<u64>) -> (Vec<Entry>, u64, u64) {
        let changed = self.list.iter().filter(|entry| since.is_none_or(|since| entry.rev > since)).cloned().collect();
        (changed, self.rev, self.list.front().map_or(self.next, |entry| entry.seq))
    }

    pub(crate) fn all(&self) -> Vec<Entry> {
        self.list.iter().cloned().collect()
    }
}

/// The end of `text`, at most `limit` bytes of it, cut where a character begins.
pub(crate) fn tail(text: &str, limit: usize) -> &str {
    if text.len() <= limit {
        return text;
    }
    let mut at = text.len() - limit;
    while !text.is_char_boundary(at) {
        at += 1;
    }
    &text[at..]
}

/// A command's output as a terminal shows it, with what an agent's output could do to that
/// terminal taken out: only color and style sequences (`ESC [ … m`) stay, with the newlines,
/// carriage returns, and tabs; any other escape sequence (a clipboard write, a link, a title, a
/// cursor move) and every other control character is dropped.
pub(crate) fn terminal_text(raw: &str) -> String {
    let mut out = String::with_capacity(raw.len());
    let mut chars = raw.chars().peekable();
    while let Some(c) = chars.next() {
        match c {
            '\x1b' => match chars.peek() {
                Some('[') => {
                    chars.next();
                    let mut body = String::new();
                    let mut end = None;
                    for next in chars.by_ref() {
                        if ('\x40'..='\x7e').contains(&next) {
                            end = Some(next);
                            break;
                        }
                        body.push(next);
                    }
                    if end == Some('m') && body.chars().all(|c| c.is_ascii_digit() || c == ';' || c == ':') {
                        out.push_str("\x1b[");
                        out.push_str(&body);
                        out.push('m');
                    }
                }
                // A string sequence (OSC, DCS, APC, PM, SOS) runs to BEL or ST.
                Some(']' | 'P' | '_' | '^' | 'X') => {
                    chars.next();
                    while let Some(next) = chars.next() {
                        if next == '\x07' || (next == '\x1b' && chars.peek() == Some(&'\\')) {
                            if next == '\x1b' {
                                chars.next();
                            }
                            break;
                        }
                    }
                }
                Some(_) => {
                    chars.next();
                }
                None => {}
            },
            '\n' | '\r' | '\t' => out.push(c),
            c if c.is_control() => {}
            c => out.push(c),
        }
    }
    out
}

/// Output without its colors, as the bot reads it.
pub(crate) fn plain(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    let mut rest = text;
    while let Some(at) = rest.find("\x1b[") {
        out.push_str(&rest[..at]);
        rest = &rest[at + 2..];
        let end = rest.find('m').map_or(rest.len(), |end| end + 1);
        rest = &rest[end..];
    }
    out.push_str(rest);
    // A line a carriage return rewrote reads as what it ended as.
    out.lines().map(|line| line.rsplit('\r').next().unwrap_or(line)).collect::<Vec<_>>().join("\n")
}

/// The diff of an edit, from the text before and after.
pub(crate) fn diff(path: String, old: &str, new: &str) -> FileDiff {
    let old: Vec<&str> = old.lines().collect();
    let new: Vec<&str> = new.lines().collect();
    let ops = line_ops(&old, &new);
    let (added, removed) = ops.iter().fold((0, 0), |(added, removed), op| match op {
        Op::Add(_) => (added + 1, removed),
        Op::Remove(_) => (added, removed + 1),
        Op::Keep(_) => (added, removed),
    });
    // Each change with `CONTEXT` lines around it; the parts between become `@@`.
    let near: Vec<bool> = (0..ops.len()).map(|at| ops[at.saturating_sub(CONTEXT)..(at + CONTEXT + 1).min(ops.len())].iter().any(|op| !matches!(op, Op::Keep(_)))).collect();
    let mut lines = Vec::new();
    let mut gap = false;
    for (at, op) in ops.iter().enumerate() {
        if !near[at] {
            gap = true;
            continue;
        }
        if gap && !lines.is_empty() {
            lines.push("@@".to_string());
        }
        gap = false;
        lines.push(match op {
            Op::Keep(line) => format!(" {line}"),
            Op::Add(line) => format!("+{line}"),
            Op::Remove(line) => format!("-{line}"),
        });
        if lines.len() >= DIFF_LINES {
            lines.push(format!("@@ … {} more lines", ops.len() - at - 1));
            break;
        }
    }
    FileDiff { path, lines, added, removed }
}

enum Op<'a> {
    Keep(&'a str),
    Add(&'a str),
    Remove(&'a str),
}

/// The lines kept, removed, and added from `old` to `new`: what both begin and end with stays, and
/// the middle is matched by its longest common run of lines when it is small enough to.
fn line_ops<'a>(old: &[&'a str], new: &[&'a str]) -> Vec<Op<'a>> {
    let start = old.iter().zip(new).take_while(|(a, b)| a == b).count();
    let end = old[start..].iter().rev().zip(new[start..].iter().rev()).take_while(|(a, b)| a == b).count();
    let (a, b) = (&old[start..old.len() - end], &new[start..new.len() - end]);
    let mut ops: Vec<Op<'a>> = old[..start].iter().map(|line| Op::Keep(line)).collect();
    if a.len() * b.len() <= 4_000_000 {
        // The longest common subsequence, from the end.
        let mut table = vec![0u32; (a.len() + 1) * (b.len() + 1)];
        let width = b.len() + 1;
        for i in (0..a.len()).rev() {
            for j in (0..b.len()).rev() {
                table[i * width + j] = if a[i] == b[j] { table[(i + 1) * width + j + 1] + 1 } else { table[(i + 1) * width + j].max(table[i * width + j + 1]) };
            }
        }
        let (mut i, mut j) = (0, 0);
        while i < a.len() && j < b.len() {
            if a[i] == b[j] {
                ops.push(Op::Keep(a[i]));
                i += 1;
                j += 1;
            } else if table[(i + 1) * width + j] >= table[i * width + j + 1] {
                ops.push(Op::Remove(a[i]));
                i += 1;
            } else {
                ops.push(Op::Add(b[j]));
                j += 1;
            }
        }
        ops.extend(a[i..].iter().map(|line| Op::Remove(line)));
        ops.extend(b[j..].iter().map(|line| Op::Add(line)));
    } else {
        ops.extend(a.iter().map(|line| Op::Remove(line)));
        ops.extend(b.iter().map(|line| Op::Add(line)));
    }
    ops.extend(old[old.len() - end..].iter().map(|line| Op::Keep(line)));
    ops
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_terminal_keeps_only_the_colors_of_what_an_agent_printed() {
        let raw = "\x1b[32mok\x1b[0m \x1b]52;c;ZXZpbA==\x07\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\\x1b[2J\x1b[1;31mfail\x1b[m\x07\u{9b}\r\n\tdone\x1b";
        assert_eq!(terminal_text(raw), "\x1b[32mok\x1b[0m link\x1b[1;31mfail\x1b[m\r\n\tdone");
        assert_eq!(plain(&terminal_text(raw)), "ok linkfail\n\tdone");
        assert_eq!(plain("50%\r100%\nnext"), "100%\nnext");
    }

    #[test]
    fn an_edit_is_its_changed_lines_with_a_few_around_them() {
        let old = (1..=20).map(|n| format!("line {n}")).collect::<Vec<_>>().join("\n");
        let new = old.replace("line 3", "line three").replace("line 18", "line eighteen\nline 18b");
        let diff = diff("src/a.rs".into(), &old, &new);
        assert_eq!((diff.added, diff.removed), (3, 2));
        assert_eq!(diff.lines[..6], [" line 1", " line 2", "-line 3", "+line three", " line 4", " line 5"]);
        assert!(diff.lines.contains(&"@@".to_string()));
        assert_eq!(diff.lines.last().unwrap(), " line 20");
        let new_file = super::diff("b.txt".into(), "", "a\nb");
        assert_eq!((new_file.lines, new_file.added), (vec!["+a".to_string(), "+b".to_string()], 2));
    }

    #[test]
    fn an_entry_changes_in_place_and_an_app_asks_only_for_what_changed() {
        let mut entries = Entries::default();
        entries.put("said:1".into(), Entry::text("said", "Look"));
        entries.put("tool:t1".into(), Entry { kind: "tool".into(), tool: Some(Tool { title: "git status".into(), status: "in_progress".into(), ..Tool::default() }), ..Entry::default() });
        let (all, rev, first) = entries.since(None);
        assert_eq!((all.len(), rev, first), (2, 2, 0));
        entries.put("said:1".into(), Entry::text("said", "Looking"));
        let (changed, rev, _) = entries.since(Some(2));
        assert_eq!((changed.len(), changed[0].seq, changed[0].text.as_str(), rev), (1, 0, "Looking", 3));
        // A new run's keys do not land on an earlier run's entries.
        entries.new_run();
        entries.put("said:1".into(), Entry::text("said", "Again"));
        assert_eq!(entries.all().iter().map(|entry| entry.seq).collect::<Vec<_>>(), [0, 1, 2]);
        let restored = Entries::restore(entries.all());
        assert_eq!(restored.since(None).1, 4);
        // Cut where a character begins: the euro sign's three bytes are kept whole or not at all.
        assert_eq!(tail("ab€cd", 4), "cd");
        assert_eq!(tail("ab€cd", 5), "€cd");
    }
}
