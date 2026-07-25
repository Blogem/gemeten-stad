# Data sources — Amsterdam public-space interventions

Catalog of the public data sources for the claims-vs-observations platform ("De Gemeten
Stad"): what each source is, how to query it, and the quirks found while probing.
**Every endpoint, example query, and sample value in this document was verified live on
2026-07-25.** All sources are free; all except EP-Online work without any key today.

Companion document: `DATA_THREAD_TREES.md` — a fully worked end-to-end example using the
tree registry as the core dataset.

---

## 0. The geography spine (read this first)

Every source keys geography differently. The official aggregation ladder and the
identifier systems on it:

```
address (BAG id)  →  postcode-6  →  buurt  →  wijk  →  stadsdeel  →  gemeente
```

| Identifier system | Example | Used by |
|---|---|---|
| BAG nummeraanduiding / verblijfsobject | `0363200000152534` | EP-Online, BAG itself |
| Postcode-6 | `1024AK` | Liander, bekendmakingen metadata |
| CBS buurt/wijk/gemeente codes | `BU0363TE01`, `WK0363NJ`, `GM0363` | CBS statistics, politie crime |
| Amsterdam "gebieden" ids (14-digit) | buurt `03630980000509`; stadsdeel Noord `03630000000019` | all Amsterdam Datapunt datasets (`gbdBuurtId`); covers buurt/wijk/stadsdeel |
| Point coordinates | RD (EPSG:28992) or WGS84 | Luchtmeetnet, bekendmakingen geometry, trees |

**Two bridges tie these together:**

1. **The local BAG (bulk-loaded)** — surface form → the whole ladder. A free-text or
   reference address resolves to a BAG object (nummeraanduiding / verblijfsobject) and,
   through it, to postcode-6, buurt, wijk, stadsdeel, and a point/footprint geometry. We do
   this resolution **locally, against the bulk-loaded BAG (§8) — not via the hosted PDOK
   Locatieserver.** The fuzzy permit-address → BAG matching is core project work we own, and
   everything the Locatieserver offered (free-text → BAG, postcode/buurt lookup) is derivable
   from the local BAG + gebieden + CBS geometries. This is a settled decision — there is
   deliberately no Locatieserver fallback (`IMPLEMENTATION_PLAN.md` §4).

   *Quirk:* reference addresses from permit texts ("**t.h.v.** Egeldonk 50" — "near
   Egeldonk 50") may not resolve to a BAG address at all (e.g. building demolished in an
   urban-renewal area); resolution then falls back to a street/point match with geometry
   only. Resolve with a point-in-buurt-polygon fallback, never assume an address match.

2. **Amsterdam gebieden API** — Amsterdam id ↔ CBS code:

   ```
   curl "https://api.data.amsterdam.nl/v1/gebieden/buurten/?_format=json&identificatie=03630980000509&_fields=identificatie,naam,code,cbsCode,ligtInWijkId"
   ```

   Returns (verified): `naam: "E-buurt"`, `cbsCode: "BU0363TE01"`. Every Datapunt record
   carrying `gbdBuurtId` is thus one call away from the CBS key space.

Point-in-polygon polygons (verified in Spike D, now `load/geo` / `location/`):
- **`gebieden` buurt/wijk polygons** — from the Datapunt API itself, `GET
  /v1/gebieden/buurten/?_format=geojson` (and `/wijken/`) with `Accept-Crs: EPSG:28992`; keyed by
  `identificatie` = the `gbdBuurtId` the registry uses, so this is the **primary** set for scoring.
- **CBS "wijken en buurten"** — PDOK WFS `https://service.pdok.nl/cbs/wijkenbuurten/2024/wfs/v1_0`
  (layers `wijkenbuurten:buurten`/`:wijken`/`:gemeenten`, default CRS **EPSG:28992**, filter
  `gemeentecode='GM0363'`); one URL per vintage year. A cross-reference only — the ladder the
  registry indexes is `gebieden`, and gebieden/CBS boundaries differ at water/harbour.

---

## 1. Officiële bekendmakingen (KOOP) — the intervention document stream

The legally mandated publication channel for every municipal decision: verkeersbesluiten,
omgevingsvergunningen (bouw/kap), evenementenvergunningen, zoning notices. **This is the
unstructured side of the platform.** Continuous feed; Amsterdam alone has **25,342
verkeersbesluiten**, with ~90 EV-charging-spot decisions in June–July 2026 alone.

### Search (SRU 2.0)

```
curl -G "https://repository.overheid.nl/sru" \
  --data-urlencode "operation=searchRetrieve" \
  --data-urlencode "version=2.0" \
  --data-urlencode "maximumRecords=8" \
  --data-urlencode 'query=(dt.creator any "Amsterdam" AND dt.type any "verkeersbesluit" AND cql.textAndIndexes any "oplaadpunt opladen laadplaats" AND dt.available>="2026-06-01")'
```

- Index discovery: `?operation=explain` lists **192 indexes**. The load-bearing ones:
  `dt.creator` (publishing authority), `dt.title`, `dt.type`, `dt.available`
  (publication date, supports `>=`/`<=` ranges), `cql.textAndIndexes` (full text),
  and the verkeersbesluit-specific set `w.typeVerkeersbesluit`, `w.vereisteVanBesluit`
  (statutory basis!), `w.verkeersbordcode`, `w.wegcategorie`, `w.weggebruiker`,
  `w.postcode`, `w.gemeentenaam`.
- Result records carry `dcterms:identifier` (e.g. `gmb-2026-291126`), title, dates.

### Retrieve documents

```
# full document XML (structured: kop, lijst, al paragraphs)
https://repository.overheid.nl/frbr/officielepublicaties/gmb/<year>/<id>/1/xml/<id>.xml
# structured metadata sidecar
https://zoek.officielebekendmakingen.nl/<id>/metadata.xml
```

### The verkeersbesluit metadata sidecar is a gift

Verified content for `gmb-2026-291126` (charging spots, Jisperveldstraat 201):

- `OVERHEIDvb.typeVerkeersbesluit = "aanwijzen parkeerplaats voor het opladen van
  elektrische voertuigen"` — a controlled scheme with a value *specifically for EV
  charging designations*
- `OVERHEIDvb.vereisteVanBesluit = "Het bepaalde in artikel 12 van het BABW"` —
  statutory basis as data
- `OVERHEIDop.gebiedsmarkering` + `OVERHEIDop.geometrie = POINT(125254 490145)` —
  **geometry in RD coordinates (EPSG:28992)**, sometimes MULTILINESTRING for road works
- `OVERHEIDop.postcode = 1024AK`
- road category + affected road-user classes, also controlled schemes

The quantitative content, however, lives **only in the body prose** — e.g. the verified
placement rule *"in de buurt Markengouw-Noord de maximaal toegestane bezettingsgraad van
65% gedurende zes maanden 52 uur is overschreden"*. Extracting that is the NER/relation
job; the metadata gives the intervention typing for free.

### Quirks

- **Unknown index or no match → silently 0 records, never an error.** Always sanity-check
  clause-by-clause hit counts when a combined query returns 0.
- `dt.creator any "Amsterdam"` (355k docs) is the publishing authority;
  `w.gemeentenaam` (14k docs) is a *different, sparsely populated* location field — don't
  confuse them. Prefer `any`/`all` relations; exact `==` frequently misses.
- **Title phrasing is per-gemeente house style.** Amsterdam writes "aanleg twee
  elektrische oplaadvakken", not "laadpaal" (a title search for "laadpaal" +
  creator=Amsterdam found only legacy-Weesp docs — Weesp merged into Amsterdam).
  Full-text (`cql.textAndIndexes`) with synonym lists beats title search.
