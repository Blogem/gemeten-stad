## Why

P13 `load koop` must dedup a case's aanvraag and besluit publications and audit the besluit — which
requires a stable **case key**, the zaaknummer. Verified against the 10,503 landed records: the
zaaknummer (`OVERHEIDop.referentienummer`) is **absent from the SRU `gzd` record P11 lands (0/10,503)**
— it lives only in the per-publication `metadata.xml` sidecar (confirmed live: it carries
`OVERHEIDop.referentienummer` e.g. `Z2022-NW001025`, whose prefix encodes the stadsdeel, plus a
controlled `OVERHEIDop.activiteit` and `OVERHEIDop.gebiedsmarkering`). Without landing that sidecar,
dedup-by-zaaknummer and Noord-scoping-by-prefix are impossible. This change lands it.

## What Changes

- The koop harvester additionally fetches and lands each publication's `metadata.xml` sidecar
  (`https://zoek.officielebekendmakingen.nl/<id>/metadata.xml`) verbatim, alongside the SRU record,
  keyed by the same publication id, with provenance — the same immutable-bronze contract as the SRU
  record.
- Landing stays **verbatim** (no reshaping); the metadata's fields (`referentienummer`, `activiteit`,
  `gebiedsmarkering`) are parsed by the *load* stage (P13), not at harvest — consistent with P11's
  "SRU structure survives only inside the landed XML" split.
- Incremental behaviour is preserved: a publication whose SRU record is unchanged and whose metadata
  is already landed is a no-op; the metadata fetch is skipped when already present.
- A publication whose `metadata.xml` is missing/unavailable (some older ids return none) SHALL be
  landed with its SRU record regardless — the missing sidecar is recorded, not fatal.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `koop-ingest`: harvest now lands the `metadata.xml` sidecar per publication (the authoritative
  zaaknummer / activiteit / gebiedsmarkering source), in addition to the SRU record.

## Impact

- **Code:** `ingest/koop` (add the sidecar fetch + landing to the existing harvest loop; a second
  landing artifact per publication). Reuses `ingest/shared` http + landing + provenance.
- **Data:** a new `koop/<id>.metadata.xml` artifact per publication in the raw store (with its own
  `.prov.jsonl`). ~10.5k additional small fetches on a full backfill; incremental thereafter.
- **Unblocks:** P13 `load-koop-assembly` (dedup by zaaknummer, audit the besluit, Noord scope by the
  `Z….-N…` prefix). This change is a **prerequisite** for that one.
- **Politeness:** the sidecar fetch doubles the request count on a clean backfill; reuse the shared
  rate-limit/paging plumbing.
