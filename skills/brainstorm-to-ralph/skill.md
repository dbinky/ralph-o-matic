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

## Phase 5: Ship (AUTOMATIC)

**Announce:** "Running pre-flight checks and shipping to ralph-o-matic..."

### Pre-flight Checks

Run these checks before submission:

```bash
# 1. Working tree clean
if [ -n "$(git status --porcelain)" ]; then
    echo "✗ Uncommitted changes detected"
    git status --short
    # Commit remaining changes
    git add -A
    git commit -m "chore: final implementation cleanup"
fi
echo "✓ Working tree clean"

# 2. Branch pushed to origin
BRANCH=$(git branch --show-current)
if ! git ls-remote --exit-code origin "$BRANCH" &>/dev/null; then
    echo "Pushing branch to origin..."
    git push -u origin "$BRANCH"
fi
echo "✓ Branch '$BRANCH' pushed to origin"

# 3. Tests exist
TEST_COUNT=$(find . -name "*_test.go" -o -name "test_*.py" -o -name "*.test.ts" | wc -l | tr -d ' ')
if [ "$TEST_COUNT" -eq 0 ]; then
    echo "✗ No tests found - ralph needs tests to verify completion"
    exit 1
fi
echo "✓ Tests found ($TEST_COUNT test files)"

# 4. Server reachable
if ! ralph-o-matic status &>/dev/null; then
    echo "✗ Cannot reach ralph-o-matic server"
    exit 1
fi
echo "✓ Server reachable"

# 5. Branch not already in queue
EXISTING=$(ralph-o-matic status --json | jq -r ".jobs[] | select(.branch == \"$BRANCH\") | .id")
if [ -n "$EXISTING" ]; then
    echo "✗ Branch already in queue as job #$EXISTING"
    exit 1
fi
echo "✓ Branch not in queue"
```

### Generate Ralph Prompt

Based on the `--open-ended` flag, generate the appropriate prompt:

**Standard prompt (bounded):**

```markdown
You are completing a feature to production-ready quality.

Specification: docs/plans/YYYY-MM-DD-{topic}-design.md

Each iteration:
1. Read the spec (every time - don't assume you remember it)
2. Run tests to see current state
3. Identify the single highest-impact gap between current state and spec
4. Fix it
5. Run tests again to verify

The code was drafted by another agent and may be incomplete or have bugs.
Do not trust it. Verify everything against the spec.

When tests pass AND the spec is fully satisfied, output:
<promise>COMPLETE</promise>

If tests don't exist for a requirement, write them first.
```

**Open-ended prompt (unbounded):**

```markdown
Polish this feature to production quality.

Specification: docs/plans/YYYY-MM-DD-{topic}-design.md

Each iteration: run tests, find the worst problem, fix it.

Do not output a <promise> tag. Continue improving until stopped.
```

### Submit to Ralph-o-matic

```bash
# Write prompt to RALPH.md
cat > RALPH.md << 'EOF'
{GENERATED_PROMPT}
EOF

git add RALPH.md
git commit -m "chore: add ralph loop prompt"
git push

# Submit job
ralph-o-matic submit \
    --priority {PRIORITY} \
    --max-iterations {MAX_ITERATIONS}
```

### Report Success

```
╔══════════════════════════════════════════════════════════════════╗
║                    Shipped to Ralph-o-matic!                     ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║  Job ID:        #52                                              ║
║  Branch:        feature/auth-refactor                            ║
║  Priority:      high                                             ║
║  Max Iterations: 50                                              ║
║  Queue Position: 1st                                             ║
║                                                                  ║
║  Dashboard:     http://192.168.1.50:9090/jobs/52                ║
║                                                                  ║
║  The ralph loop will iterate on your code until:                 ║
║  - All tests pass AND spec is satisfied, OR                      ║
║  - Max iterations reached                                        ║
║                                                                  ║
║  Monitor progress:                                               ║
║    ralph-o-matic logs 52 --follow                               ║
║                                                                  ║
╚══════════════════════════════════════════════════════════════════╝
```

---

## Error Handling

### Server Unreachable

If ralph-o-matic server is unreachable during pre-flight:

```
✗ Cannot reach ralph-o-matic server

The implementation is complete and ready for ralph. Options:

1. Start the server locally:
   ralph-o-matic-server

2. Point to a different server:
   ralph-o-matic config set server http://new-server:9090

3. Create PR manually instead:
   git push -u origin $(git branch --show-current)
   gh pr create
```

### Tests Failing

If tests are failing before submission:

```
✗ Tests failing - ralph needs passing tests as baseline

{N} tests failing. Fix these before submitting to ralph:

  FAIL  src/auth/login.test.ts
    ✗ should validate credentials
    ✗ should create session

Ralph uses test results to measure progress. Submit when tests pass.
```

### No Tests Found

```
✗ No tests found

Ralph needs tests to verify completion. The implementation includes code but no tests.

Add tests for the new functionality before submitting:
  - Unit tests for new functions
  - Integration tests for API endpoints
  - E2E tests for critical flows
```

---

## Recovery

If the workflow fails partway through, you can resume:

### Resume from Phase 2 (Planning)

If brainstorming completed but planning failed:

```
/brainstorm-to-ralph resume --from-design docs/plans/YYYY-MM-DD-{topic}-design.md
```

### Resume from Phase 4 (Execution)

If planning completed but execution failed:

```
/brainstorm-to-ralph resume --from-plans "docs/plans/YYYY-MM-DD-{topic}-design-phase-*.md"
```

### Resume from Phase 5 (Ship)

If execution completed but shipping failed:

```
ralph-o-matic submit --priority normal --max-iterations 50
```
