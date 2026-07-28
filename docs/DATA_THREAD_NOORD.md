# Data thread: Noord coverage audit — a worked example from the built pipeline

The Noord counterpart to `DATA_THREAD_TREES.md`. Where that thread demonstrated the
*mechanics* by hand in the E-buurt (Zuidoost), this one walks a single permit through the
**actual Phase-1 pipeline** — `load geo → load graph → load koop → load bomen → derive
coverage` — on the real stadsdeel-Noord corpus, and shows the graph it produced.
**Every value below was produced by the pipeline and read live on 2026-07-28** (dev stores);
the queries are the same `GRAPH ?g { … }` SPARQL and PostGIS SQL the pipeline itself uses.

The point of the thread is the platform's central loop made concrete: **a government
intervention (a felling permit) makes an implicit claim; the tree registry records what was
actually felled; the graph ties the two together per place, with a confidence and its
evidence — and surfaces what it could *not* match.**

## The example

**Permit `Z2022-N001404`** — an omgevingsvergunning *"vellen van een houtopstand (kap)"*
at **Wognumerstraat 9, 1023 CB**, in the buurt **Tuindorp Nieuwendam-Oost**
(gebieden id `03630980000453` ↔ code `NN02`). Aanvraag `gmb-2022-265980` (2022-06-13),
besluit `gmb-2022-327243` (2022-07-18).

---

## The thread, hop by hop

### Hop 1 — the unstructured trail (KOOP bekendmakingen → `load koop`)

`ingest koop` harvested the Noord besluit via SRU; `load koop` parsed it into a permit
row and minted its graph entity. The besluit's abstract is the whole claim:

> *"Aanvraag omgevingsvergunning vellen van een houtopstand (kap) Wognumerstraat 9 1023CB
> Amsterdam"*

That yields a **case id** (`Z2022-N001404`, minted as
`data:intervention/Z2022-N001404`), an **activity** (`vellen`, mapped to `gs:activity
act:vellen` — Spike C settled that *vellen* subsumes *verplanten*), and a
**BAG-addressable location** (Wognumerstraat 9). Unlike the E-buurt permit, the abstract
carries **no tree count** — kap besluiten in this corpus name addresses, not numbers, so
the audit's "how many" comes entirely from the registry side (Hop 3).

### Hop 2 — location resolution (`load koop`, against the bulk BAG)

`load koop` resolved "Wognumerstraat 9, 1023 CB" against the locally-loaded BAG (no PDOK
Locatieserver) to a point, then placed that point in a gebieden buurt polygon:

| Field | Value |
|---|---|
| `resolved_tier` | `address` (a full BAG address hit — the strongest tier) |
| `resolved_identificatie` | `03630980000453` (buurt Tuindorp Nieuwendam-Oost) |
| `resolved_confidence` | `0.90` |
| `in_noord` | `true` (buurt code `NN02` → kept in scope) |

The resolved point becomes the anchor for Hop 3's spatial search. (A determinism note: the
resolver's BAG/point-in-polygon lookups were made order-stable during P15 — see
`PHASE_1_PLAN.md` P15 — so re-running `load koop` always resolves this permit the same way.)

### Hop 3 — the registry, and the candidate net (`load bomen` → `derive`)

`load bomen` loaded the `kapenherplant` registry into PostGIS **and** projected every
**felled** tree into the graph as a `gs:Tree` + `gs:Felling` (`gs:felledOn` the felling
date). `derive` then built this permit's candidate net — the **buurt-∪-200 m** clause over
the **[publication, +3 yr]** window (`[2022-07-18, 2025-07-18]`): felled rows in buurt
`NN02` **or** within 200 m of the permit point. 17 candidate fellings fell in that net; the
exclusive best-score assignment awarded this permit **4** of them:

| Felling (`data:felling/…`) | Tree addr | Felled | Distance from permit | Buurt | Replanted |
|---|---|---|---|---|---|
| `4540107` | Wognumerstraat 13 | 2025-01-24 | 18.2 m | `NN02` (same) | 2026-03-09 |
| `4540108` | Wognumerstraat 13 | 2025-01-24 | 22.9 m | `NN02` (same) | 2026-03-09 |
| `4553239` | Monnikendammerweg 19 | 2023-01-09 | 154.0 m | `NN02` (same) | **— none** |
| `4540066` | Nieuwendammerdijk 536 | 2025-02-11 | 167.4 m | `03630980000428` (**neighbour**) | 2026-02-26 |

