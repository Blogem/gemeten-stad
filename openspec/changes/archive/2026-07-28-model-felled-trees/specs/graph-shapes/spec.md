## ADDED Requirements

### Requirement: Tree and felling shapes in core SHACL

`ontology/shapes.ttl` SHALL add core-SHACL `NodeShape`s gating the registry felling entities:

- **`gs:FellingShape`** (`sh:targetClass gs:Felling`): `gs:felledTree` present (`sh:minCount 1`,
  `sh:nodeKind sh:IRI`, `sh:pattern "^http://gemetenstad.nl/id/tree/"`) and `gs:felledOn` present
  (`sh:minCount 1`, `sh:datatype xsd:date`).
- **`gs:TreeShape`** (`sh:targetClass gs:Tree`): permissive — a `gs:Tree` is an identity node, so the
  shape imposes no required properties (geometry lives in PostGIS).

The `sh:class`-free, IRI+namespace-pattern gating on `gs:felledTree` mirrors `gs:coversIntervention`:
it is a cross-load reference (the derive and the bomen graph-load reference each other's IRIs), which
the isolated-candidate SHACL gate cannot validate with `sh:class`.

#### Scenario: A well-formed felling conforms

- **WHEN** a `gs:Felling` has `gs:felledTree` a tree-namespace IRI and `gs:felledOn` an `xsd:date`
- **THEN** it conforms

#### Scenario: A felling missing its tree or date is rejected

- **WHEN** a `gs:Felling` lacks `gs:felledTree`, or its `gs:felledOn` is not an `xsd:date`
- **THEN** the shapes report it as non-conforming

### Requirement: Matched Observation gates its felling membership

`ontology/shapes.ttl` SHALL gate a matched `gs:Observation`: `gs:includesFelling` present
(`sh:minCount 1`) with each value an IRI in the felling namespace (`sh:nodeKind sh:IRI`, `sh:pattern
"^http://gemetenstad.nl/id/felling/"`). This makes the Observation's membership explicit and
validatable while keeping felling detail (geometry, counts) in PostGIS.

#### Scenario: A matched Observation lists its fellings

- **WHEN** a `gs:Observation` is minted for a matched permit
- **THEN** it carries at least one `gs:includesFelling` pointing at a felling-namespace IRI, and
  conforms

#### Scenario: An Observation with no fellings is rejected

- **WHEN** a `gs:Observation` has no `gs:includesFelling`
- **THEN** the shapes report it as non-conforming (a matched Observation must name its members)
