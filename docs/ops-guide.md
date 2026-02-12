# Operations & Deployment Guide

This guide covers deploying ralph-o-matic for a team — reverse proxy setup, TLS, backup, monitoring, and production hardening. For basic installation and usage, see the [README](../README.md).

## Prerequisites

| Component | Required | Notes |
|-----------|----------|-------|
| `ralph-o-matic-server` | Yes | Binary or build from source |
| `git` | Yes | Used for repo operations |
| `gh` (GitHub CLI) | Yes | PR creation, authenticated clones |
| `claude` (Claude Code) | Yes | Execution engine |
| `ollama` | If using Ollama backend | Local model inference |

The server runs as a single process with an embedded SQLite database. There is no external database, cache, or message broker to manage.

## Server Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `RALPH_ADDR` | `:9090` | Listen address (`host:port` or `:port`) |
| `RALPH_DB` | `ralph.db` (cwd) | SQLite database file path |
| `RALPH_SECURE` | `false` | Set `true` behind HTTPS (enables Secure cookie flag) |
| `RALPH_AUTH_MODE` | `none` | `none` or `entra` (Microsoft Entra ID SSO) |
| `RALPH_ENTRA_TENANT_ID` | | Entra tenant ID (required if auth mode is `entra`) |
| `RALPH_ENTRA_CLIENT_ID` | | Entra client ID |
| `RALPH_ENTRA_CLIENT_SECRET` | | Entra client secret |
| `RALPH_CONFIG_FILE` | `/etc/ralph-o-matic/settings.json` | Auth settings file path |
| `ANTHROPIC_API_KEY` | | Anthropic API key (preferred over storing in DB config) |

### Listen Address

Bind to localhost when behind a reverse proxy:

```bash
RALPH_ADDR=127.0.0.1:9090
```

Bind to all interfaces for direct access (not recommended in production):

```bash
RALPH_ADDR=:9090
```

### Database Path

Place the database on reliable storage with the service user's ownership:

```bash
RALPH_DB=/var/lib/ralph-o-matic/ralph.db
```

SQLite creates WAL and SHM files alongside the database (`ralph.db-wal`, `ralph.db-shm`). Ensure the directory is writable.

## Service Management

### systemd (Linux)

Create `/etc/systemd/system/ralph-o-matic.service`:

```ini
[Unit]
Description=ralph-o-matic server
After=network.target ollama.service
Wants=ollama.service

[Service]
Type=simple
User=ralph
Group=ralph
WorkingDirectory=/var/lib/ralph-o-matic
ExecStart=/usr/local/bin/ralph-o-matic-server
Restart=on-failure
RestartSec=5

Environment=RALPH_ADDR=127.0.0.1:9090
Environment=RALPH_DB=/var/lib/ralph-o-matic/ralph.db
Environment=RALPH_SECURE=true
Environment=RALPH_AUTH_MODE=entra
EnvironmentFile=-/etc/ralph-o-matic/env

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/var/lib/ralph-o-matic
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Store secrets in `/etc/ralph-o-matic/env` (mode `0600`):

```bash
RALPH_ENTRA_TENANT_ID=your-tenant-id
RALPH_ENTRA_CLIENT_ID=your-client-id
RALPH_ENTRA_CLIENT_SECRET=your-client-secret
ANTHROPIC_API_KEY=sk-ant-...
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ralph-o-matic
sudo journalctl -u ralph-o-matic -f   # follow logs
```

### launchd (macOS)

The install script creates `~/Library/LaunchAgents/com.ralph-o-matic.server.plist`. For a team server, use a dedicated macOS user account and ensure the agent loads at login.

Logs go to `~/.config/ralph-o-matic/logs/server.log` and `server.err`.

## Reverse Proxy & TLS

The server listens on HTTP. Terminate TLS at a reverse proxy and forward to the local listener.

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name ralph.example.com;

    ssl_certificate     /etc/ssl/certs/ralph.example.com.pem;
    ssl_certificate_key /etc/ssl/private/ralph.example.com.key;

    # SSE support — disable buffering for event streams
    location /api/events {
        proxy_pass http://127.0.0.1:9090;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
    }

    location /api/jobs/ {
        # Job-specific SSE streams
        if ($uri ~ /events$) {
            proxy_pass http://127.0.0.1:9090;
            proxy_http_version 1.1;
            proxy_set_header Connection "";
            proxy_buffering off;
            proxy_cache off;
            proxy_read_timeout 3600s;
        }

        proxy_pass http://127.0.0.1:9090;
    }

    location / {
        proxy_pass http://127.0.0.1:9090;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen 80;
    server_name ralph.example.com;
    return 301 https://$host$request_uri;
}
```

### Caddy

```
ralph.example.com {
    reverse_proxy 127.0.0.1:9090 {
        # Flush immediately for SSE
        flush_interval -1
    }
}
```

Caddy provisions TLS certificates automatically via Let's Encrypt.

### Important: SSE Considerations

ralph-o-matic uses Server-Sent Events (SSE) for live dashboard updates. Your reverse proxy must:

