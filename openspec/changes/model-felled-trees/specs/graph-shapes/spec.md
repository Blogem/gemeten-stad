## ADDED Requirements

### Requirement: Tree, felling, and replanting shapes in core SHACL

`ontology/shapes.ttl` SHALL add core-SHACL `NodeShape`s gating the registry entities:

- **`gs:FellingShape`** (`sh:targetClass gs:Felling`): `gs:felledTree` present (`sh:minCount 1`,
  `sh:nodeKind sh:IRI`, `sh:pattern "^http://gemetenstad.nl/id/tree/"`) and `gs:felledOn` present
  (`sh:minCount 1`, `sh:datatype xsd:date`).
- **`gs:ReplantingShape`** (`sh:targetClass gs:Replanting`): `gs:plantedTree` present (`sh:minCount 1`,
  `sh:nodeKind sh:IRI`, tree-namespace `sh:pattern`), `gs:plantedOn` present (`sh:datatype xsd:date`),
  and `gs:replaces` present (`sh:minCount 1`, `sh:nodeKind sh:IRI`, `sh:pattern
  "^http://gemetenstad.nl/id/felling/"`).
- **`gs:TreeShape`** (`sh:targetClass gs:Tree`): permissive — a `gs:Tree` MAY be a bare identity (a
  referenced replacement tree), so the shape imposes no required properties (identity only).

The `sh:class`-free, IRI+namespace-pattern gating on `gs:felledTree`/`gs:plantedTree`/`gs:replaces`
mirrors `gs:coversIntervention`: these are cross-load references (the derive and the bomen graph-load
reference each other's IRIs), which the isolated-candidate SHACL gate cannot validate with `sh:class`.

#### Scenario: A well-formed felling conforms

- **WHEN** a `gs:Felling` has `gs:felledTree` a tree-namespace IRI and `gs:felledOn` an `xsd:date`
- **THEN** it conforms

#### Scenario: A felling missing its tree or date is rejected

- **WHEN** a `gs:Felling` lacks `gs:felledTree`, or its `gs:felledOn` is not an `xsd:date`
- **THEN** the shapes report it as non-conforming

#### Scenario: A well-formed replanting conforms

- **WHEN** a `gs:Replanting` has `gs:plantedTree` a tree IRI, `gs:plantedOn` an `xsd:date`, and
  `gs:replaces` a felling-namespace IRI
- **THEN** it conforms

#### Scenario: A replanting not referencing a felling is rejected

- **WHEN** a `gs:Replanting` has no `gs:replaces` (or one outside the felling namespace)
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
