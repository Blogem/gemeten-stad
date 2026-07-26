## Why

P8 shipped the `load/graph` write/validate primitive — `Load(ctx, fusekiURL, candidate, Config{Reset})`
SHACL-gates a candidate turtle graph and, on conform, writes it into a fresh run-stamped PROV named
graph. But it is **append-per-run**: every `Load` mints a new `run:load-<ts>` graph additively, so
re-running the pipeline duplicates every triple. P13 (`load koop`), P12b (Place skeleton), and P14
(`derive`) all write through this primitive and every one of them requires *"re-running is a no-op"*.
Without an idempotent-upsert layer the deterministic backbone cannot be re-run, and the bitemporal
never-overwrite rule (§3 D4) has no writer that honours it.

## What Changes

- **Idempotent, IRI-keyed upsert** on top of the shipped primitive: a `Load` re-run writes only
  *genuinely new or changed* data. Change detection is **per-entity** (keyed on the stable subject
  IRI), comparing the candidate's content for an entity against what the graph already holds,
  **ignoring the valid-time stamps** (`gs:validFrom`/`gs:validTo`).
- **SCD2 open/close for evolving state** (§3 D4): when a tracked field of an evolving fact changes,
  the writer **opens** the new version and **closes** the prior by stamping its `gs:validTo` —
  history is never overwritten. Immutable facts (stable identity + un-stamped triples, e.g. the
  reference-data Place skeleton, the `Claim`) are **write-once**: present → skipped.
- **A no-op run leaves no trace**: a run that finds nothing new or changed mints **no**
  `run:load-<ts>` graph and **no** `prov:Activity`. PROV records transaction time only for runs that
  actually changed the graph — so a second run is a true no-op end to end. **BREAKING** relative to
  the shipped primitive's "absent `Reset`, every conforming write adds a new run graph."
- **The malformed-candidate gate is preserved** (regression): a non-conforming candidate is still
  rejected with no partial writes.
- The writer stays **Postgres-free and takes the turtle it is given** — the `{| … |}`
  confidence-annotation *construction* stays with the callers (P12b/P13/P14), unchanged.

## Capabilities

### New Capabilities
<!-- None. This finishes an existing capability; no new spec is introduced. -->

### Modified Capabilities
- `graph-load-gate`: the "run-stamped named-graph writes" requirement changes from *additive per
  run* to *idempotent upsert with SCD2 open/close*, and a new requirement fixes the no-op-leaves-no-
  trace + never-overwrite semantics. The SHACL-gate and isolated-Fuseki-test requirements are
  unchanged (the gate is a preserved regression).

## Impact

- **Changed files:** the `load/graph/` package only — the change-detection/upsert logic plus its
  wiring into `Load`, and unit + integration tests. No ontology/shapes change is needed (the
  `gs:validFrom`/`gs:validTo` predicates already exist in `ontology/ontology.ttl`; AuditLink
  attachment modelling is P14's job).
- **Confined to `load/graph/`** — does **not** touch `ingest/` (P11 is in flight in the
  `ingest-koop` worktree) or the callers that build turtle (P12b/P13/P14).
- **Depends on:** P8 (the shipped write/validate primitive, ontology + shapes), P2 (Fuseki), P5
  (`internal/testdb` isolated-dataset harness).
- **Unblocks:** P12b (Place skeleton), P13 (`load koop`), P14 (`derive`) — all of which specify
  "re-running is a no-op" and "a changed field opens a new version / closes the prior's `validTo`".
- **Integration tests** run against the isolated `gs-test` Fuseki dataset (`GS_TEST_FUSEKI_URL`),
  guarded against production names.
