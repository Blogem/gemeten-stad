#!/usr/bin/env python3
"""Spike D harvest — the API-side inputs for the geo-backbone resolution test.

Stdlib only; read-only GETs against the public Amsterdam Datapunt API. Mirrors
spikes/spike-a/probe.py (same Noord scoping + paging quirks) and REUSES the
regenerable outputs of the earlier spikes rather than re-fetching them:

  ../spike-a/noord_kap.jsonl      — the Noord kapenherplant population (run spike-a probe.py)
  ../spike-b/noord_permits.jsonl  — the Noord kap-permit corpus WITH a point geometry
                                    (rd_x/rd_y) already parsed off the SRU record — Spike B's
                                    find that omgevingsvergunningen DO carry a locatiepunt,
                                    contradicting DATA_SOURCES §1 (run spike-b harvest_permits.py)

Run from this directory:  python3 harvest.py   (writes into ./data/)
Override the sibling inputs with SPIKE_A_KAP / SPIKE_B_PERMITS env vars (used when the
spikes live in different worktrees before they are merged back).

Produces four artifacts the load + resolve steps consume:
  data/noord_buurten.geojson  — Amsterdam gebieden buurt polygons, Noord, RD (EPSG:28992).
                                 PRIMARY point-in-polygon set; keyed by identificatie
                                 (= kapenherplant.gbdBuurtId, our ground truth).
  data/noord_wijken.geojson   — gebieden wijk polygons, Noord, RD (context/sanity).
  data/tree_points.geojson    — one Point per FELLED Noord kap row (via boomId -> stamgegevens,
                                 with the boomNieuwId fallback for replanted rows -> ~99.5%), RD,
                                 carrying the row's gbdBuurtId (ground truth) + resolved_via
                                 + dichtstbijzijndeBagAdres/Postcode.
  data/permit_points.geojson  — Spike B's Noord permits as Points (their own rd_x/rd_y),
                                 with the free-text title address parsed out. The permit
                                 geometry is GROUND TRUTH for the local-BAG address resolver,
                                 and the messy address is the thing we resolve.

Figures are written up in README.md.
"""
import json
import os
import re
import sys
import urllib.parse
import urllib.request

API = "https://api.data.amsterdam.nl/v1"
NOORD_STADSDEEL = "03630000000019"  # gebieden id, code N
HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")
RD = "EPSG:28992"
SPIKE_A_KAP = os.environ.get("SPIKE_A_KAP", os.path.join(HERE, "..", "spike-a", "noord_kap.jsonl"))
SPIKE_B_PERMITS = os.environ.get(
    "SPIKE_B_PERMITS", os.path.join(HERE, "..", "spike-b", "noord_permits.jsonl"))


def get(url, headers=None):
    req = urllib.request.Request(url, headers=headers or {})
    last = None
    for _ in range(4):
        try:
            with urllib.request.urlopen(req, timeout=90) as r:
                return r.read()
        except Exception as e:  # noqa: BLE001 — transient network, retry
            last = e
    raise last


def get_json(path, params, headers=None):
    return json.loads(get(f"{API}/{path}?{urllib.parse.urlencode(params)}", headers))


def page_all(path, params, embed_key, headers=None):
    """Yield every record across pages. NB (spike A): gebieden relation filters are
    silently ignored and return 0 — callers filter client-side."""
    page = 1
    while True:
        d = get_json(path, dict(params, _format="json", _pageSize=200, page=page), headers)
        rows = d.get("_embedded", {}).get(embed_key, [])
        yield from rows
        if len(rows) < 200:
            return
        page += 1


# ---------------------------------------------------------------- Noord scoping

def noord_scope():
    """stadsdeel -> wijk -> buurt, joined client-side (relation filters unusable)."""
    wijk_ids = {
        w["identificatie"] for w in page_all("gebieden/wijken", {}, "wijken")
        if w.get("ligtInStadsdeelId") == NOORD_STADSDEEL and w.get("eindGeldigheid") is None
    }
    buurt_ids = [
        b["identificatie"] for b in page_all("gebieden/buurten", {}, "buurten")
        if b.get("ligtInWijkId") in wijk_ids and b.get("eindGeldigheid") is None
    ]
    return sorted(buurt_ids), sorted(wijk_ids)


