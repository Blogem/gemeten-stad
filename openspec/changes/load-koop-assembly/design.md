## Context

`load koop` (P13) is the silver assembler for permits — the first stage that turns raw documents into
graph entities. It sits atop shipped pieces, and its design was corrected twice against the **real
10,503-record corpus** (`data/raw/koop/`), so the data facts below are measured, not assumed:

- **What is landed.** P11 lands the SRU `gzd` record verbatim at `koop/<id>.xml`; `koop-ingest-metadata`
  (prerequisite change) lands the `metadata.xml` sidecar at `koop/<id>.metadata.xml`. Between them:
  - **Geometry: 99.2%** (`overheidwetgeving:geometrie` RD `POINT(x y)` + `locatiepunt`) — but the
    point is a **`Gebiedsmarkering`** (area marker): 92 records share one identical point, so many
    points are coarse centroids, not exact addresses.
  - **Address** (street + huisnummer + postcode) sits in `dcterms:title`, e.g. *"Besluit
    omgevingsvergunning vellen van een houtopstand (kap) … Örehof 8 1060RW Amsterdam"*.
  - **Activiteit: 98.6%** (`overheidwetgeving:activiteit scheme="OVERHEIDop.ActiviteitOmgevingsvergunning"`
    = `kappen`); also in the metadata sidecar.
  - **Zaaknummer:** `0/10,503` in the SRU record — **only** in `metadata.xml`
    (`OVERHEIDop.referentienummer`, e.g. `Z2022-NW001025`; prefix encodes stadsdeel). Hence the
    prerequisite change.
  - **Publication kind** (title prefix): Aanvraag 5,111 · Besluit 4,659 · Verlenging 348 ·
    Ingetrokken 66 · Ontwerpbesluit / Rectificatie … The harvest is Amsterdam-wide (Noord scoping
    lands here).
- **P12 writer** — `Load(ctx, url, candidate []byte, Config{Reset})`: SHACL-gates candidate against
  `ontology/shapes.ttl` (validate merges **only** ontology+vocab+candidate — shacl.go), then IRI-keyed
  SCD2 upsert (classify.go: same-IRI signature equal → unchanged; differ+evolving → new version;
  differ+not-evolving → `immutableConflict`, skipped+logged). Postgres-free; callers build turtle.
- **P12b place skeleton** — seeds `place:<identificatie> a gs:Place ; rdfs:label … ; gs:active true
  {| gs:validFrom … |}` and `gs:within` (buurt→wijk) from `gebieden_buurten`/`_wijken`. IRI =
  `http://gemetenstad.nl/id/place/<14-digit gbdBuurtId>`. **Stadsdeel is not modelled** (only
  buurt+wijk). Its turtle helpers are package-private; P13 mirrors the IRI scheme, it cannot import.
- **P6 resolver** — `Resolve(ctx, pool, Query) (Result, error)` ladders address (0.90) → postcode
  (0.70) → buurt-via-point PIP (0.50); returns `Geom`, `Confidence`, `Caveats`, `BuurtID` (buurt
  tier). Errors only when nothing resolves.

## Goals / Non-Goals

**Goals:**
- Dedup landed publications by zaaknummer; assemble the **besluit** into a provenanced `Intervention`
  + `Claim` + `locatedAt`→`Place`, through the SHACL gate.
- Resolve location address-first (point as floor); scope to Noord by resolved buurt + zaaknummer prefix.
- Keep the **full publication trail** (aanvraag/besluit/verlenging/ingetrokken) in PostGIS as history.
- Align with P12b (shared `Place` IRI); idempotent (unchanged re-run is a true no-op).

**Non-Goals:**
- No obligation **count** on the `Claim` (Phase-2). No fulfilment/timeliness (P14/Phase-2). No
  `AuditLink` (P14). No NER beyond a best-effort address parse from the title.
