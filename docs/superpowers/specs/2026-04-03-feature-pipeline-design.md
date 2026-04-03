# Feature Pipeline Automation Design

**Date:** 2026-04-03
**Status:** Approved
**Goal:** Automate the full feature development workflow so the user only interacts during brainstorm Q&A (steps 1–2), then walks away while steps 3–9 run unattended.

## Current Workflow (10 Steps)

| Step | Name | Interactive? |
|------|------|-------------|
| 1 | Feature Spec Production | Yes — brainstorm Q&A |
| 2 | Feature Design Production | Yes — brainstorm Q&A |
| 3 | Design Doc Alignment | Manual prompt, auto execution |
| 4 | Implementation Plan Production | Manual prompt, auto execution |
| 5 | Implementation Plan Alignment | Manual prompt, auto execution |
| 6 | Draft Implementation | Manual prompt, auto execution |
| 7 | Plan-to-Ralph Prep | Yes — 7-question Q&A |
| 8 | Ralph-o-matic Submission | Manual submission |
| 9 | PR Review | Semi-manual |
| 10 | Done | — |

## Target Workflow

| Step | Name | Interactive? |
|------|------|-------------|
| 1 | Feature Spec Production | **Yes — brainstorm Q&A** |
| 2 | Feature Design Production | **Yes — brainstorm Q&A** |
| 3–6 | spec-to-design | Automated |
| 7 | auto-ralph-prep | Automated |
| 8 | auto-ralph-submit | Automated |
| 9 | Post-completion hook → PR review | Automated (server-triggered) |

User interacts during steps 1–2 only. Everything else runs unattended with Teams notifications on progress and failure.

---

## Architecture

### Skill Decomposition

Four new skills in `../dbinky-skill-set/skills/`:

```
skills/
├── spec-to-design/SKILL.md      # Steps 3–6: alignment → plans → alignment → implementation
├── auto-ralph-prep/SKILL.md     # Step 7: auto-generate RALPH.md + tracking files (no Q&A)
├── auto-ralph-submit/SKILL.md   # Step 8: pre-flight + submit to ralph
└── feature-pipeline/SKILL.md    # Master orchestrator: chains everything
```

One server change in ralph-o-matic:
- Post-completion hook in `internal/worker/hook.go`

One skill modification:
- `plan-to-ralph` gets a `--auto` flag for non-interactive mode

### Composition Model

```
feature-pipeline (master)
  ├── superpowers:brainstorming  ← INTERACTIVE (spec)
  ├── superpowers:brainstorming  ← INTERACTIVE (design)
  ├── spec-to-design             ← AUTO (steps 3–6)
  ├── auto-ralph-prep            ← AUTO (step 7)
  └── auto-ralph-submit          ← AUTO (step 8)
                                    ↓
                              ralph-o-matic server runs the loop
                                    ↓
                              post-completion hook fires
                                    ↓
                              claude --print with pr-review + "all but defer"
```

Each sub-skill is independently invocable.

### File Naming Convention

The master orchestrator derives a slug from the feature name (e.g., "user authentication system" → `user-auth`). All downstream paths use this slug:

- `docs/specs/{slug}-spec.md`
- `docs/superpowers/specs/{slug}-design-phase-*.md`
- `docs/superpowers/plans/{slug}-implementation-phase-*-task-*.md`

Sub-skills accept explicit paths so they work independently of the orchestrator.

---

## Sub-skill: spec-to-design

**Purpose:** Run steps 3–6 (design alignment, plan production, plan alignment, draft implementation) with zero interaction.

**Inputs:**
- `SPEC_PATH` — path to the feature spec
- `DESIGN_GLOB` — glob for design phase docs
- `PLAN_GLOB` — auto-derived from slug or explicitly provided

### Phase 1 — Design Alignment (Step 3)

Read the spec and all design docs. Review for alignment to the spec's intent and internal coherence across docs (terminology, data models, use cases, integration seams). Fix contradictions in-place. Commit: `docs: align design phases to spec`.

### Phase 2 — Implementation Plan Production (Step 4)

Invoke `superpowers:writing-plans` for each design phase doc. Each phase may produce multiple task files. Output: `docs/superpowers/plans/{slug}-implementation-phase-{N}-task-{M}.md`. Commit: `docs: write implementation plans`.

### Phase 3 — Plan Alignment (Step 5)

Read spec, designs, and all plan files. Review the plans holistically for coherence with each other and with the spec/designs. Fix contradictions in-place. Commit: `docs: align implementation plans`.

### Phase 4 — Draft Implementation (Step 6)

Read all plan files, analyze dependencies between phases/tasks. Spawn parallel subagents (via `superpowers:subagent-driven-development`) for independent tasks. Each subagent uses `superpowers:executing-plans` with its task file. Run full test suite after all agents complete. Commit implementation.