- **Disable response buffering** for `/api/events` and `/api/jobs/{id}/events`
- **Set a long read timeout** (at least 1 hour) — SSE connections are long-lived
- **Not cache** event stream responses

If the dashboard shows stale data or doesn't update, check proxy buffering settings first.

### Enabling Secure Cookies

When serving over HTTPS, set `RALPH_SECURE=true` so session cookies include the `Secure` flag:

```bash
Environment=RALPH_SECURE=true
```

Without this, session cookies work only over plain HTTP (unsuitable for production with auth enabled).

## Backup

### SQLite Backup Strategy

The database is a single SQLite file with WAL mode enabled. **Do not copy the `.db` file while the server is running** — you may get a corrupt backup.

#### Option 1: sqlite3 .backup (recommended)

```bash
#!/bin/bash
# /etc/cron.daily/ralph-backup
BACKUP_DIR=/var/backups/ralph-o-matic
DB=/var/lib/ralph-o-matic/ralph.db
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

mkdir -p "$BACKUP_DIR"
sqlite3 "$DB" ".backup '$BACKUP_DIR/ralph-$TIMESTAMP.db'"

# Retain 30 days
find "$BACKUP_DIR" -name "ralph-*.db" -mtime +30 -delete
```

`sqlite3 .backup` uses SQLite's online backup API and is safe to run while the server is writing.

#### Option 2: Filesystem Snapshot

If your storage supports atomic snapshots (LVM, ZFS, btrfs), snapshot the directory containing the database, WAL, and SHM files together.

#### What to Back Up

| Path | Contents |
|------|----------|
| `$RALPH_DB` (and `-wal`, `-shm`) | Jobs, config, logs |
| `/etc/ralph-o-matic/env` | Secrets |
| Reverse proxy config | TLS certs, proxy rules |

Workspace directories (`workspaces/`) contain cloned repos and are recreated on demand. Backing them up is optional.

### Restore

```bash
sudo systemctl stop ralph-o-matic
cp /var/backups/ralph-o-matic/ralph-20260212-030000.db /var/lib/ralph-o-matic/ralph.db
rm -f /var/lib/ralph-o-matic/ralph.db-wal /var/lib/ralph-o-matic/ralph.db-shm
sudo systemctl start ralph-o-matic
```

Migrations run automatically on startup, so restoring an older backup is safe — the server applies any missing migrations.

## Monitoring

### Health & Readiness

**Liveness** — confirms the process is running. Use for restart-on-hang detection:

```bash
curl -sf http://127.0.0.1:9090/health
# {"status":"ok"}
```

**Readiness** — confirms DB, Ollama (if used), and disk are healthy. Use for
load balancer routing and monitoring alerts:

```bash
curl -sf http://127.0.0.1:9090/readiness
# {"status":"ok","checks":{"database":"ok","disk":"ok","ollama":"ok"}}
```

Returns HTTP 200 when all checks pass, 503 when any check fails. Individual
check messages describe the failure:

```json
{"status":"unhealthy","checks":{"database":"ok","disk":"ok","ollama":"failed to connect to Ollama at http://localhost:11434: ..."}}
```

Both endpoints are always accessible (no auth required).

**Kubernetes / systemd probe guidance:**

| Probe     | Endpoint     | Interval | Timeout | Failure threshold |
|-----------|-------------|----------|---------|-------------------|
| Liveness  | `/health`    | 10s      | 1s      | 3                 |
| Readiness | `/readiness` | 15s      | 6s      | 2                 |

Example systemd watchdog integration:

```ini
# Add to [Service] section
ExecStartPost=/bin/sh -c 'for i in $(seq 1 30); do curl -sf http://127.0.0.1:9090/readiness && exit 0; sleep 1; done; exit 1'
```

### Log Management

The server logs to stdout/stderr using Go's `slog` structured logger. With systemd, logs go to the journal:

```bash
# Follow logs
journalctl -u ralph-o-matic -f

# Recent errors
journalctl -u ralph-o-matic --priority=err --since="1 hour ago"

# Filter by time
journalctl -u ralph-o-matic --since="2026-02-12 09:00" --until="2026-02-12 10:00"
```

For log aggregation, forward the journal to your centralized logging system (Loki, ELK, etc.) using `journald` exporters or `vector`/`promtail`.

### Uptime Monitoring

Point your monitoring tool at the health endpoint:

| Tool | Configuration |
|------|--------------|
| UptimeRobot / Uptime Kuma | HTTP check on `https://ralph.example.com/health`, expect 200 |
| Prometheus blackbox_exporter | `http_2xx` probe against `/health` |
| Simple cron | `curl -sf https://ralph.example.com/health || alert` |

### Notifications as Monitoring

ralph-o-matic sends notifications on job completion, failure, and cancellation. Configure SMTP or Teams webhooks to get alerted on failures:

```bash
ralph-o-matic server-config set notify.smtp.enabled true
ralph-o-matic server-config set notify.smtp.host smtp.example.com
# ... (see README for full SMTP/Teams config)
ralph-o-matic test-notify smtp
```

Failed jobs with circuit breaker trips indicate systemic issues worth investigating.

### Resource Monitoring

