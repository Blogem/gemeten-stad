#!/usr/bin/env python3
"""Spike A probe — where does the replant *termijn* live?  (registry side)

Enumerates stadsdeel Noord's buurten, exports the Noord `kapenherplant`
population, and quantitatively characterizes every candidate replant-deadline
date field. Establishes the reusable Noord-scoping query that Phase-1
`ingest`/`load bomen` will lift.

Stdlib only; read-only GETs against the public Amsterdam Datapunt API.
Run from this directory:  python3 probe.py
Writes:  noord_buurt_ids.txt, noord_kap.jsonl  (into the cwd)

Findings (verified 2026-07-25) are written up in README.md.
"""
import collections
import datetime
import json
import statistics
import sys
import urllib.parse
import urllib.request

API = "https://api.data.amsterdam.nl/v1"
NOORD_STADSDEEL = "03630000000019"  # gebieden id, code N

# All date fields the kapenherplant v3 record carries (derived from the data —
# the published schema lists far fewer than the API actually returns).
DATE_FIELDS = [
    "datumVergunningsaanvraag", "datumBesluitVergunningKap", "datumVergunningVerleend",
    "datumEindeBezwaar", "datumAkkoordBoomsoort", "kapmaatregelDatumUitgevoerd",
    "groeiplaatsmaatregelDatumUitgevoerd", "plantmaatregelDatumUitgevoerd",
    "datumAfrondenVoor", "datumHerplantinspectie", "datumToezichtHerplantinspectie",
    "kapDatumToezicht", "plantenDatumToezicht", "inspectiedatum", "mutatiedatum",
]


def get(path, params):
    url = f"{API}/{path}?{urllib.parse.urlencode(params)}"
    last = None
    for _ in range(4):
        try:
            with urllib.request.urlopen(url, timeout=60) as r:
                return json.load(r)
        except Exception as e:  # noqa: BLE001 — transient network, retry
            last = e
    raise last


def page_all(path, params, embed_key):
    """Yield every record across pages (page= until a short page).

    NB: gebieden relation filters (ligtInStadsdeelId=, gbdBuurtId[in]=) are
    silently ignored by the API and return 0 — so callers filter client-side.
    """
    page = 1
    while True:
        d = get(path, dict(params, _format="json", _pageSize=200, page=page))
        rows = d.get("_embedded", {}).get(embed_key, [])
        yield from rows
        if len(rows) < 200:
            return
        page += 1


def noord_buurt_ids():
    """stadsdeel -> wijk -> buurt, joined client-side (relation filters unusable)."""
    wijk_ids = {
        w["identificatie"] for w in page_all("gebieden/wijken", {}, "wijken")
        if w.get("ligtInStadsdeelId") == NOORD_STADSDEEL and w.get("eindGeldigheid") is None
    }
    buurten = {
        b["identificatie"]: b for b in page_all("gebieden/buurten", {}, "buurten")
        if b.get("ligtInWijkId") in wijk_ids and b.get("eindGeldigheid") is None
    }
    return sorted(buurten), len(wijk_ids)


def fetch_kap(buurt_ids):
    rows = {}
    for i, bid in enumerate(buurt_ids, 1):
        for r in page_all("bomen/kapenherplant", {"gbdBuurtId": bid}, "kapenherplant"):
            rows[r["id"]] = r
        print(f"\r  buurt {i}/{len(buurt_ids)}  rows: {len(rows)}",
              end="", file=sys.stderr, flush=True)
    print(file=sys.stderr)
    return list(rows.values())


def d(v):
    """Parse a (date | date-time | None) field to a date, else None."""
    if not v:
        return None
    try:
        return datetime.date.fromisoformat(v.split("T")[0])
    except ValueError:
        return None


def main():
    buurt_ids, nwijk = noord_buurt_ids()
    print(f"Noord: {nwijk} wijken, {len(buurt_ids)} buurten", file=sys.stderr)
    with open("noord_buurt_ids.txt", "w") as f:
        f.write("\n".join(buurt_ids) + "\n")

    rows = fetch_kap(buurt_ids)
    with open("noord_kap.jsonl", "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")

    felled = [r for r in rows if d(r.get("kapmaatregelDatumUitgevoerd"))]
    replanted = [r for r in felled if d(r.get("plantmaatregelDatumUitgevoerd"))]

    print("\n=== POPULATION (stadsdeel Noord) ===")
    print(f"kapenherplant rows: {len(rows)}")
    print(f"  felling executed:            {len(felled)}")
    print(f"  of felled, replant executed: {len(replanted)} "
          f"({100 * len(replanted) / len(felled):.0f}%)")
    print(f"  of felled, replant pending:  {len(felled) - len(replanted)}")

    print(f"\n=== DATE-FIELD POPULATE-RATE (over {len(felled)} felled rows) ===")
    for fld in DATE_FIELDS:
        c = sum(1 for r in felled if d(r.get(fld)))
        print(f"  {fld:38s} {c:5d}  {100 * c / len(felled):5.1f}%")

    print("\n=== datumAfrondenVoor as a replant deadline? ===")
    da = [r for r in felled if d(r.get("datumAfrondenVoor"))]
    off = [(d(r["datumAfrondenVoor"]) - d(r["kapmaatregelDatumUitgevoerd"])).days for r in da]
    before = sum(1 for o in off if o < 0)
    print(f"  populated: {len(da)}/{len(felled)}")
    print(f"  PRECEDES the felling date: {before} ({100 * before / len(off):.0f}%) "
          f"<-- impossible for a real replant deadline")
    print(f"  offset days (afronden - kap): min {min(off)}, "
          f"median {int(statistics.median(off))}, max {max(off)}")
    both = [r for r in replanted if d(r.get("datumAfrondenVoor"))]
    ro = [(d(r["plantmaatregelDatumUitgevoerd"]) - d(r["datumAfrondenVoor"])).days for r in both]
    late = sum(1 for o in ro if o > 0)
    print(f"  where replant done & afronden set ({len(ro)} rows): replant AFTER "
          f"datumAfrondenVoor in {late} ({100 * late / len(ro):.0f}%), "
          f"median {int(statistics.median(ro))} days late")

    print("\n=== 'permit granted' dates are batch-assigned (not legal-quality) ===")
    for fld in ("datumVergunningVerleend", "datumBesluitVergunningKap"):
        vals = [r[fld].split("T")[0] for r in felled if r.get(fld)]
        print(f"  {fld}: {len(set(vals))} distinct dates over {len(vals)} rows; "
              f"top: {collections.Counter(vals).most_common(3)}")
    pair = [r for r in felled if r.get("datumVergunningVerleend") and r.get("datumBesluitVergunningKap")]
    diff = sum(1 for r in pair
               if r["datumVergunningVerleend"][:10] != r["datumBesluitVergunningKap"][:10])
    print(f"  the two differ from each other in {diff}/{len(pair)} rows "
          f"({100 * diff / len(pair):.0f}%)")


if __name__ == "__main__":
    main()
