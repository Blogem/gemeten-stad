# Spike C — is prose extraction feasible, and does *verplanten* differ from *vellen*?

**Question (Phase 0, `IMPLEMENTATION_PLAN.md` §6/§2).** Can we extract the felled/moved tree
**counts / species / project** from the kap-permit prose, and how do the activity terms (kappen /
vellen / rooien / **verplanten**) map onto the registry's `boommaatregelBesluit` — does *verplanten*
(transplant, the tree survives) vs *vellen* (removal) change whether/how **herplantplicht** applies?
Read the corpus before fixing the mapping; the thread's "18 verplant = 18 vellen" match is provisional
until this settles.

**Answer.** **Counts are feasible with a lightweight deterministic parser — no NER needed for the
audit core.** And **verplanten ≡ vellen**: the Bomenverordening *defines* vellen to include verplanten,
and the registry has no separate Verplanten value at all — so "18 = 18" **holds** (with a
`transplantOrigin` caveat). Measured:

- The abstract is **formulaic prose** ("het `<verb>` van `<N>` bomen …"). A per-activity parser lifts
  the count yield from the naive largest-int baseline **35% → 70%** (citywide besluiten) / **49% → 73%**
  (Noord besluiten), reading spelled-out numbers and the `houtopstand` noun the baseline misses — and,
  unlike the baseline, it **never sums vellen + verplant** (the baseline conflated them in **12** cases,
  e.g. picking 43 where the felling was 33).
- **Species <10%** in prose (120/2299 = **5.2%** citywide, 2.5% Noord), **project ~1.3%**, boomnummer
  2.3% — and the registry's species/project fields are **0% populated city-wide**, so prose is the
  *only* source. This is the genuine, fuzzy residue → **the home for the spaCy NER lane** (off the
  audit critical path, scope-expandable — see the Decision).
- `boommaatregelBesluit` over all **35,202** city rows has **no "Verplanten" value** — only
  `Vellen (boom verwijderen)` (+ `Ecoscan - Vellen` + 3 typos). The E-buurt "verplant 18 bomen" permit's
  trees are logged as **Vellen** with a real felling date and **9/18 replanted** — the registry treats a
  transplant as a felling-with-replant.
- The SRU **abstract carries the full extraction content** (it *is* the document's `Omschrijving` line,
  empty in only **1/2299**); the body adds no reliable signal and its boilerplate injects noise → **Phase
  2 parses the abstract, no per-document fetch.**

All figures verified **2026-07-25** over the full 2022 Amsterdam kap corpus + the full city
`kapenherplant`. Reproduce: `python3 parse_activities.py`, `species_project.py`, `registry_enum.py`,
`body_vs_abstract.py` (needs `../spike-b/all_permits.jsonl`; run `../spike-a/probe.py` then
`../spike-b/harvest_permits.py` first if absent).

---

## Scope established (reusable by Phase 1/2)

- **Corpus reused, not re-harvested:** `../spike-b/all_permits.jsonl` (2,299 citywide 2022 kap permits;
  978 besluit / 1,136 aanvraag) + `noord_permits.jsonl` (365; 147 besluit / 177 aanvraag). The
  extraction target is the `abstract` field = the document's **Omschrijving** line.
- **Registry enum pulled once, city-wide** (probe 3): the `boommaatregelBesluit` / `boomAanwezigheid`
  value sets + populate-rates over all 35,202 rows — a data-enum artifact the SKOS work (§11) needs
  anyway. Noord alone was insufficient (it only ever shows "Vellen"), so the enum pull is **city-wide by
  necessity**.
- **Parse both doctypes, audit the besluit.** Yield is reported per doctype; the Phase-1 audit runs on
  besluiten (Spike B: dedup by zaaknummer), but an *aanvraag* sometimes states a count its besluit omits,
  so the zaaknummer dedup should take the count from **whichever doctype has it**.

## Finding 1 — the abstract is formulaic; per-activity parsing beats largest-int

The abstract is short, controlled-ish prose — `"het <verb> van <N> bomen (en <verb2> van <M> boom) …"` —
so a deterministic pattern parser (split clauses on `en`/`,`, bind each verb to its adjacent count,
read spelled-out `een…twintig`, match `boom`/`bomen`/`houtopstand`) is the right tool. **No NER.**

