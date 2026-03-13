# plan-to-ralph Skill Design

**Date:** 2026-03-12
**Status:** Approved

## Overview

`/plan-to-ralph` is an interactive Claude Code skill that generates the three ralph loop files (`RALPH.md`, `docs/reference/focus-areas.md`, `docs/reference/gaps-identified.md`) through a guided Q&A process. It scans the codebase to discover components, suggests focus areas and pairings, generates a persona, and produces ready-to-use loop files customized for the project.

**Scope:** This skill generates loop files only — it does not submit jobs to ralph-o-matic. After running `/plan-to-ralph`, use `/direct-to-ralph` or `ralph-o-matic submit` to start the loop. This separation is intentional: generating the loop configuration and submitting a job are distinct concerns, and users often want to review or tweak files before submitting.

**RALPH.md format:** RALPH.md is purely a prompt consumed by the Claude subprocess inside the ralph loop. The executor does not parse its structure — it only detects `<promise>FINIT</promise>` and `<promise>CLOSER</promise>` tags in stdout to determine loop continuation. FINIT means all work is done; CLOSER means more iterations are needed.

## Prerequisites

- Must be in a git repository. If not, abort with: "This skill requires a git repository. Initialize one with `git init` first."

## Arguments

- `CONTEXT` (optional, positional) — Free-text description of what the loop should focus on (e.g., "on the work on the new identity system"). When provided, narrows the codebase scan, pre-seeds the persona, and skips questions that can be answered from context. Follows the same pattern as `IDEA` in `brainstorm-to-ralph`.
- `--reset` — Skip backup and overwrite any existing files directly. This is destructive — existing RALPH.md, focus-areas.md, and gaps-identified.md are overwritten without creating historical copies.

## Phases

### Phase 1: Backup

If any of the 3 target files exist, copy them to `docs/reference/historical/` before overwriting.

**Naming:** `YYYY-MM-DD-historical-RALPH.md`, `YYYY-MM-DD-historical-focus-areas.md`, `YYYY-MM-DD-historical-gaps-identified.md`

**Same-day collision:** Append counter: `-2`, `-3`, etc.

**Skip conditions:**
- No existing files → skip silently
- `--reset` flag → skip entirely

### Phase 2: Scan

Use an Explore subagent (Agent tool with `subagent_type: "Explore"`) to discover codebase structure. This keeps scan results out of the main conversation context. Language-agnostic discovery:

**Structure discovery:**
- Top-level directories: `src/`, `internal/`, `lib/`, `cmd/`, `app/`, `packages/`, `pkg/`, `modules/`
- Entry points: `main.go`, `main.py`, `index.ts`, `Program.cs`, `app.py`, `server.ts`, `Dockerfile`
- Config/infra: `.github/workflows/`, `docker-compose.yml`, CI configs, migration directories
- Test directories: `tests/`, `test/`, `*_test.go`, `*.test.ts`, `test_*.py`, `*.spec.ts`

**Grouping:** Files grouped by directory/module to form candidate components.

**Context narrowing (when CONTEXT provided):**
- Identify components matching the context description
- Find everything touching those components: same package, co-located files, shared config, related tests
- Use directory co-location and naming patterns (not full import graph parsing) to infer relationships — this keeps the scan language-agnostic and fast
- Mark components as "direct" (matches context) or "indirect" (touches direct components)

**Empty project:** If the scan discovers zero candidate components, skip the scan-based suggestions in Q&A and ask the user to define focus areas manually.

**Output:** Structured list of candidate focus areas grouped as:
- Core components (directly related to context, or all if no context)
- Supporting components (touched by core, config, infra)
- Test coverage (test directories mapped to components)

### Phase 3: Q&A

Interactive questions, one at a time. Order:

1. **Mission** — "What is this loop reviewing?" Skip if CONTEXT provides a clear answer.

2. **Test command** — Auto-detect from project files (Makefile targets, package.json scripts, pyproject.toml, go.mod presence, etc.). Propose detected command for confirmation. Accept override. If no test command is detected, ask the user to provide one. The test command is required — it appears in the checklist and iteration structure.

3. **Persona** — Generate a persona from codebase context and Q&A answers so far. Show it and ask:
   - Accept as-is
   - See 3 alternative persona suggestions (plus the original, so 4 total)
   - Type their own

   If they ask for alternatives, show a menu of 4 personas (including original). They can pick one or type their own.

4. **Focus areas (single)** — Present discovered components as a numbered list grouped by category. The user responds with which numbers to include, exclude, or add. They can also type new focus areas not in the list. Each focus area includes: name, key files, and a one-line review scope description. Single areas get 2 review passes (pass 1 = correctness & coverage, pass 2 = robustness & extensibility).

5. **Focus areas (paired)** — Present suggested pairings as a numbered list. The user responds with which to include, exclude, or add. Suggested pairings are based on:
   - Import/dependency relationships from scan
   - When CONTEXT provided: the context system paired with everything touching it (direct and indirect)
   - Common integration seams (API + DB, client + server, config + all components, tests + components)

   User checks/unchecks/adds pairings. Each paired area includes a description of what to verify at the seam.

