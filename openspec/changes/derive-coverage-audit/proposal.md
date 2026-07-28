## Why

Phase 1 is a **working coverage audit from structure alone**, and `derive` is its last, gold
step: it answers _"for each loaded Noord permit, does a matching `kapenherplant` felling
exist?"_ — the permit↔registry link that Spike B settled as fuzzy entity resolution on
place + count + time (there is **no shared key**), carrying a graded confidence. P11/P12/P12b and
P13 have landed in main; the permit besluiten now sit in the graph with a resolved point. Nothing
yet joins a permit to the registry, and `gs:AuditLink` ships from P8 as a bare class with **no**
way to attach or gate it. This change builds the derivation and models the audit link properly.

## What Changes

- **New `derive` coverage stage** (`derive/coverage`, wired as `pipeline derive`, replacing the
  current stub), that:
  - **Generates candidate permit↔felling pairs** from the value store: individual felled
    `kapenherplant` rows in a permit's resolved buurt whose felling date
    (`kapmaatregelDatumUitgevoerd`, 100% populated, per-tree) falls in `[publication, +3yr]`. The
    **individual felling** is the matching unit — no batch-date "work-order cluster"
    (`datumVergunningVerleend` is unreliable, Spike A).
  - **Scores** each pair with Spike B's place-led model: **place** = proximity of the permit's
    resolved point (`koop_publications.resolved_geom`) to the felling geometry, graduated above a
    0.50 buurt floor (address/postcode/buurt tier → `gs:granularity`); **time** = the
    publication→felling gap; **count** using the **registry** felled count, never a permit-text
    count; an ambiguity term.
  - **Assigns each felling to its single best-scoring permit — exclusively.** A felling links to
    **at most one** permit; a permit MAY link **many** fellings. A permit's `Observation` is the
    **set of fellings assigned to it**.
  - **Models coverage as a stable anchor + versioned period nodes** (per
    `docs/RDF_STAR_MODELING.md`: a time-bounded relationship is a node grouped by a series
    anchor, not an annotated edge):
    - a per-permit **anchor** `data:auditlink/<zaaknummer> a gs:AuditLink ; gs:coversIntervention
      <intervention>` (stable, write-once — "the coverage audit of this permit");
    - one **period** per outcome `data:auditlink/<zaaknummer>/<content-key> a gs:CoveragePeriod`
      (content-keyed so an unchanged re-run is a true no-op), carrying `gs:versionOf` → the anchor,
      `gs:validFrom`, `gs:evidence`, `prov:wasDerivedFrom`, and either **matched**
      (`gs:linksObservation` → the permit's `Observation` [minted `a gs:Observation`, identity
      only], `gs:confidence`, `gs:granularity`, `gs:caveat gs:weakLink` below τ=0.60) or
      **no-source** (`gs:noSourceFound true`, no Observation). Values are **plain properties** on
      the period — core-SHACL gateable.
  - **Versions bitemporally via the node-form series writer** (`state-node-versioning`): a changed
    outcome mints a new period node and the writer closes the prior open period of that anchor
    (`gs:validTo`); history retained; unchanged re-run is a no-op.
  - **Persists derived numbers** (not the link — that lives in the graph) to a slim PostGIS
    `audit_metrics` table: matched flag, assigned-felling count + ids, candidate count, nearest
    distance, run id.
- **AuditLink modeling (P8 carry-over — done FIRST):**
  - **TBox** (`ontology/ontology.ttl`): recast `gs:AuditLink` (existing class) as the stable
    per-permit **anchor**; add `gs:coversIntervention` (AuditLink → Intervention), a new
    `gs:CoveragePeriod` class, `gs:linksObservation` (CoveragePeriod → Observation),
    `gs:granularity`, and `gs:noSourceFound`. (`gs:versionOf` is added by `state-node-versioning`.)
  - **Shapes** (`ontology/shapes.ttl`): **core-SHACL** `gs:AuditLinkShape` (anchor has
    `gs:coversIntervention` → an `Intervention`) + `gs:CoveragePeriodShape` (`gs:versionOf` → an
    `AuditLink`, `gs:validFrom` + `gs:evidence` present, `sh:xone` of matched vs no-source,
    `gs:confidence` in [0,1], `gs:granularity` in the tier set). No `sh:sparql` needed.

## Capabilities

### New Capabilities

- `coverage-audit`: The gold `derive` step — generate candidate permit↔felling pairs, score with the
  Spike B place-led model, assign each felling to its single best permit exclusively, and record the
  outcome as a stable `gs:AuditLink` anchor + a versioned `gs:CoveragePeriod` (matched → `Observation`
  with confidence, or `noSourceFound`) through the SHACL gate, idempotently, with derived numbers in a
  slim PostGIS table.

### Modified Capabilities

- `graph-ontology`: recast `gs:AuditLink` as the coverage anchor; add `gs:CoveragePeriod`,
  `gs:coversIntervention`, `gs:linksObservation`, `gs:granularity`, `gs:noSourceFound` (using
  `gs:versionOf` from `state-node-versioning`). Also carry the besluit **publication date** on the
  `gs:Intervention` as `dct:available` (`xsd:date`, reused Dublin Core) and scope the "no literals"
  rule to *quantitative/geometry* values (dates are descriptive-metadata literals — see
  `docs/RDF_MODELING.md`).
- `graph-shapes`: add core-SHACL `gs:AuditLinkShape` (anchor) + `gs:CoveragePeriodShape` (period);
  extend the Intervention structural shape to gate `dct:available` (`sh:datatype xsd:date`, exactly
  one).
- `koop-load`: emit the besluit publication date as `dct:available` on the Intervention (the graph
  becomes the source of truth for it; the derive windows fellings against it).

## Impact

- **New code:** `derive/coverage/` (candidate SQL over `kapenherplant`, the proximity/time/count
  scorer + exclusive felling-assignment adapted from `spikes/spike-b/match_rate.py`, the
  content-keyed anchor+period turtle assembler, the PostGIS `audit_metrics` stage/schema/upsert), plus
  `runCoverageDerive` wired into `cmd/pipeline`'s `derive` command (replacing the stub) via a derive
  registry mirroring `loadRegistry`.
- **Ontology:** `ontology/ontology.ttl` + `ontology/shapes.ttl` gain the anchor + period model + gates
  (embed tests extended).
- **Depends on:** **`state-node-versioning`** (node-form period versioning via `gs:versionOf` + the
  `gs:versionOf` term) — a hard prerequisite. Plus `load/graph.Load` (P12), the `kapenherplant`
  mirror with `resolvedGeom` + `kapmaatregelDatumUitgevoerd` (P7), the besluit
  `Intervention`/`Claim`/`Place` spine + `koop_publications.resolved_geom`/`resolved_tier` (P13), the
  zaaknummer sidecar (P11). Confirmed IRIs: `data:intervention/<zaaknummer>`,
  `data:place/<identificatie>`, `data:claim/<zaaknummer>`, `gs:activity act:vellen` (main
  `load/koop/graph.go`).
- **New PostGIS table** `audit_metrics` — numbers only; the link lives in the graph, geometry in
  `kapenherplant`.