- **Kap omgevingsvergunningen carry MORE metadata than first thought** (corrected by
  Spike B, `spikes/spike-b/`): the SRU record + `metadata.xml` expose a **point geometry in
  both RD and WGS84** (`overheidwetgeving:geometrie` / `locatiepunt`), a **controlled
  `OVERHEIDop.activiteit`** (`kappen`), the **zaaknummer** (`OVERHEIDop.referentienummer`,
  whose prefix encodes the stadsdeel — `Z2022-N…` = Noord), and an abstract with the tree
  count — all present ~99% of the time over the 2022 corpus. So *place* and *activity* are
  **structured**, not free text; only the **tree count** lives in prose — but it needs a
  **deterministic parser, not NER** (corrected by Spike C, `spikes/spike-c/`). The abstract is
  formulaic ("het `<verb>` van `<N>` bomen …") and *is* the document's `Omschrijving` line, so
  parsing it lifts the count yield from a naive largest-int floor **~50% → ~70%** of besluiten
  (reading spelled-out numbers + the `houtopstand` noun) — and, critically, it states **per-activity
  counts that must be split, not summed**: "vellen van 33 bomen en verplanten van 43 bomen" is
  {vellen 33, verplant 43}, felling total 33; the naive largest-int returns 43 (it conflates the two
  and can grab a *herplant* promise as the felling count). Species (<10% in prose) and project (~1%)
  are the fuzzy NER residue (§2a: 0% registry-side). The free-text "t.h.v." reference address is a
  fallback for the finer-than-buurt rung, not the primary locator. (Bouw omgevingsvergunningen not
  re-probed; the thin-metadata caveat may still hold for those.)
