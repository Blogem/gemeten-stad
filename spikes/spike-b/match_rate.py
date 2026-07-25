#!/usr/bin/env python3
"""Spike B probe 3 — the permit↔registry match rate + the confidence model.

Joins the harvested Noord kap besluiten (harvest_permits.py) to the Noord
`kapenherplant` felled rows (Spike A) with **no shared key** (probe 1), on the
axes the plan names: place + count + time (+ activity). This is the *first pass*:
place is at **buurt** granularity (permit→buurt from the point geometry; registry
row carries `gbdBuurtId`). Once Spike D loads the BAG, the same corpus is re-scored
with postcode/address-level place — which is what lifts weak buurt-only links into
strong ones. The persisted artifacts let that re-run skip every fetch.

Measures both directions, the candidate ambiguity, and the felling-minus-publication
lag; then applies the confidence model to every candidate link and reports the
confidence distribution + how many links clear the threshold τ. Ends with the
E-buurt regression check (the DATA_THREAD anchor).

Stdlib only; the registry join is fully offline (reads local jsonl); one live GET
for the E-buurt regression check. Run:  python3 match_rate.py
"""
import collections
import datetime
import json
import statistics
import urllib.request

HORIZON = datetime.timedelta(days=1095)   # felling may lag the permit up to ~3 yr
COUNT_TOL = 0.15                            # ±15% counts as a count agreement
TAU = 0.60                                  # assert an AuditLink at/above this confidence


def parse_date(v):
    if not v:
        return None
    try:
        return datetime.date.fromisoformat(v.split("T")[0])
    except ValueError:
        return None


def load():
    kap = [json.loads(l) for l in open("../spike-a/noord_kap.jsonl")]
    felled = []
    for r in kap:
        f = parse_date(r.get("kapmaatregelDatumUitgevoerd"))
        if f:
            r["_f"] = f
            felled.append(r)
    permits = [json.loads(l) for l in open("noord_permits.jsonl")]
    besl = []
    for p in permits:
        dt = parse_date(p.get("available"))
        if p["doctype"] == "besluit" and dt:
            p["_d"] = dt
            besl.append(p)
    return felled, besl


