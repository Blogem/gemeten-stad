## Why

Phase 1 turns the harvested permit stream into a working coverage audit. P11 lands the Noord
kap/verplant `gzd` records verbatim (raw SRU XML), P12 shipped the SHACL-gated SCD2 graph writer,
and P6 shipped the BAG/gebieden location resolver — but nothing yet maps a landed permit into the
`Intervention`/`Claim`/`Place` spine. `load koop` is that silver step: it is the first assembler
that turns raw documents into fully-formed, provenanced graph entities plus their PostGIS values,
and it is the direct prerequisite for P14's `derive` coverage audit.

## What Changes

- New `load koop` stage (`load/koop`, wired as a `pipeline load koop` source) that, per zaaknummer:
  - **Enumerates + parses** the landed `koop/*.xml` `gzd` records (P11 lands verbatim SRU inner
    XML with no reader), extracting the **structured** signals Spike B found: `dcterms:identifier`
    (publication id), `overheidop:referentienummer` (zaaknummer), `overheidop:activiteit`
    (controlled), `overheidwetgeving:geometrie` (**RD `POINT(x y)`**) / `locatiepunt` (WGS84),
    `overheidop:postcode`, `dcterms:title`, `dcterms:available`.
  - **Dedups aanvraag + besluit** by zaaknummer and audits the **besluit** (title-prefix
    classification: `"Verleend:"`/`"Besluit:"` vs `"Aanvraag:"` — the only distinguishing signal).
  - **Resolves location** via the P6 resolver, laddering `postcode` (+ huisnummer parsed from the
    title) → address/postcode tier, with the structured **RD point → buurt point-in-polygon** as
    the floor. **Scopes to Noord by geometry** (resolved buurt in stadsdeel Noord), cross-checked by
    the `Z….-N…` zaaknummer prefix — the harvest is deliberately Amsterdam-wide (P11), so Noord
    selection lands here.
  - **Assembles the graph turtle** it hands to the P12 writer: `Intervention` keyed by zaaknummer,
    `gs:activity` → the `act:vellen` concept (verplanten ≡ vellen; Spike C), a `{| gs:confidence …;
    gs:caveat … |}`-annotated `gs:locatedAt` edge to the resolved buurt `Place`, and a `gs:claims`
    edge to a `Claim` (the herplantplicht triggered by the permit via art. 7 — **no obligation
    count**; the count is Phase-2 extraction). Construction of the `{| … |}` annotation lives here,
    not in the writer.
  - **Writes values + geometry to PostGIS**: the exact permit point, postcode, dates, activiteit,
    resolved buurt code, resolution confidence + caveats, raw record.
  - **Idempotent** by permit IRI (graph, under P12 SCD2 upsert) and by zaaknummer (PostGIS upsert):
    an unchanged re-run is a true no-op; a changed tracked field opens a new version.
- `load/koop` asserts a minimal `<place> a gs:Place` typing itself, so its `locatedAt` edge passes
  the `InterventionShape` gate **without hard-depending on P12b** (P12b independently enriches the
  same buurt IRI with `rdfs:label` + `gs:within`; both merge under IRI-keyed upsert).
- **Unresolvable permits** (no usable point/address — a rare edge case since the point is
  structured) are persisted to PostGIS with an `unresolved` marker and **never** written to the
  graph with faked coordinates; they surface for reporting / P14's "no source found".
- P11's koop test fixtures gain a realistic geometry-bearing `gzd` record so the P13 parser is
  tested against a true payload (the current fixtures are minimal and omit the geometry block).

## Capabilities

### New Capabilities
- `koop-load`: The silver assembly of KOOP kap/verplant permits — parse landed `gzd` records,
  dedup aanvraag/besluit per zaaknummer, resolve + Noord-scope location, assemble the
  `Intervention`/`Claim`/`Place` graph turtle through the SHACL-gated writer, and persist permit
  values + geometry to PostGIS, idempotently.

### Modified Capabilities
<!-- No spec-level requirement changes to existing capabilities. location-resolver, graph-load-gate,
     graph-shapes, and domain-vocabulary are consumed as-is; koop-ingest (P11) is a prerequisite,
     not modified here. -->

## Impact

- **New code:** `load/koop/` (parser, dedup, resolver bridge, turtle assembler, PostGIS
  stage/schema/upsert), plus `runKoopLoad` + a `koop` entry in `cmd/pipeline`'s `loadRegistry`.
- **Consumes (unchanged):** `location.Resolve` (P6), `load/graph.Load` (P12), `ontology/shapes.ttl`
  + `ontology/vocab.ttl` (`act:vellen`), the BAG/gebieden PostGIS mirror (P7), the shared raw store
  (`ingest/shared`).
- **Depends on:** P11 (permits landed), P12 (graph writer), P6 (`location/`), P8 (model), P7
  (BAG/gebieden in PostGIS). **Soft** on P12b (the buurt `Place` skeleton) — coordinated by shared
  IRI keying, not a build-order gate.
- **Cross-worktree coordination:** the buurt `Place` IRI scheme (`data:place/<gebieden-code>`) must
  match P12b's keying; the `koop-ingest` fixture enrichment touches P11's worktree.
- **New PostGIS tables** under the pipeline schema (e.g. `koop_permits`), owned by this load.
