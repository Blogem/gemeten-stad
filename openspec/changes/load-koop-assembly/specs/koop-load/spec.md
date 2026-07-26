## ADDED Requirements

### Requirement: Parse structured signals from landed gzd records

The system SHALL enumerate the landed KOOP publications from the shared raw store under the `koop/`
prefix (ignoring `koop/_cursor.json` and `*.prov.jsonl` sidecars, for which no reader exists) and
parse each record's XML for its structured signals: the publication id (`dcterms:identifier`), the
zaaknummer (`overheidop:referentienummer`), the controlled activiteit (`overheidop:activiteit`), the
RD point geometry (`overheidwetgeving:geometrie`, `POINT(x y)` in EPSG:28992) and/or WGS84 point
(`locatiepunt`), the postcode (`overheidop:postcode`), the title (`dcterms:title`), and the
publication date (`dcterms:available`). A record missing a required signal SHALL be handled per the
"unresolvable permit" requirement, never dropped silently.

#### Scenario: A gzd record yields its structured fields

- **WHEN** the load parses a landed `koop/<id>.xml` gzd record carrying a geometry block
- **THEN** the zaaknummer, activiteit, RD point, postcode, title, and publication date are extracted
- **AND** the RD point is read as a `POINT(x y)` in EPSG:28992

#### Scenario: Sidecars are not treated as permits

- **WHEN** the load enumerates the `koop/` prefix
- **THEN** `koop/_cursor.json` and any `*.prov.jsonl` file are skipped

### Requirement: Dedup aanvraag and besluit per zaaknummer, audit the besluit

The system SHALL group landed publications by zaaknummer (`overheidop:referentienummer`) and, per
group, select the **besluit** publication as the audited permit. Aanvraag versus besluit SHALL be
classified from the `dcterms:title` prefix (`"Verleend:"` / `"Besluit:"` = besluit; `"Aanvraag:"` =
aanvraag) — the only distinguishing signal in the payload. A group with no besluit SHALL NOT produce
an Intervention <<i want to challenge this. i'm curious if the data is as complete as you say (that we will always get a besluit eventually). can you validate against the data pulled? older data should always have an aanvraag+besluit, newer only besluit>>.

#### Scenario: An aanvraag and besluit sharing a zaaknummer collapse to one besluit <<i want to at least have both in postgis and in the graph this should naturally build up (the besluit would become the active version), unelss we only load besluiten in the graph - depends on the point aboves>>

- **WHEN** two publications share a zaaknummer, one titled `"Aanvraag: …"` and one `"Verleend: …"`
- **THEN** a single Intervention is assembled from the besluit publication
- **AND** the aanvraag publication is not assembled as a separate Intervention

#### Scenario: A zaaknummer with only an aanvraag produces no Intervention

- **WHEN** a zaaknummer group contains only an `"Aanvraag: …"` publication
- **THEN** no Intervention is assembled for that zaaknummer

### Requirement: Resolve location and scope to Noord by geometry

The system SHALL resolve each besluit's location through the P6 resolver, preferring the structured
signals: postcode (plus a huisnummer parsed from the title, when present) for the address/postcode
tier, with the structured RD point as the buurt-tier floor via point-in-polygon. The resolved
buurt's gebieden code SHALL be the `Place` identity. The load SHALL keep only permits whose resolved
buurt lies in stadsdeel Noord, cross-checked by the `Z….-N…` zaaknummer prefix; a permit resolving
outside Noord SHALL be excluded. The resolution confidence and any caveats
(`unresolvedLocation`/`timeMismatch`) SHALL be carried onto the `locatedAt` edge and into PostGIS.

#### Scenario: A permit point resolves to a Noord buurt

- **WHEN** a besluit's RD point falls inside a gebieden buurt in stadsdeel Noord
- **THEN** the Intervention is located at that buurt `Place` with the resolver's confidence
- **AND** the exact point is stored in PostGIS

#### Scenario: A permit outside Noord is excluded

- **WHEN** a besluit resolves to a buurt not in stadsdeel Noord
- **THEN** no Intervention is assembled and no graph or PostGIS permit row is written for it

#### Scenario: Fallback resolution carries a caveat, never fake coordinates

- **WHEN** a besluit resolves only to buurt granularity (point-in-polygon floor)
- **THEN** the `locatedAt` edge carries confidence < 1.0 and an `unresolvedLocation` caveat
- **AND** no finer geometry is invented

