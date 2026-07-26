## Why

Phase 1 audits Amsterdam's tree kap/verplant obligation from structured data, but no permit
documents have been landed yet — the whole intervention stream (KOOP officiële bekendmakingen)
is still an unharvested source. P11 lands that stream so P13 can assemble permits into the graph.
The harvest logic was already proven in `spikes/spike-b/harvest_permits.py` (Python); §5's
Go-everywhere-except-NLP decision means it is reimplemented in Go against the existing
`ingest/shared` plumbing.

## What Changes

- Add a new `koop` ingest source that SRU-harvests Amsterdam kap/verplant omgevingsvergunningen
  from the KOOP repository (`repository.overheid.nl/sru`) into the raw landing store (bronze),
  each publication landed **verbatim** with a provenance record.
- Scope the SRU query to `dt.creator any "Amsterdam"` + `dt.type any "omgevingsvergunning"` +
  the kap/verplant full-text term set + `dt.available >= 2021-01-01` (2021 → present). The harvest
  is deliberately **Amsterdam-wide, not Noord-filtered** — Noord selection is deferred to P13's
  authoritative BAG location resolution, keeping harvest free of any geometry/resolver logic.
- Capture **both aanvraag and besluit** publications (they share a zaaknummer); the
  aanvraag/besluit dedup is a *load* concern (P13), not a harvest filter.
- Make the harvest **incremental**: a re-run fetches only new/changed publications (skip by
  publication id) and is otherwise a no-op, driven by a `dt.available` high-water-mark cursor plus
  a small re-query overlap for late-arriving same-day publications (SRU can filter by date, not by
  identifier).
- **Extend `ingest/shared` with SRU plumbing** — a paged `searchRetrieve` client (record
  splitting, `numberOfRecords`, `startRecord`/`maximumRecords` paging, rate-limiting) — the geo
  sources exercised HAL/atom paging but never SRU.
- Register `koop` in the `pipeline ingest` source registry so `pipeline ingest koop` (and the
  run-all path) drives it.

## Capabilities

### New Capabilities
- `koop-ingest`: SRU harvest of Amsterdam kap/verplant omgevingsvergunningen from KOOP into the
  raw landing store — scoped query, verbatim per-publication landing with provenance, and
  incremental (id-deduplicated, date-cursor) re-runs.

### Modified Capabilities
<!-- None: geo-ingest / bomen-ingest specs are unchanged; koop is a new source, and the shared
     SRU plumbing it adds is implementation detail below the ingest capability contracts. -->

## Impact

- **New code:** `ingest/koop/` (harvester, SRU query builder, incremental cursor, landing);
  SRU client additions in `ingest/shared/`.
- **Wiring:** `cmd/pipeline/main.go` — register `koop` in `ingestRegistry` and add its HTTP getter;
  `selectSources`/registry-shape tests updated to include `koop`.
- **Raw store:** new `koop/` artifacts (one verbatim publication record per gmb id) plus their
  provenance sidecars, under `GS_RAW_DATA_PATH`.
- **External dependency:** the public KOOP SRU 2.0 endpoint (`repository.overheid.nl/sru`); no auth.
- **Reference:** `spikes/spike-b/harvest_permits.py`, `docs/DATA_SOURCES.md` §1.
- **Downstream:** unblocks P13 (`load koop`); no changes to P12/P14 in this change.
