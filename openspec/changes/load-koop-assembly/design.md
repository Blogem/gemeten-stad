## Context

`load koop` (P13) is the silver assembler for permits — the first stage that turns raw documents
into fully-formed graph entities. It sits between three shipped pieces:

- **P11 `ingest koop`** lands each Noord kap/verplant publication as **verbatim SRU `gzd` inner
  XML** at `koop/<gmb-id>.xml` (via `shared.RawStore.Land`; a `.prov.jsonl` sidecar per file, and a
  `koop/_cursor.json`). P11 parses only `identifier` + `available` for its incremental cursor; **all
  other structure survives only inside the landed XML**, and there is no reader/lister — P13
  enumerates the store prefix directly.
- **P12 `load/graph.Load(ctx, fusekiURL, candidate []byte, Config{Reset})`** takes turtle bytes,
  SHACL-gates them against `ontology/shapes.ttl`, and on conform runs the SCD2 idempotent upsert
  (IRI-keyed, change-detected by an order-stable per-entity signature that ignores
  `validFrom`/`validTo`; a delta-empty run mints no run graph). It is **Postgres-free**: callers
  build the turtle, including the `{| … |}` confidence annotations.
- **P6 `location.Resolve(ctx, pool, Query) (Result, error)`** ladders address (0.90) → postcode
  (0.70) → buurt-via-point PIP (0.50) against the local BAG/gebieden mirror; returns `Geom` (EWKT),
  `Confidence`, `TimeMatch`, `Caveats`, `BuurtID` (set at the buurt tier). It errors only when
  nothing resolves.

**Correcting a false premise.** An early scan of P11's _test fixtures_ (`ingest/koop/testdata/*.xml`,
minimal hand-authored records) suggested the landed permits carried no geometry. They do:
Spike B (`spikes/spike-b/harvest_permits.py`) and `docs/DATA_SOURCES.md` §1 confirm the `gzd` record
exposes a **structured RD point** (`overheidwetgeving:geometrie` `POINT(x y)`) and WGS84
`locatiepunt`, plus `overheidop:postcode`, controlled `overheidop:activiteit`, and a
`overheidop:referentienummer` whose prefix encodes the stadsdeel (`Z2022-N…` = Noord). P11 sets no
`recordSchema`, so it lands the default `gzd` payload verbatim — the geometry is present. This design
is built on the structured signal; the fixtures are the thing that must catch up (a task below).

## Goals / Non-Goals

**Goals:**

- Assemble each Noord besluit into a provenanced `Intervention` + `Claim` + `locatedAt`→`Place`,
  through the P12 SHACL gate, with permit values/geometry in PostGIS.
- Dedup aanvraag/besluit per zaaknummer; audit the besluit.
- Resolve location from the structured point + postcode; scope to Noord by geometry.
- Idempotent: unchanged re-run is a true no-op; a changed field opens a new SCD2 version.
- Decouple from P12b: satisfy the `locatedAt`→`Place` gate without the skeleton being seeded first.

**Non-Goals:**

- No obligation **count** on the `Claim` (Phase-2 extraction). No fulfilment/timeliness (P14/Phase-2).
- No permit→registry `AuditLink` (that is P14 `derive`).
- No NER / permit-text parsing beyond a best-effort huisnummer off the title.
- No new resolver tiers and no PDOK Locatieserver fallback (settled: local mirror only).
- No stadsdeel-level `Place` node (P12b defers it; Noord scoping uses the PostGIS buurt→stadsdeel
  mapping, not a graph node).

## Decisions

### D1 — Parse the landed XML directly; a typed permit struct is the seam

`load/koop` enumerates `store.BasePath/koop/*.xml` (skipping `_cursor.json` and `*.prov.jsonl`) and
parses each with `encoding/xml` into a typed `Permit` (id, zaaknummer, activiteit, RD point,
postcode, title, available, doctype). Rationale: P11 exposes no reader and lands verbatim bytes; the
parser is P13's responsibility and the one place that knows the `gzd` shape. _Alternative rejected:_
extending P11 with a reader — out of scope, P11 is in test, and the parse concern belongs to the
consumer.

### D2 — Dedup by zaaknummer, classify by title prefix <<do we track both versions, or will we immediatly go for the besluit if it's there? i prefer to see the evolution, also on old data. of course the besluit is the active one.>>

Group by `overheidop:referentienummer`; within a group pick the besluit (`dcterms:title` starts
`"Verleend:"`/`"Besluit:"`) over the aanvraag (`"Aanvraag:"`). The title prefix is the only
aanvraag/besluit signal in the payload (both carry `dt.type omgevingsvergunning`). _Alternative
rejected:_ a doctype field — none exists.

### D3 — Location: structured point primary, postcode/address ladder, buurt is the Place

Build a `location.Query{Postcode, Huisnummer (parsed from title), Point (RD), Date (besluit
valid-time)}`. The resolver ladders to the finest tier it can; the **graph `Place` is always the
containing gebieden buurt** — a thin-graph choice: the exact point + finer precision live in PostGIS
and ride the edge as `gs:confidence`, while the graph records buurt-level membership for roll-up.
Getting the buurt code: the resolver already returns `BuurtID` at the buurt tier; at the
address/postcode tier it returns a `Geom` but no buurt, so P13 PIPs that geom into `gebieden_buurten`
to obtain the code. _Alternative considered:_ extend `location.Result` to always carry the containing
buurt — cleaner, but touches the P6 package; deferred to keep P13 additive (revisit if a second
caller needs it). _Alternative rejected:_ mint a distinct BAG-address `Place` node — the graph
skeleton is buurt/wijk only (P12b), and a per-address node buys nothing the confidence + PostGIS
point don't already carry.