| Segment | naive baseline count | per-activity parser | multi-activity | naive conflated |
|---|---|---|---|---|
| citywide · besluit (n=978) | 344 (35%) | **687 (70%)** | 18 | 4 |
| citywide · aanvraag (n=1136) | 430 (38%) | **824 (73%)** | 15 | 6 |
| Noord · besluit (n=147) | 72 (49%) | **107 (73%)** | 3 | 0 |
| Noord · aanvraag (n=177) | 88 (50%) | **128 (72%)** | 1 | 0 |

The lift comes from **spelled-out numbers** ("het kappen van *vier* bomen") and the **`houtopstand`**
noun (`"het vellen van 7 houtopstanden"`) that the baseline's `\d+ bomen` regex misses entirely. The
residual unparsed ~27% is mostly abstracts with **no stated number** (e.g. "Kappen kastanje boom",
"Vellen houtopstand (kap) Jan van Eijckstraat 12") and a species-as-noun gap ("een *populier*", no
"boom" word) — genuine `countUnknown` cases, not parser failures, feeding the Spike-B `countUnknown`
caveat. Countless buckets ("diverse/meerdere bomen") are rare (~6 citywide).

**The correctness point is the split, not just the yield.** The baseline takes the *largest* integer near
"bomen", so on `"het vellen van 33 bomen en het verplanten van 43 bomen"` it returns **43** — the
*verplant* count, not the felling. The per-activity parser returns `{vellen: 33, verplanten: 43}` and a
**felling total of 33**. It also keeps any *herplant/plant* ("herplanten van N") strictly on the replant
side, never in the felling total — the single biggest trap in the naive parser.

**Correction to `DATA_SOURCES.md` §1** ("only the tree count genuinely needs NER; present in prose ~50%"):
the count is present in prose **~70%** of besluiten once spelled-out numbers + `houtopstand` are parsed,
and it needs a **deterministic parser, not NER**; the ~50% figure was the naive-regex floor.

## Finding 2 — species/project yield is thin; this is the NER lane

Over the same abstracts (seed lexicon: Dutch common names + Latin genera + cultivar/hybrid pattern):

| Signal | citywide (n=2299) | Noord (n=365) | registry-side |
|---|---|---|---|
| names ≥1 species | **120 (5.2%)** (Dutch 108 · Latin/cultivar 14) | 9 (2.5%) | **0%** (`soortnaam`/`toeTePassenBoomsoort`) |
| names a project | **31 (1.3%)** | 0 | **0%** (`projectnaamBomen`) |
| gives a boomnummer | 52 (2.3%) | 0 | — |

Top Dutch terms: iep 41, esdoorn 20, beuk 11, berk 7, es 6, populier 6, wilg 5, kastanje 4. Species appear
as Dutch ("de Es", "(iep)"), Latin ("Betula pubescens"), or cultivar ("Ulmus 'Dodoens'", "Tilia x
europaea"); projects cluster ("De Oranje Loper" ×10, "E-Buurt Oost NZ"). Because the **registry carries no
species or project** (0% city-wide, probe 3), the prose is the *only* source for these — so this residue,
though small, is real and is exactly the fuzzy, non-formulaic target that earns a **spaCy NER lane**. It
is **off the audit critical path** (the audit works on counts + place + activity without it), so it is the
place to build/experiment with NER, and its scope can be **widened** (richer entity/relation extraction,
cross-permit project clustering) if the species/project residue proves too thin on its own.

## Finding 3 — the registry has no "Verplanten" value; transplants are logged as Vellen

`boommaatregelBesluit` over **all 35,202** city rows:

| value | count |
|---|---|
| *(null)* | 24,484 |
| `Vellen (boom verwijderen)` | 7,833 |
| *(empty string)* | 1,917 |
| `Ecoscan - Vellen` | 965 |
| `kap` / `kappen ` / `Vellen ( Boom verwijderen)` | 1 each (typos) |

**There is no Verplanten value anywhere in the city.** `boomAanwezigheid` (the fulfilment signal): `Nee`
18,360 · `Ja, nieuwe boom reeds aangeplant` 5,449 · *(null)* 5,436 · `Ja` 5,279 · `Niet te beoordelen`
677. Verplant cross-check — the E-buurt (Zuidoost) `03630980000509` renewal whose 2022 permit said *"het
verplanten van 18 bomen"*: **18 felled since 2024, 9 replanted**, `boommaatregelBesluit` = {Vellen: 7,
null: 11}, `boomAanwezigheid` = {Nee: 8, Ja: 9, "reeds aangeplant": 1}. The registry logs a transplant as
a felling, dates it, and tracks its replant — operationally identical to a vellen.

