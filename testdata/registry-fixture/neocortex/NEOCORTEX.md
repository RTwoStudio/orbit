# NEOCORTEX

Quick reference for the Orbit NeoCortex workflow in this project.
Full protocol lives in the deployed OpenCode agents (`~/.config/opencode/agents/neocortex*.md`).
State lives in `.neocortex/` (gitignored). Structure belongs to the CLI — never hand-create files there.

## Rules

- CLI owns structure. Agents only fill `<!-- Agent: ... -->` placeholders, with Orchestrator approval.
- Frontmatter (above `---`) is CLI-owned. Never hand-edit.
- Locked files (concept, plan) are immutable. Changes go through addenda.
- Status changes only via CLI: `orbit neocortex task status <ID> --set=<status>`.
- Only the human sets task `Close` / `Rework` and approves addenda.

## Lifecycle

- Concept: Draft → Locked
- Plan: Draft → Locked
- Addenda: Draft → Approved → Applied
- Task: Open → In Progress → Revise → Rework → Close

## Commands

- `/issue` — create/lock/switch an issue
- `/plan` — design architecture, build and lock the plan
- `/new-task T1 "Name"` — JIT-scaffold and define one task
- `/implement T1` — execute one task in isolation
- `/addenda "Title"` — pivot the locked plan (or record a note)
- `orbit neocortex status` — where am I?

## Project Conventions

<!-- Project-specific bullets go here: naming, test commands, forbidden patterns. -->
<!-- (Kept minimal here; full rules may live in .neocortex/CONVENTIONS.md.) -->