### Requirement: Assemble the Intervention/Claim/Place graph turtle

For each audited besluit the system SHALL construct, and hand to the graph writer, turtle that
asserts: an `Intervention` keyed by zaaknummer under the `data:` namespace; `gs:activity` referencing
the `act:vellen` concept (verplanten ≡ vellen); a `gs:locatedAt` edge to the resolved buurt `Place`
carrying an RDF-star `{| gs:confidence <c> ; gs:caveat <term> |}` annotation (the caveat present iff
`c < 1.0`); a minimal `<place> a gs:Place` typing so the edge satisfies the shape gate independently
of P12b; and a `gs:claims` edge to a `Claim` representing the herplantplicht triggered via art. 7,
**with no obligation count**. The `{| … |}` annotation construction lives in this load, not in the
writer.

#### Scenario: A besluit produces a fully-formed Intervention

- **WHEN** a Noord besluit is assembled
- **THEN** the turtle contains one `gs:Intervention` with `gs:activity act:vellen`, a `gs:locatedAt`
  edge to the buurt `Place`, and a `gs:claims` edge to a `Claim`
- **AND** the buurt `Place` carries `a gs:Place`
- **AND** the `Claim` carries no numeric obligation count

#### Scenario: The activity edge resolves to the vocabulary

- **WHEN** a permit's `overheidop:activiteit` is a felling term (kappen/vellen/rooien/verplanten)
- **THEN** `gs:activity` references `act:vellen` in the tree-audit ConceptScheme

### Requirement: Write through the SHACL gate; reject malformed candidates

The assembled turtle SHALL be written through the P12 `load/graph.Load` gate. A candidate that
violates `ontology/shapes.ttl` (e.g. a `locatedAt` edge with no `gs:confidence`, or a non-exact edge
with no caveat) SHALL be rejected with no partial write. Values and geometry SHALL be written to
PostGIS.

#### Scenario: A conforming candidate is written

- **WHEN** the assembled turtle conforms to the shapes
- **THEN** it is written into the run-stamped graph and the permit values land in PostGIS

#### Scenario: A malformed candidate is rejected

- **WHEN** an assembled `locatedAt` edge lacks its `gs:confidence` annotation
- **THEN** the load fails and nothing is written to the graph

### Requirement: Persist permit values and geometry to PostGIS

The system SHALL store each loaded permit's values in a PostGIS table keyed by zaaknummer <<since we get multiple versions (aanvraag, besluit, maybe additional updates) we cannot just key by zaaknummer, but need to follow the SCD2 pattern>>: the
publication ids, activiteit, publication/besluit dates, the exact RD point geometry, postcode, the
resolved buurt code, the resolution confidence and caveats, and the raw record. Geometry SHALL live
only in PostGIS, never as an RDF literal.

#### Scenario: A loaded permit's values are persisted

- **WHEN** a Noord besluit is loaded
- **THEN** a PostGIS row keyed by its zaaknummer holds its point geometry, postcode, dates,
  resolved buurt code, confidence, and caveats
- **AND** no geometry literal appears in the graph turtle

### Requirement: Idempotent reload by permit identity

Re-running the load on unchanged landed input SHALL be a true no-op: no new graph triples, no new
run graph, and no PostGIS row changes. A changed tracked field SHALL open a new version under the
P12 SCD2 upsert semantics (closing the prior's `gs:validTo`), never overwriting history, and SHALL
upsert the corresponding PostGIS row.

#### Scenario: Unchanged re-run is a no-op

- **WHEN** the load runs a second time against the same landed corpus
- **THEN** no new run graph is minted and no PostGIS rows change

#### Scenario: A changed field opens a new version

- **WHEN** a permit's tracked field changes between runs
- **THEN** a new graph version opens and the prior version's `gs:validTo` is stamped
- **AND** the PostGIS row is upserted to the new value

### Requirement: Handle unresolvable permits honestly

A besluit whose location cannot be resolved to any place (no usable point or address) SHALL be
persisted to PostGIS with an `unresolved` marker (zaaknummer, raw title, dates) and SHALL NOT be
written to the graph as an Intervention with fabricated coordinates. Such permits SHALL be
surfaced for downstream reporting, not silently dropped.

#### Scenario: An unresolvable besluit is recorded but not graphed

- **WHEN** a besluit has neither a usable point nor a resolvable address
- **THEN** it is stored in PostGIS with an `unresolved` marker
- **AND** no graph Intervention is asserted for it
