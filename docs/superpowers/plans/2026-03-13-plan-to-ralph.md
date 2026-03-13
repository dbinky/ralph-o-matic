# plan-to-ralph Skill Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `/plan-to-ralph` Claude Code skill that generates ralph loop files (RALPH.md, focus-areas.md, gaps-identified.md) through interactive Q&A with codebase scanning.

**Architecture:** Single skill.md file following the same pattern as `brainstorm-to-ralph` and `direct-to-ralph`. All logic lives in the skill markdown as instructions to Claude Code. A manifest.json provides metadata for skill discovery. Both installers are updated to include the new skill.

**Tech Stack:** Markdown skill definition, JSON manifest, bash/PowerShell installer updates.

**Spec:** `docs/superpowers/specs/2026-03-12-plan-to-ralph-design.md`

---

## Chunk 1: Skill Files & Installer Updates

### Task 1: Create manifest.json

**Files:**
- Create: `skills/plan-to-ralph/manifest.json`

- [ ] **Step 1: Create the manifest file**

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

Write this to `skills/plan-to-ralph/manifest.json`.

- [ ] **Step 2: Verify manifest structure matches existing skills**

Read `skills/brainstorm-to-ralph/manifest.json` and `skills/direct-to-ralph/manifest.json`. Confirm the new manifest follows the same schema (name, version, description, author, commands array, dependencies object).

- [ ] **Step 3: Commit**

```bash
git add skills/plan-to-ralph/manifest.json
git commit -m "feat: add plan-to-ralph manifest.json"
```

---

### Task 2: Create skill.md

**Files:**
- Create: `skills/plan-to-ralph/skill.md`
- Reference: `docs/superpowers/specs/2026-03-12-plan-to-ralph-design.md` (the spec)
- Reference: `skills/brainstorm-to-ralph/skill.md` (pattern to follow for structure)
- Reference: `skills/direct-to-ralph/skill.md` (pattern to follow for Q&A flow)
- Reference: `example-RALPH.md` (template for generated RALPH.md output)
- Reference: `docs/reference/example-focus-areas.md` (template for generated focus-areas.md output)
- Reference: `docs/reference/example-gaps-identified.md` (template for generated gaps-identified.md output)

This is the core deliverable. The skill.md must instruct Claude Code through all 6 phases from the spec. Below is the exact content to write.

- [ ] **Step 1: Write the skill.md file**

Write `skills/plan-to-ralph/skill.md` with the following content. This is the complete file — write it exactly as shown:

````markdown
---
name: plan-to-ralph
description: Interactive Q&A to generate ralph-o-matic loop files (RALPH.md, focus-areas.md, gaps-identified.md) customized for your project
---

# Plan to Ralph

You are generating ralph-o-matic loop files through interactive Q&A. This skill produces three files that configure a ralph refinement loop:

- `RALPH.md` — Loop instructions (persona, mission, iteration structure, checklist, promise tags)
- `docs/reference/focus-areas.md` — Review tracking table (single areas with 2 passes, paired areas with 1 pass)
- `docs/reference/gaps-identified.md` — Issue tracker (open, fixed, won't-fix sections)

After generating these files, the user runs `/direct-to-ralph` or `ralph-o-matic submit` to start the loop.

## Arguments

Parse the following from the user's command:

- `CONTEXT` (optional): Free-text description of what the loop should focus on (e.g., "on the work on the new identity system"). When provided, narrows codebase scan and pre-seeds Q&A answers.
- `--reset`: Skip backup. Overwrite existing files without creating historical copies.

## Prerequisites

Before starting, verify:

```bash
git rev-parse --is-inside-work-tree
```

If this fails, stop and tell the user: "This skill requires a git repository. Initialize one with `git init` first."

---

## Phase 1: Backup

**Skip this phase if `--reset` was passed.**

Check if any of these files exist:
- `RALPH.md`
- `docs/reference/focus-areas.md`
- `docs/reference/gaps-identified.md`

**If none exist:** Skip silently to Phase 2.

**If any exist:** Back them up before overwriting.

1. Create `docs/reference/historical/` if it doesn't exist.
2. Get today's date as `YYYY-MM-DD`.
3. For each existing file, copy it to `docs/reference/historical/` with the naming pattern:
   - `RALPH.md` → `YYYY-MM-DD-historical-RALPH.md`
   - `focus-areas.md` → `YYYY-MM-DD-historical-focus-areas.md`
   - `gaps-identified.md` → `YYYY-MM-DD-historical-gaps-identified.md`
4. If a same-day backup already exists, append a counter: `-2`, `-3`, etc. For example, if `2026-03-12-historical-RALPH.md` already exists, use `2026-03-12-historical-RALPH-2.md`.
5. Tell the user what was backed up: "Backed up existing files to `docs/reference/historical/`."

---

## Phase 2: Scan

Use the Agent tool with `subagent_type: "Explore"` to discover codebase structure. Send the subagent this prompt:

> Explore this codebase and report back a structured inventory. I need:
>
> 1. **Project type** — What language(s), framework(s), build system(s)? Look for: go.mod, package.json, pyproject.toml, Cargo.toml, *.csproj, *.sln, Makefile, CMakeLists.txt, etc.
>
> 2. **Components** — Group files by directory/module. For each component, report:
>    - Name (directory name or module name)
>    - Key files (entry points, main logic files)
>    - One-line description of what it does
>
>    Look in these standard locations: `src/`, `internal/`, `lib/`, `cmd/`, `app/`, `packages/`, `pkg/`, `modules/`, and top-level directories that contain source code.
>
> 3. **Entry points** — Files like main.go, main.py, index.ts, Program.cs, app.py, server.ts, Dockerfile
>
> 4. **Config/infra** — CI/CD workflows (.github/workflows/), docker files, migration directories, config directories
>
> 5. **Test directories** — Map test files/directories back to the components they test. Look for: tests/, test/, *_test.go, *.test.ts, test_*.py, *.spec.ts, *_test.py
>
> 6. **Design docs or specs** — Any files in docs/plans/, docs/specs/, docs/designs/, docs/reference/ that describe the system
>
> {IF CONTEXT WAS PROVIDED: "7. **Context focus** — The user wants to focus on: '{CONTEXT}'. Identify which components are directly related to this context, and which components touch those (same package, co-located files, shared config, related tests). Mark each component as 'direct' or 'indirect'."}
>
> Return the results as a structured list grouped by category.

Store the scan results for use in Phase 3.

**If the scan finds zero components** (empty repo or no source code), note this — you'll ask the user to define focus areas manually in Phase 3.

---

## Phase 3: Q&A

Ask questions **one at a time**. Skip questions that CONTEXT already answers.

### Question 1: Mission

**Skip if CONTEXT provides a clear description of what to review.**

Ask: "What should this ralph loop review and improve? Describe the feature, system, or area of code."

Store the answer as `MISSION`.

If CONTEXT was provided, use it as the MISSION and tell the user: "Based on your context, the loop will focus on: {CONTEXT}. Sound right?" Accept corrections.

### Question 2: Test Command

Auto-detect the test command by checking for:
- `Makefile` → look for `test` target (use `make test`)
- `package.json` → look for `test` script (use `npm test`)
- `pyproject.toml` → (use `pytest tests/ -v`)
- `go.mod` → (use `go test ./...`)
- `Cargo.toml` → (use `cargo test`)
- `*.sln` or `*.csproj` → (use `dotnet test`)

Present the detected command: "I detected `{COMMAND}` as your test command. Use this, or enter a different one?"

If nothing is detected, ask: "What command runs your tests? (e.g., `make test`, `npm test`, `pytest`)"

The test command is required. Store it as `TEST_COMMAND`.

### Question 3: Persona

Generate a persona paragraph based on:
- The language/framework from the scan
- The MISSION description
- Any design docs found in the scan
- The architecture patterns visible in the codebase (e.g., hexagonal, MVC, microservices)

The persona should follow this pattern (drawn from real examples):
> You are a [ROLE] who [RELEVANT EXPERTISE]. You understand [ARCHITECTURE/PATTERNS] and [DOMAIN KNOWLEDGE]. You care about [QUALITY ATTRIBUTES].

Present it and ask:
1. **Accept** — use this persona as-is
2. **See alternatives** — show 3 more persona suggestions (plus the original, so 4 total to choose from)
3. **Write your own** — type a custom persona

If they choose "See alternatives," generate 3 additional personas with different emphases (e.g., security-focused, test-coverage-focused, spec-alignment-focused, performance-focused) and present all 4 as a numbered list. They can pick a number or type their own.

Store the final persona as `PERSONA`.

### Question 4: Single Focus Areas

**If the scan found components:**

Present the discovered components as a numbered list grouped by category:

```
Based on the codebase scan, here are the candidate focus areas:

**Core Components:**
  [1] Component A (src/component_a/) — handles user authentication
  [2] Component B (src/component_b/) — payment processing logic

**Supporting:**
  [3] Database Layer (src/db/) — schema, migrations, queries
  [4] Configuration (config/) — env loading, defaults

**Infrastructure:**
  [5] CI/CD (.github/workflows/) — build, test, deploy pipelines
  [6] Docker (Dockerfile, docker-compose.yml) — container config

**Test Coverage:**
  [7] Tests (tests/) — test isolation, mocking, coverage gaps

Which areas should be included in the review? You can:
- List numbers to include (e.g., "1, 2, 3, 5")
- Say "all" to include everything
- Add areas not in the list (e.g., "also add: API endpoints (src/api/)")
- Remove areas (e.g., "drop 6")
```

**If the scan found zero components:**

Ask: "I couldn't detect clear components in this codebase. Please describe the focus areas for review. List them one per line, with a brief description of what each covers."

For each selected/added focus area, ensure it has:
- A name
- Key files (from scan or user input)
- A one-line review scope description

Store the final list as `SINGLE_AREAS`.

### Question 5: Paired Focus Areas

Generate suggested pairings from the selected single areas:

- Components that share a directory parent or have naming relationships
- When CONTEXT was provided: the context system paired with every component it touches (direct and indirect)
- Common integration seams: API + DB, client + server, config + all, tests + components

Present as a numbered list:

```
Suggested paired reviews (verifying integration seams):

  [1] Component A + Component B — data flow, error propagation
  [2] Component A + Database — query correctness, transaction boundaries
  [3] Config + All Components — env vars match behavior, defaults correct
  [4] Tests + Components — tests cover seams, not just happy paths

Which pairs to include? Same controls as above: list numbers, "all", add new pairs, or drop.
```

Store the final list as `PAIRED_AREAS`.

### Question 6: Checklist

Present the proposed checklist:

```
Proposed review checklist (assessed each iteration):

Universal (always included):
  [1] All tests pass (`{TEST_COMMAND}`)
  [2] No open issues in `docs/reference/gaps-identified.md` for this focus area
  [3] The focus area is complete and polished — you'd be proud to ship it

Proposed (based on your project):
  [4] {proposed item based on context, e.g., "The code aligns with the design doc"}
  [5] {proposed item, e.g., "Module boundaries are clean — no adapter types in core"}

Add, remove, or edit items? Or accept as-is?
```

Generate 1-3 proposed items based on what you've learned:
- If a design doc exists → add spec alignment item
- If hexagonal/layered architecture detected → add boundary cleanliness item
- If security-related MISSION → add security-specific item
- If test framework detected with coverage tools → add test scenario categories item

Store the final checklist as `CHECKLIST`.

### Question 7: Additional Constraints

Present the standard constraints that are always included:

```
These constraints are always included in the loop:
- No sub-agents for bulk generation
- Read before you write
- One focus area per iteration
- Don't invent new functionality (log gaps instead)

Any project-specific constraints to add? (e.g., "Do NOT write Python scripts for analysis", "Run linting after every change"). Enter to skip.
```

Store any additions as `EXTRA_CONSTRAINTS`.

---

## Phase 4: Generate

Write all 3 files. Create `docs/reference/` directory if it doesn't exist.

### Generate RALPH.md

Write `RALPH.md` in the repository root with this structure (substitute all `{VARIABLES}` with values from Q&A):

```markdown
# Loop Instructions

You are in an automated prompt loop and the user is unavailable for input, so you should just do the work without asking for any user input.

## Persona

{PERSONA}

## Your Mission

{MISSION_DESCRIPTION — expand the MISSION into 1-3 paragraphs describing what was built, what the loop is reviewing, and what "done" looks like}

## Tracking System

- At the start of an iteration, read `docs/reference/focus-areas.md`. Each single area (from table 1) needs **2 review passes** before being considered __done__. Each paired area (from table 2) needs **1 review pass** before being considered __done__. Use this document to track which reviews have been advanced and completed.
- DO NOT update this document until the "Wrap Up" phase at the end of each iteration. Updating is conditional on your checklist assessment.

## Constraints

- **Do NOT use sub-agents for bulk generation.** When you modify or create code, do it by hand, one component at a time, with thought behind each decision.
- **Read before you write.** Before modifying any file, read the relevant sections. Before claiming something is fine, read it and reason about quality.
- **One focus area per iteration.** Don't try to fix everything at once. Pick the most important remaining issue within the focus area, fix it well, then let the next iteration handle the next issue.
- **Do NOT invent new functionality to fill perceived gaps.** Maintain a list of things you find that should be fixed at `docs/reference/gaps-identified.md` in the `## Open Issues` section. If you perceive there is new, missing functionality beyond the current scope, log it in the `## Won't Fix (Beyond Current Scope)` section. If something on the list has been fixed in a previous loop, move it to `## Fixed Previously`.
{EXTRA_CONSTRAINTS — each as a new bullet point with bold lead, same format as above}

## Iteration Structure

1. **Read the Tracking File** — Read `docs/reference/focus-areas.md` and pick a review focus area that hasn't been completed yet. Complete single area reviews before moving to paired area reviews.
2. **Read the Area's Code** — Deeply examine the code for the chosen focus area. Read every file. Understand the patterns.
3. **Analyze findings and update the gaps list** — Cross-reference what you just read with the design doc and codebase conventions. Add any issues found to `docs/reference/gaps-identified.md` in the `## Open Issues` section.
4. **Fix the single most important issue** — Fix it well. If the issue spans multiple files, fix all of them consistently. Once fixed, move the issue to `## Fixed Previously` in `docs/reference/gaps-identified.md`.
5. **Run the tests** — Run `{TEST_COMMAND}`. Investigate and fix each failure.
6. **Assess the checklist** — Evaluate honestly, then proceed to the Wrap Up phase.

## The Checklist (be brutally honest)

Do NOT check a box unless you could defend it in a code review:

{CHECKLIST — each item as `- [ ] {item text}`}

## Wrap Up

This is the final phase of every iteration. Follow these steps in order.

**Step A — Determine if this focus area passed review.**

All checklist boxes must be honestly checked for the focus area to pass. There is no partial credit. Don't feel bad if it didn't pass — that just means the next iteration will take another crack at it. Improving things each cycle is a successful outcome.

**Step B — Update tracking (only if the focus area passed).**

- If the focus area **passed**: Mark that focus area's review as complete in `docs/reference/focus-areas.md`.
- If the focus area **did not pass**: Do NOT update `docs/reference/focus-areas.md`.

**Step C — Commit and push your branch to remote.**

Always commit and push, whether the focus area passed or not. Your work this iteration is valuable either way.

**Step D — Output your promise tag and stop.**

Check `docs/reference/focus-areas.md`. Are ALL reviews (single areas and paired areas) now marked complete?

- If **all reviews are complete**: output `<promise>FINIT</promise>`
- If **any reviews remain incomplete**: output `<promise>CLOSER</promise>`

Output exactly one `<promise>` tag, then stop. Do not output anything after the tag. The next iteration will carry on your good work.
```

### Generate docs/reference/focus-areas.md

Write `docs/reference/focus-areas.md` with this structure:

```markdown
# Focus Areas Review Tracking

This document tracks the systematic review of {MISSION_SHORT — one-line summary}.

Each **single focus area** requires **2 full review passes** before being considered done.
Each **paired focus area** requires **1 full review pass** before being considered done.

A review pass is only checked when the checklist produces a passing result for that area.

---

## Table 1: Single Area Reviews (2 passes each)

| # | Focus Area | Pass 1 | Pass 2 | Status |
|---|---|---|---|---|
{For each SINGLE_AREA, numbered starting at 1:}
| {N} | {Name} (`{key files}` — {review scope description}) | [ ] | [ ] | |

---

## Table 2: Paired Area Reviews (1 pass each)

| # | Pair | Pass | Status |
|---|---|---|---|
{For each PAIRED_AREA, numbered continuing from single areas:}
| {N} | {Area 1} + {Area 2} ({seam description}) | [ ] | |

---

## Review Guidance

### Single Area Reviews — What To Check

**Pass 1 (Correctness & Coverage):**
- All public functions have test coverage (happy path, failure, error, edge cases)
- The code does what the design doc says it should
- Error handling is thorough — no silent failures
- Module boundaries are clean (no leaking types across boundaries)

**Pass 2 (Robustness & Extensibility):**
- Code aligns to the spirit of the design, not just surface conformance
- The component is ready to be extended by future work
- Test assertions test real risks, not just happy-path ceremony
- Edge cases are handled (empty inputs, concurrent access, boundary values)

### Paired Area Reviews — What To Check

Paired reviews verify that two components integrate correctly:
- Data flows cleanly across the boundary
- Error propagation works end-to-end
- No assumptions in one component that the other doesn't satisfy
- The integration tests cover the seams between the pair
```

### Generate docs/reference/gaps-identified.md

Write `docs/reference/gaps-identified.md`:

```markdown
# Gaps Identified

Issues found during review iterations. Only the user can move items to the Won't Fix sections.

## Open Issues

_(none)_

## Fixed Previously

_(none yet)_

## Won't Fix (Beyond Current Scope)
```

---

## Phase 5: Review

Show the user a summary:

```
Ralph loop files generated:

  RALPH.md
    Persona:      {first sentence of PERSONA}
    Test command:  {TEST_COMMAND}
    Checklist:     {N} items

  docs/reference/focus-areas.md
    Single areas:  {N} (2 passes each)
    Paired areas:  {N} (1 pass each)
    Total reviews: {TOTAL} passes

  docs/reference/gaps-identified.md
    Empty template ready

Want to review or edit any of the generated files before I commit?
```

If they want to review a file, show its content using the Read tool. If they make manual edits (outside Claude Code), re-read the file to acknowledge changes. If no review needed, proceed to Phase 6.

---

## Phase 6: Commit

Stage and commit all generated files plus any historical backups:

```bash
git add RALPH.md docs/reference/focus-areas.md docs/reference/gaps-identified.md
# Also stage historical backups if they were created
git add docs/reference/historical/ 2>/dev/null || true
git commit -m "chore: generate ralph loop files via plan-to-ralph"
```

Tell the user:

```
Loop files committed. To start the ralph loop:

  /direct-to-ralph "{MISSION_SHORT}"

Or submit manually:

  ralph-o-matic submit
```
````

Write this entire content to `skills/plan-to-ralph/skill.md`.

- [ ] **Step 2: Verify skill.md frontmatter matches existing skills**

Read the first 5 lines of `skills/brainstorm-to-ralph/skill.md` and `skills/direct-to-ralph/skill.md`. Confirm the new skill uses the same YAML frontmatter format (name and description fields between `---` delimiters).

- [ ] **Step 3: Verify the generated RALPH.md template matches the example**

Read `example-RALPH.md` and compare its structure against the RALPH.md template in the skill. Confirm all sections are present: Loop Instructions preamble, Persona, Mission, Tracking System, Constraints, Iteration Structure, Checklist, Wrap Up (with Steps A-D and FINIT/CLOSER tags).

- [ ] **Step 4: Verify the generated focus-areas.md template matches the example**

Read `docs/reference/example-focus-areas.md` and compare its structure against the focus-areas.md template in the skill. Confirm: Table 1 (single areas, 2 passes), Table 2 (paired areas, 1 pass), Review Guidance section.

- [ ] **Step 5: Verify the generated gaps-identified.md template matches the example**

Read `docs/reference/example-gaps-identified.md` and compare its structure against the gaps-identified.md template in the skill. Confirm: Open Issues, Fixed Previously, Won't Fix sections.

- [ ] **Step 6: Commit**

```bash
git add skills/plan-to-ralph/skill.md
git commit -m "feat: add plan-to-ralph skill"
```

---

### Task 3: Update bash installer

**Files:**
- Modify: `scripts/install.sh:880` — the `local skills=()` array inside `install_skill()`

- [ ] **Step 1: Read the current installer skill array**

Read `scripts/install.sh` around line 880. Find the line:
```bash
local skills=("brainstorm-to-ralph" "direct-to-ralph")
```

- [ ] **Step 2: Add plan-to-ralph to the array**

Change it to:
```bash
local skills=("brainstorm-to-ralph" "direct-to-ralph" "plan-to-ralph")
```

- [ ] **Step 3: Run BATS tests to verify no regressions**

```bash
make test-bats
```

Expected: All tests pass. The new skill name in the array doesn't affect any existing test assertions.

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat: add plan-to-ralph to bash installer"
```

---

### Task 4: Update PowerShell installer

**Files:**
- Modify: `scripts/install.ps1:882` — the `$skills` array inside `Install-Skill`

- [ ] **Step 1: Read the current installer skill array**

Read `scripts/install.ps1` around line 882. Find the line:
```powershell
$skills = @("brainstorm-to-ralph", "direct-to-ralph")
```

- [ ] **Step 2: Add plan-to-ralph to the array**

Change it to:
```powershell
$skills = @("brainstorm-to-ralph", "direct-to-ralph", "plan-to-ralph")
```

- [ ] **Step 3: Commit**

```bash
git add scripts/install.ps1
git commit -m "feat: add plan-to-ralph to PowerShell installer"
```

---

### Task 5: Update README

**Files:**
- Modify: `README.md` — Claude Code Skills section and architecture tree

- [ ] **Step 1: Add plan-to-ralph to the skills feature bullet**

In the Features section, find the line mentioning Claude Code skills and update it to include `plan-to-ralph`:

```markdown
- **Claude Code skills** — `brainstorm-to-ralph` for end-to-end idea-to-job workflows, `direct-to-ralph` for submitting ready work without brainstorming, `plan-to-ralph` for generating loop configuration files through guided Q&A
```

- [ ] **Step 2: Add plan-to-ralph section under Claude Code Skills**

After the `/direct-to-ralph` section, add:

```markdown
### `/plan-to-ralph`

Generate ralph loop files (RALPH.md, focus-areas.md, gaps-identified.md) through interactive Q&A. Scans your codebase to suggest focus areas, generates a review persona, and produces ready-to-use loop configuration.

‍```
/plan-to-ralph
/plan-to-ralph "on the new authentication system"
/plan-to-ralph --reset
‍```
```

- [ ] **Step 3: Add plan-to-ralph to the architecture tree**

In the Architecture section, update the skills tree:

```
skills/
  brainstorm-to-ralph/   Idea → brainstorm → plan → draft → ralph
  direct-to-ralph/       Submit ready work directly to ralph
  plan-to-ralph/         Generate loop files via guided Q&A
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: add plan-to-ralph to README"
```

---

### Task 6: Verify packaging

**Files:**
- Reference: `Makefile` — `package-skills` target

- [ ] **Step 1: Verify the Makefile picks up the new skill**

Read the `package-skills` target in the Makefile. Confirm it iterates `skills/*/` (not a hardcoded list), which means `skills/plan-to-ralph/` will be included automatically.

- [ ] **Step 2: Run make package-skills to test**

```bash
make package-skills
```

Expected output includes:
```
Packaging plan-to-ralph skill...
Skill packaged: dist/plan-to-ralph-skill.tar.gz
Skill packaged: dist/plan-to-ralph-skill.zip
```

- [ ] **Step 3: Verify the packaged archives contain the right files**

```bash
tar -tzf dist/plan-to-ralph-skill.tar.gz
```

Expected: `plan-to-ralph/skill.md` and `plan-to-ralph/manifest.json`

```bash
unzip -l dist/plan-to-ralph-skill.zip
```

Expected: Same two files.

- [ ] **Step 4: Final commit if any changes needed**

If packaging revealed issues, fix and commit. Otherwise, this step is a no-op.