**Correction to `DATA_SOURCES.md` §2a:** the `boommaatregelBesluit` enum is now pulled — effectively a
single meaningful value, `Vellen (boom verwijderen)` (+ an `Ecoscan - Vellen` variant); **no Verplanten
value exists**. `toeTePassenBoomsoort` / `soortnaam` / `projectnaamBomen` are **0% city-wide** (the 0%
Noord finding is global). `boomAanwezigheid` is the usable fulfilment axis.

## Finding 4 — the abstract carries what the body carries

The `abstract` **is** the document's `Omschrijving` line (verified: body Omschrijving for `gmb-2022-174635`
= abstract verbatim), and it is empty in only **1/2299** permits. Fetching the full body XML for 14
hard-shape docs (2+ activities, spelled-out, species/project) and re-parsing it added **no reliable new
signal**: the body's only extra text is boilerplate (address, zaaknummer, "vellen van een houtopstand
(kap)", "Boom nummer 4, 11 en 12") that *injects noise* — a naive full-body parse **diverges** from the
correct abstract count (e.g. abstract felling 4 → body 1 via the "een houtopstand" boilerplate). → **Phase
2 parses the abstract; no per-document body fetch for extraction.** (Consistent with Spike A: the body is
a stub.) Squeezing the residual count out of noisier bodies is a fuzzy task for the NER lane, not the
deterministic parser.

---

## Decision

### Part 1 — extraction: a deterministic core, with NER as an additive lane

- **The audit core does not depend on NER.** Counts + the per-activity split come from a lightweight
  deterministic **abstract parser** (spelled-out-aware, clause-splitting, herplant-excluding), yielding
  ~70–73% of besluiten. Place + activity + zaaknummer are already structured metadata (Spike B). This is
  the "much simpler" verdict — stated as a **robustness** property: the backbone works with zero
  statistical NER.
- **spaCy NER is preserved as an additive enrichment lane**, not dropped. Its natural targets are the
  genuinely fuzzy residue the registry lacks entirely: **species** (Dutch/Latin/cultivar, ~5%), **project**
  names (~1%, they cluster), and boomnummers. It sits **off the critical path** (the audit is complete
  without it), so it is the safe place to build and experiment with NER; if that residue is too thin to be
  interesting, its scope can be **widened** (relation extraction over bodies, project clustering) rather
  than removed. The `IMPLEMENTATION_PLAN.md` §5 "Python only for spaCy NER" boundary **stands**.

### Part 2 — *verplanten* ≡ *vellen* for the audit (the "18 = 18" match holds)

Three legs, all pointing the same way:
1. **Legal definition.** Bomenverordening 2014 (CVDR323217) art. 1: *"k. **vellen**: rooien, kappen,
   kandelaberen **of verplanten**, …"* — *verplanten is a form of vellen by definition*, so the art. 7
   herplantplicht ("in beginsel altijd") triggers on a verplant exactly as on a kap.
2. **Registry reality** (probe 3). No Verplanten value exists; transplants are logged as
   `Vellen (boom verwijderen)`, dated, and replant-tracked (E-buurt: 18 logged/felled, 9 replanted).
