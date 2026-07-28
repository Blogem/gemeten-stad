# Modeling in RDF-star: statements vs things, edges vs nodes, and time

A decision guide for *how to represent a fact or relationship in the graph* — as an RDF-star
annotated edge, or as its own node — starting from **what RDF-star is actually for**, and, crucially,
**how to handle relationships that change over time**. Written up after the P14 (coverage audit) and
the felled-tree modeling surfaced the question; it governs every fuzzy or evolving relationship in the
model (`gs:locatedAt`, `gs:AuditLink`, `gs:Assessment`, `gs:Felling`/`gs:Replanting`,
`gs:LegalStatusPeriod`, …).

## The one fact that drives everything

**An asserted RDF triple has no time.** Once `:permit :locatedAt :placeA` is in the graph, it is
true *forever* — RDF has no built-in notion of "this triple stopped holding." You cannot
"un-assert" it. Every temporal pattern below is a way of working with, or around, that fact.

RDF-star / RDF 1.2 does **not** change this. It lets you attach statements *about* a triple; it does
not make the triple itself time-bounded.

## RDF 1.2 in one paragraph (so the terms are clear)

In RDF 1.2 the annotation syntax `:s :p :o {| :k :v |}` is sugar for two things: (1) it **asserts**
the base triple `:s :p :o`, and (2) it mints a **reifier** — `_:r rdf:reifies <<( :s :p :o )>>` —
and hangs `:k :v` on `_:r`. A `<<( … )>>` **triple term** merely *names* a triple (it asserts
nothing). Two important consequences:

- Because each `{| … |}` mints its **own** reifier, you can have **several** independent statements
  about the same triple (e.g. one provenance record per run). Good for annotation *history*.
- The reifier is typically a blank node bound to one specific triple; it is **not** a stable,
  referenceable identity for "the relationship." If you need to point at, gate, or evolve the
  relationship itself, that wants a real node.

## RDF-star is *statement*-metadata, not property-graph edge-properties

If you come from property graphs (Neo4j and friends), RDF-star *looks* like "put properties on an
edge" — and that reading is the single most common way to misuse it. `<< :s :p :o >> :k :v` does
**not** mean "the edge `:p` has attribute `:k`." It means **"the *statement* `:s :p :o` has property
`:k = :v`."** The subject is the *assertion itself*.

So the acid test for whether RDF-star is the right tool is one question:

> **Is the value a property of the *statement I'm making*, or a property of a *thing in the world*?**

- Property of the **statement** → RDF-star. How sure am I this holds (**confidence**), who said it
  (**provenance**), which run recorded it (**transaction-time**), what's the evidence. All metadata
  about *my assertion's* reliability or origin.
- Property of a **thing or event in the world** → a **node** (see n-ary, below). It has its own
  identity and usually several attributes of its own.

**Worked example, both ways, from this model:**

- `<< intervention gs:locatedAt place >> gs:confidence 0.7 ; gs:caveat gs:unresolvedLocation` —
  *correct* RDF-star. `0.7` is genuinely a property of the *statement*: "I'm 70% sure this permit
  sits at this place." Get better data, refine the confidence. It's about *my knowledge*, not the
  world.
