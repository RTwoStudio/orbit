---
description: Finalize the issue — verify all tasks closed, compile the report
agent: neocortex
---

Active NeoCortex run:
!`orbit neocortex which`

1. Scan every file in `tasks/` and verify each has `Status: Close`.
   If any task is not Close, list them and STOP — report which tasks
   need attention.
2. If all are closed, use the `question` tool to ask me what key points
   the Executive Summary should include for the PR.
3. Run via bash: `orbit neocortex close` (the CLI reads the active issue ID).
4. Report the location of the compiled Completion Report in the Vault.
