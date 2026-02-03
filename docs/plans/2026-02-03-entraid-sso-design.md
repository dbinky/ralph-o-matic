# EntraID SSO Integration Design

## Overview

Add optional EntraID (Azure AD) SSO authentication to ralph-o-matic, supporting a spectrum of deployments from local single-user (no auth) to enterprise multi-user (EntraID SSO). Auth is fully opt-in — existing local deployments are unaffected.

## Goals

- Enterprise teams can deploy ralph-o-matic on an Azure VM and restrict access to authenticated users via EntraID
- Authorization (roles) is managed entirely in EntraID — ralph-o-matic reads role claims from tokens
- Local and local-network deployments continue to work with zero configuration
- The same binary supports all deployment modes

## Non-Goals

- Service account / client credentials flow (future follow-on)
- Multi-tenant EntraID support
- Custom role definitions beyond User/Admin
- Database-backed session storage (single-server assumption)

---

## Auth Mode & Configuration

ralph-o-matic supports two auth modes: `none` (default) and `entra`.

### Configuration Resolution Order

1. Environment variables (highest priority)
2. `settings.json` file at platform-conventional path
3. Defaults (`auth_mode: none`)

### Environment Variables

| Variable | Description |
|----------|-------------|
| `RALPH_AUTH_MODE` | `none` or `entra` |
| `RALPH_ENTRA_TENANT_ID` | Azure AD tenant ID |
| `RALPH_ENTRA_CLIENT_ID` | App registration client ID |
| `RALPH_ENTRA_CLIENT_SECRET` | App registration client secret |
| `RALPH_CONFIG_FILE` | Override path to `settings.json` |

### settings.json

Platform-conventional paths:
- **Linux/macOS:** `/etc/ralph-o-matic/settings.json`
- **Windows:** `%ProgramData%\ralph-o-matic\settings.json`

Overridable via `RALPH_CONFIG_FILE` env var.

```json
{
  "auth": {
    "mode": "entra",
    "entra": {
      "tenant_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "client_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "client_secret": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    }
  }
}
```

### Startup Behavior

- **`auth_mode: none` (or unconfigured):** Server logs a warning — `"WARNING: running without authentication — all endpoints are open"` — and operates exactly as today.
- **`auth_mode: entra`:** Server validates that `tenant_id`, `client_id`, and `client_secret` are present, fetches the OIDC discovery document and JWKS from `login.microsoftonline.com/{tenant_id}`, and enables auth middleware. Server fails to start if the EntraID config is incomplete or the OIDC discovery endpoint is unreachable.

---

## OAuth2 Flows

Two authorization code flows, both using PKCE, for different client types:

| | Browser (dashboard) | CLI |
|---|---|---|
| Flow | Authorization code + PKCE + client secret | Authorization code + PKCE |
| Client type | Confidential | Public |
| Redirect URI | `https://ralph.example.com/auth/callback` | `http://localhost:{random}/callback` |
| Token storage | Server-side session (cookie) | Local file (`~/.config/ralph-o-matic/token.json`) |

Both are registered in the same EntraID app registration with both redirect URI types configured.

### Browser Flow

1. User hits a dashboard page without a valid session
2. Server redirects to `/auth/login`
3. `/auth/login` redirects to EntraID authorize URL with PKCE challenge
4. User signs in at EntraID
5. EntraID redirects back to `/auth/callback?code=...`
6. Server exchanges code for tokens (using client secret + PKCE verifier)
7. Server validates ID token, extracts roles from claims
8. Server creates session, sets cookie, redirects to original URL

### CLI Flow

1. CLI calls `GET /auth/config` to discover auth mode and EntraID parameters
2. CLI starts a temporary HTTP listener on a random localhost port
3. CLI opens user's default browser to EntraID authorize URL with PKCE challenge
4. User signs in at EntraID
5. EntraID redirects to `http://localhost:{port}/callback?code=...`
6. CLI's temp listener catches the callback, exchanges code for tokens using PKCE
7. CLI shows a "you can close this tab" page in the browser, shuts down listener
8. CLI caches token locally, uses it for subsequent requests

---

## Auth Middleware & Request Flow

### Middleware Stack (in order)

1. `middleware.Logger`
2. `middleware.Recoverer`
3. `middleware.Timeout(60s)`
4. `corsMiddleware`
5. `authMiddleware` (no-op when auth mode is `none`)

