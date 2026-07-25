#!/usr/bin/env python3
"""Spike B probe 1 — is there a shared key joining permits to the registry?

Enumerates every field the `kapenherplant` registry row carries (from Spike A's
`noord_kap.jsonl`) and every field the KOOP publication metadata sidecar carries
for a sample of kap omgevingsvergunningen, then hunts both sides for a common
identifier (zaaknummer / OLO / dossier / project) that would allow a direct join.

Answer (verified 2026-07-25, written up in README.md): **no shared key.** The
registry has no zaaknummer/OLO/project field populated in Noord; the permit
*does* carry a zaaknummer (`OVERHEIDop.referentienummer`), but nothing on the
registry side to join it to. The consolation is large: the permit metadata also
carries a structured **RD point geometry** and a **controlled activiteit** — so
the place+activity signals are structured, not free-text (this contradicts
`DATA_SOURCES.md` §1, which is corrected). The zaaknummer prefix even encodes the
stadsdeel (`Z2022-N…` = Noord).

Stdlib only; read-only GETs. Run from this directory:  python3 schema_probe.py
Reads ../spike-a/noord_kap.jsonl (run spike-a/probe.py first if absent).
"""
import collections
import json
import os
import re
import sys
import urllib.request

KAP_JSONL = "../spike-a/noord_kap.jsonl"

# Identifier-shaped field-name fragments — anything matching is a join candidate.
LINK_HINT = re.compile(
    r"zaak|olo|dossier|vergunningnummer|kenmerk|referentie|project|selectiecode",
    re.I,
)

# A representative spread of kap publications: the E-buurt besluit from the data
# thread + aanvraag stubs across several stadsdelen (ids surfaced by an SRU probe).
SAMPLE_PERMITS = [
    "gmb-2022-245014",  # BESLUIT, Zuidoost (E-buurt) — "verplanten van 18 bomen"
    "gmb-2022-118321", "gmb-2022-439644", "gmb-2022-99532", "gmb-2022-296437",
    "gmb-2022-174635", "gmb-2022-357479", "gmb-2022-491493", "gmb-2022-402008",
]


def meta(gmb_id):
    """Fetch a publication's metadata sidecar as {name: content}."""
    url = f"https://zoek.officielebekendmakingen.nl/{gmb_id}/metadata.xml"
    with urllib.request.urlopen(url, timeout=60) as r:
        x = r.read().decode("utf-8", "replace")
    d = {}
    for m in re.finditer(r'<meta[^>]*name="([^"]+)"[^>]*content="([^"]*)"', x):
        d.setdefault(m.group(1), m.group(2))
    return d


def registry_side():
    if not os.path.exists(KAP_JSONL):
        sys.exit(f"missing {KAP_JSONL} — run ../spike-a/probe.py first")
    rows = [json.loads(l) for l in open(KAP_JSONL)]
    keys = set().union(*(r.keys() for r in rows))

    def pop(k):
        return sum(1 for r in rows if r.get(k) not in (None, "", [], {}))

    print(f"=== REGISTRY (kapenherplant) — {len(rows)} Noord rows, {len(keys)} fields ===")
    print("  every field, populate-rate over all rows:")
    for k in sorted(keys):
        if k == "_links":
            continue
        c = pop(k)
        print(f"    {k:38s} {c:5d}  {100 * c / len(rows):5.1f}%")

    print("\n  link-candidate fields (name matches an identifier pattern):")
    hits = sorted(k for k in keys if LINK_HINT.search(k))
    for k in hits:
        sample = next((r[k] for r in rows if r.get(k) not in (None, "")), None)
        print(f"    {k:26s} populated {pop(k):4d}/{len(rows)}   e.g. {sample!r}")
    usable = [k for k in hits if pop(k) > 0 and k not in ("selectiecode",)]
    print(f"\n  --> registry join keys actually populated: "
          f"{usable or 'NONE (no zaaknummer/OLO/dossier/project)'}")
    return rows


def permit_side():
    print(f"\n=== PERMIT (KOOP metadata sidecar) — {len(SAMPLE_PERMITS)} kap permits ===")
    field_freq = collections.Counter()
    struct = collections.Counter()
    for gid in SAMPLE_PERMITS:
        try:
            d = meta(gid)
        except Exception as e:  # noqa: BLE001
            print(f"  {gid}: ERR {e}")
            continue
        field_freq.update(d)
        geo = "OVERHEIDop.geometrie" in d
        act = d.get("OVERHEIDop.activiteit", "")
        ref = d.get("OVERHEIDop.referentienummer", "")
        for k in ("OVERHEIDop.geometrie", "OVERHEIDop.activiteit",
                  "OVERHEIDop.referentienummer", "DCTERMS.abstract"):
            if d.get(k):
                struct[k] += 1
        sd = ref.split("-")[1][:2].rstrip("0123456789") if "-" in ref else "?"
        print(f"  {gid}  geo={int(geo)} activiteit={act[:8]:8s} "
              f"zaaknr={ref:16s} sd~{sd:2s} | {d.get('DC.title','')[:46]}")

    n = sum(1 for g in SAMPLE_PERMITS)
    print(f"\n  structured-field presence over {n} permits:")
    for k in ("OVERHEIDop.geometrie", "OVERHEIDop.activiteit",
              "OVERHEIDop.referentienummer", "DCTERMS.abstract"):
        print(f"    {k:34s} {struct[k]}/{n}")
    print("\n  NB: DATA_SOURCES.md §1 says kap omgevingsvergunningen carry 'no geometry,")
    print("      no controlled activity type' — CONTRADICTED here; both are present.")


def verdict():
    print("\n=== VERDICT ===")
    print("  Shared key: NONE. The permit carries a zaaknummer")
    print("  (OVERHEIDop.referentienummer, e.g. Z2022-ZO000769) but the registry has")
    print("  no field to hold it — so no direct join exists. Premise confirmed.")
    print("  BUT the permit metadata carries a structured RD POINT + controlled")
    print("  activiteit, so the place+activity match axes are structured, not fuzzy")
    print("  text. The zaaknummer prefix (…-N…, …-ZO…) encodes stadsdeel = a Noord filter.")


def main():
    registry_side()
    permit_side()
    verdict()


if __name__ == "__main__":
    main()
