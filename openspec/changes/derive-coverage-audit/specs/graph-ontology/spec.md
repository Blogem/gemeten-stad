## ADDED Requirements

### Requirement: Coverage anchor and coverage-period TBox

The ontology (`ontology/ontology.ttl`, `gs:` namespace) SHALL model the derived permit↔registry
coverage as a stable **anchor** plus time-versioned **period** nodes:

- **`gs:AuditLink`** (the class P8 already declares) SHALL be the stable per-permit **anchor** — "the
  coverage audit of this permit," one per permit, timeless — carrying `gs:coversIntervention`
  (`owl:ObjectProperty`, `gs:AuditLink` → `gs:Intervention`). Its comment SHALL be updated from
  "confidence-bearing link" to this anchor role (the confidence now lives on the periods).
- **`gs:CoveragePeriod`** (new class) SHALL be a dated coverage state: it points at its anchor via
  `gs:versionOf` (added by `state-node-versioning`) and carries `gs:validFrom`/`gs:validTo`,
  `gs:evidence`, and — when matched — `gs:linksObservation` (`owl:ObjectProperty`,
  `gs:CoveragePeriod` → `gs:Observation`), `gs:confidence`, `gs:granularity`
  (`gs:address`/`gs:postcode`/`gs:buurt`), and optional `gs:caveat`; or — when unmatched —
  `gs:noSourceFound` (boolean `true`). `gs:confidence`/`gs:evidence` are **plain properties** on the
  period (not RDF-star annotations).

The ontology SHALL define the new terms `gs:CoveragePeriod`, `gs:coversIntervention`,
`gs:linksObservation`, `gs:granularity`, and `gs:noSourceFound`, each with `rdfs:label` and
`rdfs:comment`. These complete the attachment the P8 TBox left out.

#### Scenario: The coverage terms are present and typed

- **WHEN** `ontology/ontology.ttl` is loaded into Fuseki
- **THEN** `gs:AuditLink` documents the anchor role with `gs:coversIntervention` → `gs:Intervention`;
  `gs:CoveragePeriod` is defined; and `gs:linksObservation` (range `gs:Observation`),
  `gs:granularity`, and `gs:noSourceFound` are defined, each with `rdfs:label` + `rdfs:comment`

#### Scenario: A matched coverage period is a plain-property node

- **WHEN** a matched period is written as `data:auditlink/Z/<key> a gs:CoveragePeriod ; gs:versionOf
  data:auditlink/Z ; gs:validFrom "…" ; gs:linksObservation data:observation/Z ; gs:confidence 0.7 ;
  gs:granularity gs:address ; gs:evidence "…"`, alongside `data:auditlink/Z a gs:AuditLink ;
  gs:coversIntervention data:intervention/Z`
- **THEN** a plain triple query `?p a gs:CoveragePeriod ; gs:confidence ?c` returns `0.7` (no
  SPARQL-star needed) and `?p gs:versionOf ?anchor . ?anchor gs:coversIntervention ?permit` reaches
  the permit

#### Scenario: A no-source coverage period records the attempt

- **WHEN** a period is written as `data:auditlink/Z/<key> a gs:CoveragePeriod ; gs:versionOf
  data:auditlink/Z ; gs:validFrom "…" ; gs:noSourceFound true ; gs:evidence "searched …"`
- **THEN** it is a valid coverage period recording that a match was attempted and none found, with no
  `gs:Observation` and no `gs:linksObservation`