- No aanvraag/ontwerp/verlenging as separate graph `Intervention`s — the graph carries the audited
  besluit only (below); their history lives in PostGIS.
- No stadsdeel `Place` node (P12b defers it); Noord scoping uses the resolved buurt + zaaknummer prefix.

## Decisions

### D1 — Parse both landed artifacts; a typed permit struct is the seam
`load/koop` enumerates `store.BasePath/koop/*.xml` (excluding `*.metadata.xml`, `_cursor.json`,
`*.prov.jsonl`), and for each publication reads its SRU record + its `<id>.metadata.xml`, parsing
into a typed `Publication` (id, zaaknummer, kind, activiteit, RD point, title/address, available,
raw). *Alternative rejected:* extending P11 with a reader — P11 exposes none by design; parsing is
the consumer's job.

### D2 — Dedup by zaaknummer; audit the besluit; besluit-only in the graph
Group by `OVERHEIDop.referentienummer`; classify each publication by title prefix; select the
**besluit** as the audited permit. **Measured:** ~92% of besluit-cases also carry an aanvraag (over
address-keyed grouping of the full corpus), so the besluit is the reliable auditable grant — it is
what creates the herplantplicht. Therefore **only the besluit becomes a graph `Intervention`**; the
aanvraag/ontwerp/verlenging/ingetrokken publications are recorded in PostGIS (the trail), not as
graph entities. A zaak with no besluit yet (a pending application, or the ~8% besluit-less remainder)
has **no** graph Intervention — it lives in PostGIS until its besluit lands, when the next run
assembles it. *This resolves the inline question ("do we track both, or go for the besluit"): the
graph tracks the besluit; PostGIS holds the evolution.* *Alternative rejected:* one evolving
`Intervention` per case with aanvraag→besluit SCD2 versions in the graph — the graph audits grants,
not applications; the 92% pairing means the aanvraag adds little graph signal, and it would force an
Intervention for pending/aanvraag-only cases that carry no obligation.

### D3 — Location: address-from-title primary, point as the buurt floor
Because the RD point is a coarse `Gebiedsmarkering`, the title address is often more precise. Build a
`location.Query{Postcode, Huisnummer, Point (RD), Date = besluit available}` from a best-effort
title parse (`\d{4}\s?[A-Z]{2}` postcode + preceding huisnummer) plus the point; the resolver ladders
to the finest tier. **The graph `Place` is always the containing gebieden buurt** (thin graph: exact
point + finer precision ride the edge confidence and live in PostGIS). Getting the buurt code: use
`Result.BuurtID` at the buurt tier; at the address/postcode tier PIP the resolved `Geom` into
`gebieden_buurten`. *Alternative considered:* extend `location.Result` to always carry the buurt —
cleaner but touches P6; deferred until a second caller needs it. *Alternative rejected:* point-first
— many points are neighbourhood centroids.

### D4 — Noord scoping by resolved buurt (`code LIKE 'N%'`), cross-checked by zaaknummer prefix
Keep a permit iff its resolved buurt is in stadsdeel Noord, determined by
`gebieden_buurten.code LIKE 'N%'` — the buurt `code`'s letter prefix encodes the stadsdeel, and this
is exactly the gate `load/geo` already uses to count Noord buurten (`load/geo/gates.go`). Cross-check
against the `Z….-N…` `referentienummer` prefix and log mismatches. (Note: P12b keys the Place on
`identificatie`, not `code`; P13 looks up the resolved buurt's `code` from `gebieden_buurten` for the
Noord test.) *Alternative rejected:* the spike's tree-vote hack — we have the authoritative polygons.