### D4 — Noord scoping by resolved buurt, cross-checked by zaaknummer prefix

Keep a permit iff its resolved buurt is in stadsdeel Noord (buurt→stadsdeel via the gebieden
code/`ligtInWijk`→stadsdeel mapping in PostGIS), cross-checked against the `Z….-N…` referentienummer
prefix (log a mismatch). The harvest is Amsterdam-wide by design (P11 `query.go`), so scoping lands
here. _Alternative rejected:_ the spike's tree-vote-for-buurt — a pre-BAG hack; we have the authoritative
local gebieden polygons.

### D5 — Assert `<place> a gs:Place` locally to decouple from P12b <<we shouldn't decouple>>

`InterventionShape` requires `locatedAt` → a node that **is** a `gs:Place` (`sh:class`). P13 emits a
minimal `data:place/<code> a gs:Place` alongside its edge, so the candidate conforms whether or not
P12b has seeded the skeleton. P12b independently enriches the same IRI with `rdfs:label` + `gs:within`;
under IRI-keyed upsert both contribute to one `Place`, order-independent. This realizes the user's
"P13 must not depend on P12b." _Alternative rejected:_ ordering P12b before P13 — reintroduces the
build-order coupling we were asked to avoid.

### D6 — IRI scheme

`data:intervention/<zaaknummer>`, `data:claim/<zaaknummer>`, `data:place/<gebieden-buurt-code>` (all
under `http://gemetenstad.nl/id/`, the namespace the writer's change-detection keys on). The `place/`
scheme **must match P12b** — the one hard cross-worktree contract; captured here and coordinated by
value, not build order.

### D7 — SCD2 granularity: keep v1 mostly immutable, stamp validFrom only on the evolving edge

Identity + activity + the `claims` edge are write-once (immutable facts). The **`locatedAt`
resolution** is the evolving state: its RDF-star annotation carries `gs:validFrom` = the besluit
valid-time, so a re-resolution (BAG update, better address) opens a new version and closes the prior
per D4 of the plan. Everything else being un-stamped means the common re-run is a pure no-op
(unchanged signature). _Alternative considered:_ stamp the whole Intervention as evolving — over-broad;
most permit facts never change once decided.

### D8 — PostGIS shape mirrors `load/bomen`

A `koop_permits` table keyed by zaaknummer (schema-qualified via `current_schema()`, staging +
upsert + `--reset` drop, `raw jsonb` catch-all — the `load/bomen` pattern): publication ids,
activiteit, dates, `geometry(Point, 28992)` for the RD point, postcode, resolved buurt code,
`resolvedConfidence`, `caveats`, an `unresolved` marker column, `raw`. Geometry lives here only.

### D9 — Unresolvable permits: record in PostGIS, never graph a fake

A besluit that resolves to nothing (no point, no address) is written to `koop_permits` with the
`unresolved` marker and **no** graph Intervention (it cannot satisfy `locatedAt` minCount without a
real place, and faking one violates §4). Surfaced for P14 / reporting. Rare, since the point is
structured. _Alternative rejected (Q2 option "Noord placeholder"):_ attaching to a synthetic
stadsdeel-Noord `Place` — needs a node P12b defers and dilutes the audit with low-signal edges;
revisit only if the unresolved rate is material.

## Risks / Trade-offs

- **[gzd parse drift]** Real `gzd` records are richer/messier than the minimal fixtures → parse
  against a realistic fixture (a Spike-B-shaped record with the geometry block) and treat missing
  required fields as "unresolvable", never a panic. → Add the enriched fixture as a task; parse
  defensively.
- **[P12b IRI contract]** If P12b keys `Place` by a different code than D6, resolved places split
  into two nodes. → Pin the scheme in D6, confirm P12b's key field (gebieden buurt `identificatie`
  vs `code`) before merge; an integration test asserts an Intervention's `locatedAt` target matches
  a seeded skeleton IRI.
- **[Buurt PIP at address tier]** Doing a second PIP in P13 (D3) duplicates logic the resolver
  nearly has. → Acceptable for one caller; if P14 needs the same, promote it into `location.Result`.
- **[Noord yield]** Address/huisnummer parsed from free-text titles is best-effort; but the RD point
  guarantees a buurt floor, so yield is driven by the point, not the title. → Point is primary;
  title huisnummer only sharpens confidence.
- **[valid-time source]** The besluit publication date (`dcterms:available`) is the only reliable
  date (Spike A: the administrative permit dates are unreliable). Use it as the resolution
  valid-time and the `validFrom`. → Documented; anchor elapsed-time reasoning downstream on the
  registry felling date (P14), not these.

## Migration Plan

Additive. New `load/koop` package + a `koop` entry in `cmd/pipeline`'s `loadRegistry` (after `bomen`,
before/with `graph`). New `koop_permits` PostGIS table created on first run; `--reset` drops it.
Rollback = remove the registry entry; no existing stage or table changes. Integration tests run
against the isolated Fuseki dataset + Postgres schema (never production names).

## Open Questions

- **Buurt→stadsdeel mapping source (D4):** derive Noord membership from the gebieden code prefix, or
  from a `ligtInWijk`→wijk→stadsdeel join in PostGIS? (P12b defers stadsdeel; confirm what the P7
  gebieden tables actually expose.)
- **P12b `Place` key field (D6):** confirm P12b keys on gebieden `identificatie` (the 14-digit
  `gbdBuurtId` the resolver returns) vs a short `code`, so `data:place/<…>` matches exactly.
- **Multiple besluiten per zaaknummer:** if a case has a besluit + a later `"Verlengd:"`/amendment,
  v1 audits the first/primary besluit; is amendment handling needed in Phase 1 or deferred to the
  DecisionPeriod model (Phase 2)?
