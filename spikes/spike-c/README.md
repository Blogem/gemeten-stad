# Spike C — is prose extraction feasible, and does *verplanten* differ from *vellen*?

**Question (Phase 0, `IMPLEMENTATION_PLAN.md` §6/§2).** Can we extract the felled/moved tree
**counts / species / project** from the kap-permit prose, and how do the activity terms (kappen /
vellen / rooien / **verplanten**) map onto the registry's `boommaatregelBesluit` — does *verplanten*
(transplant, the tree survives) vs *vellen* (removal) change whether/how **herplantplicht** applies?
Read the corpus before fixing the mapping; the thread's "18 verplant = 18 vellen" match is provisional
until this settles.

**Answer.** **Counts are feasible; the best extractor is a graph-vocabulary spaCy pipeline with LLM
count-binding, and a lightweight deterministic parser is the zero-dependency floor.** And **verplanten ≡
vellen**: the Bomenverordening *defines* vellen to include verplanten, and the registry has no separate
Verplanten value at all — so "18 = 18" **holds** (with a `transplantOrigin` caveat). Measured:

- The abstract is **formulaic prose** ("het `<verb>` van `<N>` bomen …"). A per-activity **deterministic
  parser** lifts obligation-count yield from the naive largest-int baseline **35% → 72%** (citywide
  besluiten) / **49% → 73%** (Noord), reading spelled-out numbers and the `houtopstand` noun the baseline
  misses — and, unlike the baseline, it **splits the felling activities** (kappen/vellen/rooien/verplant)
  and sums them instead of picking `max()` (the baseline disagrees with the obligation in **32** citywide
  besluiten, e.g. returning 43 where "vellen 33 + verplant 43" obligation is 76).
- **Vocabulary-driven recognition + LLM binding beats both** (probes 5a–c). A spaCy EntityRuler seeded
  from a species/activity vocab (the `msr-graph` pattern) lifts obligation recall **83% → 90%** by
  recognizing species-headed counts the regex can't ("drie essen", "Kappen Ceder"); and an **LLM binds
  the counts far more accurately** — on an 80-case hand-labeled hard set, exact-match **deterministic 44%
  → spaCy 55% → LLM 95%**, resolving appositives ("een boom, de Es" = 1), snoeien-mixed clauses, and
  "13 bomen, waarvan 7 … en 6" = 13 that defeat both rule binders.
- **Species <10%** in prose (120/2299 = **5.2%** citywide, 2.5% Noord), **project ~1.3%**, boomnummer
  2.3% — and the registry's species/project fields are **0% populated city-wide**, so prose is the
  *only* source; these feed the same vocabulary-driven recognizer (see the Decision).
- `boommaatregelBesluit` over all **35,202** city rows has **no "Verplanten" value** — only
  `Vellen (boom verwijderen)` (+ `Ecoscan - Vellen` + 3 typos). The E-buurt "verplant 18 bomen" permit's
  trees are logged as **Vellen** with a real felling date and **9/18 replanted** — the registry treats a
  transplant as a felling-with-replant.
- The SRU **abstract carries the full extraction content** (it *is* the document's `Omschrijving` line,
  empty in only **1/2299**); the body adds no reliable signal and its boilerplate injects noise → **Phase
  2 parses the abstract, no per-document fetch.**

All figures verified **2026-07-25** over the full 2022 Amsterdam kap corpus + the full city
`kapenherplant`. Reproduce (stdlib probes): `python3 parse_activities.py`, `species_project.py`,
`registry_enum.py`, `body_vs_abstract.py` (needs `../spike-b/all_permits.jsonl`; run `../spike-a/probe.py`
then `../spike-b/harvest_permits.py` first if absent). The spaCy/LLM comparison needs the local venv +
an LLM key — see **Finding 5** and **Files**.

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

| Segment | naive baseline count | per-activity parser | multi-activity | naive wrong |
|---|---|---|---|---|
| citywide · besluit (n=978) | 344 (35%) | **700 (72%)** | 18 | 32 |
| citywide · aanvraag (n=1136) | 430 (38%) | **824 (73%)** | 15 | 40 |
| Noord · besluit (n=147) | 72 (49%) | **107 (73%)** | 3 | 10 |
| Noord · aanvraag (n=177) | 88 (50%) | **128 (72%)** | 1 | 7 |