The last row is the **buurt-∪-200 m union earning its place**: felling `4540066` sits in the
*adjacent* buurt, so the buurt-equals clause alone would have missed it — the additive 200 m
spatial clause (167 m ≤ 200 m) pulled it in. The two Wognumerstraat 13 trees, 18–23 m from
the permit address, are the tight core of the match.

### Hop 4 — the coverage audit (`derive` → the graph)

`derive` scored the assigned set (address-tier place, felled-count, time-lag, uncontested)
at **0.90** — a strong link (≥ τ = 0.60, no `weakLink` caveat) — and wrote the outcome as
the settled `gs:AuditLink` anchor + versioned period, with a **content-addressed
`gs:Observation`** naming its exact felling set:

```turtle
data:auditlink/Z2022-N001404              # write-once anchor
    a gs:AuditLink ; gs:coversIntervention data:intervention/Z2022-N001404 .

data:observation/Z2022-N001404/17474f670cfc000a          # content key = hash of the felling set
    a gs:Observation ;
    gs:includesFelling data:felling/4540066, data:felling/4540107,
                       data:felling/4540108, data:felling/4553239 .

data:auditlink/Z2022-N001404/449b65d4fa728e53            # the current coverage period
    a gs:CoveragePeriod ;
    gs:versionOf        data:auditlink/Z2022-N001404 ;
    gs:linksObservation data:observation/Z2022-N001404/17474f670cfc000a ;
    gs:confidence       0.90 ;
    gs:granularity      gs:address ;
    gs:evidence         "matched 4 felling(s) in buurt NN02, score 0.90" ;
    gs:validFrom        "2026-07-28"^^xsd:date .
```

Because the Observation IRI is a hash of its felling set, re-running `derive` on the same
assignment reproduces the **same** period IRI (a true no-op); a *changed* assigned set would
mint a new Observation and open a new period, closing this one with `gs:validTo` — one open
period per anchor, history retained.

### Hop 5 — the grounded insight (what the agent would emit)

> **The 2022 kap permit for Wognumerstraat 9 (Tuindorp Nieuwendam-Oost) is covered by 4
> registry fellings, at 0.90 confidence** [`data:auditlink/Z2022-N001404`]. Two are the trees
> immediately at the address (Wognumerstraat 13, felled 2025-01-24, ~18–23 m); one is 154 m
> away in the same buurt (Monnikendammerweg 19, felled 2023-01-09); and one is 167 m away
> across the buurt boundary (Nieuwendammerdijk 536, felled 2025-02-11), matched by spatial
> proximity, not buurt membership. The link rests on **place + time**, not a shared identifier
> — the besluit names no felling date or count — so it is recorded as a scored hypothesis with
> its evidence, not a certainty. **Replanting: 3 of the 4 trees have been replanted
> (2026-02/03); Monnikendammerweg 19, felled 2023-01-09, has none after 3.5 years — an
> outstanding herplantplicht obligation** [`kapenherplant`].

Every clause resolves to a specific IRI or query. The confidence is per link; the
cross-boundary felling and the place-not-key basis are stated, not hidden.

---

## What this thread proves and teaches

- **The deterministic backbone runs end to end on the real corpus.** KOOP besluit →
  BAG-resolved place → registry fellings → a content-addressed `gs:AuditLink` with
  confidence and evidence, all from `load`/`derive`, no manual steps.
- **The audit is a scored hypothesis, and says so.** The besluit carries no count or felling
  date, so the match is place + time proximity; the graph records `gs:confidence` +
  `gs:evidence` per link rather than asserting a certain identity. This is the auditable-agent
  discipline the E-buurt thread argued for, now enforced by the model.
- **The buurt-∪-200 m candidate net is doing real work.** One of the four fellings lives in
  the neighbouring buurt and is caught only by the additive spatial clause — recall a
  buurt-equals join alone would have lost.
- **Replant is the Phase-2 story, and the data is already there.** 3 of 4 trees replanted,
  1 pending after 3.5 years — exactly the herplantplicht fulfilment the deferred Phase-2
  replant layer (`gs:Replanting`, `dct:isReplacedBy`, a fulfilment `Assessment`) will audit;
  Phase 1 stops at "which trees does this permit cover?".
- **Honest limits worth stating.** This permit resolved cleanly at *address* tier; the
  corpus-wide picture is coarser — postcode-tier permits match far less often, and the
  exclusive per-felling assignment means many permits with candidates still end up
  no-source (see `PHASE_1_PLAN.md` P15 for the 92.3% candidate-existence vs 42.6%
  exclusive-match split). One clean thread is a demonstration, not the aggregate.