**Failure behavior:** If any phase fails, send Teams notification with phase name, error details, and paths to all artifacts produced so far. Stop — don't continue to next phase.

---

## Sub-skill: auto-ralph-prep

**Purpose:** Replace the interactive plan-to-ralph Q&A with fully automated derivation from spec and design artifacts.

**Inputs:**
- `SPEC_PATH` — the feature spec
- `DESIGN_GLOB` — the design phase docs
- `PLAN_GLOB` — the implementation plan docs

### Auto-Derived Answers

| Question | Derivation |
|----------|------------|
| Mission | Extracted from spec's purpose/goal section |
| Test command | Auto-detected from project (Makefile, package.json, etc.) |
| Persona | Generated from spec context — senior engineer reviewer focused on the feature's domain |
| Single focus areas | Derived from implementation plan files — each plan task becomes a focus area; slightly expanded coverage. Each gets 2 review passes. |
| Paired focus areas | Derived from cross-references between plan tasks — shared models, APIs, or data flows become pairs. 1 review pass each. |
| Checklist | 3 universal items (tests pass, no regressions, code quality) + items derived from spec acceptance criteria |
| Constraints | Pulled from CLAUDE.md + any constraints section in the spec |

### Output

Same 3 files as plan-to-ralph:
- `RALPH.md` — loop prompt with persona, mission, iteration structure, checklist, FINIT/CLOSER tags
- `docs/reference/focus-areas.md` — tracking table
- `docs/reference/gaps-identified.md` — issue tracker

Backs up existing files to `docs/reference/historical/` (same as plan-to-ralph today).

Commits: `chore: generate ralph loop files for {slug}`.

### plan-to-ralph --auto

The existing `plan-to-ralph` skill gets an `--auto` flag. When `--auto` is passed, `plan-to-ralph` delegates to `auto-ralph-prep` — the auto-derivation logic lives in `auto-ralph-prep` only, not duplicated. This gives two entry points (`/auto-ralph-prep` and `/plan-to-ralph --auto`) with a single implementation.

---

## Sub-skill: auto-ralph-submit

**Purpose:** Submit to ralph-o-matic with sensible defaults, no interaction.

**Inputs (all optional with defaults):**
- `--priority` — default: `high`
- `--max-iterations` — default: `200`
- `--local` — default: `true` (use local repo, not clone)

### Flow

1. Pre-flight checks: clean working tree, branch pushed, server reachable, branch not already queued
2. If working tree dirty → commit and push automatically
3. Submit via `ralph-o-matic submit --priority {N} --max-iterations {N} --working-dir {path}`
4. Send Teams notification: "Ralph loop started for {feature} on {branch} — Job #{id}, ~{N} iterations"

Expects `RALPH.md` to already exist from `auto-ralph-prep`.

---

## Master Orchestrator: feature-pipeline

### Invocation

```
/feature-pipeline Here's what I want to build: {thorough description of the product feature}
```

### Flags

- `--slug {name}` — override auto-derived slug (default: derived from feature description)
- `--max-iterations {N}` — override ralph iteration count (default: 200)
- `--priority {level}` — override ralph priority (default: high)
- `--spec-only` — stop after steps 1–2 (produce spec + designs interactively, skip the rest)

### Flow

```
Step 1: Derive slug from feature description
         └─ "user authentication system" → "user-auth"
         └─ Set all downstream paths

Step 2: Invoke superpowers:brainstorming (INTERACTIVE)
         └─ Prompt: produce product spec at docs/specs/{slug}-spec.md
         └─ Focus on product features and outcomes, no implementation details
         └─ Q&A with user until spec approved
         └─ Spec committed

Step 3: Invoke superpowers:brainstorming (INTERACTIVE)
         └─ Prompt: produce implementation designs for the spec
         └─ Multiple phases → docs/superpowers/specs/{slug}-design-phase-*.md
         └─ Q&A with user until designs approved
         └─ Designs committed

         ── USER INTERACTION ENDS HERE ──

Step 4: Notify Teams: "Pipeline running unattended for {slug}"

Step 5: Invoke spec-to-design (AUTO)
         └─ Alignment → Plans → Alignment → Implementation
         └─ On failure → notify Teams, stop

Step 6: Invoke auto-ralph-prep (AUTO)
         └─ Generate RALPH.md + tracking files
         └─ On failure → notify Teams, stop

Step 7: Invoke auto-ralph-submit (AUTO)
         └─ Submit to ralph-o-matic server
         └─ On failure → notify Teams, stop

Step 8: Notify Teams: "Pipeline handed off to ralph. Job #{id}"
         └─ Session ends cleanly

         ── RALPH RUNS OVERNIGHT ──

Step 9: Post-completion hook fires (SERVER)
         └─ claude --print with pr-review + "all but defer"
         └─ On success → Teams: "PR ready for review: {url}"
         └─ On failure → Teams: "PR review failed: {error}"
```

