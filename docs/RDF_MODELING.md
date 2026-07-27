# RDF modeling guide

How we model the De Gemeten Stad graph, and the best practices every modeling change follows.

Leanness governs our *software* choices (see `CLAUDE.md` → "Build lean"); it is deliberately **not**
applied to the semantic model. The graph is the long-lived core of the project and a thing the
maintainer is using to *learn* semantic-web modeling, so modeling decisions are held to a higher
standard: follow established best practice even when a lazier shortcut would "work", and when the two
conflict, **correctness wins** — prefer the standards-compliant model and surface the trade-off
rather than silently cutting the corner.

The living model is three files under `ontology/`:

- `ontology.ttl` — the TBox (classes, properties, the uncertainty/temporal/caveat terms).
- `vocab.ttl` — the SKOS domain vocabulary (activity / species / status concepts). **Generated** by
  `ontology/generate_vocab.py`; do not hand-edit.
- `shapes.ttl` — the SHACL shapes that gate every load.

This guide states the *why*; those files are the *what*. When they disagree, the files win and this
guide is wrong — fix it.

**This guide is not above RDF best practice.** Doing RDF *correctly* is a core pillar of the project
(the maintainer is learning it now), so these conventions — and the existing model itself — can and
should be challenged when an established standard or recognized best practice points the other way.
If you spot such a divergence, don't silently conform and don't silently deviate: name the standard,
explain the difference, and raise it for a decision. A well-grounded challenge is welcome.

## 1. The thin graph — what belongs in RDF at all

The graph carries **identity, relations, provenance, and confidence**, plus descriptive-metadata
literals. Every *quantitative* and *geometric* value — tree counts, polygons, replant fractions,
stamdiameter — lives in the Postgres/PostGIS value store, **never** as an RDF literal.

A class or property that looks like it wants a number is a modeling error: it should reference an
entity that resolves to the value store instead. This is the single most important rule; internalize
it before adding anything. Descriptive-metadata dates are a different thing and are legitimate graph
literals: a besluit's publication date is carried on the `gs:Intervention` itself as `dct:available`
(`xsd:date`), reusing Dublin Core Terms rather than minting a `gs:` term. (Alongside it, the other
literal exceptions are structural, not domain values: `gs:confidence`/`gs:evidence` annotations and
`gs:validFrom`/`gs:validTo` timestamps.)

The line to hold onto: a first-class resource — the Intervention itself — carries its own
descriptive dates in the graph, because that resource's identity and provenance are exactly what the
graph is for. Bulk observation *values* — per-tree felling dates, counts, geometry — stay in
PostGIS, reached only through an identity-only `gs:Observation` reference; the graph never grows a
literal that is really data behind an entity it hasn't modeled.

## 2. Namespaces and IRI policy

The RDF base is `gemetenstad.nl` (no hyphen). Three project namespaces:

| Prefix  | IRI                             | Holds                                             |
|---------|---------------------------------|---------------------------------------------------|
| `gs:`   | `http://gemetenstad.nl/ns#`     | terms — the TBox in `ontology.ttl`                |
| `data:` | `http://gemetenstad.nl/id/`     | instance identities                               |
| `run:`  | `http://gemetenstad.nl/run/`    | run-stamped named graphs + provenance             |

**IRIs are permanent identifiers.** Fix the namespace and the minting scheme up front; never rename
or repurpose an IRI to mean something new — a renamed IRI is a different thing, and anything that
referred to the old one now dangles. Prefer opaque/stable local names over ones that encode facts
that can change. Instance IRIs are derived from the authoritative source code (BAG object id,
gebieden code, zaaknummer), so identity rests on codes, never on names. Where a node versions over
time, the period-node IRI is **content-derived** (a hash of the outcome excluding timestamps) so an
unchanged re-run is a true no-op — see §6 and `RDF_STAR_RELATIONSHIPS.md`.

## 3. Reuse established vocabularies before minting

The model builds on these standards; reach for them before inventing a `gs:` term:

| Vocabulary                        | Prefix  | Used for                                                    |
|-----------------------------------|---------|-------------------------------------------------------------|
| RDF / RDF Schema                  | `rdf:` `rdfs:` | typing, `rdfs:label`, `rdfs:comment`, domain/range   |
| OWL                               | `owl:`  | `owl:Class`, `owl:ObjectProperty`, `owl:TransitiveProperty` |
| SKOS                              | `skos:` | the controlled domain vocabulary (§5)                       |
| PROV-O                            | `prov:` | provenance — `prov:wasDerivedFrom`, run activities (§7)     |
| Dublin Core Terms                 | `dct:`  | `dct:description`, `dct:source`                             |
| XML Schema datatypes              | `xsd:`  | `xsd:decimal`, `xsd:string`, `xsd:date`, `xsd:boolean`      |
| SHACL                             | `sh:`   | the load-gate shapes (§8)                                   |
| RDF-star / RDF 1.2                | —       | the `{\| … \|}` annotation form (§6)                        |