The lift comes from **spelled-out numbers** ("het kappen van *vier* bomen") and the **`houtopstand`**
noun (`"het vellen van 7 houtopstanden"`) that the baseline's `\d+ bomen` regex misses entirely. The
residual unparsed ~28% is a **mix** (probe 1 breakdown): ~150 genuinely count-absent (plural "kappen van
bomen", address-only stubs) — real `countUnknown` — but ~110 species-headed ("drie essen", "7 populieren")
and ~60 singular implicit-1 ("Kappen Ceder") that a **vocabulary** recovers (Finding 5), not more regex.

**The correctness point is the split, not just the yield.** The baseline takes the *largest* single
integer near "bomen", so on `"het vellen van 33 bomen en het verplanten van 43 bomen"` it returns **43** —
a partial. The per-activity parser returns `{vellen: 33, verplanten: 43}` and the **obligation sum 76**
(verplant is a felling, §Decision), while keeping any *herplant/plant* strictly on the replant side, never
in the obligation — the single biggest trap in the naive parser.

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
species or project** (0% city-wide, probe 3), the prose is the *only* source for these. These species terms
are **not just a side-target — they are the recognition vocabulary** that also unlocks the count residue:
a species-headed count ("drie essen") is unparseable by a boom-only regex but trivial once "essen" is a
known species term. That is what Finding 5 exploits, and why species recognition sits at the *centre* of
the extractor, not off to the side.

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

## Finding 5 — vocabulary-driven spaCy recognition + LLM binding (the msr-graph pattern)

Rather than hand-grow the deterministic parser's rules to chase the residual (plural species tables,
compound-number regexes, implicit-1 heuristics — the "curated lists always miss" trap), we adopt the
`msr-graph` architecture: a **spaCy pipeline whose EntityRuler is seeded from the vocabulary** (here
`vocab.py`, a stand-in for a SKOS SPARQL read — each concept's surface forms become patterns carrying the
concept IRI in `id` → resolved via `ent_id_`), then bind counts. Two probes, then a scored comparison.

**5a — recognition (spaCy `nl_core_news_md` + EntityRuler from vocab, `extract_ner.py`).** Recognizing the
tree terms from the vocabulary lifts obligation-count recall **83% → 90%** of felling permits, recovering
146/340 of the deterministic residual — species-headed ("drie essen"→3, "7 populieren"→7), compound
numbers ("vierenveertig"→44, via a small nl number-normalizer), and singular implicit-1 ("Kappen Ceder"→1).
No plural tables or cultivar regex — the vocab supplies the forms. *Honest limit:* the `nl_core_news_md`
lemmatizer mis-normalizes botanical terms ("essen"→"Essen", "iepen"→"ie", "berken"→VERB), so the vocab must
carry plural **altLabels** — it cannot lean on lemmatization (this is what corpus-mining grows).

**5b/5c — binding (LLM vs the rule binders, `extract_llm.py` + `compare_methods.py`).** Recognition is the
easy half; **binding the count to the right activity is where the rule binders systematically err** —
spaCy over-counts species appositives ("een boom, de Es" = one tree, it says two), the deterministic
parser drops snoeien-mixed and multi-count clauses, and "13 bomen, waarvan 7 … en 6" (= 13) fools both.
`msr-graph` binds quantities with an LLM + closed-set validation; scored on an **80-case hand-labeled gold
set** (`gold_labels.py`, the 55 det↔spaCy disagreements + a 25-row residual sample — the contested tail):

| method | obligation exact-match (gold, n=80) | residual recovery (n=117) |
|---|---|---|
| deterministic regex | 44% | — (this is the residual) |
| spaCy + graph vocab | 55% | 67/117 (57%) |
| **LLM binding** (deepseek, closed-set prompt) | **95%** | **101/117 (86%)** |

The LLM's 4 gold "misses" are mostly arguable even against the gold ("… de zaailing van een derde boom" —
LLM 3, gold 2; "6 bomen (5 gekapt, 1 geweigerd)" — LLM 6, gold 5), and it does not regress on easy cases
(98/100 agree with the parser). *Honest limit:* the LLM returned **no** count on ~7% of the sample
(abstention / malformed JSON), so it needs the deterministic floor as a fallback, not a replacement.

**Verdict:** recognition is a vocabulary problem (solved by the graph-seeded EntityRuler); **binding is a
semantics problem the LLM solves decisively** (55% → 95% on the hard tail). The deterministic parser
remains the honest, dependency-free floor (72%).

---

## Decision

### Part 1 — extraction: a three-tier extractor mirroring `msr-graph`

Phase 2 builds the extractor as three tiers, so the audit works without the fancy parts but the count
recovery is near-complete with them:

1. **Deterministic floor (no dependency).** The abstract parser (`parse_activities.py`) — spelled-out-aware,
   clause-splitting, herplant-excluding — extracts the obligation count for **72%** of besluiten with zero
   NER/LLM. Place + activity + zaaknummer are already structured (Spike B). **The audit backbone never
   depends on NER or an LLM** — this is the robustness guarantee (and the offline fallback for the ~7% of
   abstracts the LLM abstains on).
2. **Vocabulary-driven recognition (spaCy EntityRuler ← the SKOS graph).** A `spacy` pipeline whose
   EntityRuler is **seeded live from the domain graph** (activity altLabels + species from IMBOR /
   Soortenregister / mined altLabels), each pattern carrying its concept IRI — the `msr-graph`
   `graph_reader → seeding` pattern. This is the primary **recognition** engine (recall 83% → 90%) and the
   home of the NER learning track; it grows as the vocabulary grows, no code changes.