The `--spec-only` flag exits after step 3. Resume later with `/spec-to-design` manually.

---

## Server: Post-Completion Hook

### New Config Key

`post_completion_command` — stored in ralph-o-matic config DB.

### Behavior

When a job transitions to `completed` or `failed` status, the worker:
1. Checks if `post_completion_command` is set
2. Spawns the command as a subprocess with env vars
3. Runs asynchronously — doesn't block the worker
4. Logs stdout/stderr to job logs

### Environment Variables

| Variable | Description |
|----------|-------------|
| `RALPH_JOB_ID` | Job ID |
| `RALPH_REPO_URL` | Repository URL |
| `RALPH_BRANCH` | Source branch |
| `RALPH_RESULT_BRANCH` | Result branch name |
| `RALPH_PR_URL` | Pull request URL (empty if failed/no PR) |
| `RALPH_WORKING_DIR` | Working directory (empty if clone mode) |
| `RALPH_EXIT_STATUS` | `completed` or `failed` |

### Configuration Example

```bash
ralph-o-matic server-config set post_completion_command \
  "claude --print -p 'Run /pr-review on the PR at \$RALPH_PR_URL. Apply all suggested fixes except those ranked Defer. Commit and push the results.'"
```

### Failure Handling

- Hook failure does NOT change the job's status — the ralph work succeeded
- If the command exits non-zero, send a Teams notification with the error
- Hook fires on both `completed` and `failed`, distinguished by `RALPH_EXIT_STATUS`

### Implementation Scope

- `internal/worker/hook.go` — new file, subprocess management
- `internal/worker/worker.go` — trigger point after job finalization
- `internal/models/config.go` — new config key definition

---

## Failure Handling & Teams Notifications

### Notification Points

| When | Message |
|------|---------|
| Steps 1–2 complete, automation begins | "Pipeline running unattended for `{slug}` on branch `{branch}`" |
| spec-to-design fails | "Pipeline failed at `{phase}` of spec-to-design. Error: `{msg}`. Resume: `/spec-to-design --spec {path}`" |
| auto-ralph-prep fails | "Pipeline failed generating ralph loop files. Error: `{msg}`. Resume: `/auto-ralph-prep --spec {path}`" |
| auto-ralph-submit fails | "Pipeline failed submitting to ralph. Error: `{msg}`. Resume: `/auto-ralph-submit`" |
| Submitted to ralph | "Ralph loop started for `{slug}` — Job #{id}, {N} iterations" |
| Ralph completes | "Ralph loop finished for `{slug}` — Job #{id}. PR review starting." |
| Ralph fails/circuit breaker | "Ralph loop failed for `{slug}` — Job #{id}. Error: `{msg}`" |
| PR review succeeds | "PR ready for review: `{pr_url}` — all fixes applied (except Defer)" |
| PR review fails | "Post-completion hook failed for Job #{id}. Error: `{msg}`" |

### Notification Mechanism

New CLI command: `ralph-o-matic notify --message "..."` — sends a message to all configured notification channels. Skills use this command to send arbitrary notifications through the existing Teams/SMTP infrastructure.

### Resume Instructions

All failure messages include the exact command with paths filled in so the user can copy-paste to resume from the failure point.

---

## Modifications to Existing Skills

### plan-to-ralph

Add `--auto` flag that:
- Skips all 7 Q&A questions
- Derives answers from spec + design docs + codebase scan
- Generates the same 3 output files
- Equivalent to invoking `auto-ralph-prep` directly

### direct-to-ralph

No changes needed. `auto-ralph-submit` handles the automated case; `direct-to-ralph` remains the interactive entry point.

---

## Implementation Scope Summary

### New Skills (in `../dbinky-skill-set/skills/`)
1. `spec-to-design/SKILL.md`
2. `auto-ralph-prep/SKILL.md`
3. `auto-ralph-submit/SKILL.md`
4. `feature-pipeline/SKILL.md`

### Server Changes (in ralph-o-matic)
1. `internal/worker/hook.go` — new file: post-completion subprocess management
2. `internal/worker/worker.go` — trigger hook after finalization
3. `internal/models/config.go` — `post_completion_command` config key
4. `cmd/cli/commands.go` — new `notify` subcommand

### Skill Modifications (in `../dbinky-skill-set/skills/`)
1. `plan-to-ralph/SKILL.md` — add `--auto` flag
