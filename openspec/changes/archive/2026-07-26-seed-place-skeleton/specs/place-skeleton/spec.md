## ADDED Requirements

### Requirement: Project the gebieden backbone into gs:Place nodes

The system SHALL provide a Go projection (a `load/places` package, sibling of `load/geo`) that reads
the `gebieden_buurten` and `gebieden_wijken` PostGIS tables and emits a Turtle candidate graph of
`gs:Place` nodes. It SHALL project the whole-city load with **no stadsdeel filter** and SHALL
project **every** row, including soft-deleted ones (`source_deleted_at IS NOT NULL`) — not only the
live rows — so that any area a downstream graph node may reference (an older Intervention resolved
to an area since deprecated) always has a Place to point at, matching `geo-load`'s non-destructive
soft-delete rationale. For each projected buurt and wijk the candidate SHALL carry:

- a **stable IRI** minted from the gebieden `identificatie` (the `gbdBuurtId` point-in-polygon
  ground truth) under the instance namespace `http://gemetenstad.nl/id/`, so the Place IRI is the
  same key the location resolver and downstream `gs:locatedAt` edges join on;
- `a gs:Place`;
- an `rdfs:label` set to the gebieden `naam`; and
- a `gs:active` status carrying valid time via the `{| … |}` RDF-star annotation (evolving state,
  per `graph-ontology`): `gs:active true` for a live row and `gs:active false` for a soft-deleted
  row, each annotated with a `gs:validFrom`.

Geometry SHALL NOT be projected — the polygon stays in PostGIS (the value store); the graph carries
identity, label, containment, and the active-status flag only. The projection SHALL be a pure
function from the read gebieden rows to Turtle bytes (the database read separated from the
rendering), so it is unit-testable without a live store. The projection SHALL NOT construct any
`gs:confidence`/`gs:evidence` annotation — a Place is an exact identity, not a fuzzy edge.

#### Scenario: Buurten and wijken become Places with identity, label, and active-status

- **WHEN** the projection runs against loaded `gebieden_buurten` and `gebieden_wijken`
- **THEN** each buurt and each wijk is emitted as a `gs:Place` with a stable IRI keyed on its
  gebieden `identificatie`, an `rdfs:label` equal to its `naam`, and a `gs:active` statement carrying
  a `gs:validFrom` annotation
- **AND** no geometry, and no numeric or geometric literal, is written for any Place

#### Scenario: A soft-deleted area is seeded as inactive, not dropped

- **WHEN** a buurt or wijk row carries a non-null `source_deleted_at`
- **THEN** it is still emitted as a `gs:Place` (so historical references resolve)
- **AND** its `gs:active` value is `false`

### Requirement: Seed the buurt→wijk containment via gs:within

The projection SHALL emit a `gs:within` edge from each buurt Place to its containing wijk Place,
derived from the buurt's `ligtinwijkid` (the buurt's `ligtinwijkid` equals the wijk's
`identificatie`). Wijken SHALL be seeded as the **top** of the projected skeleton and SHALL carry no
`gs:within` edge (the wijk→stadsdeel tier is out of scope for this change). A `gs:within` edge SHALL
be emitted for a buurt **only when** its `ligtinwijkid` is non-null and resolves to a projected wijk;
a buurt whose `ligtinwijkid` is null or does not resolve SHALL still be emitted as a `gs:Place` (with
its `rdfs:label` and `gs:active`) but WITHOUT a `gs:within` edge, and the projection SHALL emit a
loud warning diagnostic (a `places: …` prefixed log line) naming that buurt, so a dangling
containment is surfaced rather than written as a half-broken edge.

#### Scenario: A buurt is contained in its wijk

- **WHEN** a buurt whose `ligtinwijkid` matches a wijk's `identificatie` is projected
- **THEN** the candidate contains `<buurt> gs:within <wijk>` between their Place IRIs
- **AND** the wijk Place carries no `gs:within` edge

#### Scenario: A buurt with an unresolvable wijk is seeded without containment

- **WHEN** a buurt's `ligtinwijkid` is null or does not match any projected wijk
- **THEN** the buurt is still emitted as a `gs:Place` with its `rdfs:label` and `gs:active`
- **AND** no `gs:within` edge is emitted for that buurt
- **AND** a `places: …` warning naming the buurt is logged

### Requirement: Seed the skeleton through the SHACL-gated graph load with SCD2 active-status

The Place skeleton SHALL be seeded through the existing `load/graph` `Load` path (the SHACL gate +
run-stamped named-graph + PROV writer), not by writing to Fuseki directly. Because each Place
asserts a `gs:validFrom` on its `gs:active` statement, a Place is an **evolving** entity under the
idempotent upsert (`graph-load-gate`): its content is compared **excluding** the valid-time stamps,
so re-seeding an unchanged skeleton is a **true no-op** (no new `run:load-…` graph, no
`prov:Activity`, no added triples), while a genuine change to a Place's tracked content — most
importantly its `gs:active` flipping `true → false` when an area is soft-deleted — SHALL **open** a
new version (carrying the candidate's `gs:validFrom`) and **close** the prior version by stamping its
`gs:validTo`, retaining history. A non-conforming candidate SHALL be rejected by the gate with no
partial write, exactly as for any other candidate.

#### Scenario: First seeding writes the skeleton with provenance

- **WHEN** the Place skeleton is seeded into an empty graph
- **THEN** the buurt and wijk Places, their `rdfs:label`s, their `gs:within` edges, and their
  `gs:active` statements are written into a `run:load-…` named graph
- **AND** a matching `prov:Activity` with `prov:generatedAtTime` is recorded in `run:_provenance`

#### Scenario: Re-seeding the unchanged skeleton is a no-op

- **WHEN** the same Place skeleton is seeded a second time with no `Reset` (only the emitted
  `gs:validFrom` timestamps differ)
- **THEN** no second `run:load-…` graph is created, no new `prov:Activity` is recorded, and no
  triples are added (the valid-time stamps are excluded from change detection)

#### Scenario: An area going inactive opens a new version and closes the prior

- **WHEN** a Place previously seeded `gs:active true` is re-seeded `gs:active false` (its
  `source_deleted_at` became set upstream)
- **THEN** a new `gs:active false` version is written carrying the candidate's `gs:validFrom`
- **AND** the prior `gs:active true` version is closed by stamping its `gs:validTo` equal to the new
  version's `gs:validFrom`, leaving exactly one open version and retaining the prior interval

### Requirement: The graph load stage seeds the Place skeleton from PostGIS

The pipeline `graph` load source SHALL seed the Place skeleton as part of the graph load: the
`runGraphLoad` step SHALL read the gebieden tables from PostGIS, build the Place candidate, and pass
it to `loadgraph.Load` (replacing the prior reference-model-only `nil` candidate), so a single gated
`Load` ensures the reference model and seeds the skeleton. The load-source ordering SHALL guarantee
the geo backbone (`geo`) is loaded into PostGIS before the `graph` load reads it. The `--reset` flag
SHALL rebuild the skeleton from the current gebieden tables.

#### Scenario: pipeline load seeds the skeleton after the geo backbone

- **WHEN** `pipeline load` runs with the geo backbone already loaded into PostGIS
- **THEN** the `graph` load reads the gebieden tables and seeds the Place skeleton through the gated
  `Load`
- **AND** the reference model (ontology + vocab) is present in the dataset alongside the skeleton

#### Scenario: Reset rebuilds the skeleton

- **WHEN** the graph load runs with `--reset`
- **THEN** prior run graphs are cleared and the Place skeleton is rebuilt from the current gebieden
  tables
