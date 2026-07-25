#!/usr/bin/env python3
"""Spike B probe 2 — harvest Noord kap permits + measure signal availability.

SRU-harvests every Amsterdam kap/verplant omgevingsvergunning for the target year
and reads, straight from the SRU record (no per-permit fetch needed), the signals a
Phase-1 deterministic pass would have: title (+ address/postcode), publication date,
abstract (tree count + activity terms), controlled `activiteit`, and — the find that
reshapes the match — a **point geometry already given in both RD and WGS84**
(`overheidwetgeving:geometrie` / `locatiepunt`). Each permit is placed in a buurt by
voting the `gbdBuurtId` of the nearest standing trees (bomen `stamgegevens` spatial
query) — a pre-BAG permit→buurt resolver; kept if the buurt is in stadsdeel Noord.

Reports the corpus volume, the aanvraag/besluit/ingetrokken split, per-quarter Noord
volume (informs the Phase-0 quarter pick), and signal-availability rates. Persists the
harvested corpus (raw record + parsed signals inline) so probe 3 and the later BAG
re-run never re-fetch.

Stdlib only; read-only GETs. Run from this directory:  python3 harvest_permits.py
Reads ../spike-a/noord_buurt_ids.txt. Writes all_permits.jsonl, noord_permits.jsonl,
and caches buurt votes in buurt_cache.json.
"""
import collections
import concurrent.futures as cf
import json
import os
import re
import sys
import urllib.parse
import urllib.request

YEAR = 2022                         # §1 target year; the quarter is picked from this
NOORD_Y_MIN = 486000               # coarse RD-northing prefilter (IJ ≈ y 487–488k)
VOTE_RADIUS = 90                   # metres; nearby standing trees vote the buurt
SRU = "https://repository.overheid.nl/sru"
BOMEN = "https://api.data.amsterdam.nl/v1/bomen/stamgegevens/"
QUERY = ('(dt.creator any "Amsterdam" AND dt.type any "omgevingsvergunning" '
         'AND cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand" '
         f'AND dt.available>="{YEAR}-01-01" AND dt.available<="{YEAR}-12-31")')

NOORD_IDS = set(open("../spike-a/noord_buurt_ids.txt").read().split())
CACHE_PATH = "buurt_cache.json"


def sru_page(start, n):
    url = f"{SRU}?" + urllib.parse.urlencode(
        {"operation": "searchRetrieve", "version": "2.0",
         "maximumRecords": str(n), "startRecord": str(start), "query": QUERY})
    with urllib.request.urlopen(url, timeout=120) as r:
        return r.read().decode("utf-8", "replace")


def tag(rec, name):
    m = re.search(rf"<(?:\w+:)?{name}[^>]*>([^<]+)</", rec)
    return m.group(1).strip() if m else None


def parse_count(text):
    """Largest integer written next to bo(o)m/bomen — the stated tree count, else None."""
    if not text:
        return None
    nums = [int(m.group(1)) for m in re.finditer(r"(\d+)\s*(?:bo{1,2}m|bomen)", text, re.I)]
    return max(nums) if nums else None


def doctype(title):
    t = (title or "").lower()
    for kind in ("ingetrokken", "verlengd", "kennisgeving", "besluit", "aanvraag"):
        if kind in t:
            return kind
    return "other"


def harvest():
    """Yield a parsed dict per publication across all SRU pages."""
    first = sru_page(1, 1)
    total = int(re.search(r"<(?:\w+:)?numberOfRecords>(\d+)", first).group(1))
    print(f"SRU numberOfRecords for {YEAR}: {total}", file=sys.stderr)
    got = 0
    start = 1
    while start <= total:
        page = sru_page(start, 100)
        recs = re.split(r"<(?:\w+:)?record>", page)[1:]
        if not recs:
            break
        for rec in recs:
            gid = tag(rec, "identifier")
            if not gid or not gid.startswith("gmb-"):
                continue
            rd = re.search(r"POINT\((\d+)\s+(\d+)\)", rec)
            lp = re.search(r"locatiepunt>([\d.]+)\s+([\d.]+)", rec)
            title = tag(rec, "title")
            abstract = tag(rec, "abstract")
            yield {
                "id": gid,
                "title": title,
                "available": tag(rec, "available"),
                "abstract": abstract,
                "activiteit": tag(rec, "activiteit"),
                "doctype": doctype(title),
                "rd_x": int(rd.group(1)) if rd else None,
                "rd_y": int(rd.group(2)) if rd else None,
                "lat": float(lp.group(1)) if lp else None,
                "lon": float(lp.group(2)) if lp else None,
                "count": parse_count(f"{title} {abstract}"),
            }
            got += 1
        start += 100
    print(f"parsed {got} records", file=sys.stderr)