# ---------------------------------------------------------- gebieden -> GeoJSON

def geojson_polygons(path, embed_key, id_set):
    """Current polygons for the given ids as an RD GeoJSON FeatureCollection."""
    feats = []
    for rec in page_all(path, {}, embed_key, headers={"Accept-Crs": RD}):
        if rec.get("eindGeldigheid") is not None or rec.get("identificatie") not in id_set:
            continue
        if not rec.get("geometrie"):
            continue
        feats.append({
            "type": "Feature", "geometry": rec["geometrie"],
            "properties": {
                "identificatie": rec.get("identificatie"), "naam": rec.get("naam"),
                "code": rec.get("code"), "cbsCode": rec.get("cbsCode"),
                "ligtInWijkId": rec.get("ligtInWijkId"),
            },
        })
    return {"type": "FeatureCollection",
            "crs": {"type": "name", "properties": {"name": "urn:ogc:def:crs:EPSG::28992"}},
            "features": feats}


# ------------------------------------------------- felled kap rows -> tree points

def load_felled():
    """Reuse Spike A's noord_kap.jsonl; keep felled rows with a boomId OR boomNieuwId."""
    rows = [json.loads(l) for l in open(SPIKE_A_KAP)]
    return [r for r in rows if r.get("kapmaatregelDatumUitgevoerd")
            and (r.get("boomId") or r.get("boomNieuwId"))]


def fetch_geom(bid):
    """RD point of a stamgegevens tree, or None if the id is gone / has no geometry."""
    if not bid:
        return None
    try:
        tree = json.loads(get(f"{API}/bomen/stamgegevens/{bid}?_format=json", {"Accept-Crs": RD}))
    except Exception:  # noqa: BLE001 — a retired/missing id is a real, reportable gap
        return None
    return tree.get("geometrie")


def tree_points(kap_rows):
    """Fetch the RD point per felled row: boomId first, then the boomNieuwId fallback.

    A replant retires the original boomId and issues a boomNieuwId (Spike B, DATA_SOURCES §2a),
    so boomId-only silently drops exactly the replanted (audit-interesting) trees — the fallback
    recovers them. We record which id resolved the point (resolved_via)."""
    feats, n = [], len(kap_rows)
    for i, r in enumerate(kap_rows, 1):
        geom, via = fetch_geom(r.get("boomId")), "boomId"
        if geom is None:
            geom, via = fetch_geom(r.get("boomNieuwId")), "boomNieuwId"
        if geom is not None:
            feats.append({
                "type": "Feature", "geometry": geom,
                "properties": {
                    "kap_id": r["id"], "boom_id": r.get("boomId"),
                    "boom_nieuw_id": r.get("boomNieuwId"), "resolved_via": via,
                    "gbd_buurt_id": r.get("gbdBuurtId"),   # GROUND TRUTH for point-in-polygon
                    "boom_aanwezigheid": r.get("boomAanwezigheid"),
                    "bag_adres": r.get("dichtstbijzijndeBagAdres"),
                    "bag_postcode": r.get("dichtstbijzijndeBagPostcode"),
                    "replanted": r.get("plantmaatregelDatumUitgevoerd") is not None,
                },
            })
        if i % 50 == 0 or i == n:
            print(f"\r  tree points {i}/{n}  ok: {len(feats)}",
                  end="", file=sys.stderr, flush=True)
    print(file=sys.stderr)
    return {"type": "FeatureCollection",
            "crs": {"type": "name", "properties": {"name": "urn:ogc:def:crs:EPSG::28992"}},
            "features": feats}


# --------------------------------------- Spike B permits -> points + parsed address

