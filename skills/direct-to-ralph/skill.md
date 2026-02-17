---
name: direct-to-ralph
description: Use when you have code ready for ralph-o-matic refinement and want to skip brainstorming, planning, and execution phases
---

# Direct to Ralph

You are submitting work directly to ralph-o-matic for iterative refinement. This skill skips brainstorming, planning, and execution — use it when the user already has code or a task they want ralph to refine.

## Arguments

Parse the following from the user's command:

- `TASK`: The task description (required)
- `--spec <file>`: Path to spec/design doc (enables bounded prompt)
- `--max-iterations N`: Max ralph loop iterations (default: 50)
- `--priority LEVEL`: Job priority - high, normal, low (default: normal)
- `--open-ended`: Use polish prompt without exit criteria
- `--branch <name>`: Branch to submit (default: current branch)

## Workflow Overview

```
Step 1: Parse args & validate
         │
Step 2: Check for existing RALPH.md
         │  ├─ Found → Summarize, ask: use existing or create new?
         │  │            ├─ Use existing → Skip to Step 5
         │  │            └─ Create new → Step 3
         │  └─ Not found → Step 3
         │
Step 3: Q&A (fill in gaps from flags)
         │
Step 4: Generate RALPH.md
         │
Step 5: Pre-flight checks
         │
Step 6: Commit, push, submit
         │
Step 7: Report success
```

---

## Step 1: Parse & Validate

Parse the TASK and flags from the user's command. TASK is required — if missing, ask:

> What should ralph work on?

---

## Step 2: Check for Existing RALPH.md

Look for a `RALPH.md` file in the repository root.

**If found:**
- Read the file
- Summarize its contents in 2 sentences
- Ask the user using AskUserQuestion:
  - "Use this existing RALPH.md" → Skip to Step 5 (pre-flight checks)
  - "Create a new one" → Continue to Step 3

**If not found:** Continue to Step 3.

---

## Step 3: Q&A

Ask only what isn't already provided via flags. Use AskUserQuestion for each.

**Prompt type** (ask if neither `--spec` nor `--open-ended` was provided):

| Option | Description |
|--------|-------------|
| Bounded with spec | Ralph stops when spec requirements are satisfied. Ask for the spec file path. |
| Open-ended polish | Ralph keeps improving until max iterations reached or manually stopped. |
| Custom | User will write or edit RALPH.md themselves. Open the file for them and skip to Step 5 after they confirm. |

**Priority** (ask if `--priority` not provided): Offer high / normal (default) / low.

**Max iterations** (ask if `--max-iterations` not provided): Default 50. Offer 25 / 50 / 100.

---

## Step 4: Generate RALPH.md

Based on the prompt type selected, generate the prompt file.

**Bounded prompt (with spec):**

```markdown
You are refining code to meet a specification.

Spec: {SPEC_FILE}
Progress: docs/plans/{BRANCH}-ralph-status.md

Each iteration:
1. Read the spec and progress file to understand current state
2. Search the codebase before assuming anything is missing — do not reimplement existing code
3. Pick the single highest-impact remaining task
4. Implement it, keeping the change focused and testable
5. Run tests — if they fail, fix before moving on
6. Update the progress file: mark completed items, add discovered work, note what's next

The code may have been drafted by another agent. Do not trust it. Verify against the spec.

When all spec requirements are satisfied and tests pass, output:
<promise>COMPLETE</promise>
```

**Open-ended prompt:**

```markdown
You are improving this codebase toward production quality.

Progress: docs/plans/{BRANCH}-ralph-status.md

Each iteration:
1. Read the progress file to understand what's been done and what remains
2. Search the codebase before assuming anything is missing
3. Pick the single highest-impact improvement
4. Implement it, keeping the change focused and testable
5. Run tests — if they fail, fix before moving on
6. Update the progress file: mark completed items, add discovered work, note what's next

Do not output a <promise> tag. Continue improving until stopped.
```

Write the generated prompt to `RALPH.md` in the repository root.

---

## Step 5: Pre-flight Checks

Run these checks before submission:

```bash
# 1. Working tree clean
if [ -n "$(git status --porcelain)" ]; then
    echo "✗ Uncommitted changes detected"
    git status --short
    # Stage and commit remaining changes
    git add -A
    git commit -m "chore: pre-ralph cleanup"
fi
echo "✓ Working tree clean"

# 2. Branch pushed to origin
BRANCH=$(git branch --show-current)
if ! git ls-remote --exit-code origin "$BRANCH" &>/dev/null; then
    echo "Pushing branch to origin..."
    git push -u origin "$BRANCH"
fi
echo "✓ Branch '$BRANCH' pushed to origin"

# 3. Server reachable
if ! ralph-o-matic status &>/dev/null; then
    echo "✗ Cannot reach ralph-o-matic server"
    exit 1
fi
echo "✓ Server reachable"

# 4. Branch not already in queue
SERVER=$(ralph-o-matic config | grep '^server:' | awk '{print $2}')
EXISTING=$(curl -sf "$SERVER/api/jobs?status=queued,running,paused" | jq -r ".jobs[] | select(.branch == \"$BRANCH\") | .id" 2>/dev/null | head -1)
if [ -n "$EXISTING" ]; then
    echo "✗ Branch already in queue as job #$EXISTING"
    exit 1
fi
echo "✓ Branch not in queue"
```

---

## Step 6: Commit, Push, Submit

```bash
# Commit RALPH.md if it was generated or modified
if [ -n "$(git status --porcelain RALPH.md)" ]; then
    git add RALPH.md
    git commit -m "chore: add ralph loop prompt"
    git push
fi

# Submit job
ralph-o-matic submit \
    --priority {PRIORITY} \
    --max-iterations {MAX_ITERATIONS}
```

---

## Step 7: Report Success

```
Shipped to Ralph-o-matic!

  Job ID:         #{JOB_ID}
  Branch:         {BRANCH}
  Priority:       {PRIORITY}
  Max Iterations: {MAX_ITERATIONS}
  Prompt:         {PROMPT_TYPE}

  Dashboard:      {SERVER_URL}/jobs/{JOB_ID}

  Monitor: ralph-o-matic logs {JOB_ID} --follow
```

---

## Error Handling

### Server Unreachable

```
Cannot reach ralph-o-matic server.

Options:
1. Start the server: ralph-o-matic-server
2. Change server:    ralph-o-matic config set server http://host:9090
3. Skip submission:  push the branch and submit later manually
```

### Branch Already Queued

```
Branch '{BRANCH}' is already queued as job #{ID}.

Options:
1. Cancel existing job:  ralph-o-matic cancel {ID}
2. Use a different branch
3. Wait for the current job to finish
```
