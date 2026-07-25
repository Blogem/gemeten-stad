# Spike D — does the geo bulk backbone load & resolve locally?

**Question (Phase 0, `IMPLEMENTATION_PLAN.md` §6/§4).** The project made a settled decision to
resolve every location **locally, against a bulk-loaded BAG + `gebieden`/CBS polygons in PostGIS —
no PDOK Locatieserver, not even as a fallback** (§4). That decision was unproven. Does the **BAG
*LV 2.0 Extract* load via GDAL `lvbag` into PostGIS** (filtered to gemeente `0363`), do the
**`gebieden`/CBS wijk-buurt polygons load via WFS/GeoJSON**, and does **free-text/reference-address
→ BAG resolution + point-in-polygon** work locally — **and at what resolution rate?**

**Answer. Yes — the backbone loads and resolves locally; the Locatieserver is not needed.** Over
gemeente `0363` (Amsterdam) the national BAG extract loads through `lvbag` into PostGIS in ~8
minutes, the polygons load in RD, and a local, **valid-time-aware** resolver reproduces known
geography and places permit addresses at address precision:

- **point-in-polygon is exact:** **100%** (939/939) of felled-tree points fall in the `gebieden`
  buurt their own `gbdBuurtId` names (the ground truth) — the polygons + CRS are correct.
- **permit free-text → BAG address:** **95.1%** of the 365 Noord kap permits resolve to a BAG
  **address** point (median **0.0 m** from the permit's own published coordinate) — 90.4% at the
  permit's exact valid-time and a further 4.7% via an **any-time fallback** (flagged on the link,
  see Finding 3); 1.1% resolve to **postcode**, 3.8% to **buurt** only, and **0% unresolved**.
- **this is the finer place Spike B's confidence model is waiting on:** it lifts place from
  buurt-only (`0.50`) to address (`0.90`) for 95% of permits, promoting the weak buurt-only links
  Spike B measured — with **no Locatieserver and no gazetteer**.

The resolution is done **at each permit's valid-time** because BAG is bitemporal and we audit
backdated permits (Finding 3). Several `DATA_SOURCES.md`/`IMPLEMENTATION_PLAN.md` claims need
correcting (below), the biggest being that **`lvbag` cannot apply the daily mutation files** — the
refresh story is an idempotent monthly full reload, not incremental-via-`lvbag`.

All figures verified **2026-07-25** over the full stadsdeel **Noord** registry population and the
full 2022 Amsterdam kap corpus. Reproduce (Docker only; no host GDAL/psql): run `../spike-a/probe.py`
and `../spike-b/harvest_permits.py` for the base data, then `python3 harvest.py`, `./load.sh`, and
`docker compose exec -T db psql -U geo -d geo < sql/pip.sql` / `< sql/resolve.sql`.

---

## Scope established (reusable by Phase 1)

- **The backbone is a container stack, not a host install.** `compose.yaml` = `postgis/postgis` +
  `ghcr.io/osgeo/gdal` on one network; the DB lives on a named volume so the loaded `0363` BAG +
  polygons survive `docker compose down` (the ~3.6 GB extract and the all-NL parse never repeat).
  `load.sh` is incremental — each table is skipped if already populated; `--reset` rebuilds.
