You are improving this codebase toward production quality.

Progress: docs/plans/dev-ralph-direct-testing-ralph-status.md

Each iteration:
1. Read the progress file to understand what's been done and what remains
2. Search the codebase before assuming anything is missing
3. Pick the single highest-impact improvement
4. Implement it, keeping the change focused and testable
5. Verify it is **wired-and-fed**, not merely present. For every new exported field, collaborator, or config you added, `grep -rn <Symbol>` the whole repo: is it constructed/populated **outside `*_test.go`** and assembled into the real composition root (server/DI wiring) so the live path actually reaches it? If a production call site passes `nil`/empty/hardcoded where a real producer should feed it, that change is **[NOT-WIRED]** and is not done.
6. Run tests — if they fail, fix before moving on. A fully green suite does **not** by itself mean done: green over-mocked tests can coexist with an unwired feature (**[GREEN-THEATER]**). Probe each test you rely on with "would this still pass if the production input were nil/empty?" — at least one test per component must fail in that case, fakes must not ignore a load-bearing argument, and "integration" tests must assert the real sink, not an intermediate hop.
7. Update the progress file: mark completed items, add discovered work, note what's next. An inert/no-op path logged as "acceptable" is an OPEN gap, not a tolerated state — record it as work to fix, never as a reason for done.

## Definition of done — gate on NEW-vs-baseline, not all-green

The repo may already be red on arrival. Record (or read) a branch-point test baseline of which tests already fail BEFORE this work; pre-existing failures are NOT this work's job and MUST NOT block — log them under a "Won't Fix" section and move on. Compare each run's failures against that baseline so you distinguish a NEW failure from a pre-existing one.

This work is done only when ALL of:
- All work is **wired-and-fed**: every new component is constructed at the real composition root and fed real data from a producer that exists — not nil/empty/hardcoded, not only set in tests.
- Coverage is real (no **[GREEN-THEATER]**): meaningful tests across the relevant scenarios, with ≥1 test per component that would fail if its production input were nil/empty.
- No open gaps remain (including no inert/no-op paths logged as "acceptable").
- **No NEW test failures vs the recorded baseline** (current failing set ⊆ baseline) and nothing that built at baseline is now broken. Do NOT require the entire suite to be green.

Do not output a <promise> tag. Continue improving until stopped.
