---
name: brainstorm-to-ralph
description: End-to-end orchestration from idea to ralph submission
---

# Brainstorm to Ralph

You are orchestrating a complete development workflow. This skill takes an initial idea through brainstorming, planning, execution, and submission to ralph-o-matic for iterative refinement.

## Arguments

Parse the following from the user's command:

- `IDEA`: The feature or task description (required)
- `--max-iterations N`: Max ralph loop iterations (default: 50)
- `--priority LEVEL`: Job priority - high, normal, low (default: normal)
- `--open-ended`: Use polish prompt without exit criteria
- `--local`: Use local repo directory (skip clone). Auto-detected when server is localhost.

## Workflow Overview

```
Phase 1: Brainstorm (INTERACTIVE) ────► Design document
                                              │
Phase 2: Plan (AUTOMATIC) ────────────► Phase documents
                                              │
Phase 3: Execute (PARALLEL) ──────────► Implementation
                                              │
Phase 4: Ship (AUTOMATIC) ────────────► Ralph job submitted
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
- Announce: "Planning complete. {N} phases created. Starting parallel execution..."
- Proceed to Phase 3

---

## Phase 3: Execute (PARALLEL)

**Announce:** "Launching parallel execution agents..."

**REQUIRED:** Use `superpowers:dispatching-parallel-agents` to execute phase documents concurrently.

### Agent Configuration

Spawn one agent per phase document. Each agent receives:

```markdown
You are executing an implementation plan for a specific phase.

**Phase Document:** docs/plans/YYYY-MM-DD-{topic}-design-phase-{N}.md

**REQUIRED:** Invoke `superpowers:executing-plans` with this phase document.

**Task Tracking:**

Each phase document contains numbered tasks. Work through them in order:
1. Read the phase document to understand the full scope
2. For each task, implement it following TDD (tests first, then implementation)
3. Run tests after each task to verify correctness
4. Commit after each task completion

If a task depends on work from another phase that isn't complete yet,
skip it and move to the next independent task. Return to blocked tasks
after the dependency is available.

**Commit Strategy:**
- Commit after each task completion
- Use conventional commit messages
- Reference the phase and task in commit messages, e.g.: "feat(phase-1): create users table"
```

### Execution Flow

1. Dispatch agents for all phases simultaneously
2. Phases with dependencies on earlier phases will skip blocked tasks and return to them
3. Wait for all agents to complete
4. If any agent fails, review its output and either fix manually or re-run

### Handling Failures

If an agent fails:
1. Review the error in agent output
2. Either:
   - Fix manually and commit the fix
   - Re-run the agent for that specific phase

**On completion:**
- Run the full test suite to see where it stands — but do NOT treat a few red tests as a blocker. Ralph gates on NEW-vs-baseline (see Phase 4), and a red-on-arrival repo is fine.
- Announce: "Implementation complete. All {N} phases done. Preparing for ralph submission..."
- Proceed to Phase 4

---

## Phase 4: Ship (AUTOMATIC)

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
SERVER=$(ralph-o-matic config | grep '^server:' | awk '{print $2}')
EXISTING=$(curl -sf "$SERVER/api/jobs?status=queued,running,paused" | jq -r ".jobs[] | select(.branch == \"$BRANCH\") | .id" 2>/dev/null | head -1)
if [ -n "$EXISTING" ]; then
    echo "✗ Branch already in queue as job #$EXISTING"
    exit 1
fi
echo "✓ Branch not in queue"
```

> Note: pre-flight verifies that tests *exist* (ralph needs something to measure against), but does NOT require them to be green. A red-on-arrival suite is fine — the baseline below records the pre-existing failures so the loop gates on NEW failures only.

### Record the branch-point test baseline

Before submitting, capture which tests **already fail right now** so the loop gates on NEW-vs-baseline rather than demanding a fully green suite. The repo MAY be red on arrival — that is fine; pre-existing failures are not this work's job.

1. Run the project's test command (auto-detect it: `make test`, `npm test`, `go test ./...`, `pytest`, `cargo test`, `dotnet test`, etc.). For a mixed-language repo, run each suite.
2. For each suite, capture whether it built/compiled, the pass/fail counts, and the stable identifier of each failing test (no timestamps/durations).
3. Write the result to `docs/plans/{BRANCH}-test-baseline.md` — per suite: the exact command, build-ok status, pass/fail counts, and the list of pre-existing failing test ids. If a suite fails to build, note `BUILD FAILED` with a one-line summary. If no test suite exists, note that and continue.

This is the file the generated RALPH.md prompt references. Pre-existing failures here are logged, not fixed, and must not block submission. Commit this file alongside RALPH.md in the submit step.

### Local Server Detection

After pre-flight checks pass, determine whether the ralph server is local.

**How to detect:** Read the configured server URL from `ralph-o-matic config`. If it contains `localhost` or `127.0.0.1`, the server is local.

**If `--local` was passed:** Use local mode without asking.

**If server is local and `--local` was NOT passed:** Ask the user with AskUserQuestion:

| Option | Description |
|--------|-------------|
| Use local repo (Recommended) | Ralph works directly in your checkout — no clone, faster startup, changes pushed after each pass. Don't edit these files while ralph is running. |
| Clone as usual | Ralph clones into its own workspace. Slower but your working tree stays untouched. |

**If the user chooses local repo:** Get the repo root with `git rev-parse --show-toplevel`. Store this as `LOCAL_DIR` for use in the submit step.

**If server is NOT local:** Skip this — remote servers can't access local paths.

### Generate Ralph Prompt

