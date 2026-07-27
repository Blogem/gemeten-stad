## Context

`derive` (P14) is the gold step of the Phase-1 backbone: the cross-source join that turns loaded
permits + the loaded registry into a **coverage audit** — _"does a matching `kapenherplant`
felling exist for this permit, and how sure are we?"_ It sits on pieces now **all in main**:

- **Spike B** (`spikes/spike-b/`) — no shared key; fuzzy resolution on place + count + time,
  place-led, τ=0.60; finer place disambiguates. Reference scorer `match_rate.py::confidence`.
- **Spike A** — `kapmaatregelDatumUitgevoerd` (felling) is the only trustworthy per-tree date (100%);
  the "permit granted" dates are batch-assigned and unusable.
- **P12 `load/graph.Load`** — SHACL-gated, SCD2 idempotent-upsert writer; ignores
  `gs:validFrom`/`gs:validTo` in change detection; delta-empty run mints no run graph.
- **`state-node-versioning`** (prerequisite change) — extends the writer with **node-form period
  versioning** grouped by `gs:versionOf`: a period node (`gs:validFrom` + `gs:versionOf <anchor>`)
  is one period in a series; the writer keeps exactly one open period per anchor, closing the prior
  when a new one opens. Also drops `gs:validFrom` from `locatedAt` and adds the generic `gs:versionOf`
  term. This is what the coverage period nodes use.
- **P7 `load/bomen`** — `kapenherplant`: `id` + `boomId`, `gbdBuurtId`,
  `kapmaatregelDatumUitgevoerd`, felled count, `resolvedGeom` (Point **4326**). Value-store only.
- **P13 `load/koop`** — besluit `Intervention` (`data:intervention/<zaaknummer>`), `gs:activity
  act:vellen`, `gs:locatedAt` → `data:place/<identificatie>`, `gs:claims` → `data:claim/<zaaknummer>`,
  plus `koop_publications.resolved_geom` (Point **28992**) + `resolved_tier`.

**The modeling gap.** P8 shipped `gs:AuditLink` as a bare class with no attachment or gate. **User
decisions (settled collaboratively):** (1) coverage is a **time-bounded state**, so it is a **node**,
not an RDF-star annotated edge (`docs/RDF_STAR_RELATIONSHIPS.md`); (2) it versions as a stable
**anchor** node plus **one period node per outcome** (content-keyed for no-op re-runs), plain
properties, core-SHACL gate; (3) the matching unit is the **individual felling**, assigned to **at
most one** permit; (4) PostGIS stores **derived numbers only**.

## Goals / Non-Goals

**Goals:**

- Recast `gs:AuditLink` as the coverage **anchor** and add a `gs:CoveragePeriod` period class + shapes
  + `gs:linksObservation`/`gs:granularity`/`gs:noSourceFound`.
- For each besluit `Intervention`, generate candidate fellings, score on proximity + time (+ count),
  **assign each felling to its single best permit exclusively**, and record the outcome as a
  `gs:CoveragePeriod` (matched or no-source) `gs:versionOf` the permit's `gs:AuditLink` anchor.
- Persist derived numbers to a slim `audit_metrics` table.
- Idempotent: unchanged outcome = no-op (same content-keyed period IRI); changed outcome opens a new
  period, closes the prior (via `state-node-versioning`).

**Non-Goals:**

- **No fulfilment / timeliness / permit-count cross-check** — Phase 2.
- **No point geometry in the graph** — proximity scores the link and is recorded via `gs:granularity`;
  geometry stays in PostGIS.
- **No pending/timeliness interpretation** — a recent permit with no felling yet is a plain
  `noSourceFound` period; with no formal deadlines, "pending vs overdue" is a downstream reading.
- **No batch-date clustering**; **no `Assessment`** (Phase-2 fluent); **no `gs:testedAgainst` wiring**
  (coverage lives on the anchor/period; tying the Claim to the current Observation would make the
  Claim evolving — deferred to the Phase-2 fulfilment `Assessment`).

## Decisions