- **The resolver reuses the earlier spikes' outputs** — `../spike-a/noord_kap.jsonl` (the felled
  registry rows + nearest address) and `../spike-b/noord_permits.jsonl` (the 365 Noord kap permits
  **with their own RD point** — Spike B's find that omgevingsvergunningen DO carry a `locatiepunt`).
  No KOOP/registry re-fetch; `harvest.py` only adds the tree points (via `boomId→stamgegevens`) and
  the RD polygons.
- **BAG is loaded as-is (Amsterdam only), then queried temporally.** The only load-time filter is
  the municipality (`identificatie LIKE '%.0363%'`); **no column is dropped and no other row filter
  is applied** — all voorkomens (temporal versions) and all columns are kept, and the resolver picks
  the right version at query time. Loaded `0363` BAG: **811,736** nummeraanduiding voorkomens
  (**624,423** distinct addresses), **1,300,799** verblijfsobject voorkomens, **13,526** openbare
  ruimte, **4,002** ligplaats, **748** standplaats; polygons = 69 gebieden buurten / 15 wijken
  (matches Spike A) + 519 CBS buurten; all geometry **SRID 28992**. Cold load ~8 min; on-disk cache
  3.4 GB extract + 1.5 GB inner zips.

## Finding 1 — the BAG bulk load path works; load it as-is

The national *LV BAG 2.0 Extract* loads through GDAL's `lvbag` driver into PostGIS. Concretely
(what the design docs did not yet pin down):

- **The extract is national-only and ~3.6 GB** (`DATA_SOURCES.md` §8 said ~1.5 GB). The free PDOK
  atom feed (`https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml`) serves one file,
  `lvbag-extract-nl.zip` (3,610,187,048 bytes), a nested zip: outer → per-object-type inner zips,
  each holding many chunked XML files. No per-gemeente extract is downloadable without an ordered
  Kadaster BAG-Extract account, so **filtering happens at load, on municipality only**:
  `ogr2ogr … -where "identificatie LIKE '%.0363%'"`.
- **Load every object type that carries an address or a street name; drop only whole tables we don't
  need.** BAG has **three *adresseerbaar object* types** — `verblijfsobject` (point),
  **`ligplaats`** (houseboat berth) and **`standplaats`** (mobile-home plot), the last two polygons
  reachable via `hoofdadresNummeraanduidingRef`. Loading all three (+ `nummeraanduiding` and
  `openbareruimte`) is **complete address-point coverage** — Amsterdam's houseboats resolve, not
  just buildings. **Skipped** (whole tables, no address lost): `pand` (2.0 GB — building *footprints*
  only; every address point already lives on VBO/LIG/STA), `woonplaats` (city names), and the
  auxiliary `Inactief`/`InOnderzoek`/`NietBag`/`GEM-WPL` files (withdrawn objects are already in the
  main files, see Finding 2). Skipping `pand` also avoids the biggest single parse.
- **`lvbag` reads only the Standaard-Levering (ST) snapshot; it does NOT apply the daily
  Mutatie-Levering (ML) files.** `DATA_SOURCES.md` §8's "monthly full + daily mutaties keeps it
  current incrementally … via `lvbag`" is wrong. **Refresh = idempotent monthly full reload** (fits
  the plan's "re-running any stage is a no-op"); a custom ML applier / NLExtract is only needed if
  daily freshness ever is.
- **The table/column shape** the `location/` resolver builds on: `bag_openbareruimte` — `naam`
  (street), no geometry; `bag_nummeraanduiding` — `postcode`, `huisnummer`/`huisletter`/
  `huisnummertoevoeging`, `openbareruimteref`, **no geometry of its own**; `bag_verblijfsobject` —
  the address **point `geom`**, `hoofdadresnummeraanduidingref`; `bag_ligplaats`/`bag_standplaats` —
  address **polygon** (centroid → point), same ref. An address's point is reached NUM → VBO/LIG/STA.
- **Load traps** (each cost a re-run here): `identificatie` is the **IMBAG-URI form**
  `NL.IMBAG.<Type>.0363…` — the gemeente filter is `'%.0363%'`, not `'0363%'`; `-oo
  AUTOCORRECT_INVALID_DATA=YES` handles invalid geometries; a few far-future dates (`2208-05-15`) are
  warned and nulled. Cold-load time is dominated by NUM+VBO (all-NL XML is streamed even though only
  `0363` is written): **~8 min** on this machine, then cached in the volume.

## Finding 2 — the BAG temporal dimension (why we load all voorkomens)

BAG is **bitemporal**, exactly like our own audit model (`IMPLEMENTATION_PLAN.md` §"Temporal
model"): each object identificatie is timeless, and its state is a sequence of **voorkomens** with
**valid-time** (`begingeldigheid`/`eindgeldigheid` — when the state held in the world) and
**transaction-time** (`tijdstipregistratie`/`eindregistratie` — when the LV recorded it). Two
consequences settle how we must load and query it:

- **A superseded voorkomen is a real change *or* a technical correction, and BAG distinguishes
  them.** Of the 163,644 (26%) of `0363` addresses with >1 voorkomen: **142,475 (87%) are real-world
  changes** (valid-time `begingeldigheid` advances) and **21,169 (13%) are pure corrections** (same
  valid-time, only re-registered — `eindregistratie` set). So the answer to "is this a real change
  or a technical fix?" is *both occur, told apart by whether valid-time moves*; you take the
  best-known version with **`eindregistratie IS NULL`**.
- **"No longer valid" (demolished/withdrawn) is NOT a dropped row — it is a `status` change.** An
  address withdrawn in 2015 still sits in the file with a point, its latest voorkomen flagged
  `status='Naamgeving ingetrokken'` (e.g. `NL.IMBAG.Nummeraanduiding.0363200000006551`, 1094KT 122).
  The current-state file already contains **39,903 VBO + 48,966 NUM "ingetrokken"** objects. So
  filtering to the latest voorkomen never hides demolished addresses — and we should *not* filter on
  `status`, or a 2022 permit pointing at a since-demolished address would fail to resolve.
- **Therefore: load ALL voorkomens (as-is) and resolve at the intervention's valid-time.** We audit
  backdated permits, so a 2022 permit must be matched against the **2022** address state, not
  today's. Concretely it matters: of 353 permits with a postcode+huisnummer, **329 resolve to the
  address state valid at the 2022 publication date** vs **335** to a currently-active address, and
  **3 point at addresses that were valid in 2022 but are withdrawn now** (recovered only by the
  temporal query); the gap the other way is new-build addresses that did not yet exist in 2022. The
  effect is small over 2022→2026 but it is exactly the correctness the audit's temporal integrity
  needs, and it grows for older permits and in renewal areas where demolition is common.

This makes the local BAG a temporal reference store; `IMPLEMENTATION_PLAN.md` §"Temporal model"
already commits us to bitemporality, and BAG is a concrete external source that must be queried that
way, not just an internal concern.

## Finding 3 — point-in-polygon is exact, and free-text → BAG resolves at address precision

**Point-in-polygon (needs no BAG):** placing each felled tree's `stamgegevens` point into a
`gebieden` buurt with `ST_Contains` reproduces the row's own `gbdBuurtId` in **939/939 = 100.0%** of
cases — the gebieden polygons, the RD (28992) CRS and the spatial join are all correct (a mismatch
would mean silently-empty `ST_Contains` downstream). *Coverage caveat:* only **939 of 1,298** felled
Noord rows still resolve to a `stamgegevens` point — ~28% of felled trees are gone from the
standing-tree registry (removed after felling), expected and reportable. The same local join
replaces Spike B's **remote** "vote the nearest standing trees' `gbdBuurtId`" stand-in: placing each
permit's own point locally agrees with Spike B's vote in **86.8%** (317/365) of permits (0 outside
the Noord polygons); the 48 disagreements are boundary cases where exact containment beats the
nearest-tree heuristic — the offline point-in-polygon is the *more* correct of the two.

**Address resolution — Spike D's contribution to the joint audit.** Spike B linked permit↔registry
at buurt-level place (base `0.50`) and showed finer place promotes weak links. Spike D supplies it,
locally and at valid-time. Each of the 365 permits is resolved to the finest place its title address
reaches against the local BAG *as it was at the permit's publication date*, validated against the
permit's **own** published RD coordinate (Spike B) as ground truth:

| Place level | Permits | % | Spike B place score |
|---|---|---|---|
| **address** (postcode/street + huisnummer → VBO/LIG/STA point) | 347 | 95.1% | 0.90 |
| **postcode** (PC6 in BAG, huisnummer unresolved) | 4 | 1.1% | 0.70 |
| **buurt** (own point-in-polygon only) | 14 | 3.8% | 0.50 |
| **unresolved** | 0 | 0% | — |

- **Try valid-time first, then fall back to any-time — but record which on the relation.** Permit
  data can be unreliable (a wrong or batch date, an approximate address), so refusing a link because
  the date doesn't line up would needlessly lose location. Instead we match any best-known voorkomen
  and rank valid-at-date above any-time, storing the outcome as **`time_match`** on the resolved row
  (→ a caveat on the Phase-1 `AuditLink`): **330 links are `valid_at_date`** (resolved at the permit's
  own date) and **17 (4.7%) are `any_time`** — recovered by the fallback and flagged as temporally
  weak, not silently trusted. All 17 sit a median 0.0 m from the permit's own point, so they are
  spatially right, just date-mismatched (typically a permit that predates the address's
  `beginGeldigheid` — a new-build plot). This lifts address resolution 90.4% → **95.1%** while keeping
  the weakness explicit.
- **Address matches land a median 0.0 m** (mean 40.3 m) from the permit's own point — local BAG
  resolution and the permit's own geometry agree, usually to the exact building. Method: 345 via
  `(postcode, huisnummer)`, 1 exact street, 1 `pg_trgm` fuzzy street. The mean is pulled up by a few
  permits whose own point is a project-area centroid rather than the exact address (max 2,531 m).
- **Reference addresses ("nabij / t.h.v.") behave as designed.** All 5 reference-marked permits
  resolve to a BAG **address** (3 at valid-time, 2 via the any-time fallback) — a reference/near
  address resolves via the address or the permit's own point, never a false exact match
  (`IMPLEMENTATION_PLAN.md` §4; `DATA_THREAD_TREES.md` Hop 2). The classic demolished-in-renewal case
  (Egeldonk 50) is a **Zuidoost** case not in this Noord sample; the fallback path is implemented and
  exercised by the 14 buurt-tier permits. Honest limit.
- **Registry cross-check:** **92.9%** (872/939) of the felled rows' `dichtstbijzijndeBagAdres` +
  postcode resolve to a BAG nummeraanduiding — the registry's nearest-address strings are clean.

Net: local BAG resolution promotes 95% of permits from buurt-only (`0.50`) to address (`0.90`) place,
exactly the lift Spike B's model consumes from persisted candidates — **no Locatieserver, no
gazetteer, no re-fetch.** `permit_resolved` (with `time_match` per link) is left in the DB for that
re-score.

---

## Decision — the local resolver, confirmed; and the corrections

**The no-Locatieserver decision holds (`IMPLEMENTATION_PLAN.md` §4).** Everything the hosted service
offered — free-text → BAG, postcode/buurt lookup, point-in-polygon — is derived here from the
bulk-loaded BAG + `gebieden`/CBS polygons. The `location/` resolver ladder, as validated:

1. **address** — normalize `{street, huisnummer[+letter/toevoeging], postcode}`; match
   `bag_nummeraanduiding` on `(postcode, huisnummer)`, else exact street via `bag_openbareruimte.naam`,
   else `pg_trgm` fuzzy street; the point is the adresseerbaar object (VBO point / LIG·STA centroid)
   via `hoofdadresnummeraanduidingref`. Place `0.90`. **Prefer the voorkomen valid at the
   intervention date; if none matches, fall back to any best-known voorkomen and record
   `time_match = any_time` on the link** (a `timeMismatch` caveat on the `AuditLink`) — a permit still
   links to a location even when its date is off, but the weakness is captured, not hidden.
2. **postcode** — PC6 in BAG but the huisnummer doesn't resolve → place `0.70`.
3. **buurt** — no address/postcode → `ST_Contains` the permit's own point against `gebieden_buurten`;
   tag `unresolvedLocation`, place `0.50`.

Voorkomen selection is best-known (`eindregistratie IS NULL`), ranked by valid-time
(`begingeldigheid <= D AND (eindgeldigheid IS NULL OR eindgeldigheid > D)`) then distance; the
time-match quality is stored on the relation. Score point-in-polygon against the **`gebieden`**
polygons (keyed by `gbdBuurtId`), not CBS — CBS is a cross-reference only.

**Why load BAG as-is (full tables, all columns, all voorkomens, municipality filter only).** BAG is
the authoritative **master data** for addresses, buildings and the geography ladder — the reference
every source is tied to on location, and the thing we most need to keep correct and complete. So we
mirror it faithfully rather than pre-filtering to today's needs: dropping columns or rows now bakes in
assumptions we would have to unwind later (a different vertical needs `gebruiksdoel`, a backdated audit
needs an old voorkomen, a demolished address must still resolve). The one safe reduction is
municipality (we only audit Amsterdam) and leaving out **whole** object types we provably don't use
(building footprints), since that removes no address and is trivially reversible. Load-time filtering
beyond municipality, or column projection, is disallowed by rule.

**Corrections to feed back (applied to the design docs alongside this spike):**
- `DATA_SOURCES.md` §8 — extract is **national-only, ~3.6 GB** (not ~1.5 GB); **`lvbag` is
  ST-snapshot-only — daily ML mutaties are unsupported by the driver** → refresh = idempotent
  monthly full reload. **BAG is bitemporal**: load all voorkomens (municipality filter only, no
  column drop), resolve at valid-time (`eindregistratie IS NULL`); withdrawal is a `status` change,
  not a dropped row. Load traps: `'%.0363%'` IMBAG-URI filter; `AUTOCORRECT_INVALID_DATA=YES`. Load
  the three adresseerbaar types (VBO/LIG/STA) for complete address-point coverage; `pand` footprints
  are optional.
- `DATA_SOURCES.md` §0 & §8 — fill the two documented gaps: **CBS "wijken en buurten" WFS** =
  `https://service.pdok.nl/cbs/wijkenbuurten/2024/wfs/v1_0` (layers `wijkenbuurten:buurten`/`:wijken`,
  default CRS **EPSG:28992**, filter `gemeentecode='GM0363'`); **`gebieden` polygons** come from the
  Datapunt API with `_format=geojson` + `Accept-Crs: EPSG:28992` (keyed by `identificatie` =
  `gbdBuurtId`).
- `IMPLEMENTATION_PLAN.md` §"Temporal model" — BAG is a concrete bitemporal *reference* source; a
  backdated intervention's location must be resolved against the BAG state valid at its date. §6 —
  mark **Spike D DONE** with the load recipe + rates. §4 — the local resolver is validated
  (90% address precision, 100% point-in-polygon).

## Files

- `compose.yaml` — PostGIS + GDAL containers, one network, DB on a persistent volume.
- `harvest.py` — stdlib; reuses `../spike-a` + `../spike-b` outputs; writes the RD `gebieden` buurt/
  wijk polygons, the felled-tree points (`boomId→stamgegevens`), and the permits-as-points (own
  rd_x/rd_y + parsed title address) into `data/` (all `.gitignore`d).
- `load.sh` — incremental/resumable: `lvbag`→PostGIS (`0363`, all voorkomens, all columns) for
  OPR/NUM/VBO/LIG/STA; `gebieden` GeoJSON + CBS WFS + tree/permit points; indexes (GIST + `pg_trgm`);
  sanity gates. `--reset` rebuilds.
- `sql/pip.sql` — point-in-polygon accuracy vs `gbdBuurtId` ground truth (Finding 3).
- `sql/resolve.sql` — the valid-time-aware address→BAG resolver + place-granularity distribution +
  distance-to-own-point validation + the temporal comparison (Findings 2–3); builds `permit_resolved`,
  kept for Spike B's re-score.
