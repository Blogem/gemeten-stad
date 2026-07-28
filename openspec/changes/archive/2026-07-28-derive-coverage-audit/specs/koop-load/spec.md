## MODIFIED Requirements

### Requirement: Assemble the besluit Intervention/Claim/Place turtle

For each audited besluit the system SHALL construct, and hand to the graph writer, turtle asserting:
an `Intervention` keyed by zaaknummer (`data:intervention/<zaaknummer>`); the besluit **publication
date** as `dct:available` (`xsd:date`, sourced from the `koop_publications.available` value) — a
timeless descriptive-metadata literal on the Intervention, reusing Dublin Core Terms rather than a
minted `gs:` term, and therefore carrying **no** `gs:validFrom`/`gs:validTo`; `gs:activity` →
`act:vellen` (verplanten ≡ vellen); a `gs:locatedAt` edge to the resolved buurt `Place`
(`data:place/<gebieden identificatie>`, the IRI P12b seeds) carrying a `{| gs:confidence <c> ;
gs:caveat <term> |}` annotation (caveat present iff `c < 1.0`) — a **refinable-metadata edge that
holds**, so it SHALL NOT carry `gs:validFrom`/`gs:validTo` (per `docs/RDF_STAR_MODELING.md`:
location holds; its confidence is refined via transaction-time, not versioned in valid-time); a
minimal `<place> a gs:Place` typing so the edge satisfies the shape gate; and a `gs:claims` edge to a
`Claim` (`data:claim/<zaaknummer>`) representing the herplantplicht via art. 7, **with no obligation
count**. The `Intervention` therefore carries no evolving edge — its identity, activity, publication
date, claims, and (refinable) location are all non-valid-time.

The publication date is the temporal anchor the P14 coverage audit windows fellings against, so it
SHALL be present on every audited besluit Intervention (`koop_publications.available` is 100%
populated for audited besluiten).

#### Scenario: A besluit produces a fully-formed Intervention

- **WHEN** a Noord besluit is assembled
- **THEN** the turtle contains one `gs:Intervention` with `gs:activity act:vellen`, a `gs:locatedAt`
  edge to the buurt `Place`, and a `gs:claims` edge to a `Claim`
- **AND** the buurt `Place` IRI matches the P12b `data:place/<identificatie>` scheme
- **AND** the `Claim` carries no numeric obligation count
- **AND** the `gs:locatedAt` annotation carries `gs:confidence` (+ `gs:caveat` when `c < 1.0`) and
  **no** `gs:validFrom`

#### Scenario: The Intervention carries its publication date as dct:available

- **WHEN** a Noord besluit with a landed publication date is assembled
- **THEN** the turtle asserts `data:intervention/<zaaknummer> dct:available "YYYY-MM-DD"^^xsd:date`
  (Dublin Core Terms, reused — not a minted `gs:` term)
- **AND** the `dct:available` triple carries no `gs:validFrom`/`gs:validTo` (the publication date is a
  timeless descriptive fact, not evolving state)

#### Scenario: The place typing re-assertion does not disturb the P12b Place

- **WHEN** the candidate's `<place> a gs:Place` is written against a P12b-seeded live Place
- **THEN** the writer classifies it as an immutable-content match (skipped), leaving the seeded
  Place's label and `gs:within` unchanged
