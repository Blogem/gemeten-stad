## Why

`pipeline ingest` currently runs every source in one invocation (BAG, then `gebieden`/CBS). The
sources have different natural refresh cadences — the BAG _LV 2.0 Extract_ is a monthly full reload,
the `gebieden`/CBS boundaries change rarely (weekly is ample), and the later `koop`/`bomen` lanes
will be daily/weekly. With an all-or-nothing `ingest` there is no way to schedule a source on its own
cadence (a monthly cron and a weekly cron would both re-run everything). Per-source selection lets
each source map to its own cron entry / k3s CronJob.

## What Changes

- **`pipeline ingest` accepts optional positional source names.** `pipeline ingest bag` ingests only
  BAG; `pipeline ingest gebieden` only the boundaries; `pipeline ingest` (no args) ingests all
  sources, exactly as today. An unknown name fails fast (non-zero) with the valid names listed.
- Sources are registered by name in one place so the set is discoverable and extends cleanly to the
  future `koop`/`bomen` lanes. Each source stays idempotent and independently runnable (unchanged
  ingest logic) — only the CLI selection is new.

## Capabilities

### Modified Capabilities

- `geo-ingest`: adds per-source selection on the `pipeline ingest` command (positional source names;
  no args = all). The landing/idempotency behavior of each source is unchanged.

## Impact

- **Code:** `cmd/pipeline` (the `ingest` subcommand wiring + a small source registry). No change to
  `ingest/bag`, `ingest/gebieden`, or the landing plumbing.
- **Ops:** enables separate cron/CronJob schedules per source (BAG monthly, `gebieden` weekly).
- **Docs:** `deploy/compose/README.md` load recipe gains the per-source invocation form.