### D1 — Coverage is a stable `gs:AuditLink` anchor + versioned `gs:CoveragePeriod` nodes

Per permit, a write-once **anchor** `data:auditlink/<zaaknummer> a gs:AuditLink ;
gs:coversIntervention data:intervention/<zaaknummer>` ("the coverage audit of this permit"). Each
derive outcome is one **period** `data:auditlink/<zaaknummer>/<content-key> a gs:CoveragePeriod`,
carrying:

- `gs:versionOf` → the anchor — `state-node-versioning`'s writer keeps exactly one open period per
  anchor; and `gs:validFrom` (node triple; `gs:validTo` stamped on close).
- `gs:evidence` (always) — a human-readable summary of what was searched/found.
- **matched:** `gs:linksObservation` → the permit's `Observation` (D2), `gs:confidence` (decimal
  [0,1]), `gs:granularity` (`gs:address`/`gs:postcode`/`gs:buurt`), and `gs:caveat gs:weakLink` below
  τ (plus `countUnknown` etc.).
- **no-source:** `gs:noSourceFound true` (no Observation, no confidence).
- `prov:wasDerivedFrom` the permit + registry rows.

**TBox:** recast `gs:AuditLink` as the anchor; add `gs:CoveragePeriod`, `gs:coversIntervention`,
`gs:linksObservation`, `gs:granularity`, `gs:noSourceFound` (`gs:versionOf` from
`state-node-versioning`). **Shape:** core-SHACL `gs:AuditLinkShape` (anchor) + `gs:CoveragePeriodShape`
(period). _Alternative rejected (annotated edge / same-IRI node / point-at-the-permit anchor):_ an
edge annotation leaves a timeless base triple + needs `sh:sparql`; a single mutated node IRI can't
disambiguate multi-attribute periods; a dedicated anchor node (over reusing the Intervention) is the
cleaner, standard reification — an explicit "coverage of this permit" thing to point at and describe.

### D2 — The `Observation` is the permit's assigned-felling set

The `gs:linksObservation` range is a `gs:Observation` — the registry fellings assigned to this permit
(D6). Minted `data:observation/<zaaknummer> a gs:Observation` **only when matched** (identity only —
felling ids/count/geometry stay in PostGIS). One per permit, so successive matched periods link the
same Observation IRI. _Alternative rejected:_ an Observation per felling — multiplies edges and loses
the "these fellings together answer this permit" grouping.

### D3 — No-source is a period too (not a marker elsewhere)

An unmatched permit still gets a `gs:CoveragePeriod` — `gs:noSourceFound true` + `gs:evidence`
(_"searched buurt N-xx, window [pub,+3yr]: 0 fellings"_), no Observation. It is a first-class,
provenanced, **versioned** finding: when a felling later appears, a new **matched** period opens and
the writer closes this no-source period (`gs:validTo`), so the transition is retained. This realizes
the user's "record that a match was tried and didn't." _Alternative rejected (a marker on the
Intervention):_ would make the Intervention evolving and split the outcome across subjects.

### D4 — Three outcomes, all periods on the anchor

| Outcome | `gs:CoveragePeriod` |
|---|---|
| best score **≥ τ (0.60)** | `gs:linksObservation` + `gs:confidence` + `gs:granularity` (+ caveats) |
| best score **< τ, ≥1 assigned felling** | same, plus `gs:caveat gs:weakLink` |
| **0 assigned fellings** | `gs:noSourceFound true` |

"No matching source found" is reserved for a genuine **absence of any assigned felling**, not a
weak-but-present signal.

### D5 — Scoring: proximity place + publication→felling time + registry count + ambiguity

Adapts `spike-b/match_rate.py::confidence` to the per-felling signal:

