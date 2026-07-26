## Context

The graph model (`IMPLEMENTATION_PLAN.md` §3) makes `gs:Place` a first-class entity carrying
_identity + name + containment_ while the geometry lives in PostGIS. The TBox already ships those
terms: `gs:Place` (`ontology/ontology.ttl`) and `gs:within` (an `owl:TransitiveProperty`,
domain/range `gs:Place`, commented "a buurt gs:within a wijk gs:within a stadsdeel … seeded from the
gebieden tables"). The conformed backbone is in PostGIS after P6 / `geo-load`:

- `gebieden_buurten(identificatie PK, naam, code, ligtinwijkid, geom, source_deleted_at)`
- `gebieden_wijken(identificatie PK, naam, code, geom, source_deleted_at)`

Nothing projects those rows into the graph yet — `runGraphLoad` (`cmd/pipeline/main.go`) calls
`loadgraph.Load(ctx, url, nil, cfg)`, seeding only the reference model. P12b fills that gap: read
the gebieden tables, emit a Turtle candidate of `gs:Place` nodes, and seed it through the existing
SHACL-gated `Load`.

Constraints inherited from the codebase:

- **The `load/graph` writer is Postgres-free and takes the turtle it is given** (`doc.go`). The
  PostGIS→turtle projection therefore lives in a _caller_ package, not in `load/graph`.
- **Values never live in RDF** (`graph-ontology`): the graph gets identity + `rdfs:label` +
  `gs:within`; the polygon `geom` stays in PostGIS.
- **The skeleton is a projection of the authoritative gebieden tables** — PostGIS stays the single
  source of truth, the graph copy must not drift or invent hierarchy.
- **P12's upsert semantics** (`graph-writer-upsert`): an entity is _evolving_ iff it asserts a
  `gs:validFrom`; evolving entities supersede via SCD2 open/close (compared excluding valid-time),
  and a run with an empty delta leaves no trace. Places assert `gs:validFrom` on their `gs:active`
  status, so they ride P12's **evolving** path (D5).
- **`geo-load` soft-deletes, never drops** (`source_deleted_at`), expressly to _"preserve any entity
  a downstream graph node may reference."_ P12b honours that: it seeds soft-deleted areas too so a
  historical `gs:locatedAt` target never dangles.

## Goals / Non-Goals

**Goals:**

- Project **every** whole-city `gebieden` row — live **and** soft-deleted
  (`source_deleted_at IS NOT NULL`), not only the live ones — into `gs:Place` nodes: a stable IRI,
  `rdfs:label` from `naam`, `gs:within` from each buurt to its wijk (`ligtinwijkid`), and a
  `gs:active` status flag (`true` live / `false` soft-deleted). Seeding deprecated areas too keeps
  historical references resolvable — an older Intervention resolved to an area since deprecated must
  still find its Place (see D5; this is exactly why `geo-load` soft-deletes rather than drops).
- Seed the skeleton through the SHACL-gated `Load`; a Place is an **evolving** entity (its
  `gs:active` status carries `gs:validFrom`), so under P12 an unchanged re-seed is a **true no-op**
  (valid-time excluded from change detection) while an area going inactive supersedes via SCD2
  open/close (D5).
- Keep the projection a **pure, unit-testable function** from gebieden rows → Turtle bytes, so it is
  tested without a live store.
- Wire it into the `graph` load source so `pipeline load [graph]` seeds the reference model **and**
  the Place skeleton in one gated `Load`.

**Non-Goals:**

- **The stadsdeel tier.** PostGIS has no stadsdelen table and wijken carry no stadsdeel link, so the
  wijk→stadsdeel `gs:within` edge is not seeded; wijken are the top of the skeleton. Harvesting the
  gebieden stadsdelen collection is a future `geo-ingest`/`geo-load` extension (Open Questions).
- **Intervention `gs:locatedAt Place` edges and rollup queries** — P14 (`derive`). P12b only lays
  the skeleton those edges point at.
- **Projecting geometry, `code`, or `cbsCode`** into the graph — geometry stays in PostGIS; the
  short `code` and `cbsCode` are not part of the joinable skeleton (identity is the `identificatie`
  IRI). Not seeded unless a consumer needs them.
- **Any shapes change** — no `PlaceShape` is added (D3). The only TBox change is adding the
  `gs:active` term (D6); `gs:Place`/`gs:within` already exist.
- **A real-world boundary-change history** — PostGIS gebieden is a current snapshot (the ingest
  keeps only `eindGeldigheid null` records), so P12b tracks active→inactive from what we observe
  (`source_deleted_at`), not the true world-time of every historical re-boundary (D5, D6).
- **A separate `places` load source** — the projection folds into the existing `graph` source (D4).

## Decisions

### D1 — Place IRI is keyed on the gebieden `identificatie` (`gbdBuurtId`), not `code`

Each Place gets a stable IRI minted from the gebieden `identificatie` under the instance namespace,
e.g. `http://gemetenstad.nl/id/place/<identificatie>` (a dedicated `place:` prefix keeps the Turtle
readable; `/` is not legal in a Turtle prefixed-name local part, so the writer emits either a
`place:` prefix bound to `…/id/place/` or full `<…>` IRIs). `identificatie` is the buurt PK and the
`gbdBuurtId` point-in-polygon ground truth the location resolver and `bomen` both already join on —
so the Place IRI P12b seeds is exactly the key P14's `locatedAt` edges will reference. The short
human `code` (e.g. `N73a`) was rejected as the key: it is a display label, not the join key the rest
of the pipeline uses.

**Alignment constraint (verified against the merged P12):** the Place IRI MUST stay under the
`data:` namespace `http://gemetenstad.nl/id/` — P12's change detection recognizes an entity only via
`FILTER(STRSTARTS(STR(?s), "http://gemetenstad.nl/id/"))` (`signature.go`), so a Place minted outside
`data:` would be invisible to the signature diff (never classified, never upserted). `…/id/place/<id>`
satisfies this. And every Place IRI must pass P12's `assertSafeIRI` (no `<>"{}|^`\` / control chars);
numeric gebieden `identificatie` values do.

Alternative considered — reuse the raw `identificatie` as a bare `data:<id>` local name. Rejected: a
dedicated `place/` segment namespaces Places away from interventions/trees/claims and reads clearly
in SPARQL.

### D2 — Projection is a pure `rows → Turtle bytes` function; SQL read is thin

A new package (`load/places`) exposes something shaped like
`BuildCandidate(ctx, pool, schema) ([]byte, error)`, split into (a) a thin repository read of the two
gebieden tables into Go structs and (b) a **pure** `renderPlaces(buurten, wijken) []byte` that emits
the Turtle. The pure renderer is the unit-test surface (IRI minting, label escaping, `gs:within`
wiring, dangling/empty handling) with no database. This mirrors `load/geo`'s split and the
project's "unit-testable Go logic" preference. Alternative — build the candidate with an inline
SPARQL `CONSTRUCT` against a staged copy — rejected: heavier, and the source of truth is a
relational table, not a graph.

### D3 — No `PlaceShape` added to `graph-shapes`

The load gate validates the candidate against `ontology/shapes.ttl`. No shape currently targets
`gs:Place` (only `InterventionShape` references it as a `locatedAt` target), so a Place-skeleton
candidate conforms trivially and `Load` writes it. A positive `PlaceShape` (e.g. "every `gs:Place`
has an `rdfs:label`", "`gs:within` points to a `gs:Place`") was considered and **rejected for P12b**:
(a) the skeleton is a faithful projection of authoritative rows, so the malformedness a gate guards
against cannot arise from bad input the way a fuzzy `locatedAt` edge can; (b) `minCount 1` on
`rdfs:label` would break the existing bare-`gs:Place` fixtures the shipped P8 `graph-shapes`/
`graph-load-gate` tests rely on. Well-formedness is enforced by the projection logic, not SHACL.
(If a later change wants a structural Place gate, it is an additive `graph-shapes` modification then,
with its fixtures updated — out of scope now.)

### D4 — Seed within the existing `graph` load source, not a new source

`runGraphLoad` already ensures the reference model via `loadgraph.Load(ctx, url, nil, cfg)`. P12b
changes it to build the Place candidate from PostGIS and pass it in:
`loadgraph.Load(ctx, url, placeCandidate, cfg)` — one gated call ensures the reference model **and**
validates + writes the Place skeleton. `runGraphLoad` gains a Postgres pool (via
`shared.DatabaseURL` / `shared.ConnectPostgres`, like `runBomenLoad`). The `loadRegistry` order
`geo → bomen → graph` guarantees the gebieden tables are populated before the graph load reads them.
A separate `places` load source was rejected as premature registry surface — the Place skeleton _is_
part of seeding the graph.

### D5 — Places are evolving via `gs:active`; deprecation supersedes via SCD2; `gs:within` only when the wijk resolves

A Place's **identity is timeless** (IRI never changes), but its **active-status evolves**: an area
can be deprecated (a boundary revision, an upstream expunge → `source_deleted_at` set). Because a
status that flips active→inactive is evolving state (the plan's "legal force in-force → repealed"
analogue), each Place asserts its `gs:active` value with a `gs:validFrom` RDF-star annotation. That
one `gs:validFrom` makes the Place an **evolving** entity in P12's sense (D4 of `graph-writer-upsert`),
so P12's SCD2 path handles it:

- **Re-seed unchanged → no-op.** P12 compares content **excluding** valid-time; a still-active buurt
  re-rendered with a fresh `gs:validFrom` compares equal (`gs:active true` unchanged) → empty delta
  → **no run graph, no `prov:Activity`, no added triples**. The first run writes ~630 Places in one
  `run:load-…` graph + PROV.
- **Area goes inactive → supersede.** When a row's `source_deleted_at` becomes set, the candidate
  re-renders `gs:active false`; P12 sees the changed content → **opens** the new `false` version
  (carrying its `gs:validFrom`) and **closes** the prior `true` version (stamps `gs:validTo` = the
  new `gs:validFrom`), retaining history. The deprecated Place stays a valid `gs:locatedAt` target.

**`gs:validFrom` sourcing** (P12 D3: the caller owns world-time, the writer owns the close): we only
have transaction-time-ish dates (PostGIS gebieden is a current snapshot — the ingest keeps only
`eindGeldigheid null` records — so no per-row world-time history). The projection emits the
**load-run timestamp** as `gs:validFrom` for a live (`true`) row and the row's **`source_deleted_at`**
as `gs:validFrom` for a soft-deleted (`false`) row. Because P12 excludes `gs:validFrom` from change
detection, the live-row load timestamp does not defeat idempotency (re-runs still compare equal), and
on deprecation the close stamps a contiguous `[load-ts, source_deleted_at)` active interval. The
approximation (observed time, not true boundary-change time) is accepted and flagged (Non-Goals,
Risks).

`gs:within` is emitted for a buurt **only when** its `ligtinwijkid` is non-null and resolves to a
projected wijk; a buurt with a null/dangling `ligtinwijkid` is still seeded as a Place (with
`rdfs:label` and `gs:active`) but without a `gs:within` edge, and the projection logs a loud
`places: …` warning naming the buurt — no half-broken containment enters the graph, and the anomaly
is surfaced rather than dropped. Whole-city loads are expected to have every wijk present, so this
is the residual-anomaly path, not the norm.

### D6 — Add only `gs:active` to the TBox (boolean); no new SKOS terms, no `PlaceShape`

The evolving active-status needs one new predicate: `gs:active`, a boolean datatype property on
`gs:Place`, added to `ontology/ontology.ttl` alongside the existing `gs:validFrom`/`gs:validTo`
(which are reused unchanged). A boolean was chosen over a controlled term pair
(`gs:active gs:activeArea` / `gs:deprecatedArea`) because active/inactive is genuinely binary and a
boolean needs no new `domain-vocabulary` SKOS concepts — leaner, and the SHACL controlled-value
shapes (species/activity/status) do not apply to it. No `PlaceShape` is added (see D3), so the new
`gs:active` annotation is not SHACL-gated beyond the trivial conformance a Place already has; the
projection's own logic guarantees every Place carries a well-formed `gs:active` + `gs:validFrom`.

## Risks / Trade-offs

- **[Depends on P12 not-yet-merged]** — Written against `graph-writer-upsert`'s expected shape; the
  shipped additive P8 writer would duplicate every Place on re-run and would not close a prior
  `gs:active` version on deprecation, breaking both the no-op and the supersede requirements.
  → Flagged as a hard dependency; the specs are reconciled against P12 once it lands on main (the
  user's explicit review pass). If P12b is implemented before P12 merges, the no-op and
  active-flip integration tests are the canaries — they fail against the additive writer.
- **[`gs:validFrom` is observed-time, not world-time]** — the active interval boundaries are our
  load / `source_deleted_at` timestamps, not the true dates areas were re-bounded (PostGIS keeps no
  gebieden history). → Accepted and flagged; it is the best available signal, it is honest about
  *when we knew*, and P12's valid-time-excluded change detection keeps it idempotent regardless.
- **[Stadsdeel tier absent]** — The headline rollup target (stadsdeel) has no node yet, so
  `gs:within+` rolls up only to wijk. → Accepted and flagged (Non-Goals, Open Questions); buurt→wijk
  rollup works now, and the stadsdeel extension is additive (new wijk `gs:within` edges + stadsdeel
  Places) with no rework of what P12b seeds.
- **[Label / containment drift vs PostGIS]** — the graph copies `naam`/`ligtinwijkid`; if gebieden
  renames or re-parents an area, the graph holds the old value until reseeded, and (because the
  Place is evolving) a changed `naam`/`gs:within` also triggers a P12 supersede on next seed.
  → Acceptable: PostGIS stays the source of truth; a `--reset` graph load reseeds from current
  PostGIS, and the versioning is the correct bitemporal behaviour, not a bug.
- **[Whole-city vs Noord scope]** — seeding ~630 whole-city Places includes wijken/buurten outside
  the Noord POC. → Intentional (matches `geo-load`'s whole-city load and the plan's ~630-place
  sizing); the extra Places are inert until an Intervention points at them, and a Noord filter would
  be speculative flexibility the current step does not need.

## Migration Plan

Not applicable — greenfield POC, nothing released. First `pipeline load graph` (or a full
`pipeline load`) after this change seeds the skeleton; `--reset` rebuilds it from current PostGIS.
No data migration.

## Open Questions

- **Stadsdeel tier source** (deferred, not blocking P12b): seed the wijk→stadsdeel `gs:within` edge
  - stadsdeel Places by (a) extending `geo-ingest`/`geo-load` to harvest the gebieden **stadsdelen**
    collection (authoritative, preferred — keeps "projection of the gebieden tables" true), or (b)
    deriving stadsdeel identity from the wijk `code` prefix (rejected in spirit — needs a hardcoded
    code→name map, drifting from the authoritative-projection rule). To be settled when the stadsdeel
    rollup is actually needed (P14/analytics).
- **Exact IRI string / `place:` prefix binding** — `…/id/place/<identificatie>` is the intent; the
  precise prefix declaration in the emitted Turtle is an implementation detail settled in code
  review, consistent with however P13/P14 mint their instance IRIs.
