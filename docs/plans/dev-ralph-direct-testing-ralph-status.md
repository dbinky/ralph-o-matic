# Progress: dev-refinement

## Remaining
- [ ] Improve executor package test coverage (56.6%)
- [ ] Consider adding CLI client support for API key in headers

## Completed
- [x] Add API key authentication mode (RalphFactory-mdb / beads closed)
  - `AuthModeAPIKey = "apikey"` in auth package
  - `Config.APIKey` field, loaded from `RALPH_API_KEY` env var or settings.json `api_key`
  - `Middleware` updated to validate static Bearer token when apiKey is set
  - `ServerOptions.APIKey` wired through server setup
  - `cmd/server/main.go` handles apikey mode in startup
  - 8 new tests added (auth_test.go + middleware_test.go)
  - All tests pass

## Discovered
- CLI client (`internal/cli/client.go`) does not yet attach auth headers — would need update for API key mode to work end-to-end from CLI
- Tests pass at 61.6% overall coverage
- Open beads issue: RalphFactory-mdb — Add API authentication for Anthropic backend
- No `AuthModeAPIKey` exists; only `AuthModeNone` (open) and `AuthModeEntra` (OIDC SSO)
- `auth.Middleware` currently passes through entirely when `provider == nil`
- `cmd/server/main.go` logs WARNING when running without auth