Key metrics to watch on the host:

| Metric | Why |
|--------|-----|
| Disk space | SQLite DB grows with job logs; workspace clones use disk during execution |
| RAM usage | Ollama models are memory-resident; large models need 15-20 GB |
| CPU usage | Model inference is CPU-intensive when running on CPU |
| GPU VRAM | If using GPU inference, monitor VRAM to avoid OOM |

The `job_retention_days` config (default: 30) controls automatic cleanup of old job records. Workspace directories for completed jobs are cleaned up hourly.

## Production Hardening Checklist

- [ ] **TLS** — Serve over HTTPS via reverse proxy; set `RALPH_SECURE=true`
- [ ] **Authentication** — Enable Entra ID SSO or restrict network access (VPN/firewall)
- [ ] **Bind to localhost** — Set `RALPH_ADDR=127.0.0.1:9090` when behind a reverse proxy
- [ ] **Secrets management** — Store credentials in environment files (mode `0600`), not in DB config
- [ ] **Firewall** — Block direct access to port 9090 from the network
- [ ] **Dedicated user** — Run the service as a non-root user with minimal permissions
- [ ] **Backup** — Schedule daily SQLite backups with retention
- [ ] **Monitoring** — Health check polling + log aggregation
- [ ] **Disk space** — Monitor and set `job_retention_days` to limit DB growth
- [ ] **GitHub CLI auth** — Ensure `gh auth status` works for the service user (needed for PR creation)
- [ ] **Git config** — Set `git config --global user.name` and `user.email` for the service user

## Authentication Setup

### No Auth (Default)

Suitable for trusted networks, single-user setups, or when access is controlled by VPN/firewall.

### Microsoft Entra ID SSO

1. Register an application in the Azure portal
2. Set the redirect URI to `https://ralph.example.com/auth/callback`
3. Create a client secret
4. Configure the server:

```bash
export RALPH_AUTH_MODE=entra
export RALPH_ENTRA_TENANT_ID=your-tenant-id
export RALPH_ENTRA_CLIENT_ID=your-client-id
export RALPH_ENTRA_CLIENT_SECRET=your-client-secret
export RALPH_SECURE=true
```

Admin users can be configured in the settings file (`RALPH_CONFIG_FILE`):

```json
{
  "admin_users": ["user@example.com"]
}
```

Admin-only endpoints: config changes (`PATCH /api/config`), queue reordering (`PUT /api/jobs/order`), test notifications (`POST /api/config/test-notify`).

## Workspace and Disk Management

Jobs clone repositories into the workspace directory (default: `workspaces/` relative to the working directory). For team deployments, configure an absolute path with adequate disk space:

```bash
ralph-o-matic server-config set workspace_dir /var/lib/ralph-o-matic/workspaces
```

The built-in cleaner runs hourly and:
- Removes workspace directories for completed, failed, and cancelled jobs
- Deletes job records older than `job_retention_days` (default: 30, set to 0 to keep forever)

For large teams, monitor disk usage on the workspace volume — concurrent jobs each clone a full repository.

## Example: Minimal Team Deployment

```bash
# 1. Create service user
sudo useradd -r -m -d /var/lib/ralph-o-matic ralph

# 2. Install binary
sudo cp ralph-o-matic-server /usr/local/bin/

# 3. Create directories
sudo mkdir -p /var/lib/ralph-o-matic/workspaces
sudo mkdir -p /etc/ralph-o-matic
sudo chown -R ralph:ralph /var/lib/ralph-o-matic

# 4. Write secrets
sudo tee /etc/ralph-o-matic/env > /dev/null <<EOF
RALPH_ENTRA_TENANT_ID=...
RALPH_ENTRA_CLIENT_ID=...
RALPH_ENTRA_CLIENT_SECRET=...
ANTHROPIC_API_KEY=sk-ant-...
EOF
sudo chmod 600 /etc/ralph-o-matic/env

# 5. Install systemd unit (see above)
sudo systemctl enable --now ralph-o-matic

# 6. Set up reverse proxy (nginx/caddy, see above)

# 7. Configure the server
ralph-o-matic server-config set workspace_dir /var/lib/ralph-o-matic/workspaces
ralph-o-matic server-config set notify.smtp.enabled true
# ... configure notification settings

# 8. Verify
curl -sf https://ralph.example.com/health
ralph-o-matic test-notify smtp
```

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| Dashboard doesn't update live | Reverse proxy buffering SSE | Disable buffering for `/api/events` routes |
| Session cookies not persisting | Missing `RALPH_SECURE=true` behind HTTPS | Set the env var |
| Jobs stuck in `queued` | Worker not polling, or Ollama down | Check `journalctl` logs; verify `ollama serve` is running |
| "permission denied" on workspace | Service user can't write workspace dir | `chown ralph:ralph /var/lib/ralph-o-matic/workspaces` |
| PRs not created | `gh` not authenticated for service user | Run `gh auth login` as the service user |
| Database locked errors | Multiple server processes | Ensure only one instance runs |
| High disk usage | Old workspaces not cleaned | Check cleaner logs; verify `job_retention_days` is set |
