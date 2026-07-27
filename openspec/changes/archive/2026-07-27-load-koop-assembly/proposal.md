## Why

Phase 1 turns the harvested permit stream into a working coverage audit. With P11 landing the
publications (verbatim SRU `gzd`), `koop-ingest-metadata` landing the `metadata.xml` sidecar (the
zaaknummer), P12 shipping the SHACL-gated SCD2 graph writer, P12b seeding the buurt/wijk `Place`
skeleton, and P6 shipping the BAG/gebieden resolver — `load koop` is the silver step that assembles a
landed **besluit** into the `Intervention`/`Claim`/`Place` spine plus its PostGIS values. It is the
first document→entity assembler and the direct prerequisite for P14 `derive`.

## What Changes

- New `load koop` stage (`load/koop`, wired as `pipeline load koop`) that:
  - **Reads both landed artifacts per publication:** the SRU record (`overheidwetgeving:geometrie` RD
    point + `locatiepunt`; `overheidwetgeving:activiteit`; `dcterms:title` carrying the street +
    huisnummer + postcode; `dcterms:abstract`; `dcterms:available`) and the `metadata.xml` sidecar
    (`OVERHEIDop.referentienummer` = the **zaaknummer**, prefix encodes stadsdeel).
  - **Dedups by zaaknummer and audits the besluit.** Publication kind comes from the `dcterms:title`
    prefix (`Aanvraag` / `Besluit` / `Ontwerpbesluit` / `Verlenging` / `Ingetrokken`). Verified over
    the 10,503-record corpus: **~92% of besluit-cases also have an aanvraag** — the besluit is the
    reliable auditable grant, so **only the besluit becomes a graph `Intervention`**; the full
    publication trail is kept in PostGIS as history.
  - **Resolves location** via the P6 resolver, **address-from-title first** (the RD point is a coarse
    `Gebiedsmarkering` — often a neighbourhood centroid, so the title address is the more precise
    signal): postcode + huisnummer → address/postcode tier, RD point → buurt point-in-polygon floor.
    **Scopes to Noord** by the resolved buurt (in stadsdeel Noord), cross-checked by the `Z….-N…`
    zaaknummer prefix.
  - **Assembles the graph turtle** for the besluit: `Intervention` keyed by zaaknummer; `gs:activity`
    → `act:vellen` (verplanten ≡ vellen); a `{| gs:confidence … ; gs:caveat … |}`-annotated
    `gs:locatedAt` edge to the resolved buurt `Place` (`data:place/<gebieden identificatie>`, the IRI
    P12b seeds); a `gs:claims` edge to a `Claim` (herplantplicht via art. 7, **no count**). The
    `{| … |}` construction lives here, not in the writer.
  - **Persists the full publication trail to PostGIS**: one row per publication (aanvraag / besluit /
    verlenging / ingetrokken) keyed by publication id, carrying zaaknummer, kind, dates, the RD
    point, postcode, resolved buurt code, resolution confidence + caveats, an `unresolved` marker,
    and the raw record. Geometry lives only in PostGIS.
  - **Idempotent:** unchanged re-run is a true no-op (graph via P12 SCD2; PostGIS via upsert).
- **Depends on P12b** for the `Place` skeleton (no decoupling); still emits a minimal
  `<place> a gs:Place` in its own candidate so the `locatedAt` shape gate passes (`validate()` sees
  only ontology+vocab+candidate) — that re-assertion is a harmless `immutableConflict` skip against
  P12b's richer live `Place`.
- **Unresolvable / keyless remainder** (no resolvable location, or the rare missing-metadata case):
  persisted to PostGIS with markers, **never** graphed with faked coordinates; surfaced for P14.

## Capabilities

### New Capabilities
- `koop-load`: the silver assembly of KOOP kap/verplant permits — read the landed SRU record +
  metadata sidecar, dedup by zaaknummer, audit the besluit into the `Intervention`/`Claim`/`Place`
  graph through the SHACL gate, and persist the full publication trail + resolved values to PostGIS,
  idempotently.

### Modified Capabilities
<!-- none: koop-ingest is modified by the separate koop-ingest-metadata change; location-resolver,
     graph-load-gate, graph-shapes, domain-vocabulary, and the P12b place skeleton are consumed
     as-is. -->

## Impact

- **New code:** `load/koop/` (SRU + metadata parsers, dedup, resolver bridge, turtle assembler,
  PostGIS stage/schema/upsert), `runKoopLoad` + a `koop` entry in `cmd/pipeline`'s `loadRegistry`.
- **Consumes (unchanged):** `location.Resolve` (P6), `load/graph.Load` (P12), the P12b `Place`
  skeleton, `ontology/shapes.ttl` + `vocab.ttl` (`act:vellen`), BAG/gebieden PostGIS (P7), the raw
  store (`ingest/shared`).
- **Depends on:** **`koop-ingest-metadata`** (zaaknummer landed — hard prerequisite), **P12b** (buurt
  `Place` skeleton), P11, P12, P6, P8, P7.
- **Cross-worktree contract:** the buurt `Place` IRI `http://gemetenstad.nl/id/place/<identificatie>`
  (14-digit `gbdBuurtId`) must match P12b exactly.
- **New PostGIS table** (`koop_publications`) under the pipeline schema, owned by this load.
