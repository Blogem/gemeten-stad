# location-resolver Specification

## Purpose
TBD - created by archiving change geo-backbone-preload. Update Purpose after archive.
## Requirements
### Requirement: Resolve to the smallest confident place

The system SHALL resolve a location query — `{street?, huisnummer?, postcode?, point?}` at a given
valid-time date — to the smallest place it can confidently establish, against the local PostGIS
only (no PDOK Locatieserver call, not even as a fallback), returning the place level, a geometry,
and a confidence:

- **address** (`0.90`) — match `nummeraanduiding` on `(postcode, huisnummer)`, else exact
  `openbareruimte.naam` street match, else a `pg_trgm` fuzzy street match; the point is the
  adresseerbaar object (VBO point, or LIG/STA polygon centroid) reached via
  `hoofdadresnummeraanduidingref`.
- **postcode** (`0.70`) — the PC6 exists in BAG but the huisnummer does not resolve.
- **buurt** (`0.50`) — no address/postcode resolves; place the query's own point in a `gebieden`
  buurt via `ST_Contains`. This is the floor: the resolver SHALL NOT snap the query's own point to
  the nearest BAG address or postcode. The permit point is frequently a project-area centroid
  (Spike D: up to ~2.5 km from the true address), so reverse-geocoding it to a finer tier would
  manufacture false address/postcode precision — which the design forbids ("no half-broken data
  enters the graph"). The point is trustworthy only at buurt granularity, so a query whose text
  address does not resolve stays at buurt even when its point happens to fall on an address.

Match preference SHALL be: address over postcode over buurt; within address, `(postcode,
huisnummer)` over exact street over fuzzy street.

#### Scenario: Address resolves at the finest level

- **WHEN** a query with a resolvable `(postcode, huisnummer)` is resolved
- **THEN** the result place level is `address` with confidence `0.90` and a point geometry from the adresseerbaar object

#### Scenario: Postcode-only fallback

- **WHEN** the PC6 exists in BAG but the huisnummer does not resolve to an adresseerbaar object
- **THEN** the result place level is `postcode` with confidence `0.70`

#### Scenario: Buurt fallback via point-in-polygon

- **WHEN** no address or postcode resolves but the query carries its own point
- **THEN** the point is placed in a `gebieden` buurt via `ST_Contains` and the result place level is `buurt` with confidence `0.50` and the `unresolvedLocation` caveat
- **AND** the point is NOT snapped to a nearer address or postcode even if one is close by

### Requirement: Select the voorkomen at the query's valid-time

Because BAG is bitemporal and interventions are backdated, the resolver SHALL select the best-known
voorkomen (`eindregistratie IS NULL`) and prefer the one valid at the query date
(`begingeldigheid <= D AND (eindgeldigheid IS NULL OR eindgeldigheid > D)`). If no voorkomen is
valid at the date, it SHALL fall back to any best-known voorkomen and record the outcome as
`time_match` on the result (`valid_at_date` vs `any_time`), surfaced as a `timeMismatch` caveat on
the downstream `AuditLink` — it SHALL resolve anyway and flag the weakness rather than drop the link.
The resolver SHALL NOT filter on `status`.

#### Scenario: Voorkomen valid at the date is preferred

- **WHEN** a query resolves and a voorkomen is valid at the query date
- **THEN** that voorkomen is chosen and `time_match` is `valid_at_date`

#### Scenario: Any-time fallback is flagged

- **WHEN** no voorkomen is valid at the query date but a best-known voorkomen exists
- **THEN** that voorkomen is chosen and `time_match` is `any_time`, carrying a `timeMismatch` caveat

#### Scenario: Since-withdrawn address still resolves

- **WHEN** a query's address was valid at the query date but its latest voorkomen is now withdrawn (`status = '… ingetrokken'`)
- **THEN** the resolver still returns it (no `status` filter is applied)

### Requirement: Point-in-polygon against the gebieden polygons

The system SHALL place a point into a buurt using `ST_Contains` against the `gebieden` polygons
(keyed by `gbdBuurtId`), not the CBS polygons (CBS is a cross-reference only). For a point whose
source row names its own `gbdBuurtId`, the placed buurt SHALL equal that id.

#### Scenario: Point placed in its own buurt

- **WHEN** a point carrying a known `gbdBuurtId` is placed via `ST_Contains` against `gebieden_buurten`
- **THEN** the resolved buurt `identificatie` equals the point's own `gbdBuurtId`