- **The kap publication body is a stub** (Spike A, `spikes/spike-a/`): an *aanvraag* or
  *besluit* notice carries activity + address (+ count/zaaknummer for a besluit) and **no
  permit conditions and no replant termijn** — the besluit itself is "per e-mail" only, not
  published. The **only** date-term in the body is *"binnen 6 weken"*, the **bezwaar (appeal)
  window — a decoy**; never read it as a replant deadline. Any stated termijn/herplant rule
  lives in *policy* documents (the beleidsregel *'Compensatie en herplant van bomen'*), not in
  the individual permit.
- Terminology varies for the same activity: *kappen* / *vellen* / *verplanten* /
  "houtopstanden". A project-level permit may not mention any affected street by name —
  see the negative-result finding in `DATA_THREAD_TREES.md`. **`verplanten` is not a lighter
  event: the Bomenverordening (§10) defines *vellen* to include *verplanten***, so it triggers
  herplantplicht identically and counts toward the felling obligation (Spike C).
- `w.postcode` exists as an index but was empty for the probed omgevingsvergunningen —
  populated mainly for verkeersbesluiten.

**Cadence:** continuous (publications appear same-day; newest hit in probing was
published 2 days before the probe). No auth. XML responses.

---

## 2. Amsterdam Datapunt APIs (`api.data.amsterdam.nl/v1/`) — the municipal registries

REST ("DSO") APIs over ~80 datasets. The ones probed:

### 2a. Trees — `bomen` (core dataset of the worked thread)

Sub-datasets: `stamgegevens` (per-tree master data), `kapenherplant` (felling +
replanting lifecycle), plus `gebrekregistratie`, `stormmeldingen`,
`veiligheidsinspecties`, `maatregelregistratie`.

```
# trees within 150 m of a point (WGS84 needs the Accept-Crs header!)
curl -H "Accept-Crs: EPSG:4326" \
  "https://api.data.amsterdam.nl/v1/bomen/stamgegevens/?geometrie%5Bwithin%5D=POINT(4.95015%2052.39834),150&_pageSize=3&_count=true&_fields=id,soortnaamTop,jaarVanAanleg,gbdBuurtId"

# felling/replanting lifecycle, filtered
curl "https://api.data.amsterdam.nl/v1/bomen/kapenherplant/?_count=true&gbdBuurtId=03630980000509&kapmaatregelDatumUitgevoerd%5Bgte%5D=2024-01-01"
```

- `stamgegevens` (verified sample): species (`soortnaam`, `soortnaamTop`), plant year
  (`jaarVanAanleg`), height/diameter classes, owner type, point geometry, `gbdBuurtId`.
  ~1M trees in the city, ~300k municipally managed. Buurt-level counts via
  `gbdBuurtId=` + `_count=true` (E-buurt: 1,022 municipal trees).