3. **LLM count-binding (over the one-sentence abstract, closed-set validated).** The recognized activity /
   species IRIs constrain an LLM that binds the per-activity counts — resolving appositives, snoeien-mixed
   and multi-count clauses the rule binders miss (**95%** on the hard tail). This is `msr-graph`'s
   quantity-binding choice, empirically justified here.
- **Vocabulary growth (statistical `nl` model).** A fourth, offline loop: mine noun-chunk candidates the
  vocab doesn't yet cover → altLabel **proposals** → human confirmation → back into the SKOS graph (the
  plan's "living vocab" recipe = `msr-graph`'s mining loop). The `nl_core_news_md` lemmatizer is too weak
  for botanical morphology (Finding 5), so growth is via mined surface variants, not lemmatization.
- **On the §5 boundary:** the deterministic floor is Go; **Python owns spaCy** (recognition + mining); the
  LLM binding is a new, *additive* dependency the evidence earns (55% → 95%). NER is now **central**, not a
  side-lane — but the core still runs without it, honoring the earlier "core doesn't depend on NER" rule.

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
- **`IMPLEMENTATION_PLAN.md` §6** — mark **Spike C DONE**: extraction feasible (deterministic floor 72% →
  graph-vocab spaCy 90% recall → LLM binding 95% on the hard tail); verplant ≡ vellen settled, "18 = 18"
  confirmed.
- **`IMPLEMENTATION_PLAN.md` §2** — resolve the "18 verplant = 18 vellen provisional" line to a true match.
- **`IMPLEMENTATION_PLAN.md` §"Fulfilment"** — resolve Spike B's activity caveat: verplant counts enter the
  felling/obligation denominator (with a `transplantOrigin` caveat); herplant counts are fulfilment only.
- **`IMPLEMENTATION_PLAN.md` Phase 2** — the extractor is **three tiers** (Decision Part 1): a deterministic
  abstract parser (Go, no-dependency floor), a **spaCy EntityRuler seeded from the SKOS graph** for
  recognition (the `msr-graph` pattern), and **LLM count-binding** — plus a statistical-`nl` mining loop
  that grows the vocab. §5's "Python owns spaCy" holds; the LLM is a new additive dependency the evidence
  earns.
- **`IMPLEMENTATION_PLAN.md` §3 (SKOS)** — species altLabels are recognition-critical (the count residue
  rides on them) and the `nl` lemmatizer is too weak for botanical morphology → the vocab must carry plural
  surface variants, grown by corpus mining.
- **`DATA_THREAD_TREES.md`** — Hop 4's "provisional (Spike C)" note resolved: verplant ≡ vellen, so the
  18-permit ↔ 18-felled match stands.

## Files

**Stdlib probes (1–4):**
- `parse_activities.py` — probe 1: per-activity count parser vs the naive largest-int baseline; yield,
  conflation, spelled-out lift by doctype/scope. Reads `../spike-b/all_permits.jsonl`; writes
  `activities.jsonl`.
- `species_project.py` — probe 2: species/project/boomnummer mention cataloguer + yield. Writes `mentions.jsonl`.
- `registry_enum.py` — probe 3: city-wide `boommaatregelBesluit`/`boomAanwezigheid` enum + populate-rates
  + the E-buurt verplant cross-check. Writes `registry_enums.json`.
- `body_vs_abstract.py` — probe 4: hard-shape body fetch vs abstract diff. Writes `bodies_sample.jsonl`.

**spaCy/LLM comparison (5) — needs the local venv + an LLM key:**
- `vocab.py` — the seed vocabulary (activity + species concepts with IRIs); stand-in for the SKOS SPARQL read.
- `extract_ner.py` — probe 5a: `nl_core_news_md` + EntityRuler-from-vocab; count binding via the dependency
  parse. Writes `ner_counts.jsonl`.
- `extract_llm.py` — probe 5b: LLM count-binding over the comparison sample (reads `DEEPSEEK_API_KEY` /
  `LLM_MODEL_EXTRACT` from the **env**, never a file). Writes `llm_counts.jsonl`.
- `gold_labels.py` — **committed** hand-authored gold obligation counts (80 hard cases) — human ground truth.
- `compare_methods.py` — probe 5c: scores deterministic vs spaCy vs LLM against the gold set.

**Setup for probe 5** (from this dir): `uv venv .venv && . .venv/bin/activate && uv pip install "spacy>=3.8"
click && python -m spacy download nl_core_news_md`. Run: `python extract_ner.py`; then with an LLM key in
the env, `python extract_llm.py && python compare_methods.py`.

All `*.jsonl`/`*.json` artifacts + `.venv/` are `.gitignore`d; `gold_labels.py` and the scripts are committed.
