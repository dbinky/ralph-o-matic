# Progress: dev-refinement

## Current Coverage: 67.1% (up from 61.6%)

## Remaining
- [ ] `internal/auth/routes.go:97 handleCallback` at 27.3% (OAuth callback handler)
- [ ] `internal/executor/claude.go:37 resolveClaudeBinary` at 25.0% (hard to test without refactor)
- [ ] `internal/platform/hardware.go detectRAM` at 31.6%, `detectGPUs` at 44.4% (system calls)
- [ ] `cmd/cli/commands.go:466 serverConfigCmd` at 11.9%
- [ ] `internal/api/server.go Start/Shutdown` at 0.0% (needs integration test)
- [ ] `internal/api/dashboard_state.go` at 68.4%
- [ ] `internal/db/jobs.go GetByBranch/UpdatePositions` at ~77%

## Completed
- [x] Add API key authentication mode (RalphFactory-mdb closed)
  - `AuthModeAPIKey = "apikey"`, `Config.APIKey`, middleware + server wired
  - 8 tests added (auth_test.go + middleware_test.go)
- [x] Add API key support to CLI client (RALPH_API_KEY env var)
- [x] Improve executor package test coverage (56.6% -> 63.6%)
- [x] Improve git package coverage (21.2% -> 53.0%)
- [x] Improve dashboard package coverage (59.1% -> 95.5%)
- [x] Improve models + queue coverage (prompts, scheduler operations)
- [x] Improve API jobs handler coverage (70.6% -> 77.1%)
  - Cancel/Pause/Resume: 54.5% -> 100%, GetJobLogs: 0% -> 83.3%
- [x] Improve MergeJSON coverage (36.2% -> 98.3%)
- [x] Improve worker/cleaner coverage (tick 66.7%->100%, purgeExpiredJobs context paths)
- [x] Improve notify package coverage (80.7% -> 86.9%)
  - NewDispatcher nil logger, real callNotifier coverage
- [x] Improve DB jobs coverage (78.7% -> 81.8%)
  - Env JSON path, DB-closed error paths across all repo methods

## Discovered
- `testableDispatcher` in notify/dispatcher_test.go shadows real Dispatcher — kept pattern
- `resolveClaudeBinary` and `detectRAM/detectGPUs` require OS-level mock injection to fully test
- `cmd/server/main.go` has 0% coverage — needs integration or smoke tests
