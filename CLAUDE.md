# Gemeten Stad — project instructions

De Gemeten Stad audits Amsterdam government interventions (starting with the tree
felling/replanting obligation) against public observation data, with a grounded,
uncertainty-aware agent. Read the design docs before building:

- `docs/VISION.md` — the overarching idea (auditing the city).
- `docs/IMPLEMENTATION_PLAN.md` — vertical 1: the tree lifecycle in stadsdeel Noord.
- `docs/DATA_SOURCES.md` — source catalog, working queries, and data quirks.
- `docs/DATA_THREAD_TREES.md` — a live, worked end-to-end example.

## Build lean — no speculative flexibility

This is a greenfield project — but not a throwaway POC. We are building a real
production system that will be the base for further city audits. Greenfield means no
backwards-compatibility burden yet, not low stakes: build only what the current step
needs, and build it to last.

Leanness governs **software and infrastructure** choices. It is deliberately **not**
applied to the semantic model — see "RDF modeling — best practice over leanness" below.

- **No backwards-compatibility.** Nothing is released; there are no old callers to keep
  working. Change code in place; delete freely. Do not add "v2" alongside "v1".
- **No optional/fallback paths "just in case."** Every unused fallback, every
  `if legacy…`, every configurable-but-always-the-same knob is a distraction that makes the
  code harder to read *while building*. Add an alternative path only when a real, present need
  requires it — then it earns its place. (Decided example: locations resolve against the local
  BAG only — there is deliberately **no** PDOK Locatieserver fallback.)
- **Delete rather than deprecate.** Dead code, commented-out blocks, and "might need later"
  scaffolding get removed, not parked.
- Prefer the smallest thing that works now over the general thing that might help later. When
  a generalization is genuinely warranted, the design docs call it out explicitly — otherwise
  don't pre-build it.

## RDF modeling — best practice over leanness

The RDF graph is the long-lived core of this project, and the maintainer is using it to
learn semantic-web modeling. So modeling decisions are held to a **higher** standard than
the surrounding code: follow established best practice even when a lazier shortcut would
"work". When leanness and modeling correctness conflict, **correctness wins** — prefer the
standards-compliant model and surface the trade-off rather than silently cutting the corner.

`docs/RDF_MODELING.md` is the full guide (namespaces, IRI policy, SKOS, uncertainty and
temporal conventions, SHACL, and a per-change checklist); the rules below are the summary.

Concrete rules:

- **These rules can be challenged — and should be, when best practice disagrees.** Neither
  this list nor the repo's existing model is above established RDF best practice. Learning to
  do RDF *correctly* is a core pillar of this project (the maintainer is learning it now), so
  if a recognized standard or best practice points the other way, **say so** — name the
  standard, explain the divergence, and raise it for a decision. Do not silently follow a
  local convention you believe is wrong, and do not silently deviate from one either; surface
  the tension. A well-grounded challenge is welcome, not friction.
- **Reuse before minting.** The model already builds on RDFS, OWL, SKOS, PROV-O, Dublin
  Core Terms (`dct:`) and SHACL, with XSD datatypes and RDF-star / RDF 1.2 annotations.
  Prefer one of these (or another established vocabulary) over a custom term; mint in the
  `gs:` namespace only when nothing standard fits — and record why. This list is **not
  closed**: as new domains arrive, adopt further standard vocabularies under the same
  reuse-first rule (a TOOI / IMBOR / Soortenregister `skos:exactMatch` alignment is already
  earmarked). Geometry is the deliberate exception — it lives in PostGIS, so **no GeoSPARQL
  in the graph**.
- **IRIs are permanent identifiers.** Fix a stable namespace and minting scheme up front;
  never rename or repurpose an IRI to mean something new. Prefer opaque/stable local names
  over ones that encode facts that can change.
- **Naming conventions.** Classes `UpperCamelCase`, properties `lowerCamelCase`.
- **Every term is documented.** Give each minted term an `rdfs:label` and a human definition
  (`rdfs:comment` / `skos:definition`). Declare `rdfs:domain`/`rdfs:range` and
  `subClassOf`/`subPropertyOf` only where they genuinely hold — don't over-constrain.
- **Use OWL/RDFS for their real semantics.** In particular, never use `owl:sameAs` to mean
  "related to" or "roughly equal"; it asserts identity.
- **One provenance/confidence pattern.** Follow the conventions already in the repo (PROV-O +
  RDF-star / RDF 1.2 reification, and the settled `gs:auditLink` modeling); do not invent
  parallel patterns for the same job.
- **Values stay out of RDF.** Numbers and geometry live in PostGIS (the hybrid store); the
  graph holds meaning, links, and provenance.
- **Validate the shape.** A modeling change isn't done until it passes SHACL validation.
- **Explain non-trivial modeling decisions.** Note which standard was chosen, which
  alternatives were rejected, and the trade-off — both for provenance and because learning
  the *why* is an explicit goal here.

## Stack & shape (detail in `docs/IMPLEMENTATION_PLAN.md`)

- **Go** where possible; **Python** only for spaCy NER; **TypeScript + SvelteKit** for the
  frontend.
- **Hybrid store:** a thin **RDF graph** (interventions, claims, links + provenance +
  confidence via RDF-star) + a **Postgres/PostGIS** value store (numbers + geometry). Values
  never live in RDF.
- **Pipeline = raw → conformed → derived** (bronze/silver/gold). Every stage idempotent and
  incremental; the expensive NER output is cached durably; a `dump` tool snapshots
  graph + PostGIS + NER cache.
- **Claims are tracked bitemporally** — assessments evolve as data updates; supersede with
  valid-time intervals, never overwrite history.
- **Dev:** `docker compose`. **Production target:** k3s (a later, dedicated phase).
- **Server** is layered controller → service → repository, and is deployed separately from the
  batch pipeline.
- **Integration tests** run against a separate triplestore namespace and Postgres
  database/schema, with a guard against the production names — never pollute working data.