- A tree's **replant date** — *wrong* for RDF-star. `<< treeA gs:replantedBy treeB >>
  gs:plantedOn "2025-01-24"` literally asserts "the *statement* 'A was replanted by B' was planted on
  2025-01-24" — but the statement wasn't planted on a date; the **tree** was. The date is a property
  of a real-world **event** (the planting), so it belongs on a node: `data:replanting/… a
  gs:Replanting ; gs:plantedTree treeB ; gs:plantedOn "2025-01-24" ; gs:replaces <the felling>`. A
  category error that happens to be syntactically legal.

### …which is exactly the n-ary-relations rule

An older principle lands in the same place: **a relationship that has its own attributes, or more than
two participants, should be a node** (W3C *Defining N-ary Relations*). A "replacement" that happened
*on a date*, involves a *new tree*, and *discharges an obligation* is not one edge plus one annotation
— it is a first-class event with several facts hanging together, and (fatally) nothing you can
*address*: with a bare annotation you can't say "this replanting fulfils that obligation" or "list
2024's replantings," and a felled-but-not-yet-replanted tree has no edge to annotate at all. So the
n-ary rule and the acid test agree: reify it as a `gs:Replanting` node.

RDF-star does **not** replace n-ary reification — the two are orthogonal. RDF-star answers "what do I
want to say *about this statement*?"; n-ary nodes answer "what other *things and events* exist in the
domain?" Reach for RDF-star only for the first. Everything below assumes you've already passed this
test — i.e. the thing genuinely *is* statement-metadata; now the only question is whether it's
refinable (transaction-time) or truly evolving (valid-time).

## Two flavours of "evolving" — they pull opposite ways

Before reaching for a mechanism, decide which of these you actually have:

**Flavour 1 — the fact holds; only its *metadata* is refined.**
The base triple is genuinely, timelessly true; what changes is *how sure we are* or *where we heard
it* (confidence, evidence, source, run). → **RDF-star annotated edge.** This is exactly what the
annotation is for. A better resolution later is a *correction*, not a new world-state; keep it as
transaction-time (see below), not valid-time.

**Flavour 2 — the *relationship itself* starts, ends, or flips between states over time.**
The link comes into or goes out of existence, or switches between mutually-exclusive states, or the
same two endpoints are linked / unlinked / relinked as distinct episodes. → **a node.** Because the
asserted base triple can't be un-asserted, annotating it is fragile: a plain query returns *every*
episode with no notion of "current," and you must always read through reifiers + a "no valid-to"
filter. Giving the relationship **its own identity (a node)** with `validFrom`/`validTo` makes
"current vs historical" a trivial, unambiguous query. This is the classic n-ary-relation / fluent /
state-period pattern (a.k.a. SCD Type-2 in data-warehouse terms), and RDF 1.2 does not displace it.

## Three categories, one rule

Every relationship/fact in the model is one of:

| Category | Mechanism | Time | Examples |
|---|---|---|---|
| **Immutable fact** | plain triple, write-once | none (timeless) | identity, `gs:activity`, publication date |
| **Refinable metadata edge** | RDF-star annotated edge | *transaction-time* only (which run wrote it) | `gs:locatedAt` (+ `gs:confidence`/`gs:caveat`) |
| **Time-bounded state/relationship** | its own **node**, `validFrom`/`validTo` | *valid-time* (open/close, history retained) | `gs:AuditLink`, `gs:Assessment`, `gs:LegalStatusPeriod` |

**Rule of thumb:** *If the only thing that changes is a confidence or a source, annotate the edge. If
the relationship’s existence or state changes over time, make it a node.*

### Two nudges that reinforce the rule

- **SHACL.** Core SHACL cannot traverse into an RDF-star annotation — gating an annotated value
  forces a `sh:sparql`/SPARQL-star rule (see `ontology/shapes.ttl`'s `locatedAt` gate). A node's
  attributes are plain triples, gated with ordinary `sh:minCount`/`sh:datatype`. So anything whose
  structure you need to **enforce** is easier as a node.
- **Valid-time vs transaction-time.** These are different axes. *Valid-time* = when a fact held in
  the world (`validFrom`/`validTo`). *Transaction-time* = when **we** recorded it — which we already
  get for free from PROV: every load/derive run writes into its own run-stamped named graph with
  `prov:generatedAtTime`. A **correction** (we re-resolved a location from better data) is
  transaction-time — let the newer run supersede; you do **not** need valid-time on the edge for it.
  Reserve valid-time (and thus nodes) for things that genuinely change *in the world*.

## How this maps to our model

- **`gs:locatedAt` is Flavour 1.** A permit is at a place; that fact holds. Improving the resolution
  is a *correction* (transaction-time); a genuine relocation is a **new permit entity**
  (`amends`/`supersedes`), so a given permit's location always holds. → annotated edge carrying
  `gs:confidence`/`gs:caveat`, and **no `gs:validFrom`/`gs:validTo`.**
- **Coverage is Flavour 2.** A permit's coverage assessment is time-bounded and flips between
  *no-source-found* and *matched* (and the matched target can change). It also has a state — no-match
  — with **no target edge to annotate at all**. → a stable **anchor** node (`gs:AuditLink`, one per
  permit) plus a **period node** (`gs:CoveragePeriod`) per outcome, each carrying `gs:confidence`/
  `gs:granularity`/state as **plain properties** (core-SHACL gateable) and `gs:validFrom`/
  `gs:validTo`, and pointing at the anchor via `gs:versionOf`.
- **`gs:Assessment`, `gs:LegalStatusPeriod`** are Flavour 2 by construction (bundles of co-moving
  attributes with periods) — already nodes.

## How a state node versions — one node per period, grouped by a series anchor

A single node IRI **cannot** hold a multi-attribute history: flat triples
(`:n gs:confidence 0.5`, `:n gs:confidence 0.7`, two `gs:validFrom`s) can't say which value belongs
to which period. So a Flavour-2 relationship versions as **one distinct node per period**, and the
periods are tied together by a stable **series anchor** they all point at:

- Each period is its own node (`data:auditlink/<zaaknummer>/<key>`), carrying its plain-property
  state + `gs:validFrom`, and a `gs:versionOf <anchor>` pointing at a stable IRI that identifies the
  series. The anchor is a **dedicated per-series node** (for coverage, a `gs:AuditLink` node per
  permit — "the coverage audit of this permit" — that the periods hang off; the anchor itself is
  timeless, only its periods carry valid-time).
- The writer maintains **exactly one open** (no `gs:validTo`) node per `<anchor>`: opening a new
  period **closes** the prior one (stamps its `gs:validTo`). "Current state" = the open node in the
  series; history = the closed ones.
- The period node IRI is **content-derived** (a hash of the outcome, excluding the timestamps) so an
  unchanged re-run mints the same IRI and is a true no-op, while a changed outcome mints a new IRI
  and closes the prior.

## Implication for the graph writer

The SCD2 upsert writer (`load/graph`) versions **at the node/series level**: a node carrying
`gs:validFrom` + `gs:versionOf` is a period in a series; opening a new period closes the prior open
node sharing the same `gs:versionOf` anchor (one open per anchor), history retained. RDF-star
annotated **edges**, by contrast, are *refinable metadata* (transaction-time — the latest run's value
is current; prior runs remain in their run graphs) and are **never** valid-time-versioned. This is
what the `state-node-versioning` change implements, and why `load/koop` stops stamping `gs:validFrom`
on `gs:locatedAt`.

## The trap to avoid

Do **not** put a second evolving thing on a subject that already carries one, hoping RDF-star will
sort it out. Valid-time versioning belongs to the *node that owns the relationship*, so give each
time-bounded relationship its own node and its history stays independent and queryable. Reaching for
"just annotate the edge" because it's less typing is how you end up with timeless base triples and
history you can only reconstruct with fragile queries.

## References

- RDF 1.2 (triple terms + `rdf:reifies`) — the reifier model summarized above.
- W3C *Defining N-ary Relations on the Semantic Web* (note) — the reify-the-relationship pattern.
- `docs/IMPLEMENTATION_PLAN.md` §"Temporal model" — the project's two-mechanism rule (RDF-star
  annotation for a single refinable fact; state/period node for a co-moving bundle) that this
  document expands and justifies.
