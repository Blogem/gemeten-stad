#!/usr/bin/env python3
"""Spike C probe 3 — the registry `boommaatregelBesluit` enum + the verplant cross-check.

The interpretation half of Spike C hinges on one never-pulled fact: what distinct
values does `kapenherplant.boommaatregelBesluit` actually take, city-wide? Only
"Vellen (boom verwijderen)" was ever attested (and only in Noord). If there is **no**
"Verplanten" value, the registry collapses transplants into "Vellen" — which decides
whether "18 verplant = 18 vellen" (DATA_THREAD Hop 4) is a real match.

Pulls, over the full city (35,202 rows, paged with a field projection):
  - distinct `boommaatregelBesluit` values + frequencies  (is there a Verplanten value?)
  - distinct `boomAanwezigheid` values + frequencies       (the fulfilment signal)
  - populate-rates for toeTePassenBoomsoort / soortnaam / projectnaamBomen
    (confirm the 0% Noord finding is city-wide → species/project truly absent registry-side).

Then the **verplant cross-check**: the E-buurt (Zuidoost) renewal whose 2022 permit said
"het verplanten van 18 bomen" — query that buurt's felled rows and show what
`boommaatregelBesluit` they carry, whether `kapmaatregelDatumUitgevoerd` fires for a
transplant, and the replant rate (the 18-felled / 9-replanted of the worked thread).

Stdlib only; read-only GETs. Run from this dir:  python3 registry_enum.py
Writes registry_enums.json.
"""
import collections
import json
import sys
import urllib.request

BASE = "https://api.data.amsterdam.nl/v1/bomen/kapenherplant/"
FIELDS = "boommaatregelBesluit,boomAanwezigheid,toeTePassenBoomsoort,soortnaam,projectnaamBomen"
PAGE = 1000
EBUURT = "03630980000509"   # E-buurt, Zuidoost — the DATA_THREAD worked example
OUT = "registry_enums.json"


def get(url):
    for _ in range(4):
        try:
            with urllib.request.urlopen(url, timeout=120) as r:
                return json.load(r)
        except Exception:  # noqa: BLE001 — transient; retry then give up
            pass
    raise RuntimeError(f"failed: {url}")


def scan_city():
    """Page every row citywide, projected to the enum fields."""
    besluit = collections.Counter()
    aanwezig = collections.Counter()
    pop = collections.Counter()          # populate-rates
    total = 0
    url = f"{BASE}?_pageSize={PAGE}&_fields={FIELDS}"
    while url:
        d = get(url)
        rows = d.get("_embedded", {}).get("kapenherplant", [])
        for r in rows:
            total += 1
            besluit[r.get("boommaatregelBesluit")] += 1
            aanwezig[r.get("boomAanwezigheid")] += 1
            for f in ("toeTePassenBoomsoort", "soortnaam", "projectnaamBomen"):
                if r.get(f) not in (None, ""):
                    pop[f] += 1
        url = d.get("_links", {}).get("next", {}).get("href")
        if total % 5000 < PAGE:
            print(f"\r  scanned {total}/35202", end="", file=sys.stderr, flush=True)
    print(file=sys.stderr)
    return total, besluit, aanwezig, pop


def ebuurt_check():
    """E-buurt felled rows since 2024 — what did the transplants get logged as?"""
    url = (f"{BASE}?gbdBuurtId={EBUURT}"
           "&kapmaatregelDatumUitgevoerd[gte]=2024-01-01&_pageSize=200"
           "&_fields=boommaatregelBesluit,boomAanwezigheid,"
           "kapmaatregelDatumUitgevoerd,plantmaatregelDatumUitgevoerd")
    d = get(url)
    rows = d.get("_embedded", {}).get("kapenherplant", [])
    felled = [r for r in rows if r.get("kapmaatregelDatumUitgevoerd")]
    replanted = [r for r in felled if r.get("plantmaatregelDatumUitgevoerd")]
    return {
        "felled": len(felled),
        "replanted": len(replanted),
        "boommaatregelBesluit_values": dict(collections.Counter(
            r.get("boommaatregelBesluit") for r in felled)),
        "boomAanwezigheid_values": dict(collections.Counter(
            r.get("boomAanwezigheid") for r in felled)),
    }


def main():
    print("scanning kapenherplant city-wide (35,202 rows)…", file=sys.stderr)
    total, besluit, aanwezig, pop = scan_city()
    eb = ebuurt_check()
    out = {
        "total_rows": total,
        "boommaatregelBesluit": dict(besluit.most_common()),
        "boomAanwezigheid": dict(aanwezig.most_common()),
        "populate_rates": {f: {"n": pop[f], "pct": round(100 * pop[f] / total, 2)}
                           for f in ("toeTePassenBoomsoort", "soortnaam", "projectnaamBomen")},
        "verplant_case_ebuurt": eb,
    }
    json.dump(out, open(OUT, "w"), ensure_ascii=False, indent=2)

    print(f"\n=== boommaatregelBesluit (distinct values, city-wide n={total}) ===")
    for v, c in besluit.most_common():
        print(f"  {c:6d}  {v!r}")
    verplant = [v for v in besluit if v and "erplant" in v.lower()]
    print(f"  → a 'Verplanten' value exists: {'YES ' + str(verplant) if verplant else 'NO'}")
    print(f"\n=== boomAanwezigheid (distinct values) ===")
    for v, c in aanwezig.most_common():
        print(f"  {c:6d}  {v!r}")
    print(f"\n=== populate-rates (city-wide) ===")
    for f, s in out["populate_rates"].items():
        print(f"  {f:22s} {s['n']:6d}/{total} ({s['pct']}%)")
    print(f"\n=== verplant cross-check — E-buurt {EBUURT} (felled since 2024) ===")
    print(f"  felled: {eb['felled']}   replanted: {eb['replanted']}")
    print(f"  boommaatregelBesluit: {eb['boommaatregelBesluit_values']}")
    print(f"  boomAanwezigheid:     {eb['boomAanwezigheid_values']}")
    print(f"\nwrote {OUT}")


if __name__ == "__main__":
    main()