6. **Checklist** — Present checklist for approval:
   - 3 universal items (always included):
     - All tests pass (`{detected test command}`)
     - No open issues in `docs/reference/gaps-identified.md` for this focus area
     - The focus area is complete and polished — you'd be proud to ship it
   - 1-3 proposed items based on what we've learned (e.g., security items if security was mentioned, boundary checks if hexagonal architecture detected, spec alignment if a design doc exists)

   User can add, remove, or edit items.

7. **Additional constraints** — "The standard constraints (no sub-agents, read before write, one focus area per iteration, don't invent functionality) are always included. Any project-specific constraints to add?" Accept freeform or skip.

### Phase 4: Generate

Write all 3 files using the Q&A results:

**`RALPH.md`:**
- Standard preamble: "You are in an automated prompt loop and the user is unavailable for input..."
- Persona section (approved persona text)
- Mission section (scoped to user's description)
- Tracking system section (references `docs/reference/focus-areas.md`, 2 passes for single areas, 1 pass for paired areas)
- Constraints section (standard set + any user-added constraints)
- Iteration structure (read tracking → read code → analyze findings → fix most important issue → run tests → assess checklist)
- Checklist section (approved items, all unchecked)
- Wrap up section (pass/fail determination, conditional tracking update, commit and push, FINIT/CLOSER promise tags)

**`docs/reference/focus-areas.md`:**
- Table 1: Single area reviews — each row with `#`, focus area name + key files + review scope, Pass 1 `[ ]`, Pass 2 `[ ]`, Status blank
- Table 2: Paired area reviews — each row with `#`, pair description + seam verification scope, Pass `[ ]`, Status blank
- Review guidance section (pass 1 = correctness & coverage, pass 2 = robustness & extensibility, paired = integration verification)
- All checkboxes start unchecked

**`docs/reference/gaps-identified.md`:**
- Open Issues: `_(none)_`
- Fixed Previously: `_(none yet)_`
- Won't Fix (Beyond Current Scope): empty with note that only the user can move items here

### Phase 5: Review

Show the user a summary:
- Number of single focus areas and paired areas generated
- The persona (first line)
- The test command
- The checklist items
- Ask: "Want to review or edit any of the generated files before I commit?" If yes, show the requested file content. If they make manual edits, re-read the file to acknowledge changes. If no, proceed to commit.

### Phase 6: Commit

Stage and commit all generated files plus any historical backups:
- `RALPH.md`
- `docs/reference/focus-areas.md`
- `docs/reference/gaps-identified.md`
- `docs/reference/historical/*` (if backups were created)

Commit message: `chore: generate ralph loop files via plan-to-ralph`

## Standard Constraints (always included in RALPH.md)

These are included in every generated RALPH.md without asking:

- **Do NOT use sub-agents for bulk generation.** When you modify or create code, do it by hand, one component at a time, with thought behind each decision.
- **Read before you write.** Before modifying any file, read the relevant sections. Before claiming something is fine, read it and reason about quality.
- **One focus area per iteration.** Don't try to fix everything at once. Pick the most important remaining issue within the focus area, fix it well, then let the next iteration handle the next issue.
- **Do NOT invent new functionality to fill perceived gaps.** Maintain a list of things you find that should be fixed at `docs/reference/gaps-identified.md` in the `## Open Issues` section. If you perceive there is new, missing functionality beyond the current scope, log it in the `## Won't Fix (Beyond Current Scope)` section. If something on the list has been fixed in a previous loop, move it to `## Fixed Previously`.

## Files Created/Modified

**New files:**
- `skills/plan-to-ralph/skill.md` — Skill definition
- `skills/plan-to-ralph/manifest.json` — Skill metadata

**Modified files:**
- `scripts/install.sh` — Add `plan-to-ralph` to skills array
- `scripts/install.ps1` — Add `plan-to-ralph` to skills array

**No changes needed:**
- `Makefile` — `package-skills` target already iterates `skills/*/`

## Manifest

`skills/plan-to-ralph/manifest.json`:

```json
{
  "name": "plan-to-ralph",
  "version": "1.0.0",
  "description": "Interactive Q&A to generate ralph-o-matic loop files (RALPH.md, focus-areas.md, gaps-identified.md) customized for your project",
  "author": "ryan",
  "commands": [
    {
      "name": "plan-to-ralph",
      "description": "Generate ralph loop files through guided Q&A with codebase scanning",
      "usage": "/plan-to-ralph [\"<context description>\"] [--reset]"
    }
  ],
  "dependencies": {
    "tools": ["git"]
  }
}
```

No plugin dependencies — the skill is self-contained.

## Packaging & Installation

The skill ships as `plan-to-ralph-skill.tar.gz` (Unix) and `plan-to-ralph-skill.zip` (Windows) in release artifacts. The Makefile `package-skills` target picks it up automatically by iterating `skills/*/`. Both installers download and extract to `~/.claude/skills/plan-to-ralph/` (Unix) or `%USERPROFILE%\.claude\skills\plan-to-ralph\` (Windows).
