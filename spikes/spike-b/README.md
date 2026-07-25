# Spike B — permit↔registry match rate + the confidence model

**Question (Phase 0, `IMPLEMENTATION_PLAN.md` §6/§8).** There is **no shared key** joining a
KOOP kap/verplant **permit** to the `kapenherplant` **registry** rows it triggers, so the audit's
central link must be fuzzy entity resolution on *place + count + time + project*, carrying a
confidence (§2, §4). Can we link the two at all, **at what rate**, and **how do we score the
confidence** on the `AuditLink` edge?

**Answer.** Yes, at a usable but imperfect rate, and the confidence model is **place-led**.
Measured over stadsdeel **Noord**:

- **permit → registry: 90%** of Noord kap *besluiten* find felled registry rows in the same buurt
  within a plausible window; **54%** of the count-bearing besluiten also match on count.
- **registry → permit: 70%** of 2022–2025 fellings find a candidate 2022 besluit (a **lower
  bound** — only one permit-year was harvested).
- The blocker is not *finding* candidates but **disambiguating** them: a buurt+time window holds
  **~3 candidate work-order clusters** on average, so buurt-level place alone cannot pin a unique
  link. Count agreement + finer place (postcode/address, the Spike-D BAG pass) are what sharpen it.

**Two findings reshape the plan's assumptions (both verified, corrections below):**
1. **The kap permit metadata is not thin.** Every sampled kap omgevingsvergunning carries a
   structured **point geometry (RD *and* WGS84)**, a **controlled `activiteit`**, and a
   **zaaknummer** — contradicting `DATA_SOURCES.md` §1. So *place* and *activity* are **structured
   signals**, not free text to be NER'd, and permit→buurt resolves with no BAG and no gazetteer.
2. **Still no shared key.** The permit's zaaknummer (`OVERHEIDop.referentienummer`, e.g.
   `Z2022-N002608`) has **no counterpart field on the registry side** — the plan's premise holds.

All figures verified **2026-07-25** over the full Noord population + the full 2022 Amsterdam kap
corpus. Reproduce: `python3 schema_probe.py`, then `harvest_permits.py`, then `match_rate.py`
(run `../spike-a/probe.py` first for the base registry data).

---

## Scope established (reusable by Phase 1)

- **Registry side** reuses Spike A's `../spike-a/noord_kap.jsonl` (5,758 Noord rows, 1,298 felled)
  and `noord_buurt_ids.txt` (69 buurten) — not re-derived.
- **Permit side:** the KOOP SRU harvest for Amsterdam kap omgevingsvergunningen already returns,
  **per record with no per-permit fetch**, the title (+ address/postcode), publication date,
  abstract (tree count + activity terms), controlled `activiteit`, and the point geometry in **both
  RD and WGS84** (`overheidwetgeving:geometrie` / `locatiepunt`). Only the zaaknummer needs the
  `metadata.xml` sidecar.
- **Permit → buurt, pre-BAG:** convert nothing — take the WGS84 point and vote the `gbdBuurtId` of
  the nearest standing trees (`bomen/stamgegevens` spatial query, `Accept-Crs: EPSG:4326`). Robust
  (E-buurt anchor: 47/50 trees agree). This is the interim stand-in for BAG point-in-polygon.
  `bomen/kapenherplant` itself **rejects** spatial filters (HTTP 403); only `stamgegevens` accepts
  them — hence the vote-via-standing-trees trick.

## Finding 1 — no shared key, but the permit is richer than documented

`kapenherplant` carries **49 fields** (Spike A already noted the v3 record is undocumented-heavy).
Hunting for any join identifier: `projectnaamBomen` and `selectiecode` exist as fields but are
**0% populated** in Noord; `matching` is a Geovisia migration note; `inhoudBesluitVergunningKap`
is a status (`"Verleend"`), not a number. **No zaaknummer / OLO / dossier / vergunningnummer field
exists at all.** → the registry cannot be joined on a key.

The permit side, by contrast (sampled kap publications, all shapes):

| Structured field (metadata sidecar) | Presence in sample | Use |
|---|---|---|
| `OVERHEIDop.geometrie` (RD point) + WGS84 | 9/9 · **2,265/2,299 corpus** | place (spatial) |
| `OVERHEIDop.activiteit` (controlled: `kappen`) | 9/9 | activity axis |
| `OVERHEIDop.referentienummer` (zaaknummer) | 9/9 | dedup; prefix = stadsdeel |
| `DCTERMS.abstract` (count + activity prose) | 9/9 | count axis |

