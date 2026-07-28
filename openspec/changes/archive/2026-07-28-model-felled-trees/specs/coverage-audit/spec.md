## MODIFIED Requirements

### Requirement: Record the coverage outcome as an anchor plus a versioned period

For each permit the system SHALL ensure a stable `gs:AuditLink` **anchor**
(`data:auditlink/<zaaknummer> a gs:AuditLink ; gs:coversIntervention <intervention>`, write-once) and
assemble one `gs:CoveragePeriod` **period node** (`data:auditlink/<zaaknummer>/<content-key>`, the key
a hash of the outcome excluding timestamps) carrying `gs:versionOf` → the anchor, `gs:validFrom` (run
world-time), `gs:evidence`, and `prov:wasDerivedFrom` the permit + registry rows. When the permit is
assigned **≥1 felling** the period SHALL be **matched**: mint a **content-addressed** `gs:Observation`
(`data:observation/<zaaknummer>/<felling-set-key>`, where `<felling-set-key>` is a hash of the sorted
assigned `gs:Felling` IRIs) carrying `gs:includesFelling` → each assigned `gs:Felling` (the fellings
loaded by the bomen graph projection), and carry `gs:linksObservation` → that Observation,
`gs:confidence` (the score), `gs:granularity` (the tier used — `gs:address`/`gs:postcode`/`gs:buurt`),
and `gs:caveat gs:weakLink` when the score is below τ. Because the Observation IRI is content-addressed
by its felling set, a changed assigned set yields a **new** Observation IRI. When the permit is
assigned **zero fellings** the period SHALL be **no-source**: `gs:noSourceFound true`, no
`gs:Observation` and no `gs:linksObservation`. The Observation is **not** identity-only and **not** one
mutable node per permit (superseding derive-coverage-audit D2): it is an immutable, content-addressed
snapshot of the observed felling set, and its active-ness is carried by whether an open
`gs:CoveragePeriod` links it (no separate `active` flag).

#### Scenario: A matched period links a content-addressed Observation of its fellings

- **WHEN** a permit is assigned fellings `F1, F2`
- **THEN** a `gs:Observation` `data:observation/<zaaknummer>/<key over {F1,F2}>` is minted with
  `gs:includesFelling F1, F2`, and the matched `gs:CoveragePeriod` carries `gs:linksObservation` → it

#### Scenario: A changed felling set mints a new Observation and a new period

- **WHEN** a later run assigns the same permit `F1, F2, F3` (set changed)
- **THEN** a new content-addressed `gs:Observation` (different IRI) is minted with the new
  `gs:includesFelling`, a new `gs:CoveragePeriod` opens linking it, and the prior period is closed —
  even if the rounded confidence is unchanged

#### Scenario: An unchanged felling set reuses the same Observation and period

- **WHEN** a re-run assigns the identical felling set
- **THEN** the same Observation IRI and the same content-keyed period IRI are produced (a true no-op)

#### Scenario: An unmatched permit is a no-source period

- **WHEN** a permit is assigned no felling in its buurt/window
- **THEN** a `gs:CoveragePeriod` `gs:versionOf` the permit's anchor is assembled with
  `gs:noSourceFound true` and `gs:evidence`, and no `gs:Observation` is minted

### Requirement: Idempotent re-derivation via content-keyed period nodes

Re-running `derive` on unchanged inputs SHALL be a true no-op: the same content-keyed
`gs:CoveragePeriod` IRI is produced, the writer's valid-time-agnostic signature matches, no new period
is written, no prior is closed, and no new run graph or `audit_metrics` change occurs. The period's
content-key SHALL be derived such that it changes **if and only if** the period's graph content
changes — in particular it incorporates the **content-addressed Observation IRI** (which encodes the
assigned felling set), so a changed felling set (a different Observation) produces a new content-key
and a new period, and an identical set produces an identical one. The system SHALL guarantee **exactly
one open period per anchor** after any run (no duplicate open periods): because a genuinely-different
outcome always yields a genuinely-different period IRI, a changed outcome opens a new period and the
writer closes the prior open period of that anchor, retaining history.

#### Scenario: Unchanged re-run is a no-op

- **WHEN** `derive` runs a second time against the same graph + registry
- **THEN** the same period IRI is produced, no new run graph is minted, and no `audit_metrics` row
  changes

#### Scenario: A changed outcome never collides on an existing period IRI

- **WHEN** a permit's assigned felling set changes (even with the same rounded confidence)
- **THEN** the new outcome produces a new content-key/period IRI (via the new Observation IRI), the
  writer opens it and closes the prior, and exactly one open period remains for that anchor — no
  period IRI is written into two run graphs
