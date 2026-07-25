# De Gemeten Stad — auditing the city with grounded, uncertainty-aware agents

## The idea

Government continuously intervenes in public space, and it is legally required to publish
every intervention in an official channel: felling permits, traffic decisions, event
permits, zoning changes, transition plans. Each intervention carries a **claim** — an
obligation, an expected effect, a deadline — that *should* be visible in independent data:
registries, statistics, sensor feeds. Yet almost no one checks the claims against the data
systematically, because the two live in different worlds (prose vs. tables) and speak
different location languages.

De Gemeten Stad ties them together: it extracts interventions and their claims from the
official document channel, tests them against measured observations, and answers questions
with an agent that **grounds every statement in a specific document span or query result —
and says, out loud, where it is not sure and how sure it is.**

## Why it is possible now

The Netherlands publishes the raw material at every level of an official aggregation ladder
(BAG address → postcode-6 → buurt → wijk → stadsdeel → gemeente):

- **The document channel** — officiële bekendmakingen (KOOP), a continuous, queryable feed
  of every municipal decision.
- **Per-source registries and statistics** — municipal asset systems (trees, parking),
  national registers (energy labels), CBS and police figures, air-quality sensors.
- **The place backbone** — BAG (every address/building) and the CBS/gebieden geometries,
  which let otherwise-unrelated sources be joined on location.

The catalog of sources, with working queries and their quirks, is in `DATA_SOURCES.md`.

## The general model

```
   INTERVENTION            CLAIM                         OBSERVATION
   (a published decision)  (obligation / expected        (independent measured data,
        │                   effect + a "when")            at its native ladder level)
        │  the claim's source varies:                          ▲
        │   • from LAW      (e.g. a felling ⇒ herplantplicht)   │ testedAgainst
        │   • from the DOC  (e.g. "occupancy 65% exceeded")     │
        ▼                                                       │
   PLACE  ◀────────────── resolve both sides to the ladder ─────┘
   (BAG / postcode / buurt / project area)

   Everything asserted carries PROVENANCE (which span, which query) and,
   where the link is fuzzy, a CONFIDENCE (how sure, and on what evidence).
```

Two things this model insists on, learned from real data:

1. **The claim is not always stated in the document.** Sometimes it is only *implied by law*
   (a granted felling permit triggers a statutory replant obligation); sometimes it is
   buried in prose (a quantitative rule in a traffic decision); sometimes it is a controlled
   metadata field. The platform must handle all three, and treat "the ground/why is absent"
   as normal, not as a failure.
2. **Sources do not share location keys.** A permit names a (often approximate or
   since-demolished) address; a registry names the nearest address, a buurt, and a point.
   Tying them is **entity resolution**, not a join — inherently fuzzy, so links carry a
   confidence and the finest granularity that could be established.

## The trust properties (why the audit is credible)

The audit gets its independence from using sources with *different* trust characteristics —
for the tree vertical, a triangle:

- **the document channel** (external proof an intervention was authorised),
- **the operational registry** (the city's own per-item obligation + fulfilment record),
- **the aggregate self-report** (what the city tells its council),

with **the law as the external norm**. Comparing these three catches under-reporting,
unfulfilled obligations, and figures that don't reconcile — none of which a single source
can reveal about itself.

## How it is built

A hybrid store and a source-oriented pipeline (detailed in `IMPLEMENTATION_PLAN.md`):

- **Thin RDF graph** for the linkable, provenanced, uncertain things — interventions,
  claims, places, projects, actors, the relations between them, and per-assertion
  provenance + confidence (RDF-star). The graph is where heterogeneity and fuzzy links
  live.
- **Postgres + PostGIS value store** for numbers and geometry, reached through a locator; it
  does the joins, spatial work, and aggregation. Values never live in RDF.
- **Ingest per source → extract (only for unstructured) → load (resolve + assemble) →
  derive (cross-source audits, stored) → serve (a map + a grounded chat).**

## The path

Prove it on one **vertical slice** first — the Amsterdam tree felling/replanting obligation,
in one stadsdeel (**Noord**), for one backdated timeframe — end to end, including honest
"indeterminate" verdicts where data is missing. Once that holds, add further intervention
types (traffic decisions, event permits, …) that reuse the same machinery, and pursue data
the city does not publish (e.g. the herplantfonds balance) via a WOO request once the rest
demonstrably works. Each new source should require new *content*, not new engine.
