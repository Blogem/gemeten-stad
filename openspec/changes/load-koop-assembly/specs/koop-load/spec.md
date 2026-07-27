## ADDED Requirements

### Requirement: Read the landed SRU record and metadata sidecar per publication

The system SHALL enumerate landed KOOP publications from the raw store under the `koop/` prefix
(the SRU records `koop/<id>.xml`, excluding `*.metadata.xml`, `koop/_cursor.json`, and `*.prov.jsonl`)
and, for each, parse its SRU record and its `koop/<id>.metadata.xml` sidecar into a typed publication:
the publication id, the zaaknummer (`OVERHEIDop.referentienummer` from the sidecar), the publication
kind (from the `dcterms:title` prefix), the activiteit, the RD point (`overheidwetgeving:geometrie`),
the title address, and the publication date (`dcterms:available`). The zaaknummer SHALL be sourced
from the metadata sidecar (it is absent from the SRU record).

#### Scenario: A publication yields its structured fields from both artifacts

- **WHEN** the load parses a publication's SRU record and metadata sidecar
- **THEN** the zaaknummer comes from `OVERHEIDop.referentienummer`, and the RD point, activiteit,
  title address, kind, and date come from the SRU record
- **AND** sidecar/cursor/prov files are not treated as SRU records

#### Scenario: A publication with no metadata sidecar falls to the keyless remainder

- **WHEN** a publication has no landed `metadata.xml` (no zaaknummer available)
- **THEN** it is recorded in PostGIS as part of the keyless remainder and is not assembled into the
  graph

### Requirement: Dedup by zaaknummer and audit the besluit

The system SHALL group publications by zaaknummer and select the **besluit** (title prefix
`Besluit`/`Ontwerpbesluit`) as the audited permit. Only the besluit SHALL become a graph
`Intervention`; the aanvraag, verlenging, ingetrokken, and other publications of the same zaak SHALL
be retained in PostGIS as the publication trail, not assembled as graph entities. A zaak with no
besluit SHALL NOT produce a graph `Intervention`.

#### Scenario: A case with aanvraag and besluit yields one besluit Intervention

- **WHEN** a zaak has an `Aanvraag` and a `Besluit` publication
- **THEN** a single `Intervention` is assembled from the besluit
- **AND** both publications are retained as rows in PostGIS

#### Scenario: A pending case (aanvraag only) is recorded but not graphed

- **WHEN** a zaak has only an `Aanvraag` publication (no besluit yet)
- **THEN** no graph `Intervention` is assembled
- **AND** the aanvraag is retained in PostGIS, to be assembled when its besluit later lands

### Requirement: Resolve location address-first and scope to Noord

The system SHALL resolve each besluit's location through the P6 resolver, preferring the title
address (postcode + huisnummer parsed from `dcterms:title`) for the address/postcode tier, with the
RD point (a coarse `Gebiedsmarkering`) as the buurt point-in-polygon floor. The resolved gebieden
buurt code SHALL be the `Place` identity. The load SHALL keep only permits whose resolved buurt is in
stadsdeel Noord, cross-checked by the `Z….-N…` zaaknummer prefix; permits resolving outside Noord
SHALL be excluded from the graph. The resolution confidence and caveats SHALL be carried onto the
`locatedAt` edge and into PostGIS.

#### Scenario: A title address resolves to a Noord buurt

- **WHEN** a besluit's title postcode + huisnummer resolve to a BAG address in a Noord buurt
- **THEN** the Intervention is located at that buurt `Place` with the resolver's confidence
- **AND** the resolved values are stored in PostGIS

#### Scenario: Only the coarse point resolves, at the buurt floor

- **WHEN** a besluit's address does not resolve but its RD point falls in a Noord buurt polygon
- **THEN** the `locatedAt` edge carries confidence < 1.0 and an `unresolvedLocation` caveat
- **AND** no finer geometry is invented

#### Scenario: A permit outside Noord is excluded from the graph

- **WHEN** a besluit resolves to a buurt not in stadsdeel Noord
- **THEN** no graph `Intervention` is assembled for it

### Requirement: Assemble the besluit Intervention/Claim/Place turtle

For each audited besluit the system SHALL construct, and hand to the graph writer, turtle asserting:
an `Intervention` keyed by zaaknummer (`data:intervention/<zaaknummer>`); `gs:activity` → `act:vellen`
(verplanten ≡ vellen); a `gs:locatedAt` edge to the resolved buurt `Place`
(`data:place/<gebieden identificatie>`, the IRI P12b seeds) carrying a `{| gs:confidence <c> ;
gs:caveat <term> ; gs:validFrom <besluit-date> |}` annotation (caveat present iff `c < 1.0`); a
minimal `<place> a gs:Place` typing so the edge satisfies the shape gate; and a `gs:claims` edge to a
`Claim` (`data:claim/<zaaknummer>`) representing the herplantplicht via art. 7, **with no obligation
count**.

