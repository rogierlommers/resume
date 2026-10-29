---
title: Pure-Go SQLite preserves static resume builds
date: 2026-10-29
category: tooling-decisions
module: visits analytics
problem_type: tooling_decision
component: database
severity: medium
applies_when:
  - Adding embedded persistence to the resume server
  - Updating the SQLite driver or build pipeline
tags: [sqlite, go, cgo, static-build]
---

# Pure-Go SQLite preserves static resume builds

## Context

The production build deliberately uses `CGO_ENABLED=0`, including for the Linux binary copied into the container. Adding SQLite with a CGO-based driver would make persistence incompatible with that established build rather than merely adding a runtime dependency.

## Guidance

Use a pure-Go SQLite driver for embedded persistence. The visits store uses `modernc.org/sqlite`, while the application-facing `visitsStorage` interface keeps the existing in-memory implementation available. Treat a successful CGO-disabled Linux build as part of the acceptance criteria whenever the driver is added or upgraded:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o ./bin/resume ./src
```

Keep backend selection at startup through `VISITS_DB_PATH`. An unset value retains the in-memory default; a set value opens SQLite and fails startup if initialization is unsafe. This makes rollback additive: unset the variable without deleting or rewriting the database.

## Why This Matters

Go SQLite drivers differ in whether they require a C compiler and libc integration. Unit tests on a development machine can pass even when a driver breaks the actual cross-compiled, CGO-disabled production build. The explicit build check protects the deployment artifact, while the storage interface prevents SQLite concerns from entering request and dashboard logic.

## When to Apply

- When adding or replacing embedded database libraries.
- When changing `go.mod`, the Docker build, or cross-compilation settings.
- When extending visits persistence without removing the in-memory fallback.

## Examples

After a SQLite dependency update, run both `go test -race ./...` and the CGO-disabled Linux build. A normal local `go build` is not sufficient evidence because it may use the host's CGO configuration.

## Related

- `readme.md` documents `VISITS_DB_PATH` and the writable-volume requirement.
- `src/visits_sqlite.go` contains the SQLite adapter and schema initialization.