```
place : 0.50 buurt floor, graduated UP by proximity of the permit's resolved point
        (koop_publications.resolved_geom) to the felling geom (kapenherplant.resolvedGeom):
        address-tier proximate -> highest (gs:address), postcode/mid -> (gs:postcode),
        buurt-only / beyond-range -> 0.50 (gs:buurt)
count : exact +0.30 · ±15% +0.15 · unknown +0 · incompatible −0.10   # REGISTRY count only
time  : felling ≤2yr after publication +0.15 · 2–3yr +0.05 · felling BEFORE publication ⇒ excluded
ambiguity : sole/clean +0.05 · contested −0.03·(n−1) floored −0.15
clamp [0,1], round 2dp ; assert ≥ τ=0.60
```

Time anchors on `kapmaatregelDatumUitgevoerd` (felling) and the besluit publication date
(`dcterms:available`) — both reliable, neither batch-assigned. Count is `unknown` (+0) for ~all
Phase-1 permits (no extraction) → `countUnknown` caveat; place+time carry the score. Buurt floor held
fixed at 0.50 so a point-less permit matches Spike B's buurt-level calibration; proximity radii + place
values are calibrated in implementation against the spike's labeled cases (golden fixtures pin both).

### D6 — Candidate generation (buurt+time SQL) then exclusive per-felling assignment

**(1) Candidates.** Per Intervention, read its buurt code (`gs:locatedAt` → `data:place/<code>` =
`gbdBuurtId`), then `SELECT id, boomId, "kapmaatregelDatumUitgevoerd", "resolvedGeom" FROM
kapenherplant WHERE "gbdBuurtId"=$1 AND "kapmaatregelDatumUitgevoerd" BETWEEN $2 AND $3` (felled rows).
Buurt+time-scoped for recall. Per candidate, metric distance
`ST_Distance(ST_Transform(k."resolvedGeom",28992), $point)` (registry 4326 → 28992). Point-less
permits skip distance and score at the 0.50 floor.

**(2) Exclusive assignment.** Across all permits' candidate pairs, **each felling is assigned to the
single permit whose (permit↔felling) score is highest** (deterministic tie-break: distance, then
zaaknummer). A permit accumulates its assigned fellings → its `Observation`; its period confidence is
scored over that set; the `ambiguity` term reflects contention. Greedy best-score, not global optimal
— adequate for the corpus. _Alternative rejected:_ scoring permits independently — would let one
felling satisfy several permits, violating exclusivity.

### D7 — SCD2 via content-keyed period nodes (delegated to `state-node-versioning`)

The period IRI is `data:auditlink/<zaaknummer>/<content-key>`, where **content-key** is a hash of the
outcome — matched/no-source, the Observation IRI, the rounded confidence, granularity, and sorted
caveats — **excluding** `gs:validFrom`. So:

- **Unchanged outcome** → same content-key → same IRI → the writer's valid-time-agnostic signature
  matches → a true no-op (original `gs:validFrom` retained).
- **Changed outcome** → new content-key → new IRI → a **new** period; the writer (per
  `state-node-versioning`) closes the prior open period of this anchor by stamping its `gs:validTo` =
  the new period's `gs:validFrom`. History retained; exactly one open period per anchor.

The anchor is write-once (unchanged after first run). The derive stage owns the hash (a canonical,
fully-specified outcome tuple); it emits the anchor + the current period, and the writer opens/closes.

### D8 — PostGIS `audit_metrics`: derived numbers only, not the link

The link lives in the graph (D1). PostGIS stores only value-store numbers: table `audit_metrics`,
schema-qualified, staging + upsert + `--reset` drop, keyed by zaaknummer — `matched bool`,
`assigned_felling_ids text[]` (Observation membership — needed to aggregate felling attributes),
`assigned_felling_count int`, `candidate_count int`, `nearest_dist_m double precision`, `run_id text`.
**No** confidence/granularity/caveat column (in the graph) and **no** geometry (in `kapenherplant`).
The row holds the current verdict's numbers; prior outcomes are in the graph period history.

### D9 — Wiring: a `derive` registry mirroring `loadRegistry`

Replace the `derive` no-op stub with `deriveRegistry []deriveSource`, first entry
`{name:"coverage", fn: runCoverageDerive}`, resolved like `loadRegistry`.