### Request Handling (auth mode `entra`)

**Browser requests** (no `Authorization` header, accepts `text/html`):
- Check for valid session cookie
- If no cookie or expired: redirect to `/auth/login`

**API requests** (`Authorization: Bearer <token>` header):
- Validate JWT signature against EntraID's JWKS (cached, refreshed periodically)
- Validate claims: `aud` (client ID), `iss` (tenant), `exp`, `nbf`
- Extract app roles from the `roles` claim
- Set user identity and roles on request context
- Return `401` if missing/invalid, `403` if authenticated but lacking required role

### Exempt Routes (no auth required)

- `GET /health` — load balancers and monitoring
- `/auth/*` — login, callback, logout, config endpoints

### Role Enforcement

Per-route authorization via a `requireRole()` wrapper:

| Action | User role | Admin role |
|--------|-----------|------------|
| List jobs | Own jobs only | All jobs |
| View job detail | Own jobs only | All jobs |
| View job logs | Own jobs only | All jobs |
| Submit job | Yes | Yes |
| Cancel job | Own jobs only | All jobs |
| Pause/resume job | Own jobs only | All jobs |
| Reorder jobs | Own jobs only | All jobs |
| View server config | Yes | Yes |
| Update server config | No | Yes |

---

## Session & Token Management

### Server-Side Sessions (Browser)

Stored in-memory, keyed by random session ID in a secure cookie. Session contains:
- User identity (name, email, OID from EntraID)
- Roles (from the `roles` claim on the ID token)
- Access token
- Access token expiry
- Refresh token (for silent renewal)

When the access token nears expiry, the server uses the refresh token transparently. If the refresh token is also expired, the next request triggers a re-login redirect. Server restart clears all sessions; users re-authenticate.

**Cookie properties:**
- `HttpOnly` flag set
- `Secure` flag set when server URL is HTTPS
- `SameSite=Lax`
- `Path=/`
- Value is opaque (random ID, not a JWT)

### CLI Token Caching

Tokens stored at `~/.config/ralph-o-matic/token.json` with `0600` permissions:

```json
{
  "access_token": "eyJ...",
  "refresh_token": "...",
  "expires_at": "2026-02-03T15:04:05Z",
  "server": "https://ralph.example.com"
}
```

Tokens are scoped per server URL. CLI checks expiry before each request. If expired, attempts silent refresh. If refresh fails, re-launches browser flow.

### Auth Config Discovery

`GET /auth/config` — unauthenticated endpoint returning:
```json
{
  "mode": "entra",
  "client_id": "...",
  "tenant_id": "..."
}
```

Rate limited to 10 requests per minute per IP. Returns `429 Too Many Requests` when exceeded. Response includes `Cache-Control` headers.

---

## CLI Auth User Experience

**First time (auth-enabled server):**
```
$ ralph submit --prompt "Refactor error handling"
Authentication required. Opening browser to sign in...

  If the browser doesn't open, visit:
  https://login.microsoftonline.com/tenant-id/oauth2/v2.0/authorize?...

Waiting for sign-in...
✓ Authenticated as ryan@contoso.com (Admin)
Token cached at ~/.config/ralph-o-matic/token.json

Job submitted: job-abc123 (queued, position 1)
```

**Subsequent requests (cached token):**
```
$ ralph status
# works immediately, no browser, no prompt
```

**Token expired, refresh succeeds:**
```
$ ralph status
# silent refresh, user sees nothing different
```

**Token expired, refresh fails:**
```
$ ralph status
Session expired. Opening browser to re-authenticate...
```

**Local server, no auth:**
```
$ ralph submit --prompt "Fix tests"
Job submitted: job-def456 (queued, position 1)
# no auth flow, same as today
```

---

## EntraID App Registration

### Configuration

- **Display name:** `ralph-o-matic` (configurable)
- **Supported account types:** Single tenant
- **Platform configurations:**
  - Web: redirect URI `https://ralph.example.com/auth/callback`
  - Mobile/Desktop: redirect URI `http://localhost`

### App Roles

| Role | Value | Description | Allowed member types |
|------|-------|-------------|---------------------|
| User | `User` | Submit and manage own jobs | Users/Groups |
| Admin | `Admin` | Full access including server config | Users/Groups |

