## Why

The audit rolls obligations and assessments **up the buurt→wijk→stadsdeel skeleton** (a
`gs:within+` property-path traversal), and every Intervention will carry a `gs:locatedAt` edge to a
`gs:Place`. That skeleton has to exist in the graph before any of it works: before P14 can point a
`locatedAt` edge at a real Place, and before any rollup query has a hierarchy to walk. The gebieden
backbone is already conformed into PostGIS (P6 / `geo-load`), but nothing projects it into the
graph. P12b seeds that projection so the graph holds the *joinable skeleton* — identity + name +
containment — while the geometry stays in PostGIS.

## What Changes

- **A new load step projects the authoritative PostGIS `gebieden` tables into `gs:Place` nodes.**
  Whole-city buurten and wijken become Places keyed on a stable IRI minted from the gebieden
  `identificatie` (the `gbdBuurtId` point-in-polygon ground truth), each carrying its `naam` as
  `rdfs:label` and — for buurten — a `gs:within` edge to its wijk (from `ligtinwijkid`). Geometry is
  **not** projected; it stays in PostGIS.
- **Every gebieden row is seeded — including soft-deleted ones** (`source_deleted_at IS NOT NULL`),
  not just live rows, so an older Intervention resolved to an area since deprecated always has a
  Place to point at (matching `geo-load`'s reason for soft-deleting rather than dropping: *"preserve
  any entity a downstream graph node may reference"*). A Place carries a `gs:active` flag — `true`
  for a live area, `false` for a deprecated one — so consumers can filter inactive areas.
- **A Place's active-status is modeled as evolving state** (RDF-star `gs:active` + valid time), so a
  Place is an **evolving** entity under P12's idempotent upsert: content is compared excluding the
  valid-time stamps, so **re-seeding the unchanged skeleton is a true no-op**, while an area going
  inactive (`gs:active true → false`) **opens a new version and closes the prior** with a
  `gs:validTo`, retaining history. The Place's IRI/label/containment stay the stable identity.
- **The projection is wired into the `graph` load source** (`runGraphLoad`): the graph load now
  ensures the reference model **and** seeds the Place skeleton from PostGIS in one gated `Load`
  call. `runGraphLoad` gains a Postgres connection; the load registry order (`geo` → `bomen` →
  `graph`) guarantees the gebieden tables exist when the graph load runs.
- **The stadsdeel tier is deliberately deferred (OUT OF SCOPE).** PostGIS holds no stadsdelen table
  and wijken carry no stadsdeel link (the gebieden ingest keeps only `ligtInWijkId`), so seeding it
  authoritatively would require extending `geo-ingest`/`geo-load` to harvest the gebieden stadsdelen
  collection. Wijken are seeded as the **top** of the skeleton (no `gs:within`); the wijk→stadsdeel
  tier is a flagged follow-up, not built here.

## Capabilities

### New Capabilities
- `place-skeleton`: projecting the conformed PostGIS `gebieden` backbone into the graph's `gs:Place`
  containment skeleton (stable identity + `rdfs:label` + `gs:within` + evolving `gs:active`
  status), whole-city, buurt→wijk, seeded through the graph load gate.

### Modified Capabilities
- `graph-ontology`: add a `gs:active` property on `gs:Place` and fix that a Place's active-status is
  **evolving state** (RDF-star + valid time), distinct from the Place's timeless identity. (The
  `gs:Place` class and the transitive `gs:within` property already exist in the TBox and are
  unchanged; `graph-shapes` needs no `PlaceShape`, and the idempotent SHACL-gated writer is
  `graph-load-gate` / P12, used unchanged.)

## Impact

- **New package** (`load/places`) that reads `gebieden_buurten` + `gebieden_wijken` from PostGIS and
  emits the Place turtle candidate — pure projection.
- **`ontology/ontology.ttl`** gains the `gs:active` term (no `shapes.ttl` change — see design D3).
- **`cmd/pipeline` `runGraphLoad`** extended to open a Postgres pool, build the candidate, and pass
  it to `loadgraph.Load` (currently it passes `nil`).
- **Depends on:**
  - **P12 (`graph-writer-upsert`)** — the evolving SCD2 open/close + no-op-re-run semantics. Under
    the shipped additive P8 writer a re-run would duplicate every Place and an area going inactive
    would not close its prior version; P12b's idempotency + active-status supersede rest on P12
    landing. (These specs are written against P12's expected shape and will be reconciled once it
    merges to main.)
  - **P6 / `geo-load`** — the conformed `gebieden_buurten`/`gebieden_wijken` PostGIS tables (incl.
    the `source_deleted_at` soft-delete marker).
  - **P8 / `graph-load-gate` + `graph-ontology`** — the `load/graph` gate and the existing
    `gs:Place` / `gs:within` TBox terms.
- **No change to `geo-load` / `geo-ingest`.**
- **Unblocks P14 (`derive`)** — `Intervention gs:locatedAt Place` edges and the `gs:within+` rollup
  traversals now have real Place targets.
- **Integration tests** run against the isolated `gs-test` Fuseki dataset (`GS_TEST_FUSEKI_URL`) and
  the isolated test Postgres (P5 harness), guarded against production names.