**Correction to `DATA_SOURCES.md` §1** ("kap/bouw bekendmakingen have thin metadata — no geometry,
no controlled activity type; the address sits in free text"): **false for kap.** Geometry and a
controlled activiteit are present ~99% of the time. The zaaknummer prefix encodes the stadsdeel
(`…-N…` Noord, `…-ZO…` Zuidoost, `…-NW…` Nieuw-West, …) — a coarse stadsdeel filter for free.

## Finding 2 — the corpus, and what NER still has to add

Full 2022 Amsterdam kap omgevingsvergunning corpus: **2,299 publications**, **98.5% with a point**.
Doctype split: **978 besluit**, 1,136 aanvraag, 39 ingetrokken, 4 verlengd, 142 other. **Noord:
365 publications → 147 besluiten.** Publications appear as *aanvraag* **and** *besluit* for the same
zaak, so a Phase-1 load must **dedup by zaaknummer** and audit against the **besluit** (the grant).

Per-quarter Noord volume (**this is the Phase-0 quarter-pick input** §1):

| 2022 quarter | Noord publications | of which besluiten |
|---|---|---|
| Q1 | 98 | 36 |
| Q2 | 98 | 43 |
| Q3 | 92 | **45** |
| Q4 | 77 | 23 |

→ **Pick 2022-Q2 or Q3** (43–45 besluiten each — the largest observable-obligation samples).

Signal availability over the 147 Noord besluiten: point geometry **100%**, controlled activiteit
**100%**, a tree **count** in title/abstract only **49%** (72/147). **NER's Phase-2 job is the
count** — half of the besluiten state it in prose the SRU abstract doesn't capture structurally
(and *verplanten vs vellen* semantics, Spike C). Place and activity do **not** need NER.

## Finding 3 — the match rate

`match_rate.py`, buurt-level place, felling required to fall in `[publication, +3 yr]`:

| Direction | Rate | Notes |
|---|---|---|
| **permit → registry** (place+time) | **133/147 = 90%** | felled cluster in buurt + window |
| **permit → registry** (+count) | **39/72 = 54%** | of count-bearing besluiten |
| **registry → permit** | **529/752 = 70%** | fellings 2022–2025; **lower bound** (1 permit-year) |
| candidate **ambiguity** | **~3.0 clusters / hit** | buurt+time alone ≠ unique link |
| felling − publication **lag** | median **269 d**, max 1087 d | anchor time on the felling date (Spike A) |

The 90% says the *place+time* signal is strong; the ambiguity of ~3 and the 54% count rate say it
is **not unique** — which is exactly why confidence is graduated and why the BAG pass (finer place)
matters. The unmatched 30% of the registry→permit direction is not noise to hide: it is the
grounded **"no matching publication found"** finding (`DATA_THREAD` Hop 4), to be surfaced with its
own provenance, not forced into a match.

---

## Decision — the `AuditLink` confidence model

Score each candidate permit↔cluster link into `[0,1]`; assert an `AuditLink` (RDF-star,
`<< permit :auditLinkedTo cluster >> :confidence …`) at or above **τ = 0.60**; below τ keep the
best candidate but flag `weakLink`; no candidate at all → an explicit *no-link* finding.

| Axis | Rule (this pass → BAG pass) |
|---|---|
| **place** (base) | buurt containment **0.50** → postcode **0.70** → BAG address / project polygon **0.90** (the §4 smallest-area ladder; BAG pass supplies the finer rungs) |
| **count** | exact **+0.30** · within ±15% **+0.15** · unknown **+0** · incompatible **−0.10** |
| **time** | felling ≤2 yr after publication **+0.15** · 2–3 yr **+0.05** · felling *before* publication ⇒ excluded. **Anchored on the felling date; the registry permit-dates are batch-assigned and never used** (Spike A) |
| **ambiguity** | sole candidate **+0.05** · else **−0.03·(n−1)** floored at −0.15 |
| **project/zaaknummer** | reserved: near-certain link **iff** both sides expose it — but the registry does **not** (Finding 1), so this rung is currently **unavailable** |

Applied to the 147 Noord besluiten (buurt-level place, so base 0.50 for all):

- **strong (≥0.70): 36** · **asserted (≥τ): 65** (29 of them weak 0.60–0.70) · **below τ
  (weakLink/no-link): 68** · **no candidate: 14**.
- With place capped at buurt, count agreement is the only lever above τ — deliberate: it shows the
  **BAG pass is what promotes the ~68 weak links**, by lifting place from 0.50 (buurt) to
  0.70–0.90 (postcode/address). The model is designed so re-scoring the *same* persisted candidates
  with finer place needs no re-fetch.

**Caveats are first-class** (per §"Fulfilment"): every link carries `weakLink` (below τ),
`countUnknown` (permit stated no count), and the activity caveat until Spike C settles
*verplanten*≠*vellen*. Confidence and caveats qualify the fulfilment estimate; they never erase it.

**Knock-ons for the design docs** (task 5):
- `DATA_SOURCES.md` §1 — correct the "thin metadata / no geometry" claim for kap
  omgevingsvergunningen; record that geometry (RD+WGS84), controlled activiteit, and zaaknummer are
  present, and that `bomen/kapenherplant` rejects spatial filters (use `stamgegevens`).
- `IMPLEMENTATION_PLAN.md` §4 — the permit side has a **point geometry**, so location resolution is
  point-in-polygon from the permit itself, not free-text-address-first; the free-text address is a
  fallback, not the primary path. §6/§8 — mark **Spike B DONE**, record the 90% / 70% / ambiguity-3
  rate and τ=0.60 model, and the Q2/Q3 quarter pick. §5 — Phase-1 load must **dedup by zaaknummer**
  (aanvraag+besluit duplicates) and audit the besluit.

## Files

- `schema_probe.py` — probe 1: registry field inventory + KOOP metadata field inventory → shared-key
  verdict. Reads `../spike-a/noord_kap.jsonl`.
- `harvest_permits.py` — probe 2: SRU harvest + signal extraction + permit→buurt vote. Writes
  `all_permits.jsonl`, `noord_permits.jsonl`, `buurt_cache.json` (all `.gitignore`d).
- `match_rate.py` — probe 3: the join, both-direction rate, ambiguity/lag, confidence model +
  distribution, E-buurt regression. Writes `match_candidates.jsonl` (kept for the BAG re-run).
