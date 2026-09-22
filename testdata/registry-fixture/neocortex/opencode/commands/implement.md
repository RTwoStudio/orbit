---
description: Execute one task in a clean isolated context
usage: /implement T1
agent: neocortex-implementer
---

Target Task: $1

Active NeoCortex run:
!`orbit neocortex which || echo "NO_ACTIVE_RUN"`

## Your job

Execute task $1 end-to-end: preflight → implement → test → verify →
Completion Notes → `Revise`. One task per invocation. Your context comes
from the files, not from this conversation.

## Procedure

1. Read the path above. If it failed (NO_ACTIVE_RUN), stop and tell me to
   run `orbit neocortex install` / `orbit neocortex issue switch <n>` in
   the terminal. Do not improvise.
2. Follow the agent's Reading order: CONVENTIONS.md (if present),
   `00-concept.md`, `01-plan.md`, `notes/*.md`, then `tasks/$1.md`.
3. Branch on the task's frontmatter status:
    - `Open` → fresh implementation.
    - `Rework` → read `## Orchestrator Feedback (Rework)` first, restate
      each numbered point to me with how you'll address it, then run via
      bash: `orbit neocortex task status $1 --set=in-progress`
    - anything else → STOP and report the state.
4. Implement, following the plan's Architectural Decisions exactly.
   On a plan-vs-reality conflict: STOP, report, keep the status as-is.
5. Run tests. Tick each Verification box only after its check passes.
6. Fill Completion Notes (what was done, deviations, file footprint).
7. Run via bash: `orbit neocortex task status $1 --set=revise`.
8. Report: verification results, the footprint, and that the task awaits
   my audit. Remind me: I review the diff and set `Close` or `Rework`
   myself.

## Hard rules
- Never set `Close` or `Rework` — not via CLI, not by editing the file.
  Your ceiling is `Revise`.
- Never edit task frontmatter directly; every status change goes through
  `orbit neocortex task status`.
- In `.neocortex/`, write only inside `tasks/$1.md` (placeholders,
  Completion Notes, Verification ticks). Never touch any other
  `.neocortex/` file.
- One task per invocation. Do not chain into another task — I will invoke
  `/implement` again for the next one.