---
name: save-skill
description: "Bring skills that other tools or a CLI saved elsewhere, such as .claude/skills or .agents/skills, into Lorca, so every bot or just this one uses them."
---

Use this when the user says they have skills in another tool, asks you to install a skill with a CLI such as `npx skills add`, or asks to bring skills into Lorca. Lorca reads skills only from its own folders, which your system prompt names.

1. Find them. List only the folders in references/locations.md, one `ls` each, and the project folders under your working directory. Never search the home folder recursively.
2. Keep a folder only if it holds a SKILL.md with a description. Leave out the skills Lorca ships (lorca, write-skill, save-skill). Tools often install one copy and link the rest to it, so resolve each with `realpath` and count a skill once.
3. Tell the user what you found: each name, its description, and where it is. Ask once with ask_user which to bring in, where each goes (shared by every bot, yours alone, or this group's), and how:
   - Move, and leave a symbolic link at the old place, so the other tool keeps working. Offer this first.
   - Move only.
   - Copy, and leave the original.
4. For each skill chosen, check the target folder has no skill of that name, then `mv` (or `cp -R`) it in, then `ln -s <new> <old>` when a link was chosen. The folder's name must be the `name:` in its SKILL.md. When it is not a lowercase slug of letters, numbers, and single hyphens, pick one and change the `name:` line too. Where a link cannot be made, as on some Windows setups, copy instead and say so. When the old place is itself a link to the real folder, move the real folder.
5. Say what moved and where it is now.

These skills are somebody else's code. Do not run their scripts to try them; tell the user which scripts a skill holds. Each move outside Lorca's folders goes through Auto-review, and the user may be asked.

When the user wants a skill from a community site instead, the Skill Store in the inspector's Skills section adds it directly.
