## Context

`cmd/pipeline`'s `ingest` subcommand hard-codes `bag.Ingest(...)` then `gebieden.Ingest(...)` in
`runIngest`. The sources are already idempotent and independent; only the CLI invocation couples
them. This change makes the invocation per-source without touching the ingest packages.

## Goals / Non-Goals

**Goals:**

- Run one, several, or all ingest sources from `pipeline ingest`, so each can be scheduled on its own
  cadence.
- Keep the source set defined in one discoverable place, extensible to future lanes.

**Non-Goals:**

- No change to any source's ingest/landing logic or idempotency.
- No per-source selection for `pipeline load` (the geo load is a single silver unit); that can be a
  separate change if ever needed.
- No scheduler/cron config in-repo — this only makes per-source invocation possible; the schedules
  live in the operator's cron / k3s CronJobs.

## Decisions

### D1 — positional source names over a flag

`pipeline ingest [source ...]` reads more naturally for cron and composes with shell better than a
repeatable `--source` flag, and matches how a future `pipeline ingest koop` would read. No args =
all sources, preserving today's behavior.

### D2 — a name→ingester registry in `cmd/pipeline`

Sources register in a single ordered map/table (`name → func(ctx, *shared.RawStore) error`) in
`cmd/pipeline`. The `ingest` RunE resolves the requested names against it: empty args → run all in
registration order; named args → run those in the given order; an unknown name → non-zero error
listing the valid names. The resolution (names → ordered ingester set, unknown detection) is a pure
function, unit-tested without network or DB.
