# Loop Prompt Design

## Context

Research across the ralph loop ecosystem (Huntley's original, frankbria, Anthropic's official plugin, snarktank, ralph-orchestrator, claude-loop, AI Hero best practices) identified several techniques that improve iteration quality and convergence. This design updates our default prompts to incorporate the most impactful ones.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Cross-iteration memory | Living task list file | Prevents duplicate work and context rot; most proven pattern |
| RALPH_STATUS block | Optional/best-effort | Don't force structure that weaker models may not follow reliably |
| Prompt length | Moderate (~12 lines) | 3-4 key guardrails without over-constraining |
| Progress file location | `docs/plans/{date}-{branch}-ralph-status.md` | Organized with other plan docs, namespaced per branch |
| Progress file format | Living checklist (Remaining/Completed/Discovered) | Agent maintains it; more useful than append-only log |
| Committed per iteration | Yes | Survives crashes, provides history, scoped to branch |

## Prompt Templates

### Bounded (Exitable)

Used when the job has a spec and clear completion criteria. The agent exits when the spec is fully satisfied.

```markdown
You are refining code to meet a specification.

Spec: {SPEC_PATH}
Progress: docs/plans/{DATE}-{BRANCH}-ralph-status.md

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

### Open-Ended (Non-Exitable)

Used for polish/refinement work that runs until the iteration cap or manual stop.

```markdown
You are improving this codebase toward production quality.

Progress: docs/plans/{DATE}-{BRANCH}-ralph-status.md

Each iteration:
1. Read the progress file to understand what's been done and what remains
2. Search the codebase before assuming anything is missing
3. Pick the single highest-impact improvement
4. Implement it, keeping the change focused and testable
5. Run tests — if they fail, fix before moving on
6. Update the progress file: mark completed items, add discovered work, note what's next

Do not output a <promise> tag. Continue improving until stopped.
```

## Progress File Bootstrap

The loop engine creates the progress file before the first iteration if it doesn't exist.

### Bounded (with spec)

```markdown
# Progress

## Remaining
- [ ] Review spec and create initial task breakdown

## Completed

## Discovered
```

### Open-Ended (no spec)

```markdown
# Progress

## Remaining
- [ ] Audit codebase and identify improvements

## Completed

## Discovered
```

The agent owns this file from that point forward — breaking down tasks, checking them off, adding discovered work. The engine does not parse it; it exists purely for cross-iteration context.

## Key Guardrails (present in both prompts)

1. **Search before implementing** — "do not reimplement existing code." Prevents the #1 agent failure mode (Huntley).
2. **One change per iteration** — "pick the single highest-impact task." Keeps changes focused and testable.
3. **Don't proceed on broken tests** — "if they fail, fix before moving on." Backpressure prevents compounding failures.
4. **Update progress file** — cross-iteration memory so the next iteration knows what happened.

## Implementation Steps

### Step 1: Add default prompt templates to executor

Create `internal/executor/prompts.go` with:
- `DefaultBoundedPrompt(specPath, progressPath string) string`
- `DefaultOpenEndedPrompt(progressPath string) string`
- Constants for the template text

### Step 2: Add progress file bootstrap to RalphHandler

In `Handle()`, before the first iteration:
- Compute progress file path: `docs/plans/{YYYY-MM-DD}-{branch}-ralph-status.md`
- If file doesn't exist in the working directory, create the seed content
- Commit the seed file (uses existing per-iteration commit)

### Step 3: Update brainstorm-to-ralph skill

Update the prompt templates in `skills/brainstorm-to-ralph/skill.md` to match the new defaults.

### Step 4: Update RALPH.md example

Update the root `RALPH.md` to use the new open-ended template as the example.

### Step 5: Tests

- Test `DefaultBoundedPrompt` produces correct template with interpolated paths
- Test `DefaultOpenEndedPrompt` produces correct template with interpolated path
- Test progress file bootstrap creates correct seed content for both types
- Test progress file is not overwritten if it already exists

## Research Sources

- [ghuntley/how-to-ralph-wiggum](https://github.com/ghuntley/how-to-ralph-wiggum) — fresh context, IMPLEMENTATION_PLAN.md, "search before assuming"
- [frankbria/ralph-claude-code](https://github.com/frankbria/ralph-claude-code) — RALPH_STATUS, dual-condition exit gate, circuit breaker
- [Anthropic official ralph-wiggum plugin](https://github.com/anthropics/claude-code/tree/main/plugins/ralph-wiggum) — `<promise>COMPLETE</promise>`, TDD prompts
- [snarktank/ralph](https://github.com/snarktank/ralph) — JSON PRD tracking, per-story `passes` field
- [mikeyobrien/ralph-orchestrator](https://github.com/mikeyobrien/ralph-orchestrator) — scratchpad, backpressure gates
- [AI Hero: 11 Tips](https://www.aihero.dev/tips-for-ai-coding-with-ralph-wiggum) — short prompts, one feature per loop, backpressure
- [Simon Willison: Designing Agentic Loops](https://simonwillison.net/2025/Sep/30/designing-agentic-loops/)
