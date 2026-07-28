## ADDED Requirements

### Requirement: Generate candidate permit↔felling pairs

The system SHALL enumerate each besluit `gs:Intervention` from the **graph** (by SPARQL — the graph
is the source of truth for which permits exist and when they were published) and generate candidate
registry matches: **individual** felled `kapenherplant` rows in the permit's resolved buurt whose
felling date (`kapmaatregelDatumUitgevoerd`) falls within `[publication, +3yr]`. The **publication
date** SHALL be read from the Intervention's `dct:available` literal in the graph. The buurt code
(equal to the registry `gbdBuurtId`) and the resolved point/tier SHALL be read as **values** from
`koop_publications` (PostGIS), joined by zaaknummer — the clean hybrid-store split: meaning and dates
from the graph, quantitative/geometric values from PostGIS. The **individual felling** is the
matching unit — the system SHALL NOT group fellings
by the batch date `datumVergunningVerleend` (unusable per Spike A). Candidate fellings SHALL be those
in the time window that are **either in the permit's buurt OR within 200 m of the permit's resolved
point** (`ST_DWithin`, SRID-aligned to metres) — a **union**, not a replacement. The buurt clause
preserves recall for spread-out projects (a felling far from the point but in the same buurt MUST NOT
be dropped); the 200 m spatial clause additively catches near fellings **just across a buurt boundary**
that a buurt-only net would miss. The union SHALL be fragmentation-safe (the buurt is always included,
so a large multi-felling project is never split), and the 200 m radius SHALL match the scorer's
postcode proximity band (beyond it a cross-boundary felling scores at the 0.50 buurt floor and adds no
trustworthy signal). A **point-less** permit (buurt-tier) has no point, so only the buurt clause
applies (best-effort fallback). When the permit carries a resolved point
(`koop_publications.resolved_geom`), the system SHALL compute, per candidate felling, the metric
distance from that point to the felling geometry (`kapenherplant.resolvedGeom`, SRID-transformed to
metres); that distance feeds scoring.

#### Scenario: Felled rows in the buurt+window become candidate fellings

- **WHEN** a permit resolves to buurt `B` with publication date `P`, and the registry holds felled
  rows in `B` with felling dates in `[P, P+3yr]`
- **THEN** each such row is returned as an individual candidate felling (no batch-date grouping)

#### Scenario: A resolved point yields a per-felling distance

- **WHEN** the permit carries a `resolved_geom` and a candidate felling carries geometry
- **THEN** the metric distance from the permit point to the felling is computed (SRID-aligned) and
  attached to the candidate for scoring
- **AND** a permit with no `resolved_geom` skips the distance step and is scored at the buurt floor

#### Scenario: A near cross-boundary felling is a candidate

- **WHEN** a permit carries a resolved point and a felled row within `[pub,+3yr]` lies **within 200 m**
  of that point but in a **different** buurt than the permit's `resolved_identificatie`
- **THEN** that felling is a candidate (via the spatial clause), even though the buurt-equals clause
  alone would exclude it

#### Scenario: A spread-out project keeps its far same-buurt fellings

- **WHEN** a permit's felled rows in its own buurt lie beyond 200 m from the resolved point (a large
  project across a buurt-sized site)
- **THEN** they remain candidates (via the buurt clause) — the spatial radius does not shrink the
  buurt net, so the project is not fragmented

#### Scenario: A felling before publication is not a candidate

- **WHEN** a felled registry row in the permit's buurt has a felling date *earlier* than the
  permit's publication date
- **THEN** it is excluded from the candidate set

#### Scenario: Candidate time is anchored on reliable dates

- **WHEN** the time gap is evaluated
- **THEN** the felling end uses `kapmaatregelDatumUitgevoerd` (100% populated, per-tree) and the
  permit end uses the besluit publication date; no batch-assigned permit/registry grant date is used

#### Scenario: The publication date comes from the graph

- **WHEN** a permit's publication date is needed to window candidates
- **THEN** it is read from the Intervention's `dct:available` literal in the graph (the graph is the
  source of truth for it), not from a PostGIS column

### Requirement: Assign each felling to at most one permit

