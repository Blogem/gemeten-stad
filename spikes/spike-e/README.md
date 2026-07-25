# Spike E — which triple store carries the graph?

**Question (Phase 0, `IMPLEMENTATION_PLAN.md` §6; item P2 in `PHASE_0_PLAN.md`).** The graph store is not
a generic SPARQL endpoint — the design (`IMPLEMENTATION_PLAN.md` §3) leans on five load-bearing needs:
RDF-star / SPARQL-star uncertainty on the fuzzy edges, SHACL validation as a load gate, PROV run-stamped
**named graphs** for transaction-time, a truly free licence in a container, and a native dump + isolatable
namespace. `PHASE_0_PLAN.md` narrowed the field to **Jena Fuseki vs GraphDB** and leaned Fuseki; the open
risk it flagged was whether **SHACL can actually validate over RDF-star** and whether named-graph
ergonomics hold up. This spike settles that hands-on (and weighed the alternatives' downsides, not just the
checklist).

**Answer. Apache Jena Fuseki wins — every one of the five needs passes, including the two flagged risks.**
Measured against Fuseki 5.5.0 (`secoresearch/fuseki`, Apache-2.0 Jena) + the Jena 5.1.0 CLI tools:

- **RDF-star:** the `{| … |}` annotation form **asserts the base edge AND stores the annotation**; the bare
  `<< … >>` form asserts only the annotation (verified with `riot`). → the model must write confidence with
  `{| … |}` so structural SHACL still sees the `locatedAt` edge.
- **SPARQL-star:** `<< :i :locatedAt :p >> :confidence ?c` reads the confidence (+ evidence) straight back
  off the edge — the exact server/UI read path.
- **SHACL, both directions:** a well-formed instance conforms; missing-structure and missing-confidence
  instances are rejected — identically via the **Fuseki SHACL endpoint** and the **independent Jena CLI**.
- **The confidence-presence rule needs SPARQL-star:** it is **not** expressible in core SHACL (no property
  path reaches into a quoted triple); a `sh:sparql` constraint with a `<< … >>` `FILTER NOT EXISTS` does
  it, and works over both the CLI and the endpoint. **This is the pattern P8 must use.**
- **Jena #3503 does not bite our pattern:** a graph containing RDF-star *and* a real violation is still
  reported non-conforming, pinpointing the offending node — RDF-star presence does not mask violations.
- **Named graphs + PROV:** loading TriG preserves the run graphs; a cross-graph query joins each run's data
  to its `prov:generatedAtTime`. Data survives `compose down`/`up` on the named volume (what P9's dump
  round-trips).

verified 2026-07-25 · Reproduce: `./run.sh --reset` (needs Docker; brings up the stack, runs 13 checks,
prints a PASS/FAIL gate).

---

## Scope established (reusable by Phase 1)

- **The store is Fuseki.** `compose.yaml` here is the service definition **P3 lifts** into `deploy/compose/`
  (Fuseki + a named volume + a wget healthcheck; the CLI sidecar is spike-only).
- **The confidence pattern is settled for P8:** write the fuzzy edge as `:i :locatedAt :p {| :confidence x ;
  :evidence "…" |}`; enforce presence with the `sh:sparql` SPARQL-star constraint in `shapes/shapes.ttl`.
- **The load-gate call is HTTP.** The Go pipeline validates by POSTing shapes to the Fuseki `…/shacl?graph=`
  endpoint — no in-process JVM needed — and only writes a graph that conforms. The Jena CLI is the offline
  double-check.
- **Run-stamped named graphs are the write unit;** run provenance lives in a dedicated `run:_provenance`
  graph (see the unionDefaultGraph caveat below).

## Finding 1 — RDF-star: the annotation form matters

`riot` shows the two syntaxes are not interchangeable:

| Turtle written | base triple `:i :locatedAt :p` | annotation `<<…>> :confidence` |
|----------------|:---:|:---:|
| `:i :locatedAt :p {\| :confidence 0.7 \|} .` | **asserted** | stored |
| `<< :i :locatedAt :p >> :confidence 0.7 .`  | *not* asserted | stored |

If the base edge is not asserted, a core SHACL `sh:path :locatedAt` can't see it. So the model uses the
`{| … |}` form throughout — base edge asserted (structure validates) and confidence annotated (read back
via SPARQL-star).

## Finding 2 — SHACL over RDF-star: structure is core, confidence is SPARQL-star

- **Structure** (`Intervention –locatedAt→ Place`, `minCount 1`, `sh:class Place`) is plain SHACL and
  validates the asserted edge.
- **Confidence presence** cannot be a core-SHACL constraint — there is no path from a node to a *quoted
  triple*. The working constraint is `sh:sparql`:

  ```sparql
  SELECT $this ?place WHERE {
    $this gs:locatedAt ?place .
    FILTER NOT EXISTS { << $this gs:locatedAt ?place >> gs:confidence ?c . }
  }
  ```

  Both the Jena **CLI** (`shacl validate`) and the Fuseki **SHACL endpoint** report this correctly:
  well-formed conforms, missing-confidence is a `sh:SPARQLConstraintComponent` violation.