3. **Corpus usage** (probe 1). Permits pair the verbs freely ("kappen van 2 bomen en verplanten van 1
   boom"); the distinction is descriptive, not a different legal event.

| Permit prose term | Semantics | Registry `boommaatregelBesluit` | Felling obligation? | Herplantplicht? | Audit treatment |
|---|---|---|---|---|---|
| kappen / vellen / rooien | tree removed | `Vellen (boom verwijderen)` | **yes** | **yes** (art. 7) | count → diameter-class obligation |
| **verplanten** | relocated (may survive) | `Vellen (boom verwijderen)` *(no distinct value)* | **yes** | **yes** (art. 1k: verplanten *is* vellen) | counted with the felling total; carry a **`transplantOrigin`** caveat (tree may persist at the new site) |
| herplanten / planten | replacement planted | plant-side / `boomAanwezigheid` | no (fulfilment) | n/a | count on the **replant** side only, never the felling total |

So **"18 verplant = 18 vellen" is a true match**, resolving Spike B's carried activity caveat and
`DATA_THREAD_TREES.md` Hop 4's provisional note. The felling total that sizes the obligation = Σ counts of
{kappen, vellen, rooien, **verplanten**}; herplant/plant counts are fulfilment, never obligation.

### Knock-ons / corrections to feed back

- **`DATA_SOURCES.md` §1** — count is present in prose **~70%** of besluiten via a **deterministic
  abstract parser** (spelled-out + `houtopstand`), not NER and not the ~50% naive floor; abstracts state
  **per-activity** counts that must be **split, not summed** (naive largest-int conflates vellen+verplant);
  species <10% / project ~1%; the abstract = the Omschrijving line and the body adds nothing → no
  per-document fetch for extraction.
- **`DATA_SOURCES.md` §2a** — record the `boommaatregelBesluit` enum (only `Vellen (boom verwijderen)` +
  `Ecoscan - Vellen`; **no Verplanten**), the `boomAanwezigheid` value set as the fulfilment axis, and the
  city-wide 0% for `toeTePassenBoomsoort`/`soortnaam`/`projectnaamBomen`.
- **`DATA_SOURCES.md` §10** — Bomenverordening art. 1k defines *vellen* to include *verplanten*; art. 7
  herplantplicht applies to verplant.
- **`DATA_SOURCES.md` §11 / `IMPLEMENTATION_PLAN.md` §3** — the `boommaatregelBesluit` enum is pulled (feeds
  the SHACL data-enum value set); place **verplanten** as a `skos:altLabel` under the felling concept.
- **`IMPLEMENTATION_PLAN.md` §6** — mark **Spike C DONE**: extraction feasible via a deterministic parser
  (35%→70% besluit yield), verplant ≡ vellen settled, "18 = 18" confirmed.
- **`IMPLEMENTATION_PLAN.md` §2** — resolve the "18 verplant = 18 vellen provisional" line to a true match.
- **`IMPLEMENTATION_PLAN.md` §"Fulfilment"** — resolve Spike B's activity caveat: verplant counts enter the
  felling/obligation denominator (with a `transplantOrigin` caveat); herplant counts are fulfilment only.
- **`IMPLEMENTATION_PLAN.md` Phase 2** — the extractor is a **deterministic formulaic-abstract parser for the
  core** (counts/activity split), with **spaCy NER as the additive species/project enrichment lane**
  (off critical path, scope-expandable). Keep the §5 "Python only for spaCy" boundary.
- **`DATA_THREAD_TREES.md`** — Hop 4's "provisional (Spike C)" note resolved: verplant ≡ vellen, so the
  18-permit ↔ 18-felled match stands.

## Files

- `parse_activities.py` — probe 1: per-activity count parser vs the naive largest-int baseline; yield,
  conflation, spelled-out lift by doctype/scope. Reads `../spike-b/all_permits.jsonl`; writes
  `activities.jsonl`.
- `species_project.py` — probe 2: species/project/boomnummer mention cataloguer + yield (the NER lane).
  Writes `mentions.jsonl`.
- `registry_enum.py` — probe 3: city-wide `boommaatregelBesluit`/`boomAanwezigheid` enum + populate-rates
  + the E-buurt verplant cross-check. Writes `registry_enums.json`.
- `body_vs_abstract.py` — probe 4: hard-shape body fetch vs abstract diff. Reads `activities.jsonl`,
  `mentions.jsonl`; writes `bodies_sample.jsonl`.

All `*.jsonl`/`*.json` artifacts are `.gitignore`d (re-run to recreate).