Users with no assigned role get `403` — being in the tenant is not sufficient.

### Token Claims Used

| Claim | Purpose |
|-------|---------|
| `aud` | Must match client ID |
| `iss` | Must match tenant |
| `roles` | Array of app role values (`["User"]`, `["Admin"]`) |
| `preferred_username` | Display (email) |
| `name` | Display name |
| `oid` | Stable user ID for job ownership |

### Scopes Requested

- `openid`, `profile` — identity
- `offline_access` — refresh tokens

No custom API scopes needed.

---

## Job Ownership

### Model Changes

Two new fields on the `Job` model:
- `owner_id` — EntraID `oid` claim (stable GUID)
- `owner_name` — display name from token claims

Set automatically at job creation from authenticated user's claims. Empty when auth is `none`.

### Database Migration

```sql
ALTER TABLE jobs ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';
```

Migration runs unconditionally. Empty defaults maintain backward compatibility.

### Dashboard Changes

When auth is enabled, the dashboard shows job owner's name in the job list. Non-admin users see only their own jobs (filtered server-side).

---

## Package Structure

```
internal/auth/
  auth.go        — AuthMode type, Config struct, LoadConfig()
  middleware.go   — Chi middleware: authMiddleware, requireRole()
  entra.go       — OIDC discovery, JWKS caching, token validation,
                    authorization code + PKCE flow
  session.go     — In-memory session store, cookie management
  context.go     — Helpers: UserFromContext(), RolesFromContext()
```

### Integration Points

**Server (`internal/api/server.go`):** `Server` struct gets an `authConfig` field. If `mode == none`: log warning, skip auth middleware. If `mode == entra`: initialize OIDC provider, register `/auth/*` routes, add auth middleware.

**CLI (`internal/cli/client.go`):** `Client` struct gets a `tokenSource` field — an interface that returns a valid token or triggers the login flow. When auth is `none`, the token source is a no-op.

**No changes to:** `internal/queue`, `internal/executor`, `internal/db`, `internal/git`. Job ownership is set at the API layer.

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/coreos/go-oidc/v3` | OIDC discovery, JWKS fetching, ID token verification |
| `golang.org/x/oauth2` | OAuth2 flows (authorization code, token exchange, refresh) |
| `github.com/pkg/browser` | Opens user's default browser from CLI (cross-platform) |

Not adding session or JWT libraries — in-memory sessions are sufficient, and `go-oidc` handles token validation internally.

---

## PowerShell Setup Script

Located at `scripts/setup-entra.ps1`. Guides an EntraID admin through app registration setup using `az` CLI.

### Prerequisites

- `az` CLI installed and logged in
- Logged-in user has permission to create app registrations in the tenant

### Script Phases

**Phase 1: Pre-flight checks**
- Verify `az` CLI is installed
- Verify `az` CLI is logged in (`az account show`)
- Verify the logged-in user has permission to create app registrations
- Display tenant name, tenant ID, signed-in user
- Prompt: "Continue with this tenant? (Y/n)"

**Phase 2: Gather details**
- Prompt for app display name (default: "ralph-o-matic")
- Prompt for server URL (e.g., `https://ralph.example.com`)
  - Validate HTTPS; warn if HTTP, allow override for dev
- Prompt for CLI redirect URI (default: `http://localhost`, explain why)

**Phase 3: Create app registration (with confirmations at each step)**

Each step shows plain English description, the exact `az` command, and asks "Proceed? (Y/n)":

1. Create app registration (`az ad app create`)
2. Define app roles: User and Admin (`az ad app update --app-roles`)
3. Add web redirect URI for dashboard (`az ad app update --web-redirect-uris`)
4. Add localhost redirect URI for CLI (`az ad app update --public-client-redirect-uris`)
5. Create client secret, 1 year validity (`az ad app credential reset`)
6. Create service principal for user assignment (`az ad sp create`)

**Phase 4: Summary**
- Display all created resources
- Output configuration in both formats:

```
Setup complete! Configure your ralph-o-matic server with:

Environment variables:
  RALPH_AUTH_MODE=entra
  RALPH_ENTRA_TENANT_ID=xxxxxxxx-...
  RALPH_ENTRA_CLIENT_ID=xxxxxxxx-...
  RALPH_ENTRA_CLIENT_SECRET=xxxxxxxx-...

Or add to /etc/ralph-o-matic/settings.json:
  { "auth": { "mode": "entra", "entra": { ... } } }
```

