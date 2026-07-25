#!/usr/bin/env python3
"""Spike A probe — where does the replant *termijn* live?  (document side)

Fetches KOOP officiële-bekendmakingen document XML for a set of Amsterdam
kap/verplant publications and scans the body for replant-deadline language.

Finding: the individual kap publications are thin notices — an *aanvraag*
(application) or a *besluit* (decision) that states activity + address
(+ count/zaaknummer for a besluit) and NOTHING about a replant termijn. The
only time-term present is the bezwaar (appeal) window "binnen 6 weken", which
is a decoy and must never be read as a replant deadline. Every hit on
"herplant/termijn" in the wider corpus came from *policy* documents
(the beleidsregel 'Compensatie en herplant van bomen'), not permits.

Stdlib only; read-only GETs.  Run:  python3 scan_permits.py
"""
import re
import sys
import urllib.request

# A besluit (E-buurt, from the design docs), an aanvraag stub, and the
# operative herplant policy — representative of the three shapes seen.
IDS = [
    "gmb-2022-245014",  # BESLUIT: "het verplanten van 18 bomen ..." — no termijn, bezwaar 6 wk decoy
    "gmb-2021-100196",  # AANVRAAG stub: activity + address only
    "gmb-2023-267061",  # POLICY: 'Compensatie en herplant van bomen' (uitwerking Bomenverordening 2014)
]
KEYWORDS = ["plantseizoen", "herplantplicht", "termijn", "binnen", "herplant", "weken"]


def fetch(gmb_id):
    year = gmb_id.split("-")[1]
    url = (f"https://repository.overheid.nl/frbr/officielepublicaties/gmb/"
           f"{year}/{gmb_id}/1/xml/{gmb_id}.xml")
    with urllib.request.urlopen(url, timeout=60) as r:
        return r.read().decode("utf-8", "replace")


def plain(xml):
    return re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", xml)).strip()


def main():
    for gmb_id in IDS:
        txt = plain(fetch(gmb_id))
        print(f"\n=== {gmb_id} ({len(txt)} chars of text) ===")
        print("  head:", txt[:160])
        for kw in KEYWORDS:
            m = re.search(kw, txt, re.I)
            if m:
                s, e = max(0, m.start() - 80), min(len(txt), m.end() + 100)
                print(f"  [{kw}] …{txt[s:e]}…")


if __name__ == "__main__":
    main()
