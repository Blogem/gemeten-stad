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