#### Scenario: A besluit produces a fully-formed Intervention

- **WHEN** a Noord besluit is assembled
- **THEN** the turtle contains one `gs:Intervention` with `gs:activity act:vellen`, a `gs:locatedAt`
  edge to the buurt `Place`, and a `gs:claims` edge to a `Claim`
- **AND** the buurt `Place` IRI matches the P12b `data:place/<identificatie>` scheme
- **AND** the `Claim` carries no numeric obligation count

#### Scenario: The place typing re-assertion does not disturb the P12b Place

- **WHEN** the candidate's `<place> a gs:Place` is written against a P12b-seeded live Place
- **THEN** the writer classifies it as an immutable-content match (skipped), leaving the seeded
  Place's label and `gs:within` unchanged

### Requirement: Write through the SHACL gate; reject malformed candidates

The assembled turtle SHALL be written through `load/graph.Load`. A candidate that violates
`ontology/shapes.ttl` (e.g. a `locatedAt` edge with no `gs:confidence`, or a non-exact edge with no
caveat) SHALL be rejected with no partial write.

#### Scenario: A conforming candidate is written

- **WHEN** the assembled turtle conforms to the shapes
- **THEN** it is written into the run-stamped graph and the permit values land in PostGIS

#### Scenario: A malformed candidate is rejected

- **WHEN** an assembled `locatedAt` edge lacks its `gs:confidence` annotation
- **THEN** the load fails and nothing is written to the graph

### Requirement: Persist the full publication trail to PostGIS

The system SHALL store every publication of every in-scope zaak in a PostGIS table keyed by
publication id. **In-scope** means the zaak is Noord-audited (its besluit resolves to a Noord buurt),
or cannot yet be excluded (pending with no resolved besluit, unresolvable, or keyless); a zaak whose
besluit positively resolves **outside** Noord SHALL be excluded entirely — no graph Intervention and
no PostGIS row. Each in-scope row carries: the zaaknummer, the kind (aanvraag/besluit/verlenging/ingetrokken/…), the dates, the
raw RD point geometry, postcode, resolved buurt code, resolution confidence and caveats, the
resolver's precise resolved point (`resolved_geom`, the address-tier BAG point — NULL at the
postcode/buurt tier) and the resolution tier (`resolved_tier`, address/postcode/buurt), an
`unresolved` marker, and the raw record. A zaak's multiple publications SHALL each be their own row
(the table is keyed by publication id, not zaaknummer). Geometry SHALL live only in PostGIS, never as
an RDF literal.

#### Scenario: The precise resolved point is kept as silver

- **WHEN** a besluit resolves at the address tier to a BAG point
- **THEN** its row carries `resolved_geom` (that point) and `resolved_tier` = `address`
- **AND** a besluit that resolves only to its buurt carries a NULL `resolved_geom` with
  `resolved_tier` = `buurt`, the buurt code still recording the place

#### Scenario: All publications of a case are persisted

- **WHEN** a zaak with an aanvraag and a besluit is loaded
- **THEN** PostGIS holds a row per publication keyed by publication id, each carrying its zaaknummer
  and kind
- **AND** no geometry literal appears in the graph turtle

### Requirement: Idempotent reload

Re-running the load on unchanged landed input SHALL be a true no-op: no new graph triples, no new run
graph, and no PostGIS row changes. A changed tracked field on the besluit (e.g. a re-resolution) SHALL
open a new graph version under the P12 SCD2 semantics (closing the prior's `gs:validTo`) and upsert
the corresponding PostGIS row.

#### Scenario: Unchanged re-run is a no-op

- **WHEN** the load runs a second time against the same landed corpus
- **THEN** no new run graph is minted and no PostGIS rows change

#### Scenario: A re-resolution opens a new version

- **WHEN** a besluit's resolved location changes between runs
- **THEN** a new `locatedAt` version opens and the prior's `gs:validTo` is stamped
- **AND** the besluit's PostGIS row is upserted

### Requirement: Handle unresolvable permits honestly

A besluit whose location cannot be resolved SHALL be persisted to PostGIS with an `unresolved` marker
and SHALL NOT be written to the graph with fabricated coordinates — unresolvable meaning no resolvable
title address and no RD point falling in a Noord buurt. Such permits SHALL be surfaced for downstream
reporting.

#### Scenario: An unresolvable besluit is recorded but not graphed

- **WHEN** a besluit has neither a resolvable address nor a point in a Noord buurt
- **THEN** it is stored in PostGIS with an `unresolved` marker
- **AND** no graph Intervention is asserted for it
