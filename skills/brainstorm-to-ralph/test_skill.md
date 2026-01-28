# brainstorm-to-ralph Skill Test Checklist

Manual verification checklist for the skill.

## Prerequisites

- [ ] ralph-o-matic server running
- [ ] Claude Code with superpowers plugin installed
- [ ] Beads CLI (`bd`) installed
- [ ] Git repository initialized
- [ ] GitHub CLI authenticated

## Test Cases

### TC1: Basic Flow

1. Start Claude Code in a test project
2. Run `/brainstorm-to-ralph "Add a simple greeting endpoint"`
3. Verify:
   - [ ] Brainstorming questions are asked
   - [ ] Design document created in `docs/plans/`
   - [ ] Phase documents created
   - [ ] Beads tasks created (check `bd list`)
   - [ ] Parallel agents spawned
   - [ ] Implementation completed
   - [ ] Job submitted to ralph-o-matic
   - [ ] Dashboard shows new job

### TC2: With Priority Flag

1. Run `/brainstorm-to-ralph "Add logging" --priority high`
2. Verify:
   - [ ] Job submitted with high priority
   - [ ] Shows at top of queue

### TC3: With Max Iterations

1. Run `/brainstorm-to-ralph "Refactor auth" --max-iterations 100`
2. Verify:
   - [ ] Job submitted with 100 max iterations

### TC4: Open-Ended Mode

1. Run `/brainstorm-to-ralph "Polish the codebase" --open-ended`
2. Verify:
   - [ ] Prompt does NOT contain `<promise>` tag
   - [ ] Job runs until manually stopped or max iterations

### TC5: Server Unreachable

1. Stop ralph-o-matic server
2. Run `/brainstorm-to-ralph "Add feature"`
3. Complete through execution
4. Verify:
   - [ ] Pre-flight check fails gracefully
   - [ ] Provides recovery options
   - [ ] Does not lose work

### TC6: Resume from Design

1. Create a design document manually
2. Run `/brainstorm-to-ralph resume --from-design docs/plans/2026-01-28-test-design.md`
3. Verify:
   - [ ] Skips brainstorming phase
   - [ ] Proceeds to planning

### TC7: Duplicate Branch Detection

1. Submit a job for branch `feature/test`
2. Try to submit again for same branch
3. Verify:
   - [ ] Pre-flight check catches duplicate
   - [ ] Shows existing job ID

## Performance

- [ ] Brainstorming phase responds within 2s per question
- [ ] Planning phase completes within 2 minutes for medium features
- [ ] Beads setup completes within 30 seconds
- [ ] Pre-flight checks complete within 5 seconds
