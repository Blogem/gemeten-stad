# Spike A — where does the replant *termijn* live?

**Question (Phase 0, `IMPLEMENTATION_PLAN.md` §6/§8).** The audit's **timeliness** axis
(`notYetDue` / `onTime` / `overdue` / `deadlineUnknown`) needs a replant deadline `D` per
claim. Where does `D` reliably come from — the permit text, the registry field
`kapenherplant.datumAfrondenVoor` (flagged unreliable), or a policy default?

**Answer.** **Nowhere publicly and reliably.** The registry's nominal deadline field is a
work-order artifact (unusable), the published permits carry no replant termijn at all, and the
governing policy makes the termijn a *per-permit discretionary condition* (set in the
unpublished dossier, extendable, open-ended for renewal projects). So the design must default
to **`deadlineUnknown`** and must **not** invent a deadline — exactly as risk #1 anticipated.

All figures verified **2026-07-25** over the full stadsdeel **Noord** population.
Reproduce: `python3 probe.py` (registry) and `python3 scan_permits.py` (documents).

---

## Scope established (reusable by Phase 1)

`kapenherplant` is keyed by `gbdBuurtId` (buurt); there is **no stadsdeel filter**, and the
gebieden **relation filters are silently ignored** (`ligtInStadsdeelId=…`, `gbdBuurtId[in]=…`,
and `code[like]=N*` all return 0 with no error — the same "unknown filter → silent 0" quirk the
KOOP SRU has). Robust method, used here and reusable by `ingest`/`load bomen`: fetch all
wijken + buurten and join **stadsdeel → wijk → buurt client-side**.

- Noord = **15 wijken** (codes `NA`–`NQ`), **69 current buurten** (`eindGeldigheid` null).
- Noord `kapenherplant`: **5,758 rows**; **1,298** with felling executed; of those **455 (35%)
  replanted**, **843 pending**. (`noord_buurt_ids.txt` is the reusable id list.)

## Finding 1 — the registry deadline field is unusable

`datumAfrondenVoor` cannot be a replant deadline:

| Test (over 1,298 felled Noord rows) | Result |
|---|---|
| populated | **52.9%** (already absent for ~half) |
| **precedes the felling date** | **53%** of populated rows — impossible for a replant due date |
| offset `datumAfrondenVoor − kap` | median **−6 days**, min −2450, max +230 |
| where replant *is* done: replant after `datumAfrondenVoor` | **100%** of 70 rows, median **467 days** late |

It is a **work-order step deadline** (as `DATA_SOURCES.md` suspected): typically dated *before*
the tree is even felled, and overshot in every case where a replant actually landed. No other
registry date field is a deadline either — `datumHerplantinspectie` (88% populated) precedes
the felling in 48% of rows; the rest are execution/inspection/toezicht timestamps.

**The `kapenherplant` v3 record carries far more date fields than `DATA_SOURCES.md` lists.**
Populate-rates over felled rows: `datumVergunningsaanvraag` 40.8%, `datumBesluitVergunningKap`
61.3%, `datumVergunningVerleend` 61.3%, `datumEindeBezwaar` 0%, `datumAkkoordBoomsoort` 72.1%,
`kapmaatregelDatumUitgevoerd` 100%, `groeiplaatsmaatregelDatumUitgevoerd` 61.5%,
`plantmaatregelDatumUitgevoerd` 35.1%, `datumAfrondenVoor` 52.9%, `datumHerplantinspectie`
88.4%, `datumToezichtHerplantinspectie` 74.4%, `kapDatumToezicht` 78.7%, `plantenDatumToezicht`
29.4%, `inspectiedatum` 93.3%, `mutatiedatum` 100%.

**Both "permit granted" dates are batch-assigned** — not legal-quality: `datumVergunningVerleend`
has **17 distinct values over 796 rows** (top cluster 2024-11-20 ×162), `datumBesluitVergunningKap`
16 distinct values, and the two **differ from each other in 100%** of rows. → The only
trustworthy per-tree date is **`kapmaatregelDatumUitgevoerd` (felling, 100% populated)**; any
elapsed-time reasoning must anchor on it, never on a "permit" date.

## Finding 2 — the published permit states no replant termijn

The individual KOOP kap publications are thin notices (verified via `scan_permits.py`):