Mint a term in the `gs:` namespace only when nothing standard fits — and record why in its
`rdfs:comment`. **The list is not closed**: as the audit moves into new domains (a second
intervention type, new registries), adopt whatever established vocabulary fits under the same
reuse-first rule. A `skos:exactMatch` alignment to the national vocabularies (TOOI / IMBOR /
Soortenregister) is already earmarked for later; when it lands it aligns to those IRIs rather than
re-minting parallel identifiers (local IRIs + `dct:source` in the meantime, per the vocab header).

**Geometry is the deliberate exception.** GeoSPARQL is *not* used: a Place's geometry lives in
PostGIS, and the graph keeps only the resolved membership (`gs:within`) for rollup. Do not add
GeoSPARQL to pull geometry into the graph — that would violate §1.

## 4. Naming and documentation conventions

- **Naming.** Classes `UpperCamelCase` (`gs:Intervention`, `gs:CoveragePeriod`); properties
  `lowerCamelCase` (`gs:locatedAt`, `gs:validFrom`).
- **Every minted term is documented.** Give it an `rdfs:label`@en and an `rdfs:comment` that says
  what it means *and why it exists* (the existing comments cite the design-doc section and the
  real-world rule they encode — match that density; they are how the next reader learns the model).
- **Type declarations carry semantics — use them deliberately.** Declare `rdfs:domain`/`rdfs:range`
  and `rdfs:subClassOf`/`rdfs:subPropertyOf` only where they genuinely hold; don't over-constrain to
  look thorough. `gs:within` is `owl:TransitiveProperty` because containment genuinely is transitive
  and the rollup query relies on it — that is an assertion with consequences, not decoration.
- **Use OWL/RDFS for their real meaning.** In particular never use `owl:sameAs` to mean "related to"
  or "roughly the same"; it asserts identity and an inferencer will merge the two nodes.

## 5. Controlled vocabularies with SKOS

Domain classifications (activity, species, observation status) are **SKOS concepts** in a single
`skos:ConceptScheme` (`data:scheme/tree-audit`), not free-text literals and not `gs:` terms. Rules:

- **Seeded, not invented.** Concepts are generated from the authoritative value store plus
  hand-authored legal terms, each with a `dct:source`. Do not hand-write concepts into `vocab.ttl`;
  extend `generate_vocab.py` and regenerate.
- **Identity is the code/IRI, never the name.** `skos:notation` + the IRI carry identity;
  `skos:prefLabel`/`skos:altLabel` are display and matching aids. Two labels can share a concept
  (e.g. `verplanten` is an `altLabel` of `vellen` because it is a felling by law).
- **Places are not concepts.** A place's meaning is its geometry + admin hierarchy, authoritative in
  PostGIS. Places are `gs:Place` instance nodes keyed by code, not SKOS concepts — a name→code shadow
  in the vocab would add nothing.
- **The shapes enforce membership.** A `gs:activity`/`gs:species`/`gs:status` value must resolve to a
  concept `skos:inScheme` the tree-audit scheme (`shapes.ttl` → `ConceptInSchemeShape`).

## 6. Uncertainty — the RDF-star annotation convention

A fuzzy edge (one we are not certain of) carries its confidence *about the edge*, using the RDF 1.2 /
RDF-star **annotation** form:

```turtle
:i gs:locatedAt :p {| gs:confidence 0.7 ; gs:evidence "place+time match" ; gs:caveat gs:timeMismatch |} .
```

**HARD MODEL RULE — annotation form `{| … |}`, never the bare quoted form `<< … >>`.** The annotation
form both *asserts* the base edge (`:i gs:locatedAt :p`) and mints a reifier to hang the annotation
on, so structural SHACL (a plain property path) still sees the edge and the annotation is readable
via SPARQL-star. The bare `<< … >>` form asserts *nothing* (verified with `riot` in Spike E), so a
structural shape would not see the edge. This is enforced by the confidence-presence `sh:sparql`
constraint in `shapes.ttl`.

Conventions on the annotation:

- `gs:confidence` (`xsd:decimal`, in `[0,1]`) — `1.0` means an exact resolution; **any value `< 1.0`
  must carry a `gs:caveat`** naming why (shapes enforce this).
