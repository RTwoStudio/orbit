---
description: Design the architecture and task DAG for the active issue
usage: /plan | /plan resume
agent: neocortex-planner
subtask: false
---

Active NeoCortex run:
!`orbit neocortex which || echo "NO_ACTIVE_RUN"`

## Your job

Design the architecture for the ACTIVE issue and produce a locked
`01-plan.md`. Work interactively — never dump a finished plan.

## Procedure

1. Read the path above. If it failed (NO_ACTIVE_RUN), stop and tell me to
   run `orbit neocortex install` / `orbit neocortex issue switch <n>` in
   the terminal. Do not improvise.
2. Read, in order: `.neocortex/CONVENTIONS.md` (if it exists — its rules
   override your defaults), `00-concept.md`, any existing `notes/*.md`,
   and `01-plan.md`.
3. Preflight on `00-concept.md` frontmatter:
    - `Status: Draft` → STOP. The concept must be locked first. Tell me to
      finish `/issue` (fill placeholders, then `/issue lock`).
    - `Status: Locked` → continue.
4. Preflight on `01-plan.md` frontmatter:
    - `Status: Locked` → STOP. The plan is immutable. If something must
      change, tell me to run `/addenda "Title"`.
    - `Status: Draft` with existing decisions → this is a RESUME. Continue
      from where it stopped — do not re-ask answered questions.
5. Read the Architectural Decisions and Task DAG rules inside the plan
   stub and follow their exact formats.
6. For every decision question, use the `question` tool: batch related
   questions, give concrete options, and let me type custom answers.
   Section by section: propose → I decide → you write. Never write a
   section before I've agreed to its content.
7. When the Task DAG is drafted, propose the full task list to me for
   approval. Verify before proposing: IDs unique, every dependency exists,
   order is a valid topological order, each task is small enough for one
   JIT execution loop.
8. When no Open Questions remain and every section is filled, ask me via
   the `question` tool: "Lock this plan now?"
    - If yes → run `orbit neocortex plan lock`. If it refuses, fix exactly
      what it reports (with my approval) and retry once.
    - If no → stop; I can lock later with `/plan` (it will resume and offer
      the lock again).

## Hard rules
- You write ONLY inside `.neocortex/**`, and only by replacing
  `<!-- Agent: ... -->` placeholders or appending to Detail/agreed
  sections. Never touch frontmatter.
- Never hand-create or rename files in `.neocortex/`. Exception: informal
  context files under `notes/` — but only if I ask you to record something
  that is NOT a structural change.
- Tasks are single-line DAG entries. Never expand a task into detail —
  that happens Just-In-Time via `/new-task`.
- If the concept or plan state makes the request invalid, say so and name
  the correct command. Do not improvise around the protocol.