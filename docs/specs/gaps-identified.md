# Gaps Identified

Tracking spec-vs-implementation divergences and quality issues discovered during review.
Items are removed from this list when fixed.

## Spec Divergences

- [ ] Spec says server binds "LAN IP only (not 0.0.0.0)"; server actually binds to `:9090` (all interfaces) by default

## Won't Fix

These items may be divergences from the original spec, but have changed in subsequent spec revisions within the `docs/plans` directory.  Only the user may move items into the "Won't Fix" category - do not do it yourself.  Once an item is in this section, you can safely ignore it.

- [ ] Spec says `ConcurrentJobs` is a server config field (default: 1); implementation has no such field in `ServerConfig` (it was intentionally removed per design doc `2026-02-04-remove-concurrent-jobs-design.md`, so this is expected)