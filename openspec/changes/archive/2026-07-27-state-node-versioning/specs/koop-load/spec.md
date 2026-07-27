## MODIFIED Requirements

### Requirement: Assemble the besluit Intervention/Claim/Place turtle

For each audited besluit the system SHALL construct, and hand to the graph writer, turtle asserting:
an `Intervention` keyed by zaaknummer (`data:intervention/<zaaknummer>`); `gs:activity` → `act:vellen`
(verplanten ≡ vellen); a `gs:locatedAt` edge to the resolved buurt `Place`
(`data:place/<gebieden identificatie>`, the IRI P12b seeds) carrying a `{| gs:confidence <c> ;
gs:caveat <term> |}` annotation (caveat present iff `c < 1.0`) — a **refinable-metadata edge that
holds**, so it SHALL NOT carry `gs:validFrom`/`gs:validTo` (per `docs/RDF_STAR_RELATIONSHIPS.md`:
location holds; its confidence is refined via transaction-time, not versioned in valid-time); a
minimal `<place> a gs:Place` typing so the edge satisfies the shape gate; and a `gs:claims` edge to a
`Claim` (`data:claim/<zaaknummer>`) representing the herplantplicht via art. 7, **with no obligation
count**. The `Intervention` therefore carries no evolving edge — its identity, activity, claims, and
(refinable) location are all non-valid-time.

#### Scenario: A besluit produces a fully-formed Intervention

- **WHEN** a Noord besluit is assembled
- **THEN** the turtle contains one `gs:Intervention` with `gs:activity act:vellen`, a `gs:locatedAt`
  edge to the buurt `Place`, and a `gs:claims` edge to a `Claim`
- **AND** the buurt `Place` IRI matches the P12b `data:place/<identificatie>` scheme
- **AND** the `Claim` carries no numeric obligation count
- **AND** the `gs:locatedAt` annotation carries `gs:confidence` (+ `gs:caveat` when `c < 1.0`) and
  **no** `gs:validFrom`

#### Scenario: The place typing re-assertion does not disturb the P12b Place

- **WHEN** the candidate's `<place> a gs:Place` is written against a P12b-seeded live Place
- **THEN** the writer classifies it as an immutable-content match (skipped), leaving the seeded
  Place's label and `gs:within` unchanged

### Requirement: Idempotent reload

Re-running the load on unchanged landed input SHALL be a true no-op: no new graph triples, no new run
graph, and no PostGIS row changes. Because the `Intervention` carries no valid-time edge, a besluit
whose resolved location **changed** between runs is an **immutable-content change**: the graph writer
SHALL retain the stored version and surface the difference (skip-and-warn), and the PostGIS row SHALL
be upserted to the new resolution; applying the changed resolution to the graph is done via a
`--reset` rebuild. (An in-place transaction-time refinement path for refinable edges is a documented
follow-up, not required here.)

#### Scenario: Unchanged re-run is a no-op

- **WHEN** the load runs a second time against the same landed corpus
- **THEN** no new run graph is minted and no PostGIS rows change

#### Scenario: A changed resolution is surfaced, not versioned

- **WHEN** a besluit's resolved location changes between runs (no `--reset`)
- **THEN** the graph writer retains the stored `locatedAt` and emits a skip-and-warn diagnostic (no
  new valid-time version is opened, because `locatedAt` is not a valid-time edge)
- **AND** the besluit's PostGIS row is upserted to the new resolution