# Title shape (verified): "<activity> <marker?> <street> <huisnummer[-toev]> <PPPPXX> Amsterdam"
POSTCODE = re.compile(r"\b(\d{4})\s?([A-Za-z]{2})\b")
MARKERS = ("t.h.v.", "ter hoogte van", "nabij", "t.o.v.", "tegenover")


def parse_permit_title(title):
    """Extract {marker, street, huisnummer, huistoevoeging, postcode} from a kap title."""
    t = re.sub(r"\s+", " ", title or "").strip()
    m = POSTCODE.search(t)
    postcode = f"{m.group(1)}{m.group(2).upper()}" if m else None
    head = t[:m.start()].strip() if m else t
    marker = next((mk for mk in MARKERS if mk in head.lower()), None)
    if marker:
        head = head[head.lower().index(marker) + len(marker):].strip()
    elif "(kap)" in head.lower():
        head = head[head.lower().index("(kap)") + len("(kap)"):].strip()
    hn = re.search(r"(\d+)\s*[-–]?\s*([0-9A-Za-z]{1,4})?\s*$", head)
    huisnummer = int(hn.group(1)) if hn else None
    toev = hn.group(2) if hn and hn.group(2) else None
    street = (head[:hn.start()] if hn else head).strip()
    street = re.sub(r"[,\s]+$", "", street) or None
    return {"marker": marker, "street": street, "huisnummer": huisnummer,
            "huistoevoeging": toev, "postcode": postcode}


def permit_points():
    """Spike B's Noord permits -> Point features (their rd_x/rd_y) + parsed address."""
    feats = []
    for line in open(SPIKE_B_PERMITS):
        p = json.loads(line)
        parsed = parse_permit_title(p.get("title"))
        feats.append({
            "type": "Feature",
            # Permit's OWN geometry (RD) — ground truth; None-safe (some lack a point).
            "geometry": ({"type": "Point", "coordinates": [p["rd_x"], p["rd_y"]]}
                         if p.get("rd_x") and p.get("rd_y") else None),
            "properties": {
                "gmb_id": p.get("id"), "title": p.get("title"),
                "doctype": p.get("doctype"), "available": p.get("available"),
                "count": p.get("count"), "abstract": p.get("abstract"),
                "vote_buurt": p.get("buurt"),   # Spike B's pre-BAG tree-vote buurt (cross-check)
                **parsed,
            },
        })
    return {"type": "FeatureCollection",
            "crs": {"type": "name", "properties": {"name": "urn:ogc:def:crs:EPSG::28992"}},
            "features": feats}


# -------------------------------------------------------------------------- main

def write_json(name, obj):
    with open(os.path.join(DATA, name), "w") as f:
        json.dump(obj, f)


def main():
    os.makedirs(DATA, exist_ok=True)

    buurt_ids, wijk_ids = noord_scope()
    print(f"Noord: {len(wijk_ids)} wijken, {len(buurt_ids)} buurten", file=sys.stderr)
    buurts = geojson_polygons("gebieden/buurten", "buurten", set(buurt_ids))
    write_json("noord_buurten.geojson", buurts)
    wijks = geojson_polygons("gebieden/wijken", "wijken", set(wijk_ids))
    write_json("noord_wijken.geojson", wijks)
    print(f"polygons: {len(buurts['features'])} buurten, {len(wijks['features'])} wijken",
          file=sys.stderr)

    kap = load_felled()
    pts = tree_points(kap)
    write_json("tree_points.geojson", pts)
    print(f"tree points: {len(pts['features'])} of {len(kap)} felled rows", file=sys.stderr)

    permits = permit_points()
    write_json("permit_points.geojson", permits)
    with_geom = sum(1 for f in permits["features"] if f["geometry"])
    with_addr = sum(1 for f in permits["features"]
                    if f["properties"]["street"] and f["properties"]["huisnummer"])
    refs = sum(1 for f in permits["features"] if f["properties"]["marker"])
    print(f"permits: {len(permits['features'])} (geom {with_geom}, street+nr {with_addr}, "
          f"reference-marked {refs})", file=sys.stderr)


if __name__ == "__main__":
    main()