def vote_buurt(lat, lon):
    """Majority gbdBuurtId of standing trees within VOTE_RADIUS — permit→buurt, pre-BAG."""
    url = (f"{BOMEN}?geometrie%5Bwithin%5D=POINT({lon:.6f}%20{lat:.6f}),{VOTE_RADIUS}"
           "&_pageSize=60&_fields=gbdBuurtId")
    req = urllib.request.Request(url, headers={"Accept-Crs": "EPSG:4326"})
    for _ in range(3):
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                rows = json.load(r).get("_embedded", {}).get("stamgegevens", [])
            votes = collections.Counter(x.get("gbdBuurtId") for x in rows if x.get("gbdBuurtId"))
            return votes.most_common(1)[0][0] if votes else None
        except Exception:  # noqa: BLE001 — transient; retry then give up
            pass
    return None


def resolve_noord(permits):
    """Confirm the buurt (cached) for permits north of the coarse cut; tag Noord ones."""
    cache = json.load(open(CACHE_PATH)) if os.path.exists(CACHE_PATH) else {}
    cands = [p for p in permits
             if p["lat"] and p["rd_y"] and p["rd_y"] >= NOORD_Y_MIN and p["id"] not in cache]
    print(f"buurt-voting {len(cands)} candidates north of y={NOORD_Y_MIN} "
          f"({len(cache)} cached)", file=sys.stderr)
    with cf.ThreadPoolExecutor(max_workers=8) as ex:
        futs = {ex.submit(vote_buurt, p["lat"], p["lon"]): p["id"] for p in cands}
        for i, fut in enumerate(cf.as_completed(futs), 1):
            cache[futs[fut]] = fut.result()
            if i % 25 == 0:
                print(f"\r  voted {i}/{len(cands)}", end="", file=sys.stderr, flush=True)
    print(file=sys.stderr)
    json.dump(cache, open(CACHE_PATH, "w"))
    for p in permits:
        p["buurt"] = cache.get(p["id"])
        p["noord"] = p["buurt"] in NOORD_IDS
    return permits


def report(permits):
    noord = [p for p in permits if p.get("noord")]
    print(f"\n=== CORPUS (Amsterdam kap omgevingsvergunningen, {YEAR}) ===")
    print(f"  publications harvested: {len(permits)}")
    print(f"  with a point geometry:  "
          f"{sum(1 for p in permits if p['rd_y'])}/{len(permits)}")
    print("  doctype split:", dict(collections.Counter(p["doctype"] for p in permits)))

    print(f"\n=== NOORD subset (buurt-vote in stadsdeel Noord) ===")
    print(f"  Noord publications: {len(noord)}")
    print("  by doctype:", dict(collections.Counter(p["doctype"] for p in noord)))
    besl = [p for p in noord if p["doctype"] == "besluit"]
    print(f"\n  per-quarter Noord volume (informs the quarter pick):")
    for q in (1, 2, 3, 4):
        qp = [p for p in noord if p["available"] and
              (int(p["available"][5:7]) - 1) // 3 + 1 == q]
        qb = [p for p in qp if p["doctype"] == "besluit"]
        print(f"    {YEAR}-Q{q}: {len(qp):3d} publications  ({len(qb):3d} besluiten)")

    print(f"\n=== SIGNAL AVAILABILITY (over {len(noord)} Noord publications) ===")
    for label, pred in [
        ("point geometry (RD+WGS84)", lambda p: p["rd_y"] is not None),
        ("tree count in title/abstract", lambda p: p["count"] is not None),
        ("controlled activiteit", lambda p: p["activiteit"]),
        ("is a besluit (granted)", lambda p: p["doctype"] == "besluit"),
    ]:
        c = sum(1 for p in noord if pred(p))
        print(f"  {label:32s} {c:3d}/{len(noord)} ({100*c/len(noord):4.0f}%)")
    print(f"\n  besluiten with a parsed count: "
          f"{sum(1 for p in besl if p['count'] is not None)}/{len(besl)} "
          f"(NER's Phase-2 job = raise this)")
    return noord


def main():
    permits = list(harvest())
    permits = resolve_noord(permits)
    with open("all_permits.jsonl", "w") as f:
        for p in permits:
            f.write(json.dumps(p, ensure_ascii=False) + "\n")
    noord = report(permits)
    with open("noord_permits.jsonl", "w") as f:
        for p in noord:
            f.write(json.dumps(p, ensure_ascii=False) + "\n")
    print(f"\nwrote all_permits.jsonl ({len(permits)}) and "
          f"noord_permits.jsonl ({len(noord)})", file=sys.stderr)


if __name__ == "__main__":
    main()
