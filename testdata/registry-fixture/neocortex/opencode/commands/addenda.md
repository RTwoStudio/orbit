---
description: Record a plan pivot or informal note
usage: /addenda "Title" | /addenda note "Text" | /addenda apply [N]
agent: neocortex-planner
subtask: false
---

Active NeoCortex run:
!`orbit neocortex which || echo "NO_ACTIVE_RUN"`

## Your job

Interpret the arguments: $ARGUMENTS

Route by argument shape (do this yourself, do not ask me which mode):

| Arguments | Mode |
|---|---|
| `apply` or `apply <N>` | Apply a pending addenda to the plan |
| starts with `note` | Informal note — NOT a structural change |
| a quoted title or any text | Official addenda |
| empty | Ask me via the `question` tool what I want: an official addenda or a note |

## Apply procedure
1. If no number given, run `orbit neocortex addenda list` and ask me which
   one (via `question` tool, offering the pending ones).
2. Read the addenda file. Preflight checks — all must pass, else STOP and
   report:
   - `Status: Draft` (not yet applied — `Applied-At` empty).
   - No `<!-- Agent:` placeholders remain.
   - The Orchestrator Approval checkbox is TICKED. If unticked, walk me
     through Reasoning, Impact, and Plan Changes via the `question` tool.
     I tick the box myself in the file when satisfied. You must NEVER tick
     it for me.
3. Run via bash: `orbit neocortex addenda apply <N>`.
4. If it refuses (invalid delta, broken topological order), report the
   exact error, propose a corrected delta via the `question` tool, write
   it, and retry once.
5. On success, report: what changed in the plan (check the plan's new
   Amendments line), and the new effective task list from
   `orbit neocortex status`.

## Official addenda procedure
1. Preflight: read `01-plan.md` frontmatter. If `Status: Draft`, the plan
   is not locked — STOP and tell me to finish `/plan` first. Addenda only
   exist against a locked plan.
2. Run via bash: `orbit neocortex addenda new "$1"` (or with the title I
   gave). The CLI assigns the number.
3. Read the generated stub.
4. Use the `question` tool to work through it with me, section by section:
   Reasoning → Impact Analysis → Plan Changes. Propose each section's
   content, get my approval, then write it. Small pivots are fine — one
   line per section; do not pad.
5. Validate the delta before proposing it: ADD IDs don't exist yet,
   REMOVE/MODIFY IDs exist, the resulting DAG stays topologically valid.
6. When filled, present the summary and tell me: review the file, tick the
   approval box, then run `/addenda apply <N>` (or ask me and I will).

## Note procedure (informal, non-structural)
1. Confirm via the `question` tool that this is NOT a structural change.
   If it adds/removes/modifies tasks, it's an official addenda — reroute.
2. Create `notes/<short-kebab-name>.md` with the content I gave, plus a
   one-line `Created:` date header. This is the ONLY file you may
   hand-create in `.neocortex/`.
3. Remind me: notes are advisory context — they never alter the DAG and
   are compiled as an appendix at close.

## Hard rules
- If `orbit neocortex which` failed, tell me to run
  `orbit neocortex install` / `orbit neocortex issue switch <n>` first.
  Do not improvise.
- Never edit `01-plan.md` directly — the CLI applies approved deltas only.
- Never touch frontmatter in any `.neocortex/` file.
- Never tick the Orchestrator Approval checkbox yourself.
- Hand-creating files is allowed ONLY under `notes/`.