- Remind admin to assign users/groups to roles in Azure Portal
- Warn about client secret expiry date

### Error Handling

Each step checks `az` command exit code. On failure: display error, explain what went wrong, offer retry or abort. Script tells the admin what was already created and how to clean up.

---

## Migration Path & Backward Compatibility

**Zero breaking changes for existing users.**

- No env vars + no `settings.json` = auth mode `none` = today's behavior
- Database migration adds `owner_id` and `owner_name` columns unconditionally (empty string defaults, no effect on behavior)
- CLI works against unauthenticated servers with no config changes
- API response shape is additive — new fields are empty when auth is off

### Enabling Auth on an Existing Instance

1. Admin runs `setup-entra.ps1`, gets tenant/client IDs and secret
2. Admin sets env vars or creates `settings.json` on the server
3. Admin restarts server — auth middleware activates
4. Admin assigns users to roles in Azure Portal
5. Users' next CLI command triggers browser login flow
6. Existing jobs continue uninterrupted — empty `owner_id`, visible to all, manageable by admins

### Disabling Auth

Remove env vars or delete auth block from `settings.json`, restart. Server returns to `none` mode. Jobs retain `owner_id`/`owner_name` in DB but fields are ignored.

---

## Testing Strategy

All tests follow strict TDD: tests written before implementation, covering happy path, success, failure, error, and edge case scenarios.

### 11a: Config Loading

**Package:** `internal/auth` | **Function:** `LoadConfig()`

**Test approach:** Pure unit tests. `LoadConfig` accepts an `io.Reader` for file content and a func for env var lookup — fully testable without filesystem or real environment.

**Happy path:**
- Env vars fully set → returns correct `EntraConfig`
- `settings.json` with complete auth block → returns correct config
- No env vars, no file → returns `AuthConfig{Mode: "none"}`

**Precedence / success:**
- Env vars override `settings.json` — set both, env vars win
- Partial env vars with `settings.json` fallback
- `RALPH_CONFIG_FILE` overrides platform-conventional path
- Platform path resolution returns correct path per OS

**Failure:**
- `settings.json` is malformed JSON → descriptive error
- `settings.json` is unreadable (permissions) → error with path
- `auth_mode=entra` but `tenant_id` missing → validation error listing missing fields
- `auth_mode=entra` but `client_secret` missing → validation error
- Unknown `auth_mode` value → error

**Edge cases:**
- Empty `settings.json` (`{}`) → defaults to `none`
- Auth block present but `mode` is empty string → defaults to `none`
- Env vars set to empty strings → treated as unset, fall through to file
- `RALPH_CONFIG_FILE` path doesn't exist → fall through to defaults (not error)

### 11b: Auth Middleware

**Package:** `internal/auth` | **Functions:** `authMiddleware()`, `requireRole()`

**Test infrastructure:** `httptest.Server` with Chi router, test JWKS endpoint, helper to mint valid/invalid JWTs.

**Happy path — auth mode `none`:**
- All requests pass through, no context values set
- No headers or cookies needed

**Happy path — API requests (Bearer token):**
- Valid JWT with `User` role → request proceeds, correct identity in context
- Valid JWT with `Admin` role → roles include `Admin`
- Valid JWT with both roles → both available in context

**Happy path — browser requests (session cookie):**
- Valid session cookie → request proceeds
- `/auth/login` → redirects to EntraID with correct parameters
- `/auth/callback` with valid code → creates session, sets cookie, redirects

**Failure — API requests:**
- No `Authorization` header → `401`
- `Bearer` with no token → `401`
- `Basic` scheme → `401`
- Expired JWT → `401`
- Wrong signing key → `401`
- Wrong `aud` → `401`
- Wrong `iss` → `401`
- Valid JWT, no `roles` claim → `403`
- `User` role on admin-only endpoint → `403`

**Failure — browser requests:**
- No session cookie → redirect to `/auth/login`
- Expired session → redirect to `/auth/login`
- Tampered cookie → redirect to `/auth/login`
- `/auth/callback` missing `code` → error page
- `/auth/callback` invalid `state` (CSRF) → error page