**Scope of "in-scope" (PostGIS retention):** the harvest is deliberately Amsterdam-wide, but vertical 1
is Noord only, so a zaak whose besluit **positively resolves outside Noord** is dropped entirely — no
graph Intervention *and* no `koop_publications` row. The trail PostGIS retains is therefore the
**in-scope** history: Noord-audited besluiten and their full publication trail, plus the zaken we
cannot yet exclude — pending (no besluit resolved yet), unresolvable, and keyless publications (each
carrying its marker for P14). This keeps the value store scoped to the audit target rather than
mirroring the whole-city corpus. *Alternative rejected:* retain every harvested publication as history
— it would bloat the table with city-wide permits vertical 1 never audits, against the "build lean"
rule.

### D5 — Depend on P12b for the Place; still emit `<place> a gs:Place` for the gate
Per the decision **not to decouple**, P13 depends on P12b: the buurt `Place` (label + `gs:within` +
`gs:active`) is P12b's, referenced by the shared IRI. But `InterventionShape`'s `sh:class gs:Place`
runs at validation against **candidate+ontology+vocab only** (shacl.go merges no live data), so the
candidate must itself type the target — P13 emits a bare `place:<id> a gs:Place` alongside its
`locatedAt` edge. Against P12b's live richer `Place`, that bare re-assertion classifies as
`immutableConflict` (classify.go) → **skipped, not written, logged once** — harmless, no version
churn, the `Place` stays exactly as P12b made it. *This resolves the inline "we shouldn't decouple":
the dependency stands; the type triple is only a gate-satisfying formality.* *Alternatives:* (a)
extend P12's `validate()` to also merge the live `Place` types — cleaner but modifies the shipped,
tested gate (revisit if the per-run `immutableConflict` log noise proves annoying); (b) order P12b
strictly before P13 — still needs the type triple, so it does not remove the emission.

### D6 — IRI scheme (verified against merged P12b)
`data:intervention/<zaaknummer>`, `data:claim/<zaaknummer>`,
`data:place/<gebieden identificatie>`, all under `http://gemetenstad.nl/id/` (the namespace P12's
change-detection filters on). Reuse the same `assertSafeIRI` / literal-escaping guards as `load/graph`
and `load/places`. **Alignment confirmed** (P12b now merged): `load/places` mints
`http://gemetenstad.nl/id/place/<identificatie>` from `gebieden_buurten.identificatie` (`load/places/read.go`,
`places.go` `placeNS = dataNS + "place/"`), and the resolver's buurt-tier PIP returns that **same
column** — `SELECT b.identificatie FROM gebieden_buurten` (`location/sql.go` `buurtPIPSQL`). So
`data:place/<Result.BuurtID>` lands exactly on the seeded skeleton node. P13's own address-tier PIP
(D3) MUST likewise select `gebieden_buurten.identificatie` (not `code`).

### D7 — SCD2 in the graph is minimal (besluit is stable); evolution lives in PostGIS
With besluit-only in the graph, the `Intervention` is largely immutable (a granted permit's facts do
not change). The one evolving edge is `locatedAt`: its `{| … |}` annotation carries `gs:validFrom` =
the besluit date, so a **re-resolution** (BAG update, better address) opens a new version and closes
the prior — the rest being un-stamped makes the common re-run a pure no-op. The aanvraag→besluit
progression is captured as PostGIS rows, not graph versions.

### D8 — PostGIS: one row per publication (the trail), keyed by publication id
A `koop_publications` table (schema-qualified via `current_schema()`, staging + upsert + `--reset`,
`raw jsonb` catch-all — the `load/bomen` pattern): `gmb_id` PK, `zaaknummer`, `kind`
(aanvraag/besluit/…), `available` + parsed dates, `geometry(Point, 28992)` (the raw permit point),
postcode, resolved buurt code, `resolved_confidence`, `caveats`, `resolved_geom geometry(Point, 28992)`,
`resolved_tier`, an `unresolved` marker, `raw`. Keyed by publication id (**not** zaaknummer — a zaak
has multiple publications; the inline note is right). The audited-besluit resolution is stored on the
besluit's row. Geometry lives here only.

