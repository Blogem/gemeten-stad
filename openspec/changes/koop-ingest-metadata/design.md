## Context

P11 `ingest koop` (merged) harvests Amsterdam kap/verplant omgevingsvergunningen from the KOOP SRU
2.0 endpoint and lands each publication's SRU `gzd` record **verbatim** at `koop/<id>.xml`, keyed by
`dcterms:identifier`, with a `.prov.jsonl` sidecar and a `koop/_cursor.json` high-water mark. It sets
no `recordSchema`, so it lands the default `gzd` payload — which carries geometry, title, abstract,
and `overheidwetgeving:activiteit`, but **not** the zaaknummer.

Verified against the 10,503 landed records: `referentienummer` appears in **0** of them. The
zaaknummer lives in the separate `metadata.xml` sidecar (confirmed live: `OVERHEIDop.referentienummer`
= `Z2022-NW001025`, `OVERHEIDop.activiteit` = `kappen`, `OVERHEIDop.gebiedsmarkering`). P13 needs it
to dedup aanvraag/besluit per case and to scope to Noord by the stadsdeel prefix.

## Goals / Non-Goals

**Goals:**
- Land each publication's `metadata.xml` verbatim + provenance, keyed by publication id, reusing the
  P11 landing/provenance plumbing.
- Preserve incrementality/idempotency: no redundant sidecar fetches on an unchanged re-run.
- Keep parsing in the load stage (bronze stays verbatim).

**Non-Goals:**
- No parsing/field extraction at harvest (that is P13).
- No new SRU query or scope change (harvest stays Amsterdam-wide).
- No backfill orchestration beyond the existing incremental harvest loop.

## Decisions

### D1 — A second landing artifact per publication, same key
Land the sidecar as its own artifact (e.g. `koop/<id>.metadata.xml`) under the same publication id,
via `shared.RawStore.Land` — so the store holds two verbatim artifacts per publication (SRU record +
metadata), each with its own `.prov.jsonl`. Rationale: mirrors P11's verbatim-bronze contract; the
load stage reads both by id. *Alternative rejected:* merging metadata into the SRU record file —
breaks "verbatim landing" and conflates two sources.

### D2 — Fetch the sidecar inside the existing harvest loop, gated on need
For each publication the harvester already processes, fetch `metadata.xml` only when it is not
already landed (or the SRU record changed), reusing the sha256 content-unchanged skip. Rationale:
keeps the doubled request count off unchanged re-runs. *Alternative considered:* a separate backfill
pass — redundant; the harvest loop already visits every publication.

### D3 — Missing sidecar is recorded, not fatal
Some older ids return no `metadata.xml`. Land the SRU record regardless and record the absence (a
provenance note / empty-marker), so P13 can fall back to heuristic handling for those few cases
rather than the harvest failing. *Alternative rejected:* dropping publications without a sidecar —
loses auditable data.

### D4 — URL source
Use `https://zoek.officielebekendmakingen.nl/<id>/metadata.xml` (the documented sidecar URL,
`DATA_SOURCES.md` §1). *Alternative:* the `repository.overheid.nl` FRBR path — heavier (full
document); the zoek metadata URL is the minimal authoritative source for the fields P13 needs.

### D5 — Rate-limit + retry the sidecar fetch; distinguish 404 from transient (added during apply)
A full backfill issues ~10k individual sidecar fetches, and the host **resets connections under
rapid-fire load** (observed live: `EOF` / `unexpected EOF`, escalating to an IP-level block). So the
fetch is **paced** (`metadataRateInterval`, 200 ms, matching `shared.SRURateInterval`) and each is
**retried with capped exponential backoff** (`metadataMaxAttempts`=8, 1 s→20 s — mirroring the SRU
page policy). Crucially, a transient reset MUST NOT be mistaken for absence: an HTTP 404 is surfaced
as `shared.ErrNotFound` (skip, no retry), while any transport error is retried; a sidecar still
failing after every attempt is left **unlanded** (non-fatal) so a later run retries it. *Alternative
rejected:* treating every error as "sidecar absent" (the first implementation) — it silently dropped
the zaaknummer for every throttled record, defeating the change's purpose. *Alternative deferred:*
making the pace env-configurable — kept the 200 ms default (consistent with SRU); revisit only if a
paced run still throttles.

## Risks / Trade-offs

- **[Doubled fetch volume on backfill]** ~10.5k extra small requests on a clean volume. → Reuse the
  shared rate-limit plumbing; incremental thereafter (D2).
- **[Sidecar availability]** A minority of ids may 404. → D3 makes it non-fatal; P13 handles the
  keyless remainder heuristically.
- **[Two-artifact keying]** The load must read the right artifact per id. → A fixed naming
  convention (`<id>.metadata.xml`) keeps the seam unambiguous; guard with a contract test.