**Edge cases:**
- JWT `nbf` in the future → `401`
- JWT `exp` exactly at current time → defined boundary behavior
- Both cookie and Bearer present → Bearer takes precedence
- JWKS unavailable, cached keys exist → use cache
- JWKS unavailable, no cache → `500`
- Concurrent requests during JWKS rotation → no races (`-race`)
- Oversized JWT → rejected before full parse

**Role enforcement:**
- `User` accessing own job → allowed
- `User` accessing other's job → `403`
- `Admin` accessing any job → allowed
- `User` on `PATCH /api/config` → `403`
- `Admin` on `PATCH /api/config` → allowed
- Job with empty `owner_id` (pre-auth) → accessible by any authenticated user

### 11c: Session Management

**Package:** `internal/auth` | **Struct:** `SessionStore`

**Happy path:**
- Create session → retrievable by ID
- Get by valid ID → correct identity and roles
- Delete session → no longer retrievable
- Session with refresh token → stored and retrievable

**Success:**
- Multiple concurrent sessions for different users → isolated
- Same user, multiple sessions → both valid independently
- Full lifecycle: create, retrieve, delete

**Failure:**
- Unknown session ID → nil/error
- Empty string ID → nil/error
- Delete nonexistent session → no-op

**Expiry:**
- Session before expiry → retrievable; after → nil
- Refresh extends expiry
- Refresh token exchange fails → session marked expired

**Edge cases:**
- 1000 concurrent create/get/delete with `-race`
- Valid UUID format but not in store → unauthenticated
- New store instance clears all sessions
- Silent token refresh updates session, same session ID
- Expired sessions cleaned up (no memory leak)

**Cookie security (via `httptest`):**
- `HttpOnly`, `Secure` (when HTTPS), `SameSite=Lax`, `Path=/`
- Cookie value is opaque

### 11d: CLI Auth Flow

**Package:** `internal/cli`

**Test infrastructure:** `httptest.Server` for mock ralph-o-matic server and mock EntraID token endpoint. Browser-open function injected as dependency.

**Happy path:**
- Server `auth.mode=none` → no auth, no `Authorization` header
- Server `auth.mode=entra` → localhost listener, browser-open called with correct URL, mock callback delivers code, token obtained, Bearer header sent
- Cached token valid → no browser flow
- Cached token expired, refresh valid → silent refresh

**Success:**
- First request: full flow → cached → second request uses cache
- Token for different server URL → not used, starts new flow
- Multiple servers in cache → correct token per server
- PKCE `code_challenge` and `code_challenge_method=S256` present
- Localhost listener shuts down after callback

**Failure:**
- `/auth/config` unreachable → clear error
- `/auth/config` unexpected format → error with details
- `/auth/config` rate-limited (`429`) → retry then error
- User closes browser without completing → timeout with helpful message
- Token exchange fails → error, no partial token cached
- Refresh rejected → cached token deleted, browser flow restarted
- Token file corrupted → delete and start fresh
- Token file wrong permissions → warn, re-create with `0600`

**Edge cases:**
- Port conflict → try another random port
- Two CLI commands need auth simultaneously → file lock, second waits
- Token expires between check and request → `401`, retry with refresh
- Server switches `entra` → `none` → CLI stops sending Bearer
- Server switches `none` → `entra` → CLI discovers via `401`, initiates flow
- PKCE verifier is correct length (43-128 chars per RFC 7636)

### 11e: EntraID Integration

**Package:** `internal/auth`

**Test infrastructure:** `httptest.Server` with mock OIDC discovery and JWKS. Test RSA key pair for signing JWTs.

**Happy path:**
- OIDC discovery parses correctly
- JWKS returns valid key set
- Token signed with test key validates
- `roles` with single role → `[]string{"User"}`
- `roles` with multiple roles → `[]string{"User", "Admin"}`

**Success:**
- JWKS key rotation — old and new keys both validate
- JWKS cached — no re-fetch for subsequent validations
- Unknown `kid` triggers single JWKS re-fetch before failing

**Failure:**
- OIDC discovery non-200 → startup error
- OIDC discovery invalid JSON → startup error
- JWKS non-200 → error
- JWKS valid JSON, no matching `kid` → `401` after re-fetch
- Token with no `kid` → `401`
- Unsupported algorithm (`HS256`) → `401`
- `alg: none` → `401`

