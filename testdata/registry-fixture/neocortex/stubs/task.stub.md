---
id: {{TASK_ID}}
name: "{{TASK_NAME}}"
issue-id: {{ISSUE_ID}}
status: Open
dependsOn: [{{DEPENDS_ON}}]
origin: {{ORIGIN}}
amendments: []
created: {{DATE}}
registry-version: {{REGISTRY_VERSION}}
---

# {{TASK_ID}} — {{TASK_NAME}}

> **Status lives in the frontmatter above** (single source of truth) and MUST
> only change via the CLI: `orbit neocortex task status {{TASK_ID}} --set=<status>`.
> The CLI enforces valid transitions — skipped states are refused.
>
> Lifecycle: `Open → In Progress → Revise → Rework → Close`
> The Worker Agent may set `In Progress` and `Revise` (and return `Rework`
> to `In Progress` after fixing). The Worker Agent is STRICTLY FORBIDDEN
> from setting `Close` or `Rework` — those belong to the Orchestrator.
> Never touch anything above the `---` frontmatter delimiter: the CLI owns
> `status`, `dependsOn`, `origin`, and `amendments`.

- **Priority:** <!-- Agent: High / Medium / Low — agree with the Orchestrator -->

- **Objective & Scope:**
    <!-- Agent: Clear, concise description of what needs to be achieved in
    this specific task. Before filling, read this task's row in the plan's
    Task DAG and ask the Orchestrator 1–2 clarifying questions via the
    question tool. Propose; write only after approval. -->

- **Context & Background:**
    <!-- Agent: Why is this being done? What is the current behavior or
    problem? Reference 00-concept.md and the relevant Architectural
    Decisions in 01-plan.md. Propose; write only after approval. -->

- **Task Dependencies & Blocking Matrix:**
  - **Blocked By:** {{BLOCKED_BY}} <!-- CLI-prefilled from dependsOn — never edit -->
  - **Blocks:** {{BLOCKS}} <!-- CLI-computed inverse — never edit -->
  - **Related:** <!-- Agent: Other Task IDs or None — from this task's row
      context in the plan DAG. -->

- **Input Requirements & Prerequisites:**
    <!-- Agent: Specific files, configs, or documentation required to start.
    Propose; write only after approval. -->

- **Step-by-Step Action Plan:**
    <!-- Agent: Ordered implementation steps, agreed with the Orchestrator
    via the question tool, one step per line. -->

- **Expected Deliverables & Acceptance Criteria:**
    <!-- Agent: Concrete, verifiable criteria. Each one must map to a
    Verification checkbox below. Propose; write only after approval. -->

- **Technical Notes:**
    <!-- Agent: Constraints, trade-offs, warnings, architectural notes. -->

- **Open Questions / Decisions:**
  - [ ] <!-- Agent: Question/Decision 1 -->

- **Verification:**
  - [ ] <!-- Agent: Verification step or test 1 -->

- **Completion Notes:**
    <!-- Agent: Fill ONLY when transitioning to `Revise` (the CLI refuses
    the transition while this is empty). Summarize what was actually
    implemented, any deviations from the plan, and the final footprint
    (files created/modified/deleted). -->

## Orchestrator Feedback (Rework)

<!-- Human: write rework feedback here when setting Status: Rework. One
numbered point per issue. Leave empty otherwise. The Worker Agent must
address every point explicitly before returning the task to In Progress. -->