The system SHALL assign every candidate felling to **at most one** permit — the permit whose
permit↔felling score is highest. A permit MAY be assigned **multiple** fellings; a permit's
`Observation` is the **set of fellings assigned to it**. Assignment SHALL be deterministic (a stable
tie-break — e.g. distance, then zaaknummer). A felling that is a candidate for several permits SHALL
NOT be counted toward more than one permit's Observation.

#### Scenario: A contested felling goes to its best permit only

- **WHEN** one felling is a candidate for two permits, scoring higher against permit A
- **THEN** the felling is assigned to permit A's Observation only, and does not appear in permit B's

#### Scenario: A multi-tree permit is assigned several fellings

- **WHEN** several fellings each score best against the same permit
- **THEN** all of them are assigned to that permit's Observation and its assigned-felling count equals
  the size of that set

#### Scenario: Assignment is stable across runs

- **WHEN** the same permits and fellings are scored on two runs
- **THEN** the felling→permit assignment is identical (deterministic tie-break)

### Requirement: Score the permit↔felling link with the place-led model

The system SHALL score each candidate pair into `[0,1]`: a **distance-graduated place term with a
0.50 buurt floor** (proximity of the permit's resolved point to the felling — address-tier proximate
highest, postcode/mid between, buurt-only/beyond-range at 0.50); a **time** adjustment on the
publication→felling gap (≤2yr +0.15, 2–3yr +0.05); a **count** adjustment using the **registry**
felled count (exact +0.30, ±15% +0.15, unknown +0, incompatible −0.10) and **never** a permit-text
count; and an **ambiguity** adjustment for assignment contention (sole/clean +0.05, else −0.03·(n−1)
floored −0.15), clamped to `[0,1]`. A permit's link confidence SHALL be scored over its assigned set.
The threshold SHALL be τ = 0.60. The buurt floor SHALL be held fixed so a point-less permit scores
identically to the buurt-level Spike B calibration; proximity radii/values SHALL be calibrated against
the spike's labeled cases at implementation time.

#### Scenario: Count-unknown buurt-only match scores at the buurt floor

- **WHEN** a permit with **no resolved point** and no known count is assigned one felling within 2 yr
- **THEN** the place term is 0.50 and the score is 0.50 + 0 + 0.15 + 0.05 = 0.70, identical to the
  Spike B buurt-level calibration

#### Scenario: A resolved point near a felling lifts place above the floor

- **WHEN** a permit resolved to an address-tier point is assigned a felling essentially at that point,
  same buurt+time window, unknown count
- **THEN** the place term exceeds 0.50 (granularity `gs:address`) and the link scores strictly higher
  than the buurt-only equivalent

#### Scenario: The registry count is used, never a permit-text count

- **WHEN** the count axis is evaluated
- **THEN** the registry felled count is the compared value; no permit-text/extracted count is read

### Requirement: Record the coverage outcome as an anchor plus a versioned period

For each permit the system SHALL ensure a stable `gs:AuditLink` **anchor**
(`data:auditlink/<zaaknummer> a gs:AuditLink ; gs:coversIntervention <intervention>`, write-once) and
assemble one `gs:CoveragePeriod` **period node** (`data:auditlink/<zaaknummer>/<content-key>`, the key
a hash of the outcome excluding timestamps) carrying `gs:versionOf` → the anchor, `gs:validFrom` (run
world-time), `gs:evidence`, and `prov:wasDerivedFrom` the permit + registry rows. When the permit is
assigned **≥1 felling** the period SHALL be **matched**: mint `data:observation/<zaaknummer> a
gs:Observation` (identity only), and carry `gs:linksObservation` → it, `gs:confidence` (the score),
`gs:granularity` (the tier used — `gs:address`/`gs:postcode`/`gs:buurt`), and `gs:caveat gs:weakLink`
when the score is below τ. When the permit is assigned **zero fellings** the period SHALL be
**no-source**: `gs:noSourceFound true`, no `gs:Observation` and no `gs:linksObservation`.

#### Scenario: A strong match is a matched period on the permit's anchor

