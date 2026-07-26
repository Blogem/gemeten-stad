## ADDED Requirements

### Requirement: Load the BAG extract into PostGIS as-is

The system SHALL load the landed BAG extract into PostGIS via the GDAL `lvbag` driver, applying
**only** the municipality filter `identificatie LIKE '%.0363%'` — no column projection and no other
row filter. It SHALL load all voorkomens (temporal versions) and all columns, and SHALL load the
three *adresseerbaar object* types plus their address keys: `verblijfsobject` (VBO),
`ligplaats` (LIG), `standplaats` (STA), `nummeraanduiding` (NUM), and `openbareruimte` (OPR).
Building footprints (`pand`) and `woonplaats` SHALL be omitted as whole tables. Invalid geometries
SHALL be handled with `AUTOCORRECT_INVALID_DATA=YES`. The bulk load SHALL run through the GDAL
sidecar (`ogr2ogr`), orchestrated from Go.

#### Scenario: BAG tables loaded with the municipality filter

- **WHEN** the BAG load runs against a landed extract
- **THEN** the OPR, NUM, VBO, LIG, and STA tables are populated with only `0363` objects (matched via `'%.0363%'`)
- **AND** all voorkomens and all columns are retained (no per-column or per-status filtering)
- **AND** `pand` and `woonplaats` are not loaded

#### Scenario: Withdrawn objects are retained

- **WHEN** the BAG load completes
- **THEN** objects whose latest voorkomen has a withdrawn `status` (e.g. `… ingetrokken`) are still present in the loaded tables (not dropped)

### Requirement: Load the boundary geometries into PostGIS

The system SHALL load the landed whole-city `gebieden` buurt/wijk polygons and the CBS reference
into PostGIS in SRID 28992, keyed so `gebieden_buurten` carries the `gbdBuurtId` (`identificatie`).
All loaded geometry SHALL share SRID 28992.

#### Scenario: Polygons loaded in RD

- **WHEN** the polygon load runs
- **THEN** `gebieden_buurten`, `gebieden_wijken`, and the CBS reference table are populated with whole-city geometry in SRID 28992

### Requirement: Non-destructive reload (never lose referenced entities)

A refresh SHALL NOT physically delete previously loaded rows. The load SHALL stage the new extract
and **upsert** into the target tables keyed by the voorkomen identity (object `identificatie` +
`begingeldigheid` + `tijdstipregistratie`): new voorkomens are inserted, and a previously loaded
row that is absent from the new extract is **soft-deleted** by stamping `source_deleted_at` with
the load timestamp, not removed. This preserves any entity a downstream graph node may reference and
matches the project's bitemporal stance (never overwrite history). Within BAG this is rare, because
withdrawal is already an in-band `status` change on a retained voorkomen; the soft-delete covers the
residual case of an object fully expunged upstream. Re-running the same extract SHALL be a no-op.

#### Scenario: New extract adds a voorkomen

- **WHEN** a refresh loads an extract containing a new voorkomen for an existing object
- **THEN** the new voorkomen is inserted and the prior voorkomens are retained unchanged

#### Scenario: Object absent from the new extract is soft-deleted

- **WHEN** a refresh loads an extract in which a previously loaded object no longer appears
- **THEN** that object's row is retained with `source_deleted_at` set to the load timestamp (not physically deleted)

#### Scenario: Re-running the same extract is a no-op

- **WHEN** the load runs a second time against the same landed extract
- **THEN** no rows are inserted, updated, or soft-deleted

### Requirement: Build the resolver indexes

The system SHALL create the indexes the resolver relies on: a GIST index on each geometry column
used for point-in-polygon, a `pg_trgm` GIN index on `lower(openbareruimte.naam)`, and the b-tree
indexes on the NUM/VBO/LIG/STA address-join keys and `(postcode, huisnummer)`.

#### Scenario: Indexes present after load

- **WHEN** the load completes
- **THEN** the GIST geometry indexes, the `pg_trgm` street index, and the address-join / `(postcode, huisnummer)` b-tree indexes exist

### Requirement: Sanity gates after load

After loading, the system SHALL assert sanity gates and fail loudly (non-zero exit) if any is not
met: a single distinct geometry SRID of 28992 across the loaded tables, and — as the Spike D
verified anchor — the stadsdeel Noord ground truth present in the whole-city load (69 Noord buurten,
15 Noord wijken). Whole-city row counts SHALL be logged.

#### Scenario: Sanity gates enforced

- **WHEN** the load completes on a clean volume
- **THEN** every loaded geometry is SRID 28992
- **AND** the 69 Noord buurten and 15 Noord wijken are present among the loaded whole-city polygons
- **AND** if any gate is not met the load exits with a non-zero status

### Requirement: Reset rebuilds from landed data

The load SHALL support a reset flag that drops and rebuilds the target tables from the landed data,
for a clean dev rebuild. Absent the flag, the load is the non-destructive upsert above.

#### Scenario: Reset rebuilds

- **WHEN** the load runs with the reset flag
- **THEN** the target tables are dropped and rebuilt from the landed data
