# The skill format

Lorca reads skills in the open agentskills.io format, the same one Claude Code, Codex, and Cursor read.

```
weekly-report/
├── SKILL.md          required: the frontmatter and the steps
├── references/       optional: text read when a step needs it
├── scripts/          optional: helpers a step runs
└── assets/           optional: templates and other files
```

## SKILL.md frontmatter

| Field | Needed | Notes |
| --- | --- | --- |
| `name` | yes | Matches the folder's name. Lowercase letters, numbers, single hyphens. At most 64. |
| `description` | yes | What it does and when to use it. At most 1024 characters. |
| `license` | no | A license name or a bundled file. |
| `compatibility` | no | What the skill needs: a tool, a package, network access. |
| `metadata` | no | Free key and value pairs. |

Lorca keeps the fields it does not use when the app saves a skill.

## A checklist

- The description says what the skill does and when, in the words a request would use.
- The steps are in order, and each says what to do, not only what to think about.
- Anything long lives in `references/`, named in the steps.
- A script is named in the steps, with what it needs and what it prints.
- Nothing in the folder is a password, a key, or a person's private detail.
- A request the description matches was tried once, start to finish.