- **WHEN** a permit's assigned fellings score ≥ 0.60
- **THEN** the permit's `gs:AuditLink` anchor exists (`gs:coversIntervention` the Intervention) and a
  `gs:CoveragePeriod` `gs:versionOf` that anchor is assembled with `gs:linksObservation` → the
  `Observation`, `gs:confidence` = the score, and `gs:granularity` = the tier used

#### Scenario: A below-τ match is a matched period with a weakLink caveat

- **WHEN** a permit is assigned ≥1 felling but scores below 0.60
- **THEN** the matched period additionally carries `gs:caveat gs:weakLink`

#### Scenario: An unmatched permit is a no-source period

- **WHEN** a permit is assigned no felling in its buurt+window
- **THEN** a `gs:CoveragePeriod` `gs:versionOf` the permit's anchor is assembled with
  `gs:noSourceFound true` and `gs:evidence` naming the searched buurt and window, and no
  `gs:Observation` is minted

### Requirement: Write derived coverage through the SHACL gate

The assembled turtle SHALL be written through the P12 `load/graph.Load` gate, which validates against
`ontology/shapes.ttl` (`gs:AuditLinkShape` + `gs:CoveragePeriodShape`). An anchor missing
`gs:coversIntervention`, or a period missing `gs:versionOf`/`gs:validFrom`/`gs:evidence`, or a period
matching neither the matched branch (`gs:linksObservation` + `gs:confidence`) nor the no-source branch
(`gs:noSourceFound true`), or with a `gs:confidence` outside `[0,1]`, SHALL be rejected with no
partial write.

#### Scenario: A conforming coverage period is written

- **WHEN** the derive turtle conforms to `ontology/shapes.ttl`
- **THEN** it is written into a run-stamped graph

#### Scenario: A malformed coverage period is rejected

- **WHEN** a `gs:CoveragePeriod` carries `gs:linksObservation` but no `gs:confidence` (matches neither
  branch of the shape)
- **THEN** the derive run fails and nothing is written to the graph

### Requirement: Persist derived numbers to PostGIS

The system SHALL store each permit's derived coverage **numbers** in a slim PostGIS `audit_metrics`
table keyed by zaaknummer: the matched flag, the assigned-felling ids and count, the candidate count,
the nearest-felling distance, and the derive run id. The link itself (confidence, granularity,
caveats, the anchor/period/Observation) SHALL NOT be duplicated here — it lives in the graph;
geometry SHALL NOT be duplicated — it lives in `kapenherplant`.

#### Scenario: A matched permit's derived numbers are persisted

- **WHEN** a permit is assigned ≥1 felling
- **THEN** an `audit_metrics` row records `matched = true`, the assigned-felling ids + count, the
  candidate count, and the nearest distance, with no confidence/granularity/caveat column

#### Scenario: A no-source outcome is persisted

- **WHEN** a permit is assigned no felling
- **THEN** an `audit_metrics` row records `matched = false` with a zero assigned-felling count

### Requirement: Idempotent re-derivation via content-keyed period nodes

Re-running `derive` on unchanged inputs SHALL be a true no-op: the same content-keyed
`gs:CoveragePeriod` IRI is produced, the writer's valid-time-agnostic signature matches, no new period
is written, no prior is closed, and no new run graph or `audit_metrics` change occurs. When a permit's
coverage outcome changes (no-source → matched, weak → strong, or a changed assigned set), the derive
SHALL produce a **new** content-keyed period node; the writer SHALL open it and close the prior open
period of that permit's anchor (stamping `gs:validTo`), retaining history, and the `audit_metrics` row
SHALL be upserted.

#### Scenario: Unchanged re-run is a no-op

- **WHEN** `derive` runs a second time against the same graph + registry
- **THEN** the same period IRI is produced, no new run graph is minted, and no `audit_metrics` row
  changes

#### Scenario: A newly appearing felling opens a matched period and closes the prior no-source

- **WHEN** a later run assigns a felling to a permit previously recorded as a `gs:noSourceFound` period
- **THEN** a new matched `gs:CoveragePeriod` opens (`gs:linksObservation` → the Observation)
- **AND** the prior no-source period's `gs:validTo` is stamped (closed), retained in history
- **AND** exactly one open period remains for that permit's anchor