Based on the `--open-ended` flag, generate the appropriate prompt:

**Standard prompt (bounded):**

```markdown
You are refining code to meet a specification. The user is unavailable — do the work without asking for input.

Spec: docs/plans/YYYY-MM-DD-{topic}-design.md
Progress: docs/plans/{BRANCH}-ralph-status.md
Test baseline: docs/plans/{BRANCH}-test-baseline.md (tests that already failed before this work — these do NOT block)

Steps:
1. Read the spec, progress file, and test baseline to understand current state
2. Search the codebase before assuming anything is missing — do not reimplement existing code
3. Pick the single highest-impact remaining task
4. Implement it, keeping the change focused and testable
5. Run tests — compare against the baseline. Fix any NEW failure (not in the baseline) and any new build break before moving on. Pre-existing baseline failures are NOT your job; do not chase a fully green suite.
6. Update the progress file: mark completed items, add discovered work, note what's next
7. Commit and push

The code may have been drafted by another agent. Do not trust it. Verify against the spec.

Wired-and-fed: a task is done only when its component is constructed at the REAL composition root (not just in a test) AND fed real data from a producer that exists — not nil/empty/hardcoded. For every new exported field/collaborator/config, `grep -rn <Symbol>` and confirm it is populated outside `*_test.go`. A fully green suite does NOT by itself mean done: green over-mocked tests can hide an unwired feature — probe "would this test still pass if the prod input were nil?"

When all spec requirements are satisfied AND wired-and-fed AND there are no NEW test failures vs the baseline (and nothing that built at baseline is now broken), output:
<promise>FINIT</promise>

Otherwise, output:
<promise>CLOSER</promise>

Output exactly one tag, then stop.
```

**Open-ended prompt (unbounded):**

```markdown
You are improving this codebase toward production quality. The user is unavailable — do the work without asking for input.

Progress: docs/plans/{BRANCH}-ralph-status.md
Test baseline: docs/plans/{BRANCH}-test-baseline.md (tests that already failed before this work — these do NOT block)

Steps:
1. Read the progress file and test baseline to understand what's been done and what remains
2. Search the codebase before assuming anything is missing
3. Pick the single highest-impact improvement
4. Implement it, keeping the change focused and testable
5. Run tests — compare against the baseline. Fix any NEW failure (not in the baseline) and any new build break before moving on. Pre-existing baseline failures are NOT your job; do not chase a fully green suite.
6. Update the progress file: mark completed items, add discovered work, note what's next
7. Commit and push

Keep every improvement wired-and-fed: constructed at the real composition root and fed a real producer (not nil/empty/hardcoded, not only set in tests) — a green over-mocked test can hide an unwired change. Do not output a <promise> tag. Continue improving until stopped.
```

### Submit to Ralph-o-matic

```bash
# Write prompt to RALPH.md
cat > RALPH.md << 'EOF'
{GENERATED_PROMPT}
EOF

git add RALPH.md "docs/plans/$(git branch --show-current)-test-baseline.md"
git commit -m "chore: add ralph review prompt and test baseline"
git push

# Submit job
# If LOCAL_DIR is set, pass --working-dir to use the local repo directly
ralph-o-matic submit \
    --priority {PRIORITY} \
    --max-iterations {MAX_ITERATIONS} \
    ${LOCAL_DIR:+--working-dir "$LOCAL_DIR"}
```

### Report Success

```
Shipped to Ralph-o-matic!

  Job ID:         #{JOB_ID}
  Branch:         {BRANCH}
  Priority:       {PRIORITY}
  Max Iterations: {MAX_ITERATIONS}
  Mode:           {LOCAL_DIR ? "local repo (direct)" : "cloned workspace"}

  Dashboard:      {SERVER_URL}/jobs/{JOB_ID}

  Monitor: ralph-o-matic logs {JOB_ID} --follow
```

If local mode was used, add:

```
  Note: Ralph is working directly in {LOCAL_DIR}.
        Changes are pushed to the remote after each pass.
        Avoid editing files in this repo until the job completes.
```

---

## Error Handling

### Server Unreachable

If ralph-o-matic server is unreachable during pre-flight:

```
Cannot reach ralph-o-matic server

The implementation is complete and ready for ralph. Options:

1. Start the server locally:
   ralph-o-matic-server

2. Point to a different server:
   ralph-o-matic config set server http://new-server:9090

3. Create PR manually instead:
   git push -u origin $(git branch --show-current)
   gh pr create
```

### Tests Failing (not a blocker — record the baseline)

A red-on-arrival suite is **fine**. Ralph gates on NEW failures vs the recorded branch-point baseline, not on a fully green suite. Do NOT block, and do NOT try to fix pre-existing failures yourself — they are not this work's job.

If tests are failing before submission, just make sure the failures are captured in `docs/plans/{BRANCH}-test-baseline.md` (see "Record the branch-point test baseline" above), then proceed:

```
Tests failing - recording them as the branch-point baseline (NOT blocking)

{N} tests failing. These are recorded in docs/plans/{BRANCH}-test-baseline.md
as pre-existing failures. Ralph will gate on NEW failures vs this baseline —
it will not be asked to make the whole suite green. Proceeding to submission.
```

### No Tests Found

```
No tests found

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

### Resume from Phase 3 (Execution)

If planning completed but execution failed:

```
/brainstorm-to-ralph resume --from-plans "docs/plans/YYYY-MM-DD-{topic}-design-phase-*.md"
```

### Resume from Phase 4 (Ship)

If execution completed but shipping failed:

```
ralph-o-matic submit --priority normal --max-iterations 50
```