- **#3503 check:** a graph mixing a valid RDF-star edge with a plain violation still fails validation and
  names the bad node — so the reported issue does not affect this pattern on 5.5.0.

## Finding 3 — named graphs, PROV, and the unionDefaultGraph caveat

- POSTing TriG to `…/data` preserves each `run:load-…` named graph; a cross-graph query joins run data to
  `run:_provenance`'s `prov:generatedAtTime` and returns one row per run, time-ordered.
- **Caveat:** the dataset serves with `unionDefaultGraph` on, so a plain `{ ?s ?p ?o }` pattern is the
  **union of the named graphs**, and the stored *default* graph is shadowed. Run provenance therefore lives
  in its own named graph, not the default graph. (Good for reads — the server sees all runs at once — but
  worth pinning in the assembler config P3 writes.)
- Data persists across `compose down`/`up` on the `fuseki-data` volume → P9's dump/restore has something
  durable to snapshot.

## Finding 4 — downsides weighed, not just the checklist

The alternatives were eliminated on their downsides, not their feature lists:

- **GraphDB Free** — proprietary with real vendor lock-in on a project meant to outlive any vendor, a free
  licence that (from v11) must be requested and manually installed, and a 2-concurrent-query cap. Not worth
  trading the Apache-2.0 baseline for.
- **Oxigraph** — **no native SHACL**; the load gate would have to move to external pySHACL, splitting it
  out of the store. SPARQL-star also still experimental.
- **RDF4J** — RDF-star is beta *and* carries an explicit vendor warning that RDF 1.2 may break it — that is
  exactly the feature we lean on hardest. Kept only as the fallback.

Fuseki's own serious downsides do **not** bite this workload: TDB2 is single-writer / single-machine with
no native clustering, but the pipeline is a single batch writer against a read-mostly server on one machine
/ one Noord quarter. Two real operational notes carry forward: keep the JVM heap modest so it doesn't
starve TDB2's memory-mapped OS cache (set `-Xmx2g` here), and the update/SHACL endpoints have no built-in
access control — bind them to localhost / behind the server, not the public internet (a P3/deploy note).

---

## Decision

**Apache Jena Fuseki is the triple store.** It is the only candidate that is genuinely open source (Apache
-2.0) and satisfies all five needs in one container, and the two flagged risks — SHACL-over-RDF-star and
named-graph ergonomics — both hold up empirically. The confidence-presence rule is enforceable via a
`sh:sparql` SPARQL-star constraint (the P8 pattern), validated identically by the endpoint and the CLI, and
Jena #3503 does not affect it on 5.5.0.

### Corrections / knock-ons to feed back

- `IMPLEMENTATION_PLAN.md` §6 — mark **Spike E DONE** (`spikes/spike-e/`), triple store = Fuseki.
- `PHASE_0_PLAN.md` — turn P2's "Recommendation" into a settled decision; drop Spike E from the "still open"
  line (§ Spike status); note P3 and P8 unblocked.
- **P8 hand-off:** model confidence with the `{| … |}` annotation form; enforce presence with the
  `sh:sparql` SPARQL-star constraint from `shapes/shapes.ttl` (core SHACL cannot express it); keep run
  provenance in a dedicated named graph.
- **P3 hand-off:** lift `compose.yaml`'s `fuseki` service (named volume, wget healthcheck, `-Xmx2g`,
  `ENABLE_SHACL=true`); pin `unionDefaultGraph` in the dataset assembler; protect the update/SHACL
  endpoints (localhost / behind the server). The Go load path validates by POSTing shapes to `…/shacl`.

## Files

- `compose.yaml` — `fuseki` (Apache Jena Fuseki 5.5.0, named volume, SHACL endpoint on) + an idle
  `jena-tools` sidecar (Jena 5.1.0 CLI) for the independent `shacl validate`.
- `run.sh` — idempotent orchestrator: brings up the stack, loads the sample, runs the four required checks
  + two risk probes, prints a PASS/FAIL gate. `--reset` rebuilds.
- `data/sample.trig` — two run-stamped named graphs (each an Intervention with a confidence-annotated
  `locatedAt` edge) + a `run:_provenance` graph with `prov:generatedAtTime`.
- `data/wellformed.ttl`, `data/reject-structure.ttl`, `data/reject-confidence.ttl` — SHACL accept + the two
  reject paths (missing edge; missing confidence annotation).
- `shapes/shapes.ttl` — the load-gate shape: core structure + the `sh:sparql` confidence-presence rule.
- `sparql/star-read.rq` — the SPARQL-star confidence read; `sparql/cross-graph.rq` — the two-named-graph
  join to per-run transaction-time.
