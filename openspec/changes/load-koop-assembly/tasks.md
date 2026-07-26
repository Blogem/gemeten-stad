## 1. Parse landed publications (SRU record + metadata sidecar)

- [ ] 1.1 Add an enumerator (`load/koop/read.go`) over `store.BasePath/koop/*.xml` that pairs each
      SRU record with its `<id>.metadata.xml`, excluding `*.metadata.xml`, `_cursor.json`, `*.prov.jsonl`.
- [ ] 1.2 Add SRU-record parsing (`load/koop/parse.go`): RD point (`overheidwetgeving:geometrie`
      `POINT(x y)`), activiteit (`overheidwetgeving:activiteit`), title, `dcterms:available`; parse
      defensively (missing field flagged, never panic).
- [ ] 1.3 Add metadata parsing: `OVERHEIDop.referentienummer` (zaaknummer), and fall back to the
      keyless remainder when the sidecar is missing.
- [ ] 1.4 Add a title-prefix `kind` classifier (aanvraag/besluit/ontwerpbesluit/verlenging/
      ingetrokken/other) and a best-effort postcode+huisnummer extractor from the title.
- [ ] 1.5 Unit tests: full parse of a realistic record+sidecar; kind classification across prefixes;
      huisnummer/postcode extraction; missing-sidecar and missing-field paths.

## 2. Dedup and select the audited besluit

- [ ] 2.1 Group publications by zaaknummer and select the besluit per group; keep all publications
      for the PostGIS trail (`load/koop/dedup.go`).
- [ ] 2.2 Unit tests: aanvraag+besluit → one besluit audited, both retained; aanvraag-only → no
      Intervention, retained in trail; multiple-besluit group picks the primary (document the rule).

## 3. Resolve location and scope to Noord

- [ ] 3.1 Build `location.Query{Postcode, Huisnummer, Point (RD), Date = besluit available}` and call
      `location.Resolve`; capture `Confidence`, `Caveats`, `Geom`, `BuurtID` (`load/koop/resolve.go`).
- [ ] 3.2 Resolve the containing gebieden buurt code: `Result.BuurtID` at the buurt tier, else PIP the
      resolved `Geom` into `gebieden_buurten`. This code is the `Place` identity.
- [ ] 3.3 Scope to Noord: keep permits whose buurt is in stadsdeel Noord (per design D4 / open
      question — buurt→stadsdeel source), cross-checked by the `Z….-N…` zaaknummer prefix; log mismatches.
- [ ] 3.4 Classify each besluit resolved / unresolvable; unresolvable ones bypass graph assembly and
      are recorded per task 6.
- [ ] 3.5 Tests: title address → Noord buurt (0.90); point-only → buurt floor (0.50 +
      `unresolvedLocation`); outside-Noord excluded; unresolvable bucketed.

## 4. Assemble the besluit graph turtle

- [ ] 4.1 Turtle builder (`load/koop/graph.go`): `data:intervention/<zaak> a gs:Intervention` with
      `gs:activity act:vellen`, `gs:claims data:claim/<zaak>`, `data:claim/<zaak> a gs:Claim` (no
      count), and a minimal `data:place/<identificatie> a gs:Place` (D5). Mirror the P12b/`load/graph`
      namespace consts + `assertSafeIRI`/literal-escaping guards.
- [ ] 4.2 Emit `gs:locatedAt` with `{| gs:confidence <c> ; gs:caveat <term> ; gs:validFrom
      <besluit-date> |}` — caveat iff c<1.0, mapped from resolver caveats to the controlled `gs:` terms.
- [ ] 4.3 Map felling activiteit terms → `act:vellen`; map resolver caveats → `gs:unresolvedLocation`/
      `gs:timeMismatch`.
- [ ] 4.4 Golden-turtle unit tests: resolved besluit → conforming turtle; exact (1.0) omits caveat;
      fallback includes it; Place typed; IRIs match the D6 scheme.

## 5. Write through the P12 graph gate

- [ ] 5.1 Concatenate per-besluit turtle into one candidate and call `load/graph.Load`; surface
      non-conformance as a load error (`load/koop/load.go`).
- [ ] 5.2 Integration test (isolated Fuseki): conforming corpus written; a `locatedAt` missing
      `gs:confidence` rejected with no partial write; the `<place> a gs:Place` re-assertion against a
      pre-seeded P12b Place is an immutableConflict skip (Place label/`gs:within` intact).

## 6. Persist the publication trail to PostGIS

- [ ] 6.1 `koop_publications` schema (`load/koop/schema.go`, `load/bomen` pattern): `gmb_id` PK,
      `zaaknummer`, `kind`, dates, `geometry(Point, 28992)`, postcode, resolved buurt code,
      `resolved_confidence`, `caveats`, `unresolved` marker, `raw jsonb`; schema-qualified;
      `--reset` drops/rebuilds.
- [ ] 6.2 Stage + upsert keyed by `gmb_id` (`load/koop/stage.go`, `upsert.go`): every publication of
      an in-scope zaak is a row; unchanged re-run is a no-op; unresolvable/keyless rows carry their
      markers, no geometry faked.
- [ ] 6.3 Integration test (isolated schema): a zaak's aanvraag+besluit are two rows; besluit row
      holds point/postcode/dates/buurt/confidence/caveats; unresolvable row marked, no geometry.

## 7. Top-level load + pipeline wiring

- [ ] 7.1 `koop.Load(ctx, pool, store, fusekiURL, Config{Reset})` orchestrating parse → dedup →
      resolve → assemble besluiten → graph write → PostGIS upsert; log counts (besluiten loaded /
      excluded-non-Noord / pending / unresolvable / keyless); return non-nil on failure.
- [ ] 7.2 Register `koop` in `cmd/pipeline`'s `loadRegistry` (after `bomen`, and after `graph`/the
      places skeleton so Places exist); wire Fuseki URL, DB pool, raw store.
- [ ] 7.3 Update the load subcommand help and `load/koop/doc.go`.

## 8. Idempotency and end-to-end verification

- [ ] 8.1 Integration test: full unchanged re-run is a true no-op (no new run graph, no PostGIS
      changes); a re-resolution opens a new SCD2 version and upserts the row.
- [ ] 8.2 Integration test over a fixture corpus (aanvraag+besluit pairs, a pending case, an
      out-of-Noord case, an unresolvable case) through the real resolver + graph writer + PostGIS,
      asserting an Intervention's `locatedAt` target matches a P12b-seeded `data:place/<identificatie>`.

## 9. Cross-change coordination

- [ ] 9.1 Confirm `koop-ingest-metadata` is merged and the metadata sidecars are landed before
      running P13 end to end (hard prerequisite).
- [ ] 9.2 Confirm the P12b `Place` key field is the gebieden `identificatie` (14-digit `gbdBuurtId`)
      and align `data:place/<…>` in task 4.1 before merge; share the geometry+metadata fixture with
      the koop-ingest-metadata tests.
