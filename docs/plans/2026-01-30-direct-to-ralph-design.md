# Direct-to-Ralph Skill Design

## Overview

A new `/direct-to-ralph` slash command that provides a lightweight path to submit existing code on the current branch to ralph-o-matic for iterative refinement. Skips brainstorming, planning, and execution — just collects parameters via interactive Q&A and submits.

### Use Cases

1. Existing code on a branch that needs polish/iteration
2. Sending code back for further refinement after it emerges from a previous ralph loop

## Q&A Flow

All confirmations use `AskUserQuestion`. No CLI flags — everything is collected interactively.

### Step 1: Repo & Branch

- Auto-detect repo URL and branch from git (`git remote get-url origin`, `git branch --show-current`)
- Show detected values to user for confirmation
- Allow override if wrong

### Step 2: Prompt Mode

Ask user to choose one of:

- **Bounded template** — Standard iteration prompt with spec file reference and `<promise>COMPLETE</promise>` exit criteria
- **Open-ended template** — Polish prompt with no exit criteria (runs until stopped)
- **Custom prompt** — User types the prompt directly

If bounded or open-ended is selected:
- Auto-detect most recent file in `docs/plans/` as the spec reference
- Confirm with user, allow them to specify a different path

### Step 3: Max Iterations

- Default: 50
- Ask user to confirm or change

### Step 4: Priority

- Default: normal
- Ask user to confirm or change (high / normal / low)

## Pre-flight Checks

Run after Q&A, before submission:

1. **Uncommitted changes** — If dirty tree, show `git status` and offer to commit+push with confirmation. Abort if user declines.
2. **Branch pushed to origin** — If not pushed, push automatically with `-u`.
3. **Tests exist** — If no test files found, warn but continue (not a blocker).
4. **Server reachable** — Check via `ralph-o-matic status`. If unreachable, show troubleshooting and abort.
5. **Branch not in queue** — If already queued, show existing job ID and abort.

## Submission

1. Write prompt to `RALPH.md`
2. Commit and push
3. Run `ralph-o-matic submit` with collected parameters
4. Display success banner with job ID, dashboard link, and `logs --follow` command

## Changes to brainstorm-to-ralph

Add `--prompt "..."` flag to the existing skill. In Phase 5:

- If `--prompt` provided: use verbatim as RALPH.md content
- If `--open-ended` provided: use open-ended template (existing behavior)
- Otherwise: use bounded template (existing default)

## File Changes

- **New:** `~/.claude/skills/direct-to-ralph/manifest.json`
- **New:** `~/.claude/skills/direct-to-ralph/skill.md`
- **Modified:** `~/.claude/skills/brainstorm-to-ralph/skill.md` (add `--prompt` flag support)
- **Modified:** `~/.claude/skills/brainstorm-to-ralph/manifest.json` (add `--prompt` to usage)
