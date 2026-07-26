## 1. Parse landed gzd records

- [ ] 1.1 Add a `Permit` struct and an `encoding/xml` parser in `load/koop/parse.go` extracting id
      (`dcterms:identifier`), zaaknummer (`overheidop:referentienummer`), activiteit
      (`overheidop:activiteit`), RD point (`overheidwetgeving:geometrie` `POINT(x y)`) + WGS84
      `locatiepunt`, postcode (`overheidop:postcode`), title (`dcterms:title`), available
      (`dcterms:available`); parse defensively (missing required field → flagged, never panic).
- [ ] 1.2 Add a store enumerator in `load/koop/read.go` that lists `store.BasePath/koop/*.xml`,
      skipping `koop/_cursor.json` and `*.prov.jsonl`, and reads each file's bytes for the parser.
- [ ] 1.3 Add a title-prefix classifier (`doctype`: besluit = `"Verleend:"`/`"Besluit:"`, aanvraag =
      `"Aanvraag:"`, else other) and a best-effort huisnummer extractor from the title.
- [ ] 1.4 Unit tests for the parser (a realistic geometry-bearing record → all fields), the
      enumerator (sidecars skipped), the classifier, and huisnummer extraction, incl. missing-field
      cases.

## 2. Dedup and select the audited besluit

- [ ] 2.1 Group parsed permits by zaaknummer and select the besluit per group; a group with no
      besluit yields nothing (`load/koop/dedup.go`).
- [ ] 2.2 Unit tests: aanvraag+besluit sharing a zaaknummer collapse to one besluit; aanvraag-only
      group yields no Intervention; multiple-besluit group picks the primary (document the rule).

## 3. Resolve location and scope to Noord

- [ ] 3.1 Build a `location.Query{Postcode, Huisnummer, Point (RD), Date=besluit available}` and call
      `location.Resolve`; capture `Confidence`, `Caveats`, `Geom`, `BuurtID` (`load/koop/resolve.go`).
- [ ] 3.2 Resolve the containing gebieden buurt code: use `Result.BuurtID` at the buurt tier;
      otherwise PIP the resolved `Geom` into `gebieden_buurten`. This code is the `Place` identity.
- [ ] 3.3 Determine stadsdeel-Noord membership of the buurt (per design D4 open question — code
      prefix or a gebieden join) and drop non-Noord permits; cross-check the `Z….-N…` zaaknummer
      prefix and log mismatches.
- [ ] 3.4 Classify each besluit as resolved / unresolvable (no point and no address); unresolvable
      ones bypass graph assembly (task 5) and are recorded per task 6.
- [ ] 3.5 Unit/integration tests: a Noord point resolves to a buurt Place; an outside-Noord permit
      is excluded; a point-only permit lands at the buurt tier with `unresolvedLocation` + conf<1.0.

## 4. Assemble the graph turtle

- [ ] 4.1 Add a turtle builder in `load/koop/graph.go`: `data:intervention/<zaak> a gs:Intervention`
      with `gs:activity act:vellen`, `gs:claims data:claim/<zaak>`, and `data:claim/<zaak> a
      gs:Claim` (no count); emit `data:place/<code> a gs:Place` minimally (D5, decouples from P12b).
- [ ] 4.2 Emit the `gs:locatedAt` edge with the RDF-star `{| gs:confidence <c> ; gs:caveat <term> ;
      gs:validFrom <besluit-date> |}` annotation — caveat present iff c<1.0; caveat terms mapped from
      the resolver caveats to the controlled `gs:` individuals. Escape all literals/IRIs safely.
- [ ] 4.3 Map the resolver's caveat strings (`unresolvedLocation`/`timeMismatch`) to the ontology
      `gs:` caveat individuals; map felling activiteit terms to `act:vellen`.
- [ ] 4.4 Unit tests (golden turtle): a resolved besluit → conforming turtle; an exact resolution
      (conf 1.0) omits the caveat; a fallback resolution includes it; Place is typed.

## 5. Write through the P12 graph gate

- [ ] 5.1 Concatenate per-permit turtle into one candidate and call `load/graph.Load(ctx, fusekiURL,
      candidate, cfg)`; surface non-conformance as a load error with detail (`load/koop/load.go`).
- [ ] 5.2 Integration test against the isolated Fuseki dataset: a conforming corpus is written; a
      candidate with a `locatedAt` edge missing `gs:confidence` is rejected with no partial write.

## 6. Persist permit values to PostGIS

- [ ] 6.1 Add `koop_permits` schema (`load/koop/schema.go`, `load/bomen` pattern): zaaknummer PK,
      publication ids, activiteit, dates, `geometry(Point, 28992)`, postcode, resolved buurt code,
      `resolvedConfidence`, `caveats`, `unresolved` marker, `raw jsonb`; schema-qualified via
      `current_schema()`; `--reset` drops/rebuilds.
- [ ] 6.2 Stage + upsert keyed by zaaknummer (`load/koop/stage.go`, `upsert.go`): unchanged re-run is
      a no-op; changed row upserts; unresolvable permits stored with the `unresolved` marker + raw
      title/dates, no geometry faked (D9).
- [ ] 6.3 Integration test (isolated schema): resolved permit row holds point/postcode/dates/buurt/
      confidence/caveats; unresolvable permit stored with the marker and no geometry.

## 7. Top-level load + pipeline wiring

- [ ] 7.1 Add `koop.Load(ctx, pool, store, fusekiURL, Config{Reset})` orchestrating parse → dedup →
      resolve → assemble → graph write → PostGIS upsert, logging counts (loaded / excluded-non-Noord
      / unresolvable) and returning non-nil on any failure.
- [ ] 7.2 Add `runKoopLoad` and register `koop` in `cmd/pipeline`'s `loadRegistry` (after `bomen`);
      wire the Fuseki URL (`shared.FusekiURL`), DB pool, and raw store as the other loaders do.
- [ ] 7.3 Update the load subcommand help/docs and `load/koop/doc.go` to describe the stage.

## 8. Idempotency and end-to-end verification

- [ ] 8.1 Integration test: full unchanged re-run is a true no-op (no new run graph, no PostGIS
      changes); a changed tracked field opens a new SCD2 version (prior `gs:validTo` stamped) and
      upserts the PostGIS row.
- [ ] 8.2 Integration test loading a small fixture permit set through the real resolver + graph
      writer + PostGIS, asserting an Intervention's `locatedAt` target IRI matches the
      `data:place/<code>` scheme (the P12b contract).

## 9. Fixture coordination (P11 worktree)

- [ ] 9.1 Add a realistic geometry-bearing `gzd` fixture (Spike-B-shaped: RD `POINT`, `locatiepunt`,
      postcode, activiteit, referentienummer, aanvraag+besluit pair) usable by the P13 parser tests;
      coordinate with the `koop-ingest` worktree so its harvest/landing tests can share it.
- [ ] 9.2 Confirm the P12b `Place` IRI key field (gebieden `identificatie`/`gbdBuurtId` vs short
      `code`) and align `data:place/<…>` in task 4.1 before merge.