- **Aanvraag** stub (`gmb-2021-100196`): activity + address + zaaknummer, sometimes the count
  (*"het kappen van 117 bomen en herplanten van 117 bomen in Middenmeer Noord"*) — **no termijn**.
- **Besluit** (`gmb-2022-245014`, the E-buurt case): *"het verplanten van 18 bomen binnen het
  projectgebied E-Buurt Oost NZ (t.h.v. Egeldonk 50)"*, zaaknummer, send date — **no termijn**.
  The actual besluit + conditions are **not published** ("*Het besluit en bijbehorende stukken
  kunt u per e-mail ontvangen*") — any per-permit termijn lives only in that dossier.
- **Decoy:** the only time-term in either is *"binnen 6 weken"* — the **bezwaar (appeal)
  window**, not a replant deadline. A naïve extractor would grab it; the Phase-2 NER must
  **exclude the bezwaartermijn.**

Every "herplant/termijn" hit in the wider Amsterdam corpus came from **policy** documents, not
permits (the beleidsnotitie/wijzigingsbesluit *'Compensatie en herplant van bomen'*).

## Finding 3 — the policy sets no fixed public default either

The operative beleidsregel **'Compensatie en herplant van bomen'** (`gmb-2023-267061`, the
*uitwerking van de Amsterdamse Bomenverordening 2014*; earlier/wijziging versions
`gmb-2021-443032`, `gmb-2023-271337`) makes the termijn **discretionary and per-permit**:

- the vergunningverlener *"stelt de vergunningvoorwaarden vast"* and *"**kan de termijn voor
  compensatie verlengen**"*;
- money/replant must be spent *"binnen de door de vergunningverlener gestelde periode"*;
- for herstructurering/renovatie (e.g. E-buurt): compensation *"dient **aan het einde van de
  stedelijke herstructurering** of groenrenovatie te zijn besteed"*, with replant *"direct of op
  een later moment binnen het plangebied."*

There is **no universal calendar default** (no blanket "eerstvolgende plantseizoen" for the
herplantplicht — that phrase exists only in the *subsidy* regeling, a different instrument).
So a policy fallback yields at best an open-ended, project-scoped horizon, not a firm `D`.
*(Honest limit: the Bomenverordening art. 7 verbatim was not independently retrieved this
session; the beleidsregel that operationalises it is quoted above.)*

---

## Decision — the deadline-source ladder + timeliness mapping

Compute `D` by this ladder; stop at the first rung that fires:

1. **Explicit permit-text termijn** (Phase-2 NER, from the permit body) — highest confidence
   **but expect ≈0 hits** (publications carry none; conditions are unpublished). **Guard:**
   never accept the bezwaartermijn ("binnen N weken") as `D`.
2. **Policy/project horizon** — only a *soft* bound for herstructurering claims ("by end of
   project"); emit as `deadlineApprox` with low confidence, never a hard date. No generic
   calendar default exists.
3. **`deadlineUnknown`** — the honest default for the vast majority of Noord claims.
   `datumAfrondenVoor` is **explicitly rejected** as a source.

**Timeliness axis:**
- Most Noord claims resolve to **`deadlineUnknown`**. Per design, this **must not block** the
  **fulfilment estimate**, which rests on reliable signals: `kapmaatregelDatumUitgevoerd` set
  + `plantmaatregelDatumUitgevoerd` null/set + elapsed time (843/1,298 Noord = replant pending).
- An **advisory** "long-overdue" flag MAY be derived from *elapsed time since the felling date*
  (never a permit date) — surfaced as a **caveat**, not a hard `overdue` verdict, because the
  legal deadline is genuinely unknown and legitimately open-ended for renewal projects.

**Knock-ons:**
- Temporal model: `D` is not a stable fact — a permit amendment or an explicit extension
  (*"kan de termijn ... verlengen"*) revises it; model it as validity-stamped state if a real
  `D` ever materialises, otherwise it stays absent.
- `DATA_SOURCES.md` corrections applied: the undocumented v3 date fields, the batch-assigned
  permit dates, and the "kap publication is a thin stub / bezwaar decoy" caveat.

## Files

- `probe.py` — scoping + registry characterization (stdlib; writes `noord_buurt_ids.txt`,
  `noord_kap.jsonl`).
- `scan_permits.py` — document-side termijn scan over representative publications.
