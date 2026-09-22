---
description: Create, lock, or switch a NeoCortex issue 
usage: /issue | /issue 42 | /issue docs/request.md | /issue "Title" | /issue lock | /issue 42 lock | /issue switch 42
agent: neocortex
---

Active environment:
!`orbit neocortex which || echo "NO_ACTIVE_RUN"`

## Your job

Interpret the arguments: $ARGUMENTS

Route by argument shape (do this yourself, do not ask me which mode):

| Arguments | Mode |
|---|---|
| `lock` | Lock the ACTIVE issue |
| `<number> lock` (e.g. `7 lock`) | Lock issue 7 |
| `switch <number>` (e.g. `switch 7`) | Change the ACTIVE issue pointer |
| empty | Create — Interactive mode |
| bare number or issue URL (e.g. `42`) | Create — Remote mode |
| path ending in `.md` | Create — File mode |
| anything else | Create — Interactive mode, treating the text as the initial feature description |

## Lock / Switch procedures

### Lock (active or pointed issue)
1. Run via bash: `orbit neocortex issue lock` (add `--issue=<number>` if one
   was given).
2. If it fails with an unfilled-placeholder error, read the named sections,
   propose content for them via the `question` tool, write approved text,
   and retry once.
3. On success, report the lock confirmation. Remind me: requirement changes
   from now on go through `/addenda`.

### Switch
1. Run via bash: `orbit neocortex issue switch <number>`.
2. Report the new ACTIVE issue and its status from
   `orbit neocortex issue list`.

## Create procedures

### Interactive mode
1. Use the `question` tool to interview me about the feature: the problem,
   the motivation, and anything already decided. Batch questions with
   concrete options where possible.
2. Once you understand the feature, propose a short title via the question
   tool and wait for my approval.
3. Run via bash:
   `orbit neocortex issue new --title "<approved title>" --interactive`
4. Read the generated `00-concept.md`.
5. Replace the `<!-- Agent: ... -->` placeholder in **Detail** with the
   agreed problem description from our conversation (verbatim what we
   agreed — do not embellish).
6. For **Objective**, **Requirements**, and **Scope & Boundaries**:
   propose the content to me via the `question` tool FIRST, get approval
   or corrections, THEN write the approved text over the placeholders.
7. When all placeholders are gone, show me the final concept summary and
   ask (via `question` tool): "Lock this concept now?"
   - If yes → run `orbit neocortex issue lock` and report the result.
   - If no → stop and tell me I can lock later with `/issue lock`.

### Remote mode
1. Run via bash:
   `orbit neocortex issue new --title "<short title you derive>" --from-remote <number-or-url>`
2. Read the generated `00-concept.md` — Detail is already injected verbatim
   from the remote issue. Do NOT rewrite it.
3. Follow steps 6–7 from Interactive mode (propose Objective, Requirements,
   Scope via `question` tool, then lock on my approval).

### File mode
1. Run via bash:
   `orbit neocortex issue new --title "<short title you derive>" --from-file <path>`
2. Read the generated `00-concept.md` — Detail is already injected verbatim.
   Do NOT rewrite it.
3. Follow steps 6–7 from Interactive mode.

## Hard rules
- If `orbit neocortex which` failed (NO_ACTIVE_RUN) and the requested mode
  needs an active environment, tell me to run `orbit neocortex install` in
  the terminal first. Do not attempt to create directories yourself.
  (Exception: `issue new` works without a pre-existing ACTIVE pointer — the
  CLI sets it.)
- Never hand-create or edit anything in `.neocortex/` except replacing
  `<!-- Agent: ... -->` placeholders in the concept file.
- Never touch the frontmatter (anything above the `---` delimiter). The CLI
  owns `Status`, `Locked-At`, and `Lock-Hash`.
- If `issue lock` fails with an unfilled-placeholder error, fill the named
  sections (with my approval) and retry once.
- After lock, the concept is immutable. Any later requirement change goes
  through `/addenda`.
