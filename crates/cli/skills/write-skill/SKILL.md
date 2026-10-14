---
name: write-skill
description: "How to write and keep a skill, a reusable way of working, so Lorca's bots use it: where the folder goes, the format, and what makes one good."
---

A skill is a folder with a SKILL.md. Write one when the user asks you to keep a way of working, when you have done the same kind of job the same way more than once, or when the user has corrected you on the same point more than once.

## Where it goes

Your system prompt names three skills folders:

- Yours, for you alone. This is the default.
- This group's, when the skill is about the group's work. Every bot in the group uses it.
- The shared one, only when the user asks for every bot on this Runner to have it.

## Write it

Make the folder `<skills folder>/<name>/` and write its SKILL.md with the write tool. Saving is writing the file: the skill is used from your next turn.

```
---
name: weekly-report
description: What the skill does and when to use it, in a sentence or two, with the words a request would use.
---

The steps, short and in order.
```

- `name` is the folder's name: lowercase letters, numbers, and single hyphens, at most 64.
- `description` decides whether a bot reads the skill at all, so say what it does and when. At most 1024 characters.
- Keep SKILL.md short, under about 500 lines. Put detail in `references/<topic>.md` and helpers in `scripts/`, and point to them with paths relative to the skill's folder.
- Write the procedure, not one example of it. Leave out names, private details, passwords, and keys; ask for a secret with request_secret when a step needs one.
- A skill grants no permissions. Its scripts run through bash and Auto-review like any command.
- Do not write `scripts/browser-steps.json` by hand. When the user's message carries a browser recording, its text says how to turn it into that file.

## Check it

Read the file back. Then try a realistic request that its description matches and see that each step works. Tell the user the skill's path so they can open it under Skills in the inspector, or edit the file.

To change a skill, read it, edit its file, and keep its folder name. To remove one, tell the user, then move its folder out of the skills folder.

Skills other tools saved elsewhere, such as in `.claude/skills`, are brought in with the save-skill skill. See references/format.md for a template and a checklist.
