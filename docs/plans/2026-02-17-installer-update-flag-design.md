# Installer --update Flag Design

**Date:** 2026-02-17
**Status:** Approved

## Problem

Running the installer again after initial setup re-runs the entire interactive flow (mode selection, model setup, notifications, etc.) when the user just wants to update the software. There's no way to quickly update binaries and skills without sitting through all the prompts. Additionally, neither installer stops the running server before overwriting binaries.

## Solution

Add `--update` flag to both bash and PowerShell installers for quick software-only updates. Both the update path and the normal install path should stop the server before replacing binaries and restart it after.

## Design

### New Flag

- Bash: `--update` sets `UPDATE_FLAG=true`
- PowerShell: `-Update` switch parameter

### New Function: `stop_server`

Extract stop logic from `start_server()` into a dedicated function. Always attempt stop, suppress errors if server isn't running.

**Bash:**
- macOS: `launchctl bootout "gui/$(id -u)/com.ralph-o-matic.server" 2>/dev/null || true`
- Linux: `systemctl --user stop ralph-o-matic.service 2>/dev/null || true`

**PowerShell:**
- `Stop-ScheduledTask -TaskName "RalphOMaticServer"` + `Stop-Process`

### Updated `install_skill`

Install all skills (both `brainstorm-to-ralph` and `direct-to-ralph`), not just the first one. Loop over a list of skill names.

### Updated `main()` — Two Paths

**`--update` path:**
```
detect_platform → stop_server → install_binaries → install_skill → start_server → verify
```

**Normal path (add stop_server before install):**
```
... → stop_server → install_binaries → install_skill → configure_ralph → ...
```

### Updated `start_server()`

Remove inline stop logic. Call `stop_server()` then start.

### What --update Skips

| Step | Normal | --update |
|------|--------|----------|
| detect_platform | Yes | Yes |
| prompt_mode | Yes | Skip |
| select_backend | Yes | Skip |
| check_ram | Yes | Skip |
| check/install deps | Yes | Skip |
| model selection/pull | Yes | Skip |
| stop_server | Yes | Yes |
| install_binaries | Yes | Yes |
| install_skill | Yes | Yes |
| configure_ralph | Yes | Skip |
| configure_notifications | Yes | Skip |
| start_server | Yes | Yes |
| apply_model_config | Yes | Skip |
| verify | Yes | Yes |

## Test Plan (17 BATS tests)

### Flag parsing (3 tests)
1. `--update` sets `UPDATE_FLAG=true`
2. `--update` + `--yes` both flags set
3. `--update` + `--backend=anthropic` both parsed

### stop_server function (3 tests)
4. macOS: calls `launchctl bootout` with correct path
5. Linux: calls `systemctl --user stop`
6. No error when server not running

### install_skill multi-skill (4 tests)
7. Installs both skills from local path
8. Falls back to download when local not found
9. Skips when `claude` not found
10. Creates skills directory if missing

### Update flow (3 tests)
11. Calls stop/install/start/verify
12. Does NOT call prompt_mode, select_backend, configure_ralph, etc.
13. With MODE=client skips server stop/start

### Normal install stop-before-install (2 tests)
14. Calls stop_server before install_binaries
15. start_server delegates stop to stop_server

### Edge cases (2 tests)
16. First install (no service) — stop_server is no-op
17. --update + --yes skips "Start server now?" prompt