**Error:**
- OIDC discovery timeout → startup fails
- JWKS timeout at runtime → use cache if available
- Network error during token exchange → descriptive error

**Edge cases:**
- `roles` is empty array → `403` on role-protected endpoints
- No `roles` claim → `403`
- Missing `oid` claim → reject
- Wrong tenant's token → rejected by issuer
- Clock skew tolerance configured and tested
- Very long `roles` array → only `User`/`Admin` recognized
- Duplicate `kid` in JWKS → deterministic (first match)

### 11f: Rate Limiting & Job Ownership

**Rate limiting on `GET /auth/config`:**

- Single request → `200`
- 10 requests quickly from same IP → all succeed
- 11th request within minute → `429` with `Retry-After`
- Different IPs → independent buckets
- Rate resets after window → requests succeed
- Concurrent load → no races (`-race`)
- Behind reverse proxy → rate by `X-Forwarded-For` (configurable)

**Job ownership — auth enabled:**
- Create job → `owner_id`/`owner_name` set from claims
- List as `User` → own jobs only
- List as `Admin` → all jobs
- Get/cancel/pause own job as `User` → success

**Job ownership — auth disabled:**
- Create job → empty `owner_id`/`owner_name`
- List → all jobs
- All operations allowed

**Failure:**
- `User` accessing other's job → `403`
- `User` canceling other's job → `403`
- `User` on `PATCH /api/config` → `403`
- Unauthenticated on protected endpoint → `401`

**Edge cases:**
- Pre-auth job (empty `owner_id`) → visible to all authenticated users, manageable by admins
- `Admin` cancels other's job → succeeds, `owner_id` retained
- Status filter + ownership filter → both applied
- Pagination + ownership → page counts reflect filtered set
- Reorder as `User` → only own jobs

**Database migration:**
- Adds columns to existing table → existing rows get empty defaults
- `owner_id` filter query returns correct subset
- No filter (auth off) returns all

### 11g: Integration & End-to-End

Tagged `//go:build integration`. Full server with mock EntraID provider.

**Full lifecycle:**
- Server starts with `auth_mode=entra` and mock OIDC → auth enabled
- Unauthenticated browser request → redirect to mock EntraID
- Unauthenticated API request → `401`
- Browser flow → session created, dashboard renders with user name
- CLI flow → token obtained, API succeeds
- Submit with auth → correct `owner_id`
- User A's job viewed by User B → `403`
- User A's job viewed by Admin → `200`

**Mode switching:**
- No auth config → all endpoints open, startup warning logged
- Incomplete entra config → server fails to start with clear error

**Health endpoint:**
- `GET /health` with auth enabled, no token → `200`

### PowerShell Script Tests (Pester)

**Pre-flight:**
- `az` not installed → exit with message
- `az` not logged in → exit with message
- No app registration permission → exit with error
- Correct permissions → displays tenant info, proceeds

**Interactive flow (mocked `az`):**
- All steps confirmed → all resources created
- Decline at step 3 → steps 1-2 completed, clean exit, reports what was created
- Decline at step 1 → nothing created
- `az ad app create` fails → error shown, retry or abort
- `az ad app update` fails → reports what was created, advises cleanup

**Output validation:**
- Correct tenant ID, client ID, client secret in output
- Both env var and settings.json formats present
- Client secret expiry warning with correct date

### Test Summary

| Category | Happy | Success | Failure | Error | Edge |
|----------|-------|---------|---------|-------|------|
| Config loading | 3 | 4 | 5 | — | 5 |
| Auth middleware | 4 | 3 | 9 | — | 7 |
| Session management | 4 | 3 | 3 | 2 | 5 |
| CLI auth flow | 3 | 6 | 7 | — | 6 |
| EntraID integration | 5 | 3 | 6 | 3 | 6 |
| Rate limiting | 2 | — | 1 | — | 4 |
| Job ownership | 6 | 3 | 5 | — | 5 |
| Integration/E2E | 8 | — | 2 | — | 1 |
| PowerShell (Pester) | — | 2 | 4 | — | — |

All tests use mock OIDC providers. Real EntraID integration is validated manually during initial deployment. This avoids tenant credentials in CI, network flakiness, and slow test runs.
