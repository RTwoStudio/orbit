---
description: Scaffold and interactively define a new task
usage: /new-task T1 "Setup Auth"
agent: neocortex-task-creator
subtask: false
---

Task ID: $1
Task Name: $2

Active NeoCortex run:
!`orbit neocortex which || echo "NO_ACTIVE_RUN"`

## Your job

Scaffold task $1 via the CLI and fill its placeholders with me, section by
section. Deliberately STOP at `Status: Open` — implementation is a separate
command with a separate context.

## Procedure

1. Read the path above. If it failed (NO_ACTIVE_RUN), stop and tell me to
   run `orbit neocortex install` / `orbit neocortex issue switch <n>` in
   the terminal. Do not improvise.
2. Read `.neocortex/CONVENTIONS.md` (if it exists — its rules override your
   defaults) and `01-plan.md` frontmatter.
3. Preflight:
   - Concept `Status: Draft` → STOP. Tell me to finish `/issue` first.
   - Plan `Status: Draft` → STOP. Tell me to finish `/plan` first.
   - Plan `Status: Locked` → continue.
4. Find $1's row in the plan Task DAG. If $1 does not exist in the DAG
   (check the plan's Amendments section too), STOP — new tasks enter the
   DAG via `/addenda`, never directly.
5. Run via bash: `orbit neocortex task new $1 "$2"`.
   - If the CLI reports the task ALREADY EXISTS: read the existing
     `tasks/$1.md`. If it still has `<!-- Agent:` placeholders, skip to
     step 7 and resume filling. If it is fully filled, tell me it is
     already defined and stop — suggest `/implement $1` or
     `orbit neocortex task show $1` for review. Never re-scaffold.
   - If the CLI warns that a dependency is not yet `Close`, relay the
     warning to me and ask whether to proceed. Never decide this yourself.
6. Read the generated `tasks/$1.md` — note the CLI-prefilled `Blocked By`,
   `Blocks`, and frontmatter (never touch those).
7. Use the `question` tool to ask me 1–2 clarifying implementation
   questions, with concrete options where possible.
8. Replace the `<!-- Agent: ... -->` placeholders section by section:
   propose each section's content, get my approval, then write it.
   Base Objective, Context, and Related strictly on the plan row and
   concept — do not invent scope.
9. When all placeholders are gone, show me a summary and tell me:
   - Review the file, then run `/implement $1` when ready to start coding.
   - Or run `/new-task <next-id> "<name>"` to define another task first.

## Hard rules
- Leave `status: Open` in frontmatter. Never touch any frontmatter key —
  the CLI owns `status`, `dependsOn`, `origin`, `amendments`.
- Do not start coding. Do not run tests. Do not set any other status.
- Only write inside `tasks/$1.md`, only replacing
  `<!-- Agent: ... -->` placeholders.
- If the task state or plan state makes the request invalid, say so and
  name the correct command. Do not improvise around the protocol.