`resolved_geom` + `resolved_tier` keep the resolver's **precise** output as silver: `resolved_geom`
is the address-tier BAG point (`location.Result.Geom` when `PlaceLevel == address`; NULL at the
postcode tier — no single point — and at the buurt tier, where the buurt code already captures the
place), and `resolved_tier` is the `PlaceLevel` (address/postcode/buurt). This is koop enriched by the
BAG/gebieden **master data** (a fact about the permit itself), so it belongs on the source-load trail,
unlike a cross-source audit derivation. It exists because P14's distance-graduated place matches a
permit to a registry felling by proximity to this point (median 0 m at the address tier, Spike D) —
without it, the precise resolution P6 already computes would be discarded and the graph's buurt-level
`locatedAt` would be P14's only place signal.

## Risks / Trade-offs

- **[Metadata prerequisite]** P13 cannot dedup without `koop-ingest-metadata` landed. → Hard
  dependency, sequenced first; a publication whose sidecar is missing (rare) falls to the keyless
  remainder (PostGIS, no graph).
- **[Coarse point]** A `Gebiedsmarkering` point may PIP into the wrong buurt at a boundary. → Address
  tier is primary; buurt-floor edges carry `unresolvedLocation` + confidence 0.50, never written as
  exact; the zaaknummer prefix cross-checks the stadsdeel.
- **[Title address parsing]** Free-text titles are messy (~11% have no extractable postcode). → Those
  fall to the point-in-buurt floor or the unresolvable bucket; never fabricate coordinates.
- **[P12b IRI contract]** A key mismatch splits a `Place` into two nodes. → D6 pins the scheme to
  P12b's `identificatie`; an integration test asserts an Intervention's `locatedAt` target equals a
  seeded skeleton IRI.
- **[immutableConflict log noise]** One log line per referenced `Place` per run (D5). → Acceptable;
  escalate to the `validate()`-merges-live-types option only if it becomes noisy.
- **[valid-time source]** Only `dcterms:available` (besluit publication date) is reliable (Spike A). →
  Use it as the resolution valid-time and `validFrom`; anchor elapsed-time reasoning downstream (P14)
  on the registry felling date, not permit dates.
- **[per-record resilience]** A batch load must not be sunk by one bad record. → A single record that
  fails to parse, whose besluit fails to resolve (infra error), whose IRIs are unsafe (dropped from
  the graph candidate), or whose row fails to stage is **logged and skipped**, and the load continues
  with the rest; the run logs skip counts. Only batch-level failures stay fatal: the koop dir cannot
  be listed, the staging table cannot be truncated, or `load/graph.Load` rejects the whole assembled
  candidate against the shapes (the SHACL gate is never weakened to a per-record skip).

## Migration Plan

Sequence `koop-ingest-metadata` → this change. Additive: new `load/koop` package + a `koop` entry in
`cmd/pipeline`'s `loadRegistry` (after `bomen`, and after `graph`/`places` so the skeleton exists);
new `koop_publications` table created on first run, dropped by `--reset`. Rollback = remove the
registry entry. Integration tests run against the isolated Fuseki dataset + Postgres schema.

## Open Questions

- **RESOLVED — Buurt→stadsdeel mapping (D4):** `gebieden_buurten.code LIKE 'N%'` (the geo-load gate,
  `load/geo/gates.go`) is the Noord test, cross-checked by the `Z….-N…` zaaknummer prefix.
- **RESOLVED — P12b Place key (D6):** verified `identificatie`; the resolver's buurt PIP returns the
  same column, so `data:place/<BuurtID>` joins the skeleton exactly.
- **Multiple besluiten / amendments per zaak:** if a case has a besluit + a later `Verlenging`/amendment,
  v1 audits the primary besluit; is the DecisionPeriod (amendment) model needed in Phase 1 or deferred
  to Phase 2?
- **Keyless remainder handling:** for the rare publication with no metadata sidecar, confirm the
  PostGIS-only fallback is sufficient for P14, or whether a heuristic zaak key is worth it.
