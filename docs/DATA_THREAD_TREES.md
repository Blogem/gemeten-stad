# Data thread: trees as the core dataset — the E-buurt worked example

A real, end-to-end demonstration of the platform's central loop — **a government
intervention makes claims; independent registries record what actually happened; the
graph ties them together per place and aggregates up the ladder** — with Amsterdam's
tree registry as the core structured dataset. **Every value below was retrieved live on
2026-07-25**; the exact queries are in `DATA_SOURCES.md`.

## Why trees are a strong core

The `kapenherplant` dataset is unusual: it records the **entire obligation lifecycle
per tree** — permit application → permit granted → felling executed → replanting
executed, with the nearest BAG address, the buurt id, and the species that must be
replanted. That means the audit loop ("did the promised thing happen, and when?") can be
computed *within* one structured dataset, while the documents supply the promises,
motivations, and project context. No other probed source has claims and fulfillment this
close together.

## The setting

**E-buurt, Amsterdam Zuidoost** (Bijlmer) — an active urban-renewal area
("Vernieuwing E-Buurt Oost"). Identifiers: Amsterdam gebieden id `03630980000509`
↔ CBS `BU0363TE01` (bridge verified via the gebieden API).

---

## The thread, hop by hop

### Hop 1 — the unstructured trail (KOOP bekendmakingen)

A full permit paper trail for the project area, found via SRU full-text search:

| Publication | Date | Content |
|---|---|---|
| `gmb-2022-203707` / `gmb-2022-203722` | 2022-05-05 | **Aanvraag** omgevingsvergunning, Egeldonk 50 |
| `gmb-2022-245014` | 2022-05-31 | **Besluit**: *"het verplanten van 18 bomen binnen het projectgebied E-Buurt Oost NZ (t.h.v. Egeldonk 50)"*, zaaknummer `Z2022-ZO000769`, OLO `6934379` |
| `stcrt-2020-4593` | 2020-01-22 | Ontwerpbestemmingsplan **E-buurt Oost** (project context) |
| `gmb-2022-311987` | 2022-07-11 | Verkeersbesluit D-/E-buurt parkeerschijfzone (same area, other dimension) |

The besluit contains a **numeric claim** ("18 bomen"), a **project-area location**
(not a BAG address — "t.h.v. Egeldonk 50"), and a **case id** (`Z2022-ZO000769`) that a
graph should mint as an entity.

### Hop 2 — canonicalization (with two honest lessons)

- "t.h.v. Egeldonk 50, 1103 AK" does **not** resolve to a BAG address in PDOK — the
  E-buurt renewal demolished buildings; PDOK falls back to a street match with geometry
  only. Reference addresses need a point-in-buurt-polygon fallback, not an address join.
- The registry side needs no geocoding at all: `kapenherplant` records carry
  `gbdBuurtId=03630980000509` directly, and the gebieden API maps it to `BU0363TE01`
  and the name "E-buurt" in one call.

### Hop 3 — the core structured dataset (bomen: kapenherplant + stamgegevens)

Registry facts for E-buurt (all live-counted via `_count=true`):

- Citywide: **3,593** trees with felling executed since 2024-01-01.
- **E-buurt: 18 trees felled since 2024 — the same number the 2022 permit named.**
- Of those 18, only **9 have replanting executed** as of 2026-07-25.
- E-buurt's standing stock: **1,022 municipal trees** (`stamgegevens` count).

Individual lifecycle records (verbatim from the API, trimmed):

```
Gooiseweg 51, 1103BZ:  vergunning 2023-10-30 → gekapt 2024-01-25 → herplant 2025-04-01  (14 months)
Edenburg 32, 1103BJ:   vergunning 2023-10-30 → gekapt 2024-01-24 → herplant 2025-03-25
Ekangala 26, 1103AZ:   vergunning 2023-10-30 → gekapt 2024-01-23 → herplant: NULL  (2.5 years, pending)
EKapa 21, 1103AX:      vergunning 2023-10-30 → gekapt 2024-01-22 → herplant: NULL  (pending)
Eensgevonden 28, 1103BA: vergunning 2023-10-30 → gekapt 2024-01-24 → herplant: NULL  (pending)
```