- `kapenherplant` (verified sample): **the full audit lifecycle per tree** —
  `datumVergunningsaanvraag` → `datumVergunningVerleend` → `kapmaatregelDatumUitgevoerd`
  → `plantmaatregelDatumUitgevoerd`, plus `dichtstbijzijndeBagAdres`/`Postcode`,
  `toeTePassenBoomsoort` (species that must be replanted), `datumAfrondenVoor`,
  `boommaatregelBesluit` ("Vellen (boom verwijderen)"), `gbdBuurtId`. 3,593 records with
  felling executed since 2024-01-01 (citywide).
  - **`boommaatregelBesluit` enum, pulled city-wide (all 35,202 rows, Spike C):** the only
    meaningful value is **`Vellen (boom verwijderen)`** (7,833) — plus an `Ecoscan - Vellen`
    variant (965), 3 typo singletons, and ~75% null/empty. **There is no `Verplanten` value**:
    the registry cannot record a transplant as anything but a felling, so `verplanten` permits
    are logged as `Vellen` (confirming §10's legal definition and settling the audit mapping).
    The fulfilment axis is **`boomAanwezigheid`**: `Nee` (18,360) / `Ja, nieuwe boom reeds
    aangeplant` (5,449) / `Ja` (5,279) / `Niet te beoordelen` (677) / null (5,436).
    `toeTePassenBoomsoort` / `soortnaam` / `projectnaamBomen` are **0% populated city-wide** — so
    species and project exist *only* in the permit prose (§1), never registry-side.
  - **The record is schema `v3` and carries far more date fields than the published schema
    lists** (verified via `spikes/spike-a/`): also `datumBesluitVergunningKap`,
    `datumEindeBezwaar`, `datumAkkoordBoomsoort`, `groeiplaatsmaatregelDatumUitgevoerd`,
    `datumHerplantinspectie`, `datumToezichtHerplantinspectie`, `kapDatumToezicht`,
    `plantenDatumToezicht`, `inspectiedatum`, `mutatiedatum`, plus work-order codes
    `deelopdrachtGroeiplaats`/`deelopdrachtPlanten`. Only `kapmaatregelDatumUitgevoerd` (felling)
    is 100% populated; none of the fields is a usable replant *deadline* (Spike A).

**Quirks (bomen):**
- `datumAfrondenVoor` is a **work-order step deadline, not a replant due date** — quantified
  over stadsdeel Noord (Spike A): populated in only 53% of felled rows, **precedes the felling
  date in 53%** of those (median −6 days), and where a replant actually landed it was overshot
  in **100%** of cases (median 467 days late). Do not use it as the herplant termijn. The robust
  audit signal is `kapmaatregelDatumUitgevoerd` set + `plantmaatregelDatumUitgevoerd` null +
  elapsed time.
- **Both "permit granted" dates are batch-assigned** — `datumVergunningVerleend` *and*
  `datumBesluitVergunningKap` each take only ~16 distinct values over 796 Noord rows (and differ
  from each other 100% of the time). Treat as administrative, not as the bekendmaking date;
  anchor elapsed-time reasoning on the felling date instead.
- `soortnaam`/`toeTePassenBoomsoort` are frequently null in `kapenherplant`; join to
  `stamgegevens` via `boomId` (with the `boomNieuwId` fallback below) for species.
- The `[isnull]` filter operator did not work in probing (empty response, no error) —
  filter null lifecycle dates client-side.
- **`kapenherplant` rejects spatial filters** (`geometrie[within]` → HTTP 403); only
  `stamgegevens` accepts them (Spike B). To place a permit point at buurt level pre-BAG,
  query `stamgegevens` near the point and vote the `gbdBuurtId` of the nearest standing trees.
- **Resolving a felled tree to a point needs a `boomNieuwId` fallback** (Spike B). Because
  `kapenherplant` carries no queryable geometry (the 403 above), a felled row's coordinates come
  only from joining `boomId` → `stamgegevens`. But `boomId` alone resolves just **~71%** of felled
  Noord rows: when a replacement tree is planted the registry **retires the original `boomId` and
  issues a `boomNieuwId`** for the new tree, so the old id vanishes from `stamgegevens` (the
  `boomAanwezigheid = "Ja, nieuwe boom reeds aangeplant"` cases — 170/1,298 in Noord — resolve
  0% via `boomId`). Resolve `boomId` first, then fall back to `boomNieuwId` on a miss → **~98%**
  resolution. Note this is *not* "vellen removed it": removed (`Nee`) trees stay in the snapshot
  and resolve fine; it is specifically the replant → new-id swap that breaks the join — i.e. the
  misses are exactly the audit-interesting *replanted* trees, so `boomId`-only silently drops them.
- **`kapenherplant` has no permit-reference field** — no zaaknummer/OLO/dossier; `projectnaamBomen`
  and `selectiecode` exist but are 0% populated in Noord (Spike B). Hence permit↔registry linkage
  is fuzzy resolution, never a key join.

### 2b. Parking spots — `parkeervakken`

```
curl "https://api.data.amsterdam.nl/v1/parkeervakken/parkeervakken/?format=json&_pageSize=50&straatnaam=Jisperveldstraat"
```

Per-spot records: `type` (Langs/Haaks), `soort` (FISCAAL/NIET FISCAAL/MULDER), `eType`
(E-sign designation: E6a/E6b disabled, E8 category-restricted, etc.), street, geometry.
**Quirk:** registry lags fresh verkeersbesluiten — spots designated in a June decision
were not yet registered in July. Decision→registry latency is itself measurable.

### 2c. Other catalog entries confirmed to exist (not probed in depth)

`aardgasvrijezones`, `energieverbruik`, `meldingen` (public-space complaints), `bbga`
(Amsterdam's own buurt statistics), `milieuzones`, `wagenpark`, `evenementen`,
`crowdmonitor`, `gebieden` (the id bridge, §0), `bag`, `woz`, `vergunningen`,
`stroomstoringen`, `ecologie`. Catalog: `https://api.data.amsterdam.nl/v1/docs/index.html`.

**Cross-cutting Datapunt mechanics:**
- Default CRS is **RD (EPSG:28992)** — send `Accept-Crs: EPSG:4326` for lat/lon
  (geometry filter syntax: `geometrie[within]=POINT(lon lat),meters`).
- Filters: `field=`, `field[gte]=`, `field[lte]=`, `field[like]=`; projection `_fields=`;
  `_count=true` for totals; `_pageSize`/`page=`; `_format=json|csv|geojson`.
- No auth today; docs signal a **mandatory (free) API key is coming soon** — the exact date
  is not yet decided — and recommend provisioning one now (`X-Api-Key` header).
- Freshness: sourced from the municipal asset systems; `mutatieDatum`/`lastupdate`
  values observed days-to-months old depending on dataset.

---

## 3. Police crime statistics (OData v3)

Monthly registered crimes per buurt, 2012→now, ~1 month lag.

```
# table: 47022NED (monthly), 47018NED (annual); catalog: dataderden.cbs.nl/ODataCatalog
curl "https://dataderden.cbs.nl/ODataApi/odata/47022NED/TypedDataSet?\$format=json&\$filter=WijkenEnBuurten%20eq%20'BU0363TE01'%20and%20SoortMisdrijf%20eq%20'0.0.0%20'%20and%20Perioden%20eq%20'2026MM06'"
```

Verified: E-buurt `2026MM06` → 8 misdrijven; crime-type breakdown via the
`SoortMisdrijf` dimension (`1.1.1` woninginbraak, etc.).

**Quirks:**
- `SoortMisdrijf` keys have a **trailing space** (`'0.0.0 '`) — mandatory in filters;
  `GM` codes are space-padded to 10 chars.
- **`$orderby` is silently ignored** — enumerate the `Perioden` dimension endpoint for
  the latest period instead.
- OData **v3** dialect (`substringof(...)`, not v4 `contains`).
- The table is enormous; never query unfiltered (an unfiltered `$top` pages from 2012).
- The `WijkenEnBuurten` dimension endpoint doubles as a name→code lookup and carries a
  `Municipality` field for enumerating all buurten of `GM0363`.

---

## 4. CBS Kerncijfers wijken en buurten (OData v3)

Annual socio-demographic profile per buurt: population, households, cars/household,
income, housing. **One table per vintage year**: `86165NED` (2025), `85984NED` (2024),
`85618NED` (2023), … discover via the catalog.

```
curl "https://opendata.cbs.nl/ODataApi/odata/86165NED/TypedDataSet?\$format=json&\$filter=WijkenEnBuurten%20eq%20'BU0363TE01'&\$select=WijkenEnBuurten,AantalInwoners_5,HuishoudensTotaal_29,PersonenautoSPerHuishouden_107"
```

Verified: E-buurt 2025 → 2,365 inhabitants, 1,025 households, 0.9 cars/household.

**Quirks:**
- **Column-name suffixes drift between vintages** (`GemiddeldInkomenPerInwoner_78` in
  2025 vs `_81` in 2023) — resolve column names per table via its `DataProperties`
  endpoint; never hardcode.
- Income fields are **null in the newest vintage** (backfilled ~2 years later) — read
  income from an older vintage.
- Small-cell privacy suppression → nulls; string values right-padded with spaces.
- Same `WijkenEnBuurten` key space as the police table — direct joins.

---

## 5. Luchtmeetnet air quality (REST)

Hourly measurements (NO2, PM2.5, PM10, O3, …) from official stations; near-real-time
(the 00:00 UTC value was retrievable at 01:56 UTC).

```
curl -sL "https://api.luchtmeetnet.nl/open_api/measurements?station_number=NL49003&formula=NO2&start=2026-07-24T00:00:00Z&end=2026-07-25T00:00:00Z"
```

Verified: NL49003 (Amsterdam-Nieuwendammerdijk, GGD) NO2 23.1 µg/m³ at 2026-07-25T00:00Z.

**Quirks:**
- The API **302-redirects** (`api.` → `iq.luchtmeetnet.nl`) — plain curl gets an empty
  body; always `-L`. Omitted start/end auto-injects a 7-day window.
- Max ~1 week per request; paginated.
- **Point coordinates only, ordered [lon, lat]** — no buurt key; ~11 Amsterdam stations,
  so buurt mapping is nearest-station or point-in-polygon, and coverage is sparse
  (a buurt gets its nearest station, not its own). Fair-use limit ~100 req/5 min.

---

## 6. Liander kleinverbruik (annual energy per postcode-6)

Standardized annual electricity (SJV/SJA, kWh) and gas (m³) consumption per postcode-6
range, plus connection counts and type. Reference date Jan 1, one file per year.

```
# 2025+ (slim format):
https://www.liander.nl/-/media/files/open-data/kleinverbruikdata/verbruiksdata-kv-2026.csv   (13.8 MB, 268,815 rows)
# ≤2024 (classic format):
https://www.liander.nl/-/media/files/open-data/kleinverbruikdata/kleinverbruikgegevens-<year>.zip
```

Verified sample (2026 file): `1024AA ELK 1x25 → SJA gemiddeld 1913 kWh`;
`1024AA-1024AB GAS G4 → 26 m³` (a nearly-gasless postcode — the energy-transition signal
at its rawest).

**Quirks:**
- **Format break at 2025**: new files are tab-separated *with each whole line wrapped in
  double quotes* (parse accordingly) and slimmer; classic files are semicolon-separated,
  space-padded, and richer (street name, city, `SLIMME_METER_PERC`, low-tariff %).
- Only **2019–2026 hosted** on liander.nl today (2009–2018 URL patterns 404) — source
  older years from mirrors if trends need them.
- 2024's zip has an inconsistent filename spelling (`kleinverbruiksgegevens-2024.zip`).
- ELK and GAS are separate rows; postcode *ranges* aggregate small groups (k-anonymity
  ≥10 connections) — model suppression, don't impute.
- Same page hosts **terugleverdata** (solar feed-in, 2023–2026) and a decentral-PV
  dataset. Liander covers Amsterdam; other DSOs (Stedin, Enexis) publish equivalents for
  other regions.

---

## 7. EP-Online energy labels (RVO) — per-address, needs a free key

The national register of energy labels, **per address with BAG ids**.

- **Access:** free self-service API key at `https://apikey.ep-online.nl/` (email
  activation, ~5 minutes, no account). No keyless path (verified: file download → 400,
  API → 401 without key).
- **Files:** monthly full snapshot (`v20260701_v4_csv.zip`, ~226 MB zipped) + **daily
  mutation files** (25–430 KB) — a natural incremental-ingest design forcing function.
- **API:** `GET /api/v5/PandEnergielabel/Adres?postcode=&huisnummer=` per-address;
  swagger at `https://public.ep-online.nl/swagger/v5/swagger.json` (publicly readable).
- **Fields:** `Postcode, Huisnummer, BAGVerblijfsobjectID, BAGPandIDs, Bouwjaar,
  Energieklasse, EnergieIndex, BerekendeCO2Emissie, Registratiedatum, Geldig_tot,
  Gebouwklasse, Gebouwtype, …`
- **Quirk:** current file schema is v4 while the API is v5 (near-identical fields).

---

## 8. BAG bulk extract (Kadaster LV BAG 2.0) — the local place backbone

The national address/building register, **bulk-loaded locally** so all location resolution
(§0) runs against our own copy — no PDOK Locatieserver dependency. **Characterised in Spike D
(verified 2026-07-25, now implemented in `ingest/bag`, `ingest/gebieden`, `load/geo`, `location/` —
see `deploy/compose/README.md` for the load recipe): the backbone loads and resolves locally at 90%
address precision / 100% point-in-polygon.**

- **Source:** Kadaster *LV BAG 2.0 Extract* — a free **national-only** dump (~**3.6 GB**;
  `lvbag-extract-nl.zip` = 3,610,187,048 bytes), refreshed monthly (~the 8th). Via the PDOK atom
  feed (`https://service.pdok.nl/kadaster/adressen/atom/v1_0/index.xml`, verified 2026-07-25). A
  per-gemeente extract needs an ordered Kadaster BAG-Extract account, so **filter at load, not at
  download**. Nested zip: outer → per-object-type inner zips (NUM 353 MB, VBO 1.2 GB, PND 2.0 GB, …).
- **`lvbag` reads only the ST snapshot — the daily Mutatie-Levering (ML) files are NOT supported by
  the driver.** So there is **no incremental-via-`lvbag`**; the refresh is an **idempotent monthly
  full reload** (a custom ML applier / NLExtract is only needed if daily freshness ever is).
- **Load as-is; the only filter is municipality (data rule).** BAG is the authoritative **master
  data** for addresses/buildings and the geography ladder — the reference every other source is tied
  to on location — so we mirror it faithfully and keep it correct and complete rather than
  pre-filtering to today's needs. **Rule: load full tables, no column projection, all voorkomens; the
  only load-time filter is municipality**, because pre-dropping columns or rows bakes in assumptions
  we'd have to unwind (another vertical needs another column; a backdated audit needs an old
  voorkomen; a demolished address must still resolve). The safe reductions are municipality (we only
  audit Amsterdam) and omitting **whole** object types we provably don't use (footprints) — both
  remove no address and are trivially reversible.
  `ogr2ogr -f PostgreSQL -oo AUTOCORRECT_INVALID_DATA=YES /vsizip//…/9999<TYPE>…zip
  -where "identificatie LIKE '%.0363%'"` — note `identificatie` is the IMBAG-URI form
  `NL.IMBAG.<Type>.0363…`, so match `'%.0363%'`, **not** `'0363%'`. Load the three **adresseerbaar
  object**
  types — `verblijfsobject` (address point), **`ligplaats`** (houseboat berth) and **`standplaats`**
  (polygons → centroid, reached via `hoofdadresNummeraanduidingRef`) — for complete address-point
  coverage, plus `nummeraanduiding` + `openbareruimte`. `pand` (footprints) and `woonplaats` are
  optional whole-table omissions (no address point is lost). `gebieden`/CBS polygons load separately
  via WFS/GeoJSON (§0), **not** the `lvbag` driver.
- **BAG is bitemporal — load ALL voorkomens and resolve at valid-time.** Each object has a sequence
  of voorkomens with valid-time (`beginGeldigheid`/`eindGeldigheid`) and transaction-time
  (`tijdstipRegistratie`/`eindRegistratie`). 26% of `0363` addresses have >1 voorkomen; **87% are
  real-world changes** (valid-time advances), **13% technical corrections** (same valid-time,
  re-registered). Because we audit **backdated** interventions, prefer the state valid at the
  intervention's date: `beginGeldigheid <= D AND (eindGeldigheid IS NULL OR eindGeldigheid > D) AND
  eindRegistratie IS NULL`. **But permit dates/addresses can be unreliable, so if nothing is valid at
  D, fall back to any best-known voorkomen and record the outcome on the link** (`time_match =
  valid_at_date | any_time` → a `timeMismatch` caveat on the `AuditLink`): resolve anyway, flag the
  weakness rather than drop the link (Spike D: lifts permit address resolution 90.4% → 95.1%).
  **Withdrawal/demolition is a `status` change** (`… ingetrokken`) on the latest voorkomen, *not* a
  dropped row (the file holds 39,903 VBO + 48,966 NUM ingetrokken for `0363`) — so never filter on
  `status`, or a permit pointing at a since-demolished address fails.
- **Table/column shape:** an address row (`nummeraanduiding`: `postcode`, `huisnummer`,
  `openbareruimteref`) has **no geometry**; the point is on `verblijfsobject`/`ligplaats`/
  `standplaats` via `hoofdadresnummeraanduidingref`; `openbareruimte.naam` is the street. Reach a
  point NUM → adresseerbaar object.

---

## 9. Bomenboekhouding (Amsterdam) — the aggregate self-report

The city's own published tree accounting — the **third leg of the audit triangle**
(`VISION.md`; `IMPLEMENTATION_PLAN.md` §2): the aggregate figures the municipality reports,
checked against the per-item `kapenherplant` registry and the permit stream. Comparing the
self-report against our bottom-up counts is what catches under-reporting and figures that
don't reconcile.

- **Where:** `https://www.amsterdam.nl/leefomgeving/groen/bomen/bomenboekhouding/`.
- **Access quirk:** automated fetch returns **HTTP 403** (verified 2026-07-25). Either dump
  the data manually, or adjust the access method (headers/session) to get past the block.
  Volume and exact figures to be characterised when we wire it in (Phase-4 reconciliation).

---

## 10. Regulation & policy (CVDR) — the external norm

The legal basis the audit tests against (`VISION.md`: "the law as external norm"), published
in the CVDR (Centrale Voorziening Decentrale Regelgeving).

- **Bomenverordening 2014** — `https://lokaleregelgeving.overheid.nl/CVDR323217/2` (verified
  2026-07-25). Art. 7 = the herplantplicht ("in beginsel altijd"); the termijn is set per
  permit by the college. This is the trigger for the claim being audited. **Art. 1 defines
  *vellen* as "rooien, kappen, kandelaberen **of verplanten**"** (verified, Spike C) — so a
  *verplant* is legally a velling, triggers herplantplicht identically, and counts toward the
  felling obligation; the registry's logging of transplants as `Vellen` (§2a) is correct.
- **Compensatie en herplant van bomen** (beleidsregel) —
  `https://lokaleregelgeving.overheid.nl/CVDR697591` (verified 2026-07-25). The
  compensation/replant rules, including the **diameter-class equivalence** (a mature tree →
  several young ones) needed to turn a felling into a computed obligation quantity, and the
  herplantfonds mechanics.

Both also seed the legal top of the SKOS domain vocabulary (§11).

---

## 11. Domain vocabularies — SKOS sources

Authoritative thesauri that seed the NER EntityRuler, ground the agent, and back the SHACL
controlled value sets (`IMPLEMENTATION_PLAN.md` §3 — import only the slices we touch, align
with `skos:exactMatch`, grow altLabels from the permit corpus).

- **TOOI** — `https://standaarden.overheid.nl/tooi/waardelijsten/` — the government's SKOS
  thesauri for official publications; the `OVERHEID*/OVERHEIDvb` schemes the bekendmakingen
  are tagged with. Document/rubriek + intervention typing (thin for kap-omgevingsvergunningen).
- **IMBOR** (CROW, published in RDF) — `https://github.com/Stichting-CROW/imbor` — the
  standard public-space object model, incl. the **BOOM** object (species + management
  measures), aligned with the Norminstituut Bomen *Handboek Bomen*. The authoritative
  tree-domain source.
- **Nederlands Soortenregister** — species Latin/Dutch names + synonyms; fills gaps IMBOR +
  `stamgegevens.soortnaam` leave.
- **Local enums & regulation** — distinct values of `kapenherplant.boommaatregelBesluit` /
  `boomgebreken` / `stamgegevens.soortnaam`, `gebieden`/CBS names + codes, and the concepts
  from the regulation (§10). The `boommaatregelBesluit` enum is **pulled** (Spike C, §2a): a
  single meaningful value `Vellen (boom verwijderen)`, **no Verplanten** — so *verplanten* is a
  `skos:altLabel` under the felling concept, not a separate concept.

---

## 12. Source × dimension × key summary

| Source | Dimension | Geography key | Cadence | Auth |
|---|---|---|---|---|
| KOOP bekendmakingen | interventions (docs) | postcode + RD geometry (verkeersbesluiten); **RD+WGS84 point** + free-text address (kap vergunningen — Spike B) | continuous | none |
| Datapunt bomen | ecology / tree lifecycle | point + `gbdBuurtId` (+ nearest BAG address in kapenherplant) | days–months | none (free key coming soon) |
| Datapunt parkeervakken | parking inventory | street + geometry | lags decisions | none (free key coming soon) |
| Politie 47022NED | crime | CBS buurt code | monthly, ~1 mo lag | none |
| CBS KWB | demographics, cars, income | CBS buurt code | annual vintage | none |
| Luchtmeetnet | air quality | station coords [lon,lat] | hourly | none |
| Liander kleinverbruik | energy (elec + gas) | postcode-6 range | annual (Jan 1) | none |
| EP-Online | building energy labels | BAG id / postcode+number | monthly full + daily deltas | free key |
| BAG bulk (LV BAG 2.0) | place backbone / resolution | BAG id ↔ whole ladder | monthly full + daily deltas | none |
| Bomenboekhouding | tree accounting (aggregate self-report) | buurt / stadsdeel (aggregate) | periodic report | none (403 on scrape) |
| CVDR (Bomenverordening + herplant policy) | legal norm + diameter classes | — | stable | none |
| TOOI / IMBOR / Soortenregister | domain vocabulary (SKOS) | — | stable | none |
| PDOK Locatieserver | not used — resolution runs against local BAG (§8) | — | — | — |
| Datapunt gebieden | (id bridge) | Amsterdam id ↔ CBS code | stable | none |
