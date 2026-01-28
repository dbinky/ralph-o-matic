---
name: brainstorm-to-ralph
description: End-to-end orchestration from idea to ralph loop submission
---

# Brainstorm to Ralph

You are orchestrating a complete development workflow. This skill takes an initial idea through brainstorming, planning, execution, and submission to ralph-o-matic for iterative refinement.

## Arguments

Parse the following from the user's command:

- `IDEA`: The feature or task description (required)
- `--max-iterations N`: Max ralph loop iterations (default: 50)
- `--priority LEVEL`: Job priority - high, normal, low (default: normal)
- `--open-ended`: Use polish prompt without exit criteria

## Workflow Overview

```
Phase 1: Brainstorm (INTERACTIVE) ────► Design document
                                              │
Phase 2: Plan (AUTOMATIC) ────────────► Phase documents
                                              │
Phase 3: Beads Setup (AUTOMATIC) ─────► Task DAG
                                              │
Phase 4: Execute (PARALLEL) ──────────► Implementation
                                              │
Phase 5: Ship (AUTOMATIC) ────────────► Ralph job submitted
```

---

## Phase 1: Brainstorm (INTERACTIVE)

**Announce:** "Starting brainstorming session for: {IDEA}"

**REQUIRED:** Invoke `superpowers:brainstorming` with the IDEA.

This phase is **interactive**. You will:
1. Explore the current project context
2. Ask clarifying questions one at a time
3. Propose 2-3 approaches with trade-offs
4. Present the design in sections, validating each

**Output:** `docs/plans/YYYY-MM-DD-{topic}-design.md`

**On completion:**
- Commit the design document
- Announce: "Design complete. Moving to planning phase."
- Proceed to Phase 2

---

## Phase 2: Plan (AUTOMATIC)

**Announce:** "Creating implementation plan from design..."

**REQUIRED:** Invoke `superpowers:writing-plans` to create detailed phase documents.

Using the design document from Phase 1, create:
- `docs/plans/YYYY-MM-DD-{topic}-design-phase-1.md`
- `docs/plans/YYYY-MM-DD-{topic}-design-phase-2.md`
- `docs/plans/YYYY-MM-DD-{topic}-design-phase-3.md`
- (etc., as many phases as needed)

Each phase document must follow the writing-plans format:
- Clear task breakdown with exact file paths
- TDD approach (tests before implementation)
- Step-by-step instructions
- Commit points

**On completion:**
- Commit all phase documents
- Announce: "Planning complete. {N} phases created. Setting up task tracking..."
- Proceed to Phase 3

---

## Phase 3: Beads Setup (AUTOMATIC)

**Announce:** "Setting up task tracking with Beads..."

Initialize Beads if needed and create the task structure:

### Step 1: Initialize Beads

```bash
# Check if Beads is initialized
if [ ! -d ".beads" ]; then
    bd init
fi
```

### Step 2: Create Phase Tasks

For each phase document, create a parent task:

```bash
# Example for 3 phases
bd add "Phase 1: Database Schema" --id PHASE-1
bd add "Phase 2: API Endpoints" --id PHASE-2 --blocked-by PHASE-1
bd add "Phase 3: Frontend Integration" --id PHASE-3 --blocked-by PHASE-2
```

### Step 3: Create Sub-Tasks

Parse each phase document and create sub-tasks for each "Task N:" section:

```bash
# Example sub-tasks for Phase 1
bd add "Create users table" --id PHASE-1-1 --parent PHASE-1
bd add "Create sessions table" --id PHASE-1-2 --parent PHASE-1 --blocked-by PHASE-1-1
bd add "Add indexes" --id PHASE-1-3 --parent PHASE-1 --blocked-by PHASE-1-1,PHASE-1-2
```

### Step 4: Verify Structure

```bash
bd list --tree
```

Should show:
```
Phase 1: Database Schema [PHASE-1]
├── Create users table [PHASE-1-1] ○
├── Create sessions table [PHASE-1-2] ○ (blocked by PHASE-1-1)
└── Add indexes [PHASE-1-3] ○ (blocked by PHASE-1-1, PHASE-1-2)

Phase 2: API Endpoints [PHASE-2] (blocked by PHASE-1)
├── POST /auth/register [PHASE-2-1] ○
├── POST /auth/login [PHASE-2-2] ○
└── POST /auth/logout [PHASE-2-3] ○
...
```

**On completion:**
- Commit `.beads/` directory
- Announce: "Task tracking ready. {N} tasks created. Starting parallel execution..."
- Proceed to Phase 4

---

## Phase 4: Execute (PARALLEL)

**Announce:** "Launching parallel execution agents..."

**REQUIRED:** Use `superpowers:dispatching-parallel-agents` to execute phase documents concurrently.

### Agent Configuration

Spawn one agent per phase document. Each agent receives:

```markdown
You are executing an implementation plan for a specific phase.

**Phase Document:** docs/plans/YYYY-MM-DD-{topic}-design-phase-{N}.md

**REQUIRED:** Invoke `superpowers:executing-plans` with this phase document.

**Beads Integration:**

Before starting any task:
```bash
bd list --ready
```

After completing a task:
```bash
bd done {TASK-ID}
```

If blocked by another agent's work:
```bash
bd list --blocked
# Wait and poll every 30 seconds until unblocked
```

After all tasks complete, verify:
```bash
bd list --status PHASE-{N}
# Should show all tasks complete
```

**Commit Strategy:**
- Commit after each task completion
- Use conventional commit messages
- Reference task ID in commit message: "feat: create users table [PHASE-1-1]"
```

### Execution Flow

1. Dispatch agents for all phases simultaneously
2. Agents that are blocked will wait automatically (via Beads)
3. Monitor progress through Beads:
   ```bash
   watch -n 5 'bd list --compact'
   ```
4. Wait for all agents to complete

### Handling Failures

If an agent fails:
1. Check which task failed: `bd list --failed`
2. Review the error in agent output
3. Either:
   - Fix manually and mark done: `bd done {TASK-ID}`
   - Reset and retry: `bd reset {TASK-ID}` and re-run agent

**On completion:**
- Verify all Beads tasks are complete: `bd list --summary`
- Run full test suite to verify implementation
- Announce: "Implementation complete. All {N} phases done. Preparing for ralph submission..."
- Proceed to Phase 5

---