### Hop 4 — a negative result that shapes the design

The registry rows say `datumVergunningVerleend = 2023-10-30`, but **no October/November
2023 felling permit for these streets is discoverable in the bekendmakingen** — searches
by street names, "E-buurt", felling synonyms (*kappen/vellen/houtopstanden*), and date
windows all came up empty, while the same searches happily find the 2022 *verplant*
permit and everything else about the area. Two implications:

1. **Document↔registry linkage is entity resolution, not string matching.** The link
   runs through the *project* (E-Buurt Oost, zaaknummer) and *spatial containment*, with
   dates as weak evidence — the registry's permit date looks batch-assigned (dozens of
   records across Zuidoost share `2023-10-30`) and the permit terminology differs
   (*verplanten* in the document vs *"Vellen (boom verwijderen)"* in the registry).
2. **"No matching publication found" is itself a reportable, grounded finding** — either
   the search modality is incomplete or the publication trail has a gap. The agent must
   be able to say so instead of forcing a match.

### Hop 5 — the buurt profile (independent dimensions, same key)

All fetched live for `BU0363TE01` / postcode `1103`:

| Dimension | Source | Value (verified) |
|---|---|---|
| Residents / households | CBS KWB 2025 (`86165NED`) | 2,365 / 1,025 |
| Cars per household | CBS KWB 2025 | 0.9 |
| Registered crimes | politie `47022NED` | 7 (May 2026), 8 (June 2026) |
| Municipal trees | Datapunt `stamgegevens` | 1,022 |
| Energy per postcode | Liander 2026 file | rows for 1103xx present (ELK + GAS) |
| Air quality | Luchtmeetnet | nearest station, hourly NO2 |

### Hop 6 — the grounded insight (what the agent would emit)

> **Replanting in the E-buurt renewal is at 50% after 2.5 years.** The 2022 permit for
> project E-Buurt Oost NZ covered 18 trees near Egeldonk 50 [gmb-2022-245014,
> zaaknummer Z2022-ZO000769]. The municipal tree registry records exactly 18 fellings in
> E-buurt since January 2024; 9 have a replanting executed (e.g. Gooiseweg 51: felled
> 2024-01-25, replanted 2025-04-01 — 14 months), and 9 have none as of 2026-07-25
> (e.g. Ekangala 26, felled 2024-01-23) [kapenherplant, gbdBuurtId 03630980000509].
> The outstanding obligations equal ~0.9% of the buurt's 1,022 municipal trees
> [stamgegevens]. Caveat: the felling permit dated 2023-10-30 in the registry could not
> be located in the official publications; the document↔registry link rests on project
> area and counts, not on a shared identifier.

Every clause resolves to a specific document or a specific API query — including the
caveat. That last sentence is the auditable-agent discipline in one line: confidence is
reported per link, and a missing source is surfaced, not papered over.

---

## What this thread proves and teaches

- **Lifecycle-as-claims works**: permit → obligation → execution → fulfillment is native
  to the registry; the platform's Claim/Observation model maps onto it without forcing.
- **The aggregation ladder is real**: tree → address/postcode → `gbdBuurtId` → CBS
  buurt code → wijk/stadsdeel, with live counts at each level and the gebieden API as
  the bridge.
- **The NER/extraction difficulty is calibrated**: controlled metadata gives typing for
  verkeersbesluiten, but kap permits are nearly all free text ("verplanten van 18 bomen",
  "t.h.v. Egeldonk 50", zaaknummers) — exactly the extraction targets, with the registry
  as ground truth to validate against.
- **Entity resolution needs project entities**: interventions cluster under projects
  (bestemmingsplan, zaaknummer, project name); modeling the project as a first-class
  node is what makes the document↔registry join robust.
- **Negative evidence is a feature**: the missing 2023 permit shows why the agent must
  treat "not found" as a finding with its own provenance (which searches, which indexes,
  which date windows).