- `gs:caveat` values are a **fixed, controlled set** of four `gs:Caveat` individuals
  (`gs:unresolvedLocation`, `gs:timeMismatch`, `gs:weakLink`, `gs:transplantOrigin`) — extend the set
  in `ontology.ttl` *and* the shape's `sh:in`, never write an ad-hoc string.
- `gs:evidence` (`xsd:string`) — a short human-readable statement of what the confidence rests on.

## 7. Time and provenance

Two distinct time axes; keep them straight:

- **Valid-time** (`gs:validFrom`/`gs:validTo`) — when a fact held *in the world*. Stamped only on
  **evolving state**, never on immutable facts (a publication date, a felling date are timeless).
  Over-stamping is an anti-pattern — every query then drags time filters.
- **Transaction-time** — when *we* recorded something. Not modeled with `gs:` terms at all: each
  load/derive run writes a run-stamped named graph and a `prov:Activity` with
  `prov:generatedAtTime`. A *correction* (we re-resolved from better data) is transaction-time — let
  the newer run supersede; do **not** reach for valid-time.

**Whether an evolving relationship is an annotated edge or its own node is the central modeling
decision, and it has its own guide: `docs/RDF_STAR_RELATIONSHIPS.md`.** In short — if only the
*metadata* of a timelessly-true fact changes (confidence, source), annotate the edge (§6); if the
relationship itself starts/ends/flips between states, give it a **node** with `validFrom`/`validTo`
(the SCD-Type-2 fluent: `gs:Assessment`, `gs:LegalStatusPeriod`, `gs:CoveragePeriod`). A new period
opens by writing a new node and stamping `validTo` on the prior — history is retained, never
overwritten. Read that doc before modeling anything that changes over time.

Amendments follow the same principle: an amended permit is a **new** decision entity linked to the
prior (`amends`/`supersedes`), never an in-place edit.

## 8. Validation — SHACL is the load gate

No half-broken data enters the graph. The load path (`load/graph`) POSTs the candidate graph plus
`shapes.ttl` to Fuseki's `…/shacl` endpoint; a non-conforming report **fails the load loudly**. A
modeling change is not done until it validates.

Two mechanisms, by necessity:

- **Structure and controlled value sets** — core SHACL (`sh:property`/`sh:path`/`sh:minCount`/
  `sh:class`/`sh:node`). Works on plain triples and on the *base* edge of an annotation.
- **Confidence/caveat on a fuzzy edge** — `sh:sparql` with a SPARQL-star `<< … >>` pattern, because a
  core SHACL path cannot traverse into an annotation (it hangs off the quoted triple, not a node).

Corollary (reinforces §7): anything whose structure you need to *enforce* is easier as a node, whose
attributes are plain triples gated with ordinary `sh:minCount`/`sh:datatype`.

Validation must run against a dataset that also holds `vocab.ttl`, so the controlled-value shapes can
see each concept's `skos:inScheme` triple (the load path loads vocab before gating).

## 9. Checklist for a modeling change

Before a change to `ontology.ttl` / `vocab.ttl` / `shapes.ttl` is done:

- [ ] No number or geometry entered the graph as a literal (§1).
- [ ] Reused a standard vocabulary where one fits; any new `gs:` term is justified in its comment (§3).
- [ ] New terms follow the naming convention and carry `rdfs:label` + a `why`-bearing `rdfs:comment`
      (§4); `domain`/`range`/`subClassOf` asserted only where true.
- [ ] No IRI was renamed or repurposed (§2).
- [ ] Fuzzy edges use the `{\| … \|}` form and carry confidence (+ caveat if `< 1.0`) (§6).
- [ ] Evolving-over-time state was classified edge-vs-node per `RDF_STAR_RELATIONSHIPS.md` (§7).
- [ ] SHACL shapes updated to gate the new structure/values, and the change validates (§8).
- [ ] Vocab changes went through `generate_vocab.py`, not a hand-edit (§5).
- [ ] The non-trivial decision is explained — which standard, what was rejected, the trade-off.

## References

- `ontology/ontology.ttl`, `ontology/vocab.ttl`, `ontology/shapes.ttl` — the living model.
- `docs/RDF_STAR_RELATIONSHIPS.md` — edges vs nodes, and modeling relationships that change over time.
- `docs/IMPLEMENTATION_PLAN.md` §3 — the graph model and the temporal/uncertainty decisions (D2–D7).
- W3C: RDF 1.2, SKOS Reference, PROV-O, SHACL, *Defining N-ary Relations on the Semantic Web*.