### D10 — The besluit publication date lives in the graph as `dct:available`

The derive windows fellings against the permit's publication date. P13 landed that date only in
`koop_publications.available`, not in the graph. Rather than reading it from PostGIS (the leanness
shortcut), we put it **in the graph** on the `gs:Intervention` as `dct:available "…"^^xsd:date`,
because that is what best practice dictates: a government act's publication date is **descriptive
metadata of a first-class resource**, which the standards ecosystem (Dublin Core, DCAT, PROV) models
as an RDF literal — not a quantitative value to push into a side store. `docs/RDF_MODELING.md` §1's
"no literals" rule is scoped to its real intent (numbers + geometry); descriptive dates via `dct:`
join `gs:confidence`/`gs:evidence` and `gs:validFrom`/`gs:validTo` as legitimate graph literals.
Reuse-first is satisfied (`dct:` already declared; no minted term). The teachable line: the
**Intervention** (first-class resource) carries its descriptive dates in the graph; the individual
registry **fellings** are bulk observation values behind an identity-only `Observation` node, so
their per-tree dates/counts/geometry stay in PostGIS.

Consequences: (1) `load/koop/graph.go` emits `dct:available` (100% populated for audited besluiten,
verified against the dev corpus — so the shape can require it); (2) `InterventionShape` gates it
(`sh:minCount 1`, `sh:maxCount 1`, `sh:datatype xsd:date`); (3) the derive **enumerates Interventions
from the graph** (SPARQL, reading `dct:available` + `gs:locatedAt`) and joins PostGIS only for the
buurt code + resolved point/tier values. This also settles Open Question "keyless besluiten" (D6/6.3):
the audit iterates exactly the Interventions the graph holds, and P13 emits none for keyless/unresolved
publications. _Alternative rejected (read the date from PostGIS):_ leaves the graph's descriptive
metadata incomplete and splits the Intervention's facts across two stores against best practice.

## Risks / Trade-offs

- **[Hard dep on `state-node-versioning`]** The period nodes cannot version without the node-form
  series writer. → Sequence `state-node-versioning` first; derive's integration test needs it merged.
- **[Content-hash correctness]** A bad hash collides (misses a change) or is unstable (churns
  versions). → Hash a canonical, fully-specified outcome tuple; unit-test it (same outcome → same key;
  each distinct outcome → distinct key).
- **[Greedy assignment sub-optimal under contention]** → ~3 candidates/hit is modest; proximity
  disambiguates; `ambiguity` + `weakLink` surface it; `candidate_count` stored. Revisit with optimal
  matching only if measured contention warrants.
- **[Count unknown for ~all Phase-1 permits]** → by design; proximity + the Phase-2 count promote weak
  links. Report the confidence distribution vs Spike B, split by resolution tier.
- **[SRID mismatch]** registry `resolvedGeom` 4326 vs permit 28992 → transform to 28992 (metric); pin
  a known-distance integration test.

## Migration Plan

Additive. Ontology gains `gs:CoveragePeriod`/`gs:coversIntervention`/`gs:linksObservation`/
`gs:granularity`/`gs:noSourceFound` + the two shapes (embed tests extended). New `derive/coverage`
package + a `deriveRegistry` entry replacing the stub. New `audit_metrics` table on first run;
`--reset` drops it. Rollback = revert the registry entry + table; ontology additions are
backward-compatible. Integration tests against the isolated Fuseki dataset + Postgres schema, never
production names, and require `state-node-versioning` merged.

## Open Questions

- **Proximity radii + per-tier place values (D5):** buurt floor fixed at 0.50; the address/postcode
  bands + values are calibrated at implementation against the spike's labeled cases (golden fixtures
  pin them). Widen with a hand-labeled Noord sample if the spike cases are too few.
- **Keyless besluiten:** P13 marks a publication with no sidecar as keyless (zaaknummer `""`).
  Resolved by D10: the audit enumerates exactly the Interventions the graph holds, and P13 emits none
  for keyless/unresolved publications (verify at apply time — task 6.3).
