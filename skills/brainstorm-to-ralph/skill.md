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
