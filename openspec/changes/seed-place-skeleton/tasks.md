## 1. Ontology term (gs:active)

- [ ] 1.1 Add `gs:active` to `ontology/ontology.ttl` — an `owl:DatatypeProperty` with
      `rdfs:domain gs:Place`, `rdfs:range xsd:boolean`, `rdfs:label`, and an `rdfs:comment` stating
      active-status is evolving state (RDF-star `gs:validFrom`/`gs:validTo`), distinct from the
      Place's timeless identity (D6).
- [ ] 1.2 Confirm the ontology still loads clean (embed test / `go test ./ontology/...`) and that a
      Place candidate with `gs:active true {| gs:validFrom … |}` conforms against `shapes.ttl` (no
      `PlaceShape`, D3).

## 2. Turtle projection (pure Go — unit-tested first)

- [ ] 2.1 Add a `load/places` package with row structs for a projected buurt
      (`identificatie, naam, ligtinwijkid, source_deleted_at`) and wijk
      (`identificatie, naam, source_deleted_at`), and a Place IRI minter keyed on `identificatie`
      under `http://gemetenstad.nl/id/place/` (D1).
- [ ] 2.2 Implement a pure `renderPlaces(buurten, wijken, loadTS) []byte` that emits Turtle: each
      buurt and wijk (live **and** soft-deleted) as `a gs:Place` with an escaped `rdfs:label`, a
      `gs:active` statement (`true` when `source_deleted_at` is null else `false`) annotated with a
      `gs:validFrom` (`loadTS` when active, the row's `source_deleted_at` when inactive — D5), and,
      for a buurt whose `ligtinwijkid` resolves to a projected wijk, a `gs:within` edge; wijken carry
      no `gs:within`. No geometry, no `gs:confidence`/`gs:evidence`.
- [ ] 2.3 In `renderPlaces`, for a buurt whose `ligtinwijkid` is null or does not resolve to a
      projected wijk, emit the Place + label + `gs:active` WITHOUT a `gs:within` edge and record it
      for a loud `places: …` warning (D5).
- [ ] 2.4 Unit tests (table-driven, testify) for `renderPlaces` covering: a live buurt within its
      wijk (`gs:active true`, `validFrom = loadTS`); a wijk with no `gs:within`; a soft-deleted row
      (`gs:active false`, `validFrom = source_deleted_at`); a buurt with null `ligtinwijkid` (seeded,
      no edge, warned); a dangling `ligtinwijkid` (seeded, no edge, warned); label escaping; IRI
      minting from `identificatie`; and that no geometry/confidence annotation is emitted.

## 3. PostGIS read + candidate assembly

- [ ] 3.1 Add a thin repository read that selects **all** rows (no `source_deleted_at` filter) of
      `gebieden_buurten` (`identificatie, naam, ligtinwijkid, source_deleted_at`) and
      `gebieden_wijken` (`identificatie, naam, source_deleted_at`) into the row structs,
      schema-qualified like `load/geo`.
- [ ] 3.2 Add `BuildCandidate(ctx, pool, schema) ([]byte, error)` that reads (3.1) then renders
      (2.2) with the load timestamp, returning the Turtle candidate and logging any
      dangling-containment warnings.

## 4. Pipeline wiring (graph load seeds the skeleton — D4)

- [ ] 4.1 Rewire `runGraphLoad` (`cmd/pipeline/main.go`) to open a Postgres pool
      (`shared.DatabaseURL` / `shared.ConnectPostgres`, like `runBomenLoad`), call
      `places.BuildCandidate`, and pass the candidate to `loadgraph.Load(ctx, url, candidate, cfg)`
      (replacing the `nil` candidate), so one gated `Load` ensures the reference model and seeds the
      skeleton.
- [ ] 4.2 Confirm the `loadRegistry` order keeps `geo` before `graph` so the gebieden tables exist
      when the graph load reads them; update the `runGraphLoad` doc comment to describe seeding the
      Place skeleton (no longer reference-model-only).

## 5. Integration tests (isolated gs-test Fuseki + test Postgres — P5 harness)

- [ ] 5.1 Seed-and-verify: with a small gebieden fixture in the isolated test Postgres, seed the
      skeleton via `Load`; assert the buurt/wijk Places, their `rdfs:label`s, their buurt→wijk
      `gs:within` edges, and their `gs:active`+`gs:validFrom` annotations are written into a
      `run:load-…` graph with a matching `prov:Activity`.
- [ ] 5.2 No-op re-seed: seeding the same skeleton twice (only the emitted `gs:validFrom` differs)
      leaves no second `run:load-…` graph, no new `prov:Activity`, and no added triples (rests on
      P12's valid-time-excluded change detection).
- [ ] 5.3 Deprecation supersede: re-seed after setting a buurt's `source_deleted_at` in the fixture;
      assert a new `gs:active false` version carrying its `gs:validFrom`, the prior `gs:active true`
      version closed with a matching `gs:validTo`, exactly one open version, and the prior interval
      retained.
- [ ] 5.4 Reset: `--reset`/`Config{Reset}` rebuilds the skeleton from the current fixture.

## 6. Validation

- [ ] 6.1 `go build ./...` and `go vet ./load/places/... ./cmd/pipeline/... ./ontology/...` clean.
- [ ] 6.2 `go test ./load/places/... ./ontology/...` (unit) passes.
- [ ] 6.3 `go test -tags=integration ./load/places/... ./cmd/pipeline/...` passes against
      `GS_TEST_FUSEKI_URL` + the isolated test Postgres (guarded against production names).
- [ ] 6.4 `openspec validate seed-place-skeleton --strict` passes.