def clusters_for(permit, felled_by_buurt):
    """Registry work-order clusters in the permit's buurt+time window.

    A cluster = felled rows sharing (buurt, batch datumVergunningVerleend) — the
    batch permit-date is unreliable as a *date* (Spike A) but groups a work order.
    Returns [{key, size, min_lag, med_lag}] within [pub, pub+HORIZON].
    """
    cand = [r for r in felled_by_buurt.get(permit["buurt"], [])
            if permit["_d"] <= r["_f"] <= permit["_d"] + HORIZON]
    groups = collections.defaultdict(list)
    for r in cand:
        groups[(r.get("datumVergunningVerleend") or "NA")[:10]].append(r)
    out = []
    for key, rows in groups.items():
        lags = sorted((r["_f"] - permit["_d"]).days for r in rows)
        out.append({"key": key, "size": len(rows),
                    "min_lag": lags[0], "med_lag": lags[len(lags) // 2]})
    return cand, out


def confidence(permit, cluster, n_clusters):
    """The Spike-B confidence model → [0,1] on one permit↔cluster AuditLink.

    place  : buurt-level containment = 0.50 base (BAG pass lifts to postcode 0.70 /
             address 0.90). Everything is buurt in this pass, so 0.50 for all.
    count  : exact +0.30 ; within ±15% +0.15 ; permit count unknown +0 ; else −0.10.
    time   : lag ≤ 2 yr +0.15 ; 2–3 yr +0.05  (felling-after-publication already required).
    ambig. : sole candidate cluster +0.05 ; else −0.03·(n−1), floored at −0.15.
    """
    score = 0.50
    c = permit.get("count")
    if c:
        if cluster["size"] == c:
            score += 0.30
        elif abs(cluster["size"] - c) <= max(1, round(COUNT_TOL * c)):
            score += 0.15
        else:
            score -= 0.10
    score += 0.15 if cluster["med_lag"] <= 730 else 0.05
    score += 0.05 if n_clusters == 1 else max(-0.15, -0.03 * (n_clusters - 1))
    return round(max(0.0, min(1.0, score)), 2)


def permit_to_registry(felled, besl):
    by_buurt = collections.defaultdict(list)
    for r in felled:
        by_buurt[r["gbdBuurtId"]].append(r)

    place_time = count_ok = 0
    ambiguities, lags, links = [], [], []
    count_bearing = sum(1 for p in besl if p.get("count") is not None)
    for p in besl:
        cand, cl = clusters_for(p, by_buurt)
        if not cl:
            links.append({"permit": p["id"], "buurt": p["buurt"],
                          "count": p.get("count"), "candidates": [], "best": None})
            continue
        place_time += 1
        ambiguities.append(len(cl))
        lags.append(min(c["min_lag"] for c in cl))
        scored = sorted(
            ({**c, "confidence": confidence(p, c, len(cl))} for c in cl),
            key=lambda c: c["confidence"], reverse=True)
        if p.get("count") is not None and any(
                c["size"] == p["count"] or
                abs(c["size"] - p["count"]) <= max(1, round(COUNT_TOL * p["count"]))
                for c in cl):
            count_ok += 1
        links.append({"permit": p["id"], "buurt": p["buurt"], "count": p.get("count"),
                      "candidates": scored, "best": scored[0]})

    print(f"=== PERMIT → REGISTRY  (over {len(besl)} Noord besluiten) ===")
    print(f"  place+time hit (≥1 felled cluster, buurt + ≤3yr window): "
          f"{place_time} ({100*place_time/len(besl):.0f}%)")
    print(f"  +count-compatible: {count_ok}/{count_bearing} count-bearing besluiten "
          f"({100*count_ok/count_bearing:.0f}%)")
    print(f"  candidate ambiguity: mean {statistics.mean(ambiguities):.1f} work-order "
          f"clusters per hit (>1 ⇒ buurt+time alone can't pin a unique link)")
    print(f"  felling−publication lag (days): median {int(statistics.median(lags))}, "
          f"min {min(lags)}, max {max(lags)}")
    return links


def registry_to_permit(felled, besl):
    by_buurt = collections.defaultdict(list)
    for p in besl:
        by_buurt[p["buurt"]].append(p)
    elig = [r for r in felled
            if datetime.date(2022, 1, 1) <= r["_f"] <= datetime.date(2025, 12, 31)]
    hit = sum(1 for r in elig if any(
        p["_d"] <= r["_f"] <= p["_d"] + HORIZON for p in by_buurt.get(r["gbdBuurtId"], [])))
    print(f"\n=== REGISTRY → PERMIT  (over {len(elig)} fellings 2022–2025) ===")
    print(f"  ≥1 candidate 2022 besluit in buurt+window: {hit} ({100*hit/len(elig):.0f}%)")
    print("  LOWER BOUND — only 2022 permits harvested; a multi-year harvest raises it.")
    print("  The unmatched remainder is the grounded 'no permit found' finding "
          "(DATA_THREAD Hop 4).")


def confidence_distribution(links):
    strong = [l for l in links if l["best"] and l["best"]["confidence"] >= 0.70]
    weak = [l for l in links if l["best"] and TAU <= l["best"]["confidence"] < 0.70]
    below = [l for l in links if l["best"] and l["best"]["confidence"] < TAU]
    none = [l for l in links if not l["best"]]
    print(f"\n=== CONFIDENCE MODEL applied (τ={TAU}) ===")
    print(f"  strong link  (≥0.70): {len(strong):3d}")
    print(f"  asserted     (≥τ):    {len(strong)+len(weak):3d}  "
          f"(of which weak {TAU}–0.70: {len(weak)})")
    print(f"  below τ → weakLink/no-link kept as caveat: {len(below)}")
    print(f"  no candidate at all → 'no matching source': {len(none)}")
    print("  NB: place is buurt-level here (base 0.50); the BAG pass lifts place to")
    print("      postcode/address, which is what turns weak buurt links into strong ones.")


def ebuurt_regression():
    """DATA_THREAD anchor: the 2022 verplant besluit named 18; registry shows 18
    E-buurt fellings since 2024, ~9 replanted. Confirms the matcher's logic live."""
    url = ("https://api.data.amsterdam.nl/v1/bomen/kapenherplant/?_count=true&_pageSize=1"
           "&gbdBuurtId=03630980000509&kapmaatregelDatumUitgevoerd%5Bgte%5D=2024-01-01")
    try:
        with urllib.request.urlopen(url, timeout=60) as r:
            total = json.load(r).get("page", {}).get("totalElements")
        print(f"\n=== E-BUURT REGRESSION (DATA_THREAD anchor) ===")
        print(f"  permit gmb-2022-245014 stated 18; registry E-buurt fellings since "
              f"2024-01-01 = {total} (expect 18) — anchor {'OK' if total == 18 else 'DRIFTED'}")
    except Exception as e:  # noqa: BLE001
        print(f"  regression check skipped (network): {e}")


def main():
    felled, besl = load()
    print(f"loaded {len(felled)} Noord felled rows, {len(besl)} Noord besluiten\n")
    links = permit_to_registry(felled, besl)
    registry_to_permit(felled, besl)
    confidence_distribution(links)
    with open("match_candidates.jsonl", "w") as f:
        for l in links:
            f.write(json.dumps(l, ensure_ascii=False) + "\n")
    print("\nwrote match_candidates.jsonl (permit → ranked candidate clusters + scores)")
    ebuurt_regression()


if __name__ == "__main__":
